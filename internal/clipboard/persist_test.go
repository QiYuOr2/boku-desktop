package clipboard

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

// slowStore is a history store whose write takes a while, so that a test can
// see whether the caller waited for it.
type slowStore struct {
	delay  time.Duration
	saves  chan []domain.ClipboardEntry
	closed chan struct{}
}

func newSlowStore(delay time.Duration) *slowStore {
	return &slowStore{
		delay:  delay,
		saves:  make(chan []domain.ClipboardEntry, 16),
		closed: make(chan struct{}),
	}
}

func (s *slowStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	return nil, nil
}

// What this fake is about is the history write; the pictures beside it are not
// stored, which is what a store with nowhere to put one does.
func (s *slowStore) SaveImage(context.Context, string, []byte) error { return nil }

func (s *slowStore) LoadImage(context.Context, string) ([]byte, error) {
	return nil, domain.ErrUnsupportedClipboardFormat
}

func (s *slowStore) DeleteImage(context.Context, string) error { return nil }

func (s *slowStore) SaveEntries(_ context.Context, entries []domain.ClipboardEntry) error {
	time.Sleep(s.delay)
	select {
	case s.saves <- append([]domain.ClipboardEntry(nil), entries...):
	default:
	}
	return nil
}

// A click must not wait for the history file. The write happens behind the
// click, so the frame that shows the click landed is not held up by a file
// the size of the whole history.
func TestCopyDoesNotWaitForTheHistoryFile(t *testing.T) {
	device := &fakeDevice{}
	store := newSlowStore(300 * time.Millisecond)
	clipboard, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  store,
		Tools:  &fakeTools{},
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)

	device.set(textReading("a copy to write back"))
	clipboard.Read(context.Background())

	start := time.Now()
	if err := clipboard.Copy(context.Background(), textEntry("written back")); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("Copy waited %s for the history file", elapsed)
	}

	// The write does happen, behind the call.
	select {
	case entries := <-store.saves:
		if len(entries) == 0 {
			t.Error("the history was written empty")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the history was never written")
	}
	// And a second copy while the first write is still running does not
	// queue a second write of its own: the newest history is written once.
	if err := clipboard.Copy(context.Background(), textEntry("written back again")); err != nil {
		t.Fatalf("the second Copy failed: %v", err)
	}
	select {
	case entries := <-store.saves:
		found := false
		for _, entry := range entries {
			if entry.Content == "written back again" {
				found = true
			}
		}
		if !found {
			t.Errorf("the write after the second copy did not include it: %v", entries)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second write never happened")
	}
}

// Reading the same content over and over does not rewrite the history file:
// a poll that changed nothing is not a change.
func TestAnUnchangedHistoryIsNotRewritten(t *testing.T) {
	device := &fakeDevice{}
	store := &countingStore{}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  store,
		Tools:  &fakeTools{},
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)

	ctx := context.Background()
	device.set(textReading("the same copy"))
	for i := 0; i < 5; i++ {
		clipboard.Read(ctx)
	}
	waitFor(t, "the first write", func() bool { return clipboard.settled() && store.count() >= 1 })
	before := store.count()

	// The same copy, read five times again: the history did not change, so
	// the file is not written again.
	for i := 0; i < 5; i++ {
		clipboard.Read(ctx)
	}
	waitFor(t, "the reads to settle", func() bool { return clipboard.settled() })
	if got := store.count(); got != before {
		t.Errorf("five reads of the same copy wrote the history %d more times", got-before)
	}
}

// countingStore counts writes.
type countingStore struct {
	mu    chan struct{}
	saves int
}

func (s *countingStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	return nil, nil
}

// This fake counts history writes; the pictures beside the history are not what
// it is about.
func (s *countingStore) SaveImage(context.Context, string, []byte) error { return nil }

func (s *countingStore) LoadImage(context.Context, string) ([]byte, error) {
	return nil, domain.ErrUnsupportedClipboardFormat
}

func (s *countingStore) DeleteImage(context.Context, string) error { return nil }

func (s *countingStore) SaveEntries(context.Context, []domain.ClipboardEntry) error {
	if s.mu == nil {
		s.mu = make(chan struct{}, 1)
	}
	s.mu <- struct{}{}
	s.saves++
	<-s.mu
	return nil
}

func (s *countingStore) count() int {
	if s.mu == nil {
		s.mu = make(chan struct{}, 1)
	}
	s.mu <- struct{}{}
	defer func() { <-s.mu }()
	return s.saves
}
