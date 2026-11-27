package clipboard

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

// slowStore 是写入要花些时间的存储，使测试能看出调用方是否等了它。
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

// 这个替身关心的是历史写入；其旁的图片并不存储，这是无处可放的存储的做法。
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

// 点击不能等历史文件。写入发生在点击背后，使显示点击已生效的帧
// 不被一份历史大小的文件拖住。
func TestCopyDoesNotWaitForTheHistoryFile(t *testing.T) {
	device := &fakeDevice{}
	store := newSlowStore(300 * time.Millisecond)
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:   device,
		Store:    store,
		Section:  NewSection(),
		Settings: newFakeTools(),
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

	// 写入确实在调用背后发生了。
	select {
	case entries := <-store.saves:
		if len(entries) == 0 {
			t.Error("the history was written empty")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the history was never written")
	}
	// 而在第一次写入仍在进行时的第二次复制不会另排一次写入：最新历史只写一次。
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

// 反复读取相同内容不会重写历史文件：未带来改变的轮询不是改变。
func TestAnUnchangedHistoryIsNotRewritten(t *testing.T) {
	device := &fakeDevice{}
	store := &countingStore{}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:   device,
		Store:    store,
		Section:  NewSection(),
		Settings: newFakeTools(),
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

	// 同一份复制再读五次：历史未改变，因此文件不会再次写入。
	for i := 0; i < 5; i++ {
		clipboard.Read(ctx)
	}
	waitFor(t, "the reads to settle", func() bool { return clipboard.settled() })
	if got := store.count(); got != before {
		t.Errorf("five reads of the same copy wrote the history %d more times", got-before)
	}
}

// countingStore 统计写入次数。
type countingStore struct {
	mu    chan struct{}
	saves int
}

func (s *countingStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	return nil, nil
}

// 这个替身统计历史写入；其旁的图片不是它关心的。
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
