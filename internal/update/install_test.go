package update

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"boku/internal/domain"
)

func TestInstallAppliesReportsProgressAndRestarts(t *testing.T) {
	f := newFakeUpdater()
	f.release = aRelease(t, "0.5.2")
	f.installSteps = [][2]int64{{50, 100}, {100, 100}}
	relauncher := &fakeRelauncher{}

	var mu sync.Mutex
	var sawDownloading bool
	var maxPercent int
	var s *Service
	s = newService(t, f, withRelauncher(relauncher), func(o *Options) {
		o.OnChange = func() {
			st := s.State()
			mu.Lock()
			if st.Status == domain.UpdateDownloading {
				sawDownloading = true
				if p, ok := st.Percent(); ok && p > maxPercent {
					maxPercent = p
				}
			}
			mu.Unlock()
		}
	})
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := s.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, installs := f.counts(); installs != 1 {
		t.Fatalf("the updater was asked to install %d times, want once", installs)
	}
	if f.installed != "0.5.2" {
		t.Errorf("the updater was asked to install %q, want 0.5.2", f.installed)
	}
	mu.Lock()
	if !sawDownloading || maxPercent != 100 {
		t.Errorf("expected downloading progress to reach 100%%, saw=%v max=%d", sawDownloading, maxPercent)
	}
	mu.Unlock()
	if relauncher.count() != 1 {
		t.Errorf("the application was restarted %d times, want once", relauncher.count())
	}
	state := s.State()
	if state.Status == domain.UpdateReady {
		t.Error("the state still offers the update that was just installed")
	}
	if err := s.Install(context.Background()); !errors.Is(err, domain.ErrUpdateUnavailable) {
		t.Errorf("a second Install = %v, want ErrUpdateUnavailable", err)
	}
	if _, installs := f.counts(); installs != 1 {
		t.Errorf("a second Install reached the updater %d times", installs)
	}
}

func TestInstallNeedsAReadyUpdate(t *testing.T) {
	s := newService(t, newFakeUpdater())
	if err := s.Install(context.Background()); !errors.Is(err, domain.ErrUpdateUnavailable) {
		t.Fatalf("Install = %v, want ErrUpdateUnavailable", err)
	}
}

func TestAFailedInstallDoesNotRestart(t *testing.T) {
	f := newFakeUpdater()
	f.release = aRelease(t, "0.5.2")
	f.installErr = errors.New("GET https://example.invalid/archive: 500")
	relauncher := &fakeRelauncher{}
	s := newService(t, f, withRelauncher(relauncher))
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	err := s.Install(context.Background())
	if !errors.Is(err, domain.ErrUpdateUnavailable) {
		t.Fatalf("Install = %v, want ErrUpdateUnavailable", err)
	}
	if relauncher.count() != 0 {
		t.Error("a failed install restarted the application")
	}
	state := s.State()
	if state.Status != domain.UpdateFailed {
		t.Errorf("the state = %s, want failed", state.Status)
	}
	if state.Detail == "" {
		t.Error("a failed install says nothing")
	}
	if strings.Contains(state.Detail, "http") {
		t.Errorf("the detail quotes the updater's own words: %q", state.Detail)
	}
}

func TestInstallWithoutARelauncherSaysWhenItApplies(t *testing.T) {
	f := newFakeUpdater()
	f.release = aRelease(t, "0.5.2")
	s := newService(t, f)
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := s.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if state := s.State(); state.Status != domain.UpdateUpToDate {
		t.Errorf("the state = %s, want the installed version to be current", state.Status)
	}
}
