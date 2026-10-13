package usecase

import (
	"context"
	"errors"
	"time"

	"boku/internal/domain"
)

// Clock is where the application reads the time, so a test can decide it.
type Clock interface {
	Now() time.Time
}

// SystemClock is the clock the application runs on: wall time, in UTC, the
// way docs/data-model.md §11 stores it.
type SystemClock struct{}

// Now returns the current time in UTC, truncated to the second.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// Watcher reads the clipboard every interval and hands what changed to Emit.
//
// It runs on a goroutine of its own: the device it reads through is what
// hops to the main thread, because that is where the platform keeps the
// clipboard, and this loop must not hold that thread while it sleeps.
type Watcher struct {
	Device   domain.ClipboardDevice
	Interval time.Duration
	Now      func() time.Time
	// Emit is called for content the loop has not seen before, and for the
	// first reading of a loop's life. It runs on the watcher's goroutine
	// and must not block on the interface.
	Emit func(domain.ClipContent)

	// last is the reading the loop saw last, whether or not it emitted it:
	// the clipboard holds what is on it, so the same content must not be
	// recorded twice.
	last    *domain.ClipContent
	started bool
}

// Run polls until ctx is canceled. It returns when the context ends, so a
// caller that cancels knows the goroutine is on its way out.
//
// An interval below ClipboardMinPollInterval is raised to it, and a read
// that failed is skipped rather than treated as an empty clipboard.
func (w *Watcher) Run(ctx context.Context) error {
	if w.Device == nil {
		return errors.New("clipboard: the watcher has no device")
	}
	interval := w.Interval
	if interval < domain.ClipboardMinPollInterval {
		interval = domain.ClipboardDefaultPollInterval
	}
	now := w.Now
	if now == nil {
		now = SystemClock{}.Now
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

// poll reads the clipboard once and emits what is new.
func (w *Watcher) poll(ctx context.Context, at time.Time) {
	if w.Device == nil {
		return
	}
	// A canceled context means the loop is shutting down: no further read
	// is worth making, which is what "closed means closed" requires.
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

func (w *Watcher) emit(reading domain.ClipContent) {
	if w.Emit != nil {
		w.Emit(reading)
	}
}
