package playtime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"boku/internal/platform/gamepad"
)

// heartEvery 是运行中的会话被保存的频率。结束会话会立即保存；轮询本身不保存。
const heartEvery = 30 * time.Second

// Sampler 是平台视角下当前正在运行的游戏。
type Sampler interface {
	Sample() (Sample, error)
}

// Recorder 在 goroutine 上采样并把会话写入存储。
type Recorder struct {
	tracker *Tracker
	store   *Store
	covers  *covers
	section *Section
	sampler Sampler

	mu      sync.Mutex
	data    sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	changed func()
}

// Open 读取 dir 下的会话数据库。数据库不可读时不阻断应用：记录保持关闭，
// 以免后续保存覆盖它。
func Open(dir string, section *Section) *Recorder {
	if section == nil {
		section = NewSection()
	}
	if !canRecord() {
		return &Recorder{section: section}
	}
	store := NewStore(dir)
	tracker := &Tracker{}
	sessions, err := store.Load()
	if err != nil {
		slog.Error("playtime file was not readable; this run will not record", "err", err)
		return &Recorder{tracker: tracker, store: store, covers: newCovers(dir), section: section}
	}
	tracker.Restore(sessions)
	if tracker.CloseAbandoned() {
		if err := store.Save(tracker.Sessions()); err != nil {
			slog.Error("playtime file could not be written", "err", err)
		}
	}
	return &Recorder{tracker: tracker, store: store, covers: newCovers(dir), section: section}
}

// Start 开始采样。在没有进程列表的系统上什么也不做。
func (r *Recorder) Start() {
	if r == nil || r.store == nil || !r.store.writable {
		return
	}
	sampler, err := newSampler()
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.sampler = sampler
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.loop(ctx)
}

// SetChanged 在采样 goroutine 上于会话变化后被调用，会话仍在运行时也会调用，
// 以便屏幕上的时钟走动。请在 Start 之前设置。
func (r *Recorder) SetChanged(fn func()) {
	if r == nil {
		return
	}
	r.changed = fn
}

// Available 报告当前系统是否记录游戏时长。macOS 不记录，
// 所以不创建数据库，浮窗也不绘制任何内容。
func (r *Recorder) Available() bool {
	return r != nil && r.store != nil
}

// StoreName 是缓存在封面旁的简体中文标题，文件存在前为空。已记录会话的
// 名称不会被改动。
func (r *Recorder) StoreName(appID string) string {
	if r == nil {
		return ""
	}
	return r.covers.title(appID)
}

// Cover 是某个 app 的缓存图片，尚未就绪时为 nil。
// 字节已在磁盘上，不会发起网络请求。
func (r *Recorder) Cover(appID string) []byte {
	if r == nil {
		return nil
	}
	return r.covers.bytes(appID)
}

// Sessions 是已记录运行的副本，可从 UI 线程安全读取。
func (r *Recorder) Sessions() []Session {
	if r == nil || r.tracker == nil {
		return nil
	}
	r.data.Lock()
	defer r.data.Unlock()
	return append([]Session(nil), r.tracker.Sessions()...)
}

// Stop 停止采样并写入未关闭会话的最后一次观察。
func (r *Recorder) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	if r.store != nil {
		_ = r.store.Close()
	}
}

func (r *Recorder) loop(ctx context.Context) {
	defer close(r.done)
	interval := r.section.Current().Interval()
	ticker := time.NewTicker(interval)
	heart := time.NewTicker(heartEvery)
	defer ticker.Stop()
	defer heart.Stop()
	slog.Info("recording steam playtime", "every", interval)
	r.ensureCovers(r.Sessions())
	r.look()
	for {
		select {
		case <-ctx.Done():
			r.persistLook()
			return
		case <-ticker.C:
			next := r.section.Current().Interval()
			if next != interval {
				ticker.Reset(next)
				interval = next
			}
			if r.section.Current().Enabled {
				r.look()
			}
		case <-heart.C:
			if sessions, ok := r.touch(time.Now().UTC()); ok {
				r.save(sessions)
				r.notify()
			}
		}
	}
}

func (r *Recorder) look() {
	sample, err := r.sampler.Sample()
	if err != nil {
		slog.Warn("playtime sample failed", "err", err)
		return
	}
	sample.Devices = deviceNames(gamepad.Snapshot())
	sessions, changed, err := r.observe(sample)
	if err != nil {
		slog.Warn("playtime sample was not recorded", "err", err)
		return
	}
	if changed {
		r.save(sessions)
	}
	if changed || sessionOpen(sessions) {
		r.notify()
	}
}

func (r *Recorder) observe(sample Sample) ([]Session, bool, error) {
	r.data.Lock()
	defer r.data.Unlock()
	changed, err := r.tracker.Observe(sample)
	if err != nil {
		return nil, false, err
	}
	return append([]Session(nil), r.tracker.Sessions()...), changed, nil
}

func (r *Recorder) touch(now time.Time) ([]Session, bool) {
	r.data.Lock()
	defer r.data.Unlock()
	if !r.tracker.Touch(now) {
		return nil, false
	}
	return append([]Session(nil), r.tracker.Sessions()...), true
}

func (r *Recorder) persistLook() {
	r.data.Lock()
	sessions := append([]Session(nil), r.tracker.Sessions()...)
	r.data.Unlock()
	r.save(sessions)
}

func (r *Recorder) save(sessions []Session) {
	if err := r.store.Save(sessions); err != nil {
		slog.Error("playtime file could not be written", "err", err)
	}
	r.ensureCovers(sessions)
}

func (r *Recorder) ensureCovers(sessions []Session) {
	if r == nil || r.covers == nil {
		return
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session.AppID == "" || seen[session.AppID] {
			continue
		}
		seen[session.AppID] = true
		ids = append(ids, session.AppID)
	}
	r.covers.Want(ids, r.notify)
}

func (r *Recorder) notify() {
	if r.changed != nil {
		r.changed()
	}
}

func deviceNames(pads []gamepad.Pad) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(pads))
	for _, pad := range pads {
		name := pad.Kind.Name()
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func sessionOpen(sessions []Session) bool {
	for _, session := range sessions {
		if session.EndedAt.IsZero() {
			return true
		}
	}
	return false
}
