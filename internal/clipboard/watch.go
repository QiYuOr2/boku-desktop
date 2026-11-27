package clipboard

import (
	"context"
	"errors"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// Watcher 按固定间隔读取剪贴板，把变化交给 Emit。
type Watcher struct {
	Device   domain.ClipboardDevice
	Interval time.Duration
	Now      func() time.Time
	// Emit 在循环首次读取以及发现新内容时调用；它运行在 watcher 自己的
	// goroutine 上，不得阻塞界面。
	Emit func(domain.ClipContent)

	// last 是循环上次看到的读取结果（无论是否已 emit）：剪贴板内容不变
	// 时不能重复记录。
	last    *domain.ClipContent
	started bool
}

// Run 轮询直到 ctx 被取消；返回即表示 goroutine 正在退出，
// 调用方取消后可以据此确认它已结束。
//
// 低于 MinPollInterval 的间隔会被提升到该下限；读取失败会被跳过，
// 而不是当成空剪贴板处理。
func (w *Watcher) Run(ctx context.Context) error {
	if w.Device == nil {
		return errors.New("clipboard: the watcher has no device")
	}
	interval := w.Interval
	if interval < MinPollInterval {
		interval = DefaultPollInterval
	}
	now := w.Now
	if now == nil {
		now = domain.SystemClock{}.Now
	}

	w.poll(ctx, now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.poll(ctx, now())
		}
	}
}

// poll 读取一次剪贴板并发出新内容。
func (w *Watcher) poll(ctx context.Context, at time.Time) {
	if w.Device == nil {
		return
	}
	// context 已取消说明循环正在关闭，不应再发起读取。
	if err := ctx.Err(); err != nil {
		return
	}
	reading, err := w.Device.Read(ctx)
	if err != nil {
		return
	}
	if reading.CapturedAt.IsZero() {
		reading.CapturedAt = at
	}
	if !reading.Valid() {
		return
	}
	reading.ImageRef = imageRef(reading)
	if !w.started {
		w.started = true
		w.last = &reading
		w.emit(reading)
		return
	}
	if w.last != nil && reading.Unchanged(*w.last) {
		return
	}
	w.last = &reading
	w.emit(reading)
}

// imageRef 返回读取结果中位图的标识（其字节的哈希），历史以此作为图片的存储名。
// 必须用字节哈希而非尺寸区分：同尺寸的两张截图靠尺寸无法区分。
// 摘要属于存储决策，所以在这里算而不是在 device 里算；剪贴板上存在图片时
// 每次轮询会多算一次哈希。已带标识的读取结果直接沿用（device 会为仍在剪贴板上的
// 同一张图复用标识，无需重复哈希）。
func imageRef(reading domain.ClipContent) string {
	if reading.ImageRef != "" {
		return reading.ImageRef
	}
	if len(reading.Image) == 0 {
		return ""
	}
	return kit.ClipImageRef(reading.Image)
}

func (w *Watcher) emit(reading domain.ClipContent) {
	if w.Emit != nil {
		w.Emit(reading)
	}
}
