package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
)

const (
	// FirstCheckDelay is how long the application waits after starting
	// before asking the feed. A launch is already busy — windows, shortcuts,
	// the clipboard poll — and a launch that is not looked at is the one a
	// user notices going slow.
	FirstCheckDelay = time.Minute
	// CheckInterval is how often the feed is asked afterwards. A desktop
	// application runs for weeks, and a day is often enough for a program
	// whose releases are not urgent.
	CheckInterval = 24 * time.Hour
)

// Options is what the service is assembled from. Updater is required; the
// rest has a working default.
type Options struct {
	// Current is the running version, as the feed compares it: the version
	// mygo.App.Version reports, or the build-time one when that is empty.
	Current string
	// Dir is the directory the service owns for anything it keeps between
	// checks. It is required because it is cleaned: an update is installed
	// by the updater, which downloads into a directory of its own, so what
	// is found here is a leftover, and it is removed rather than kept.
	Dir string
	// Updater is the release feed. It is required: a service with no feed
	// has nothing to check, and saying so at assembly is better than a
	// state that never moves.
	Updater Updater
	// Relauncher restarts the application once an update is applied. A nil
	// relauncher installs the update and does not restart: the next launch
	// runs it, and the state says so.
	Relauncher Relauncher
	// FirstDelay and Interval override the schedule. Zero keeps the
	// defaults; a negative FirstDelay means the loop makes no check of its
	// own until the interval has passed, which is what a test that drives
	// Check itself wants.
	FirstDelay time.Duration
	Interval   time.Duration
	// Clock is where the service reads the time. It only stamps when a
	// check was made — no behaviour depends on it — and a nil clock is the
	// system's.
	Clock domain.Clock
	// OnChange is called from whatever goroutine changed the state, once
	// per change, after the state is readable. It is how the views are told
	// to redraw, so it must not block: it is a repaint request, and the
	// repaint reads State() itself.
	OnChange func()
}

// Service is the update feature: it decides what the interface draws, when
// the feed is asked, and what installing means.
//
// Every method is safe to call from any goroutine, and State is cheap enough
// to call from the drawing thread: it is one lock and one copy.
type Service struct {
	current  string
	dir      string
	updater  Updater
	relaunch Relauncher
	firstIn  time.Duration
	interval time.Duration
	now      func() time.Time

	mu       sync.Mutex
	state    domain.UpdateState
	onChange func()
	// running says a Check is in flight. It is the single flight: a check
	// that arrives while one is running returns at once instead of asking
	// the feed a second time.
	running bool
	// release is the release a Ready state is about, kept whole so that
	// Install knows what to apply.
	release *Release
	// checkedAt is when the last check started, which is what the loop
	// uses to keep itself to one check per interval.
	checkedAt time.Time

	// cancel and done are the loop, while it runs.
	cancel context.CancelFunc
	done   chan struct{}
}

// New returns the service.
func New(opts Options) (*Service, error) {
	if opts.Updater == nil {
		return nil, fmt.Errorf("%w: the update service has no updater", domain.ErrInvalidArgument)
	}
	if opts.Dir == "" {
		return nil, fmt.Errorf("%w: the update service has no directory", domain.ErrInvalidArgument)
	}
	if !filepath.IsAbs(opts.Dir) {
		return nil, fmt.Errorf("%w: the update directory must be an absolute path", domain.ErrInvalidArgument)
	}
	s := &Service{
		current:  opts.Current,
		dir:      opts.Dir,
		updater:  opts.Updater,
		relaunch: opts.Relauncher,
		firstIn:  opts.FirstDelay,
		interval: opts.Interval,
		now:      domain.SystemClock{}.Now,
		onChange: opts.OnChange,
		state:    domain.UpdateState{Status: domain.UpdateIdle},
	}
	if opts.Clock != nil {
		s.now = opts.Clock.Now
	}
	if opts.FirstDelay == 0 {
		s.firstIn = FirstCheckDelay
	}
	if opts.Interval <= 0 {
		s.interval = CheckInterval
	}
	return s, nil
}

// State is what the interface draws. It is a plain copy, safe to keep for
// the length of a frame.
func (s *Service) State() domain.UpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetOnChange replaces the callback. It is what an assembly that builds its
// windows after the service does, since the windows are what a repaint
// needs.
func (s *Service) SetOnChange(onChange func()) {
	s.mu.Lock()
	s.onChange = onChange
	s.mu.Unlock()
}

