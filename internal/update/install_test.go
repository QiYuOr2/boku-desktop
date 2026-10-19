package update

import (
	"context"
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

// Install asks the updater for the version the check found and, when the
// installation was replaced, restarts into it: that is the order that leaves
// a working application behind.
func TestInstallAppliesAndRestarts(t *testing.T) {
	f := newFakeUpdater()
	release := aRelease(t, "0.1.0-beta.6", []byte("payload"))
	f.release = release
	serve(f, release, []byte("payload"))
	relauncher := &fakeRelauncher{}
	s := newService(t, f, withRelauncher(relauncher))
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := s.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, _, installs := f.counts(); installs != 1 {
		t.Fatalf("the updater was asked to install %d times, want once", installs)
	}
	if f.installed != "0.1.0-beta.6" {
		t.Errorf("the updater was asked to install %q, want the version the check found", f.installed)
	}
	if relauncher.count() != 1 {
		t.Errorf("the application was restarted %d times, want once", relauncher.count())
	}
	// The update is applied: it is not offered again, because a second
	// restart for it would do nothing.
	state := s.State()
	if state.Status == domain.UpdateReady {
		t.Error("the state still offers the update that was just installed")
	}
	if err := s.Install(context.Background()); !errors.Is(err, domain.ErrUpdateUnavailable) {
		t.Errorf("a second Install = %v, want ErrUpdateUnavailable", err)
	}
	if _, _, installs := f.counts(); installs != 1 {
		t.Errorf("a second Install reached the updater %d times", installs)
	}
}

// Install without a ready update is an error rather than a restart into
// nothing.
func TestInstallNeedsAReadyUpdate(t *testing.T) {
	s := newService(t, newFakeUpdater())
	if err := s.Install(context.Background()); !errors.Is(err, domain.ErrUpdateUnavailable) {
		t.Fatalf("Install = %v, want ErrUpdateUnavailable", err)
	}
}

// An install the updater could not complete is a failure, and it does not
// restart: the running version is still the one that works.
func TestAFailedInstallDoesNotRestart(t *testing.T) {
	f := newFakeUpdater()
	release := aRelease(t, "0.1.0-beta.6", []byte("payload"))
	f.release = release
	serve(f, release, []byte("payload"))
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

// Without a relauncher the update is installed and takes effect at the next
// launch: the about box says so rather than offering a restart that is not
// going to happen.
func TestInstallWithoutARelauncherSaysWhenItApplies(t *testing.T) {
	f := newFakeUpdater()
	release := aRelease(t, "0.1.0-beta.6", []byte("payload"))
	f.release = release
	serve(f, release, []byte("payload"))
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
