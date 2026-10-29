package playtime

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// heartEvery is how often an open session is saved while it is still
// running. Ending one saves immediately. The poll itself does not.
const heartEvery = 30 * time.Second

// Sampler is the platform's look at which games are running.
type Sampler interface {
	Sample() (Sample, error)
}

// Recorder samples on a goroutine and writes sessions to the store.
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

// Open reads the session database in dir. A database that cannot be read
// does not stop the application: recording stays off so a later save
// cannot replace it.
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

// Start begins sampling. On a system with no process list it does nothing.
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

// SetChanged is called on the sampling goroutine after a session changes,
// and while one is still running so a clock on screen can move. Set it
// before Start.
func (r *Recorder) SetChanged(fn func()) {
	if r == nil {
		return
	}
	r.changed = fn
}

// Available reports whether this system records playtime. macOS does not,
// so the database is not created and the flyout draws nothing.
func (r *Recorder) Available() bool {
	return r != nil && r.store != nil
}

// StoreName is the simplified-Chinese title cached beside the cover.
// It is empty until that file exists. Recorded session names are not changed.
func (r *Recorder) StoreName(appID string) string {
	if r == nil {
		return ""
	}
	return r.covers.title(appID)
}

// Cover is the cached image for one app, or nil when it is not here yet.
// The bytes are already on disk; this does not ask the network.
func (r *Recorder) Cover(appID string) []byte {
	if r == nil {
		return nil
	}
	return r.covers.bytes(appID)
}

// Sessions is a copy of the recorded runs, safe to read from the UI thread.
func (r *Recorder) Sessions() []Session {
	if r == nil || r.tracker == nil {
		return nil
	}
	r.data.Lock()
	defer r.data.Unlock()
	return append([]Session(nil), r.tracker.Sessions()...)
}

// Stop ends sampling and writes the open sessions' last look.
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

func sessionOpen(sessions []Session) bool {
	for _, session := range sessions {
		if session.EndedAt.IsZero() {
			return true
		}
	}
	return false
}