// OnChange installs the callback the same way SetOnChange does. The two names
// are one method because the presentation declares what it needs from the
// service — State, Check, Install and OnChange — and this is that
// declaration, met. It is installed once, before the service is used.
func (s *Service) OnChange(onChange func()) { s.SetOnChange(onChange) }

// set replaces the state and tells the views, unless nothing changed. It is
// the one place the state moves, so OnChange is called once per change and
// always after the new state can be read.
func (s *Service) set(state domain.UpdateState) {
	s.mu.Lock()
	if s.state == state {
		s.mu.Unlock()
		return
	}
	s.state = state
	onChange := s.onChange
	s.mu.Unlock()
	// The callback is read under the lock and called outside it: it ends up
	// on the main thread, and holding this lock while the draw thread waits
	// for State would be a deadlock waiting for the right moment.
	if onChange != nil {
		onChange()
	}
}

// Start begins the background loop: a check after FirstDelay, then one every
// Interval. A build that cannot update itself is marked Unsupported here and
// gets no timer at all: a loop that wakes up to learn the same thing every
// task it is given is noise. It is safe to call twice; the second call does
// nothing.
func (s *Service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.updater.Enabled() {
		s.set(domain.UpdateState{Status: domain.UpdateUnsupported})
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	firstIn, interval := s.firstIn, s.interval
	s.mu.Unlock()

	slog.Info("the update check started",
		"firstIn", firstIn.String(),
		"interval", interval.String(),
		"version", s.current,
	)

	go func() {
		defer close(done)
		s.loop(runCtx, firstIn, interval)
	}()
}

// loop is the timer: one check after delay, then one every interval, until
// the context ends. A delay at or below zero means the loop waits out an
// interval before its first check, which is what a test that drives Check
// itself wants.
func (s *Service) loop(ctx context.Context, delay, interval time.Duration) {
	timer := time.NewTimer(max(delay, 0))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// The check runs here, on the loop's goroutine. One that is already
		// running — started by a button, say — makes this call return at
		// once, and the next tick waits the interval: that is the single
		// flight.
		_ = s.Check(ctx)
		timer.Reset(interval)
	}
}

// Stop ends the loop and waits for the goroutine. A check in flight is
// cancelled with the context it was started with, because the loop's context
// is its parent. main.go calls it as the application quits.
func (s *Service) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	slog.Info("the update check stopped")
}

// Check asks the feed once and, when there is a newer version, downloads it.
// The status goes Checking, then one of UpToDate, Ready or Failed; a build
// that cannot update itself goes Unsupported, returns
// domain.ErrUpdateUnsupported, and is not a failure.
//
// A Check that arrives while another is running returns immediately with a
// nil error: the answer the caller wants is the one on its way to the state,
// and asking the feed twice would spend two requests to learn the same
// thing.
func (s *Service) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	if !s.updater.Enabled() {
		s.mu.Unlock()
		// Nothing at warn level: a development build asks nothing and gets
		// nothing, and saying so on every launch is the noise this feature
		// exists to keep out of the log.
		s.set(domain.UpdateState{Status: domain.UpdateUnsupported})
		return domain.ErrUpdateUnsupported
	}
	s.running = true
	s.checkedAt = s.now()
	// The new check answers the question again: a release that was ready is
	// no longer offered while the answer is unknown. A check that decides
	// the same version is the newest downloads nothing and is Ready again a
	// moment later.
	s.release = nil
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	s.set(domain.UpdateState{Status: domain.UpdateChecking})

	release, err := s.updater.Release(ctx, s.current)
	if err != nil {
		return s.fail(err)
	}
	if release == nil {
		slog.Info("the application is up to date", "version", s.current)
		s.discard()
		s.set(domain.UpdateState{Status: domain.UpdateUpToDate, Version: s.current})
		return nil
	}
	if err := release.Validate(); err != nil {
		return s.fail(err)
	}
	asset, _ := release.Archive()
	// The release names an archive this application could install, and the
	// updater is what installs it: the signature is checked there, against
	// the key compiled into the build. What is settled here, before the user
	// is asked to restart, is that the archive can be had at the size the
	// manifest promised — so that a release that is not really there is a
	// failure now rather than a restart into nothing.
	if err := s.verify(ctx, release, asset); err != nil {
		return s.fail(err)
	}
	s.mu.Lock()
	s.release = release
	s.mu.Unlock()
	s.discard()
	// The updater fetches, checks the signature and installs the version;
	// this state says that it may be asked to, and what it will install.
	s.set(domain.UpdateState{
		Status:  domain.UpdateReady,
		Version: release.Version,
		Notes:   release.Notes,
	})
	slog.Info("an update is ready", "version", release.Version, "bytes", asset.Size)
	return nil
}

