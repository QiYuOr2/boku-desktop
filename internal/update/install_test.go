package update

import (
	"context"
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

// Install 向 updater 要求检查发现的版本，替换成功后重启进入它：这个顺序才能留下
// 可运行的应用。
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
	// 更新已应用：不再提供，因为为它再重启一次不会做任何事。
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

// 没有就绪更新时 Install 是错误，而不是重启进空白。
func TestInstallNeedsAReadyUpdate(t *testing.T) {
	s := newService(t, newFakeUpdater())
	if err := s.Install(context.Background()); !errors.Is(err, domain.ErrUpdateUnavailable) {
		t.Fatalf("Install = %v, want ErrUpdateUnavailable", err)
	}
}

// updater 无法完成的安装是失败，且不重启：运行版本仍是能用的那个。
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

// 没有 relauncher 时更新被安装、下次启动生效：关于框如实说明，而不是给一个不会
// 发生的重启。
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
