package usecase

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

// Opening the history window must not wait on the clipboard. On Windows a
// clipboard read can wait on whatever else is holding the clipboard, and the
// window is already on screen while that happens: the wait would be spent
// with a drawn but empty window.
func TestOpenDoesNotWaitForASlowClipboard(t *testing.T) {
	emitted := make(chan struct{}, 4)
	device := &fakeDevice{readDelay: 300 * time.Millisecond}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  &fakeStore{},
		Tools:  &fakeTools{},
		Emit:   func() { emitted <- struct{}{} },
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	device.set(textReading("a copy that was already on the clipboard"))

	start := time.Now()
	clipboard.Open(context.Background())
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("Open waited %s for the clipboard", elapsed)
	}

	// The read does happen, behind the call.
	select {
	case <-emitted:
	case <-time.After(3 * time.Second):
		t.Fatal("the read never happened")
	}
	if _, ok := clipboard.Latest(); !ok {
		t.Error("the reading never reached the service")
	}
}

// A click on a history row must not wait on the write either: the write goes
// to the platform's thread, and that is the thread drawing the frame the
// click was in.
func TestCopyAsyncDoesNotWaitForASlowClipboard(t *testing.T) {
	device := &fakeDevice{writeDelay: 300 * time.Millisecond}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  &fakeStore{},
		Tools:  &fakeTools{},
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)

	done := make(chan error, 1)
	start := time.Now()
	clipboard.CopyAsync(textEntry("written back"), func(err error) { done <- err })
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("CopyAsync waited %s for the clipboard", elapsed)
	}

	// The entry is already at the top of the history: a click is visible in
	// the list whatever the platform answers.
	if got := len(clipboard.History(1)); got != 1 {
		t.Fatalf("the history holds %d entries after the click, want 1", got)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the write failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the write never happened")
	}
	if got := device.written(); len(got) != 1 || got[0] != "written back" {
		t.Errorf("the clipboard holds %v, want the copy", got)
	}
}

// A write the platform refuses is reported back, so the row can take its mark
// back rather than claim a copy that did not happen.
func TestCopyAsyncReportsARefusedWrite(t *testing.T) {
	device := &fakeDevice{}
	device.failWrite(domain.ErrUnsupportedClipboardFormat)
	clipboard, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  &fakeStore{},
		Tools:  &fakeTools{},
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)

	done := make(chan error, 1)
	clipboard.CopyAsync(textEntry("written back"), func(err error) { done <- err })
	select {
	case err := <-done:
		if err == nil {
			t.Error("a refused write was reported as a copy")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the write never reported")
	}
}

// The interface asks whether a reading has just happened before spending
// another one on a window it is opening.
func TestPolledRecentlyFollowsTheRead(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	if clipboard.PolledRecently() {
		t.Error("a service that has read nothing reports a recent read")
	}
	device.set(textReading("a copy"))
	clipboard.Read(context.Background())
	if !clipboard.PolledRecently() {
		t.Error("a service that has just read reports no recent read")
	}
}