// verify reads the release's archive once, without keeping it, so that a
// release that cannot be downloaded or is not the size it said is a failure
// now rather than after the user restarts. It is bounded by MaxArchiveSize:
// one byte more than the manifest promised is enough to know the answer, and
// the memory it holds is a buffer, not the archive.
//
// It is the one place this package touch the network, and it does so through
// what was injected: the package itself opens nothing.
func (s *Service) verify(ctx context.Context, release *Release, asset Asset) error {
	if asset.Size <= 0 || asset.Size > MaxArchiveSize {
		return wrapUnavailable(fmt.Sprintf("the release gives the archive %d bytes, which is not a size it can be", asset.Size), nil)
	}
	body, err := s.updater.Fetch(ctx, asset.URL)
	if err != nil {
		return unavailableFrom("the release could not be downloaded", err)
	}
	defer body.Close()
	read, err := io.CopyN(io.Discard, body, asset.Size+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return unavailableFrom("the release could not be downloaded", err)
	}
	if read != asset.Size {
		return wrapUnavailable(fmt.Sprintf("the release promises %d bytes and served %d", asset.Size, read), nil)
	}
	return nil
}

// fail puts the state on Failed and returns the error that caused it, in the
// shape the caller branches on: a release that could not be read stays a bad
// response, and everything else is an unavailable update.
func (s *Service) fail(err error) error {
	detail := detailOf(err)
	s.set(domain.UpdateState{Status: domain.UpdateFailed, Detail: detail})
	slog.Warn("the update check failed", "error", detail)
	return err
}

// detailOf is the sentence a Failed state carries, and the one a log record
// repeats. Every word of it comes from this function: an error the updater
// injected may quote the URL it was asked for, and a URL or a response body
// on the settings page — or in the log — is not something the user asked to
// see. The cause is kept on the error for errors.Is; it is never repeated
// here.
func detailOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrProviderBadResponse):
		return "the release could not be read"
	case errors.Is(err, context.Canceled):
		return "the check was cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "the feed did not answer in time"
	case errors.Is(err, domain.ErrUpdateUnavailable):
		// This package's own failures carry a reason of its own — "the
		// release could not be downloaded" — which says what failed without
		// saying where it was asked.
		return reasonOf(err)
	default:
		return "the feed could not be read"
	}
}

// reasonOf is the reason an error of this package was built with: what
// follows the wrapped sentinel, without the cause that was appended to it for
// errors.Is. Everything written here is "<sentinel>: <reason>[: <cause>]",
// and the cause is the part that may carry the platform's own words.
func reasonOf(err error) string {
	text := err.Error()
	for _, sentinel := range []error{domain.ErrUpdateUnavailable, domain.ErrProviderBadResponse} {
		if prefix, ok := strings.CutPrefix(text, sentinel.Error()+": "); ok {
			text = prefix
			break
		}
	}
	if reason, _, ok := strings.Cut(text, ": "); ok {
		text = reason
	}
	// A reason this package wrote itself is a sentence; anything that still
	// looks like an address is not one, and is replaced rather than shown.
	if strings.Contains(text, "://") {
		return "the update could not be completed"
	}
	return semantic(text)
}

// discard removes everything under Dir that an earlier version of this
// application may have left there.
//
// The service keeps nothing between runs — the updater downloads into its own
// temporary directory and replaces the installation from there — so anything
// under Dir is a leftover, and a leftover of a download is a file of an
// update nobody is going to install. It is collected rather than kept so that
// the directory does not grow with every version for the life of the user's
// account; a failure to remove one is logged and not a failure of the check.
func (s *Service) discard() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(s.dir, entry.Name())); err != nil {
			slog.Warn("a leftover file under the update directory could not be removed", "error", err)
		}
	}
}
