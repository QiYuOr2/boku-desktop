package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// newService builds a service over the fake, with a directory of its own and
// the schedule out of the way: a test that drives Check itself wants no
// timer of the service's.
func newService(t *testing.T, f *fakeUpdater, opts ...func(*Options)) *Service {
	t.Helper()
	options := Options{
		Current: "0.1.0-beta.5",
		Dir:     t.TempDir(),
		Updater: f,
		// A negative FirstDelay is "the loop makes no check of its own",
		// which is what a test that calls Check itself wants.
		FirstDelay: -1,
		Interval:   time.Hour,
	}
	for _, apply := range opts {
		apply(&options)
	}
	s, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func withRelauncher(r Relauncher) func(*Options) {
	return func(o *Options) { o.Relauncher = r }
}

func withCurrent(version string) func(*Options) {
	return func(o *Options) { o.Current = version }
}

func withDelays(first, interval time.Duration) func(*Options) {
	return func(o *Options) { o.FirstDelay, o.Interval = first, interval }
}

// A service is assembled from a feed and a directory, and refuses to be
// assembled from neither: a service that checked nothing would be a state
// that never moves.
func TestNewRequiresAFeedAndADirectory(t *testing.T) {
	for _, c := range []struct {
		name string
		opts Options
	}{
		{"no updater", Options{Dir: t.TempDir()}},
		{"no directory", Options{Updater: newFakeUpdater()}},
		{"relative directory", Options{Dir: "updates", Updater: newFakeUpdater()}},
	} {
		if _, err := New(c.opts); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("%s: New = %v, want ErrInvalidArgument", c.name, err)
		}
	}
}

// A fresh service has not looked at the feed yet, and says so: the row for
// Idle draws nothing.
func TestAFreshServiceIsIdle(t *testing.T) {
	s := newService(t, newFakeUpdater())
	if state := s.State(); state.Status != domain.UpdateIdle {
		t.Errorf("the fresh state = %s, want idle", state.Status)
	}
}

// The happy path: a newer release is found, its archive is really there at
// the size the release said, and the state ends Ready with the version and
// the notes the row shows.
func TestCheckReachesReady(t *testing.T) {
	f := newFakeUpdater()
	payload := []byte(strings.Repeat("boku ", 1024))
	release := aRelease(t, "0.1.0-beta.6", payload)
	f.release = release
	serve(f, release, payload)

	changes := &counter{}
	s := newService(t, f, func(o *Options) { o.OnChange = changes.add })
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	state := s.State()
	if state.Status != domain.UpdateReady {
		t.Fatalf("the state = %s (%s), want ready", state.Status, state.Detail)
	}
	if state.Version != "0.1.0-beta.6" {
		t.Errorf("version = %q", state.Version)
	}
	if state.Notes != release.Notes {
		t.Errorf("notes = %q, want the release's", state.Notes)
	}
	if state.Detail != "" {
		t.Errorf("a ready state carries a detail: %q", state.Detail)
	}
	// The archive was asked for once: this check fetches it to learn that it
	// is there. The install is the updater's, and it downloads again.
	if _, fetches, _ := f.counts(); len(fetches) != 1 {
		t.Errorf("the archive was fetched %d times, want once: %v", len(fetches), fetches)
	}
	if changes.get() < 2 {
		t.Errorf("the views were told %d times, want at least checking and ready", changes.get())
	}
}

// Being up to date is a state, not an error, and it fetches nothing.
func TestCheckUpToDateFetchesNothing(t *testing.T) {
	f := newFakeUpdater()
	s := newService(t, f)
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	state := s.State()
	if state.Status != domain.UpdateUpToDate {
		t.Fatalf("the state = %s, want up to date", state.Status)
	}
	if state.Version != "0.1.0-beta.5" {
		t.Errorf("version = %q, want the running one", state.Version)
	}
	if _, fetches, _ := f.counts(); len(fetches) != 0 {
		t.Errorf("an up-to-date check fetched %v", fetches)
	}
}

