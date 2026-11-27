package clipboard

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// gatedDevice 在 gate 关闭前一直不返回，测试可以借此挂住一次读取，
// 观察循环在此期间的行为。
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

// 轮询中的读取必须等上一次返回后才再次发起：剪贴板是系统资源，
// 平台慢时并发读取会把轮询变成读取风暴。
func TestWatcherDoesNotOverlapItsReads(t *testing.T) {
	device := newGatedDevice()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	watcher := &Watcher{
		Device:   device,
		Interval: MinPollInterval,
		Emit:     func(domain.ClipContent) {},
	}
	go func() {
		defer close(done)
		_ = watcher.Run(ctx)
	}()

	// 首次读取已在飞行中且不返回，期间经过多个轮询周期。
	select {
	case <-device.reads:
	case <-time.After(time.Second):
		t.Fatal("the watcher never read the clipboard")
	}
	time.Sleep(3 * MinPollInterval)
	select {
	case <-device.reads:
		t.Error("the watcher started a second read while the first was in flight")
	default:
	}

	// 取消 context 会释放这次读取，循环因此才能结束：读取拿到的是调用时的 context。
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher did not return after its context was canceled")
	}
}

// 读取失败的设备不等于空剪贴板：什么都不发出，循环继续，
// 待设备恢复后记录它读到的新内容。
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
		Interval: MinPollInterval,
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
	time.Sleep(2 * MinPollInterval)

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

// 该用例单独检验 Watcher 循环与服务共用的同一份代码，锁定其承诺：
// 每次变化只 emit 一次，重复内容不 emit，读取失败也完全不 emit。
func TestWatcherEmitsOnlyWhatChanged(t *testing.T) {
	device := &fakeDevice{}
	device.set(textReading("first"))
	var mu sync.Mutex
	var emitted []string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := &Watcher{
		Device:   device,
		Interval: MinPollInterval,
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
	// 同一值多次轮询不得重复 emit。
	time.Sleep(3 * MinPollInterval)
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

	// 设备读取失败时不 emit。
	device.fail(errors.New("the clipboard is busy"))
	before := len(emitted)
	time.Sleep(2 * MinPollInterval)
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
	time.Sleep(MinPollInterval + 50*time.Millisecond)
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
