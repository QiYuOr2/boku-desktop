package clipboard

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

// 打开历史窗口不能等剪贴板。Windows 上读取剪贴板可能等待占用它的其它程序，
// 而窗口此时已在屏幕上：这段等待会耗在一个已绘制却空无一物的窗口上。
func TestOpenDoesNotWaitForASlowClipboard(t *testing.T) {
	emitted := make(chan struct{}, 4)
	device := &fakeDevice{readDelay: 300 * time.Millisecond}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:   device,
		Store:    &fakeStore{},
		Section:  NewSection(),
		Settings: newFakeTools(),
		Emit:     func() { emitted <- struct{}{} },
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

	// 读取确实在调用背后发生了。
	select {
	case <-emitted:
	case <-time.After(3 * time.Second):
		t.Fatal("the read never happened")
	}
	if _, ok := clipboard.Latest(); !ok {
		t.Error("the reading never reached the service")
	}
}

// 点击历史行同样不能等写入：写入发生在平台线程，而那正是绘制点击所在帧的线程。
func TestCopyAsyncDoesNotWaitForASlowClipboard(t *testing.T) {
	device := &fakeDevice{writeDelay: 300 * time.Millisecond}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:   device,
		Store:    &fakeStore{},
		Section:  NewSection(),
		Settings: newFakeTools(),
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

	// 条目此时已在历史顶部：无论平台如何回应，点击在列表中都是可见的。
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

// 平台拒绝的写入会被回报，使该行可以撤销标记，而非宣称一次并未发生的复制。
func TestCopyAsyncReportsARefusedWrite(t *testing.T) {
	device := &fakeDevice{}
	device.failWrite(domain.ErrUnsupportedClipboardFormat)
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:   device,
		Store:    &fakeStore{},
		Section:  NewSection(),
		Settings: newFakeTools(),
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

// 界面在打开窗口前会先问是否刚读过，以免为已有内容再读一次。
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