// A build that cannot update itself is quiet about it: the status is
// Unsupported, the error is its own name, the feed is never asked and
// nothing is written at warn level.
func TestUnsupportedIsNotAFailure(t *testing.T) {
	f := newFakeUpdater()
	f.enabled = false
	s := newService(t, f)
	err := s.Check(context.Background())
	if !errors.Is(err, domain.ErrUpdateUnsupported) {
		t.Fatalf("Check = %v, want ErrUpdateUnsupported", err)
	}
	state := s.State()
	if state.Status != domain.UpdateUnsupported {
		t.Errorf("the state = %s, want unsupported", state.Status)
	}
	if state.Detail != "" {
		t.Errorf("unsupported carries a detail: %q", state.Detail)
	}
	if releases, _, _ := f.counts(); releases != 0 {
		t.Errorf("an unsupported build asked the feed %d times", releases)
	}
}

// Start on a build that cannot update itself starts no timer: a goroutine
// waking up every day to learn the same thing is noise.
func TestUnsupportedStartsNoLoop(t *testing.T) {
	f := newFakeUpdater()
	f.enabled = false
	s := newService(t, f, withDelays(time.Millisecond, time.Millisecond))
	s.Start(context.Background())
	defer s.Stop()
	if state := s.State(); state.Status != domain.UpdateUnsupported {
		t.Errorf("the state = %s, want unsupported", state.Status)
	}
	if releases, _, _ := f.counts(); releases != 0 {
		t.Errorf("the loop asked the feed %d times", releases)
	}
}

// A release the feed could not describe is a failure, and its detail is this
// package's own words: a manifest URL or a response body never reaches the
// page.
func TestAFailedCheckSaysWhyWithoutQuotingTheFeed(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*fakeUpdater, *Release)
		want  domain.UpdateStatus
	}{
		{
			name: "the feed cannot be reached",
			setup: func(f *fakeUpdater, _ *Release) {
				f.releaseErr = fmt.Errorf("%w: GET https://example.invalid/secret-token", domain.ErrUpdateUnavailable)
			},
			want: domain.UpdateFailed,
		},
		{
			name: "the manifest is not one",
			setup: func(f *fakeUpdater, _ *Release) {
				f.releaseErr = BadRelease("version", "the release names no version")
			},
			want: domain.UpdateFailed,
		},
		{
			name: "the release names no archive",
			setup: func(f *fakeUpdater, _ *Release) {
				f.release = &Release{Version: "0.1.0-beta.6"}
			},
			want: domain.UpdateFailed,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeUpdater()
			var release *Release
			c.setup(f, release)
			s := newService(t, f)
			if err := s.Check(context.Background()); err == nil {
				t.Fatal("Check accepted a check that failed")
			}
			state := s.State()
			if state.Status != c.want {
				t.Errorf("the state = %s, want %s", state.Status, c.want)
			}
			if state.Detail == "" {
				t.Error("a failed check has nothing to say")
			}
			for _, leak := range []string{"http", "example.invalid", "secret-token", "GET "} {
				if strings.Contains(state.Detail, leak) {
					t.Errorf("the detail carries %q: %q", leak, state.Detail)
				}
			}
		})
	}
}

// An archive that cannot be downloaded, or that is not the size the release
// promised, is a failure of the check: the restart is not offered for a
// release that is not really there.
func TestAnArchiveThatIsNotThereIsAFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		with func(*fakeUpdater, *Release)
	}{
		{
			name: "the archive cannot be fetched",
			with: func(f *fakeUpdater, release *Release) {
				asset, _ := release.Archive()
				f.fetchErr[asset.URL] = fmt.Errorf("%w: connection reset", domain.ErrUpdateUnavailable)
			},
		},
		{
			name: "nothing serves the archive",
			with: func(_ *fakeUpdater, _ *Release) {},
		},
		{
			name: "the archive is shorter than promised",
			with: func(f *fakeUpdater, release *Release) {
				asset, _ := release.Archive()
				f.archive[asset.URL] = []byte("short")
			},
		},
		{
			name: "the archive is longer than promised",
			with: func(f *fakeUpdater, release *Release) {
				asset, _ := release.Archive()
				f.archive[asset.URL] = []byte(strings.Repeat("x", int(asset.Size)+10))
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeUpdater()
			payload := []byte("the promised bytes")
			release := aRelease(t, "0.1.0-beta.6", payload)
			f.release = release
			c.with(f, release)
			s := newService(t, f)
			err := s.Check(context.Background())
			if !errors.Is(err, domain.ErrUpdateUnavailable) {
				t.Fatalf("Check = %v, want ErrUpdateUnavailable", err)
			}
			state := s.State()
			if state.Status != domain.UpdateFailed {
				t.Errorf("the state = %s, want failed", state.Status)
			}
			if strings.Contains(state.Detail, "http") {
				t.Errorf("the detail quotes a URL: %q", state.Detail)
			}
		})
	}
}

