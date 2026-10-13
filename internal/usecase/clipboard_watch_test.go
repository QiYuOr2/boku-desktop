package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// The watcher is the polling half of the clipboard tool. What it promises is
// pinned here: a read that is slow is not read again on top of itself, and
// stopping ends the read that is in progress.

// gatedDevice answers nothing until its gate is closed, so a test can hold a
// read open and see what the loop does meanwhile.
type gatedDevice struct {
	gate  chan struct{}
	reads chan struct{}
	once  bool
}

func newGatedDevice() *gatedDevice {
	return &gatedDevice{gate: make(chan struct{}), reads: make(chan struct{}, 16), once: true}
}

func (d *gatedDevice) Read(ctx context.Context) (domain.ClipContent, error) {
	select {
	case d.reads <- struct{}{}:
	default:
	}
	select {
	case <-d.gate:
	case <-ctx.Done():
		return domain.ClipContent{}, ctx.Err()
	}
	return domain.ClipContent{Kind: domain.ClipKindText, Text: "held", CapturedAt: time.Now().UTC()}, nil
}

func (d *gatedDevice) WriteText(context.Context, string) error { return nil }

func (d *gatedDevice) WriteFiles(context.Context, []string) error { return nil }

func (d *gatedDevice) WriteImage(context.Context, []byte) error { return nil }

// A read on the poll is started again only after the one before it returned:
// the clipboard is a system resource, and two reads at once is how a poll
// turns into a storm when the platform is slow.
func TestWatcherDoesNotOverlapItsReads(t *testing.T) {
	device := newGatedDevice()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	watcher := &Watcher{
		Device:   device,
		Interval: domain.ClipboardMinPollInterval,
		Emit:     func(domain.ClipContent) {},
	}
	go func() {
		defer close(done)
		_ = watcher.Run(ctx)
	}()

	// The first read is in flight and does not return; several intervals
	// pass while it is.
	select {
	case <-device.reads:
	case <-time.After(time.Second):
		t.Fatal("the watcher never read the clipboard")
	}
	time.Sleep(3 * domain.ClipboardMinPollInterval)
	select {
	case <-device.reads:
		t.Error("the watcher started a second read while the first was in flight")
	default:
	}

	// Canceling the context releases the read, which is what lets the loop
	// finish: the read is given the context it was called with.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher did not return after its context was canceled")
	}
}

// A device whose read fails is not an empty clipboard: nothing is emitted,
// and the loop goes on and records what it finds when the device recovers.
func TestWatcherKeepsGoingAfterAFailedRead(t *testing.T) {
	device := &fakeDevice{}
	device.fail(errors.New("the clipboard is busy"))
	var mu sync.Mutex
	emitted := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	watcher := &Watcher{
		Device:   device,
		Interval: domain.ClipboardMinPollInterval,
		Emit: func(domain.ClipContent) {
			mu.Lock()
			emitted++
			mu.Unlock()
		},
	}
	go func() {
		defer close(done)
		_ = watcher.Run(ctx)
	}()
	time.Sleep(2 * domain.ClipboardMinPollInterval)

	device.fail(nil)
	device.set(textReading("after the failure"))
	waitFor(t, "a reading after the failure", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return emitted == 1
	})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher did not return")
	}
	mu.Lock()
	defer mu.Unlock()
	if emitted != 1 {
		t.Errorf("the watcher emitted %d readings, want 1", emitted)
	}
}

// The watcher loop is the same code the service runs; it is exercised on its
// own to pin what it promises: one emit per change, none for a repeat, and
// none at all for a failed read.
func TestWatcherEmitsOnlyWhatChanged(t *testing.T) {
	device := &fakeDevice{}
	device.set(textReading("first"))
	var mu sync.Mutex
	var emitted []string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := &Watcher{
		Device:   device,
		Interval: domain.ClipboardMinPollInterval,
		Emit: func(reading domain.ClipContent) {
			mu.Lock()
			emitted = append(emitted, reading.Text)
			mu.Unlock()
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = watcher.Run(ctx)
	}()

	waitFor(t, "the first emit", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(emitted) >= 1
	})
	// Several polls of the same value must not emit again.
	time.Sleep(3 * domain.ClipboardMinPollInterval)
	mu.Lock()
	count := len(emitted)
	mu.Unlock()
	if count != 1 {
		t.Errorf("the watcher emitted %d times for one value, want 1: %v", count, emitted)
	}

	device.set(textReading("second"))
	waitFor(t, "the second emit", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(emitted) >= 2
	})

	// A failing device emits nothing.
	device.fail(errors.New("the clipboard is busy"))
	before := len(emitted)
	time.Sleep(2 * domain.ClipboardMinPollInterval)
	mu.Lock()
	after := len(emitted)
	mu.Unlock()
	if after != before {
		t.Errorf("a failing device emitted %d readings", after-before)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher did not return after its context was canceled")
	}
}

func TestWatcherRejectsAnIntervalBelowTheFloor(t *testing.T) {
	device := &fakeDevice{}
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := &Watcher{Device: device, Interval: time.Nanosecond, Emit: func(domain.ClipContent) {}}
	go func() {
		defer close(done)
		_ = watcher.Run(ctx)
	}()
	time.Sleep(domain.ClipboardMinPollInterval + 50*time.Millisecond)
	cancel()
	<-done
	if got := device.readCount(); got > 3 {
		t.Errorf("a nanosecond interval produced %d reads: the floor was not applied", got)
	}
	if got := device.readCount(); got == 0 {
		t.Error("the watcher never read the clipboard")
	}
}

func TestWatcherWithoutADeviceReportsIt(t *testing.T) {
	watcher := &Watcher{}
	if err := watcher.Run(context.Background()); err == nil {
		t.Error("a watcher without a device ran anyway")
	}
}
