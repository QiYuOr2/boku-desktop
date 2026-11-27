package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

func newService(t *testing.T, f *fakeUpdater, opts ...func(*Options)) *Service {
	t.Helper()
	options := Options{
		Current:    "0.1.0-beta.5",
		Dir:        t.TempDir(),
		Updater:    f,
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

func withDelays(first, interval time.Duration) func(*Options) {
	return func(o *Options) { o.FirstDelay, o.Interval = first, interval }
}

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

func TestAFreshServiceIsIdle(t *testing.T) {
	s := newService(t, newFakeUpdater())
	if state := s.State(); state.Status != domain.UpdateIdle {
		t.Errorf("the fresh state = %s, want idle", state.Status)
	}
}

func TestCheckReachesReady(t *testing.T) {
	f := newFakeUpdater()
	release := aRelease(t, "0.5.2")
	f.release = release

	changes := &counter{}
	s := newService(t, f, func(o *Options) { o.OnChange = changes.add })
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	state := s.State()
	if state.Status != domain.UpdateReady {
		t.Fatalf("the state = %s (%s), want ready", state.Status, state.Detail)
	}
	if state.Version != "0.5.2" {
		t.Errorf("version = %q", state.Version)
	}
	if state.Notes != release.Notes {
		t.Errorf("notes = %q, want the release's", state.Notes)
	}
	if state.Detail != "" {
		t.Errorf("a ready state carries a detail: %q", state.Detail)
	}
	if changes.get() < 2 {
		t.Errorf("the views were told %d times, want at least checking and ready", changes.get())
	}
}

func TestCheckUpToDate(t *testing.T) {
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
}

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
	if releases, _ := f.counts(); releases != 0 {
		t.Errorf("an unsupported build asked the feed %d times", releases)
	}
}

func TestUnsupportedStartsNoLoop(t *testing.T) {
	f := newFakeUpdater()
	f.enabled = false
	s := newService(t, f, withDelays(time.Millisecond, time.Millisecond))
	s.Start(context.Background())
	defer s.Stop()
	if state := s.State(); state.Status != domain.UpdateUnsupported {
		t.Errorf("the state = %s, want unsupported", state.Status)
	}
	if releases, _ := f.counts(); releases != 0 {
		t.Errorf("the loop asked the feed %d times", releases)
	}
}

func TestAFailedCheckSaysWhyWithoutQuotingTheFeed(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*fakeUpdater)
		want  domain.UpdateStatus
	}{
		{
			name: "the feed cannot be reached",
			setup: func(f *fakeUpdater) {
				f.releaseErr = fmt.Errorf("%w: GET https://example.invalid/secret-token", domain.ErrUpdateUnavailable)
			},
			want: domain.UpdateFailed,
		},
		{
			name: "the manifest is not one",
			setup: func(f *fakeUpdater) {
				f.releaseErr = BadRelease("version", "the release names no version")
			},
			want: domain.UpdateFailed,
		},
		{
			name: "the release has invalid version",
			setup: func(f *fakeUpdater) {
				f.release = &Release{Version: "../evil"}
			},
			want: domain.UpdateFailed,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeUpdater()
			c.setup(f)
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

func TestChecksInFlightAreSingleFlight(t *testing.T) {
	gate := make(chan struct{})
	f := newFakeUpdater()
	s := newService(t, f)
	blocking := blockingUpdater{f: f, gate: gate, entered: make(chan struct{})}
	s.updater = blocking

	first := make(chan error, 1)
	go func() { first <- s.Check(context.Background()) }()
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
	if releases, _ := f.counts(); releases != 1 {
		t.Errorf("the feed was asked %d times, want once", releases)
	}
}

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

func (b blockingUpdater) Install(ctx context.Context, version string, progress func(downloaded, total int64)) error {
	return b.f.Install(ctx, version, progress)
}

func TestTheLoopWaitsBeforeAsking(t *testing.T) {
	f := newFakeUpdater()
	s := newService(t, f, withDelays(200*time.Millisecond, time.Hour))
	s.Start(context.Background())
	defer s.Stop()

	time.Sleep(80 * time.Millisecond)
	if releases, _ := f.counts(); releases != 0 {
		t.Fatalf("the feed was asked %d times before the first delay", releases)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if releases, _ := f.counts(); releases == 1 {
			break
		} else if releases > 1 {
			t.Fatalf("the loop asked %d times, want once before the interval", releases)
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop never asked the feed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	if releases, _ := f.counts(); releases != 1 {
		t.Errorf("the loop asked %d times, want once until the interval", releases)
	}
}

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

func TestStateIsReadableDuringACheck(t *testing.T) {
	f := newFakeUpdater()
	f.release = aRelease(t, "0.5.2")
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