// Two checks at once ask the feed once: the second is the same question.
// Two checks at once ask the feed once: the second is the same question.
func TestChecksInFlightAreSingleFlight(t *testing.T) {
	gate := make(chan struct{})
	f := newFakeUpdater()
	s := newService(t, f)
	blocking := blockingUpdater{f: f, gate: gate, entered: make(chan struct{})}
	s.updater = blocking

	first := make(chan error, 1)
	go func() { first <- s.Check(context.Background()) }()
	// Wait until the first check is really inside the feed, not merely
	// started: a second check that arrives before it is in flight is not the
	// case under test.
	<-blocking.entered
	for i := 0; i < 3; i++ {
		if err := s.Check(context.Background()); err != nil {
			t.Fatalf("a check during one in flight = %v, want nil", err)
		}
	}
	close(gate)
	if err := <-first; err != nil {
		t.Fatalf("the first check: %v", err)
	}
	if releases, _, _ := f.counts(); releases != 1 {
		t.Errorf("the feed was asked %d times, want once", releases)
	}
}

// blockingUpdater is the fake with its feed gated, so that a second check is
// made while the first is still waiting.
type blockingUpdater struct {
	f       *fakeUpdater
	gate    chan struct{}
	entered chan struct{}
}

func (b blockingUpdater) Enabled() bool { return true }

func (b blockingUpdater) Release(ctx context.Context, current string) (*Release, error) {
	close(b.entered)
	select {
	case <-b.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.f.Release(ctx, current)
}

func (b blockingUpdater) Fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	return b.f.Fetch(ctx, url)
}

func (b blockingUpdater) Install(ctx context.Context, version string) error {
	return b.f.Install(ctx, version)
}

// The timer: the first check waits FirstDelay, and a check that has just run
// is not run again before the interval.
func TestTheLoopWaitsBeforeAsking(t *testing.T) {
	f := newFakeUpdater()
	s := newService(t, f, withDelays(200*time.Millisecond, time.Hour))
	s.Start(context.Background())
	defer s.Stop()

	// Before the first delay has passed, nothing was asked.
	time.Sleep(80 * time.Millisecond)
	if releases, _, _ := f.counts(); releases != 0 {
		t.Fatalf("the feed was asked %d times before the first delay", releases)
	}
	// After it, exactly one check.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if releases, _, _ := f.counts(); releases == 1 {
			break
		} else if releases > 1 {
			t.Fatalf("the loop asked %d times, want once before the interval", releases)
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop never asked the feed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// And not a second time while the interval has not passed.
	time.Sleep(150 * time.Millisecond)
	if releases, _, _ := f.counts(); releases != 1 {
		t.Errorf("the loop asked %d times, want once until the interval", releases)
	}
}

// Stop ends the loop, and a service that was never started stops cleanly.
func TestStopIsSafeBeforeAndAfterStart(t *testing.T) {
	s := newService(t, newFakeUpdater())
	s.Stop()
	s.Start(context.Background())
	s.Start(context.Background())
	finished := make(chan struct{})
	go func() { s.Stop(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not end the loop")
	}
}

// State is read from the drawing thread while a check runs on another: the
// race detector is what this is for, and the answer has to be a real state.
func TestStateIsReadableDuringACheck(t *testing.T) {
	f := newFakeUpdater()
	payload := []byte(strings.Repeat("x", 64))
	release := aRelease(t, "0.1.0-beta.6", payload)
	f.release = release
	serve(f, release, payload)
	s := newService(t, f)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if !s.State().Status.Valid() {
					t.Error("a state that is not a status")
					return
				}
			}
		}
	}()
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	close(stop)
	wg.Wait()
}

// counter counts calls from any goroutine.
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) add() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}

func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// The directory the service owns is collected, not grown: what is left there
// is a download of an update the updater keeps nowhere.
func TestTheServiceCollectsItsDirectory(t *testing.T) {
	dir := t.TempDir()
	leftover := filepath.Join(dir, "0.1.0-beta.4")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "archive.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFakeUpdater()
	s := newService(t, f, func(o *Options) { o.Dir = dir })
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("the leftover %s was kept", leftover)
	}
}
