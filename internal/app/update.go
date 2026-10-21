package app

import (
	"log/slog"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	updateui "boku/internal/update/ui"
)

// This file is the update check as the presentation knows it: the check
// dialog, the one line the tray menu gains while an update is ready, and the
// notice that says so when the application is not on screen.
//
// What the presentation knows about updating is the interface below and
// nothing else. Whether a release is newer, what it is called, how it is
// downloaded and how it is signed are decided in internal/update, over
// MyGo's own updater; this package imports neither, because a view that knew
// what a feed is would be a view that talks to one.
//
// A State with no service is a build without one — a test, or an assembly
// that wired nothing: no row is drawn, nothing checks on its own and the
// menu is what it always was. Nothing here is allowed to fail because of it.

type UpdateService = updateui.Service

// desktopNotifier is how the presentation puts a notice on the desktop. It is
// an interface for one reason: the system's notification needs a running
// application, and what to say — and how often — is worth testing where
// there is none.
type desktopNotifier interface {
	// Supported reports whether the desktop can show a notice.
	Supported() bool
	// Notify shows one notice, and runs open when it is clicked.
	Notify(id, title, body string, open func())
}

// mygoNotifier is the real one: the framework's own notifications module.
type mygoNotifier struct{}

func (mygoNotifier) Supported() bool { return mygo.NotificationsSupported() }

func (mygoNotifier) Notify(id, title, body string, open func()) {
	notice := mygo.NewNotification(mygo.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
		Group: "update",
	})
	notice.OnClick(open)
	if err := notice.Show(); err != nil {
		// The check dialog still says what is ready: a desktop that will not
		// show a notice is not a failed update.
		slog.Warn("the update notice could not be shown", "error", err)
	}
}

func (s *State) prepareUpdates(updates UpdateService) {
	if updates == nil {
		return
	}
	s.updates = updates
	if s.notifier == nil {
		s.notifier = mygoNotifier{}
	}
	host := s.featureHost()
	s.updateUI = updateui.New(updates, s.notifier, host, s.showUpdateSettings, s.version)
}
func (s *State) showUpdateSettings() { s.openSettings(); s.openSettingsSection(sectionAbout) }
func (s *State) refreshReadyUpdate() {
	if s.updateUI != nil {
		s.updateUI.RefreshReadyUpdate()
	}
}
func (s *State) updateState() domain.UpdateState {
	if s.updateUI == nil {
		return domain.UpdateState{Status: domain.UpdateUnsupported}
	}
	return s.updateUI.UpdateState()
}
func (s *State) startUpdateCheck() {
	if s.updateUI != nil {
		s.updateUI.StartUpdateCheck()
	}
}
func (s *State) installUpdate() {
	if s.updateUI != nil {
		s.updateUI.InstallUpdate()
	}
}
func (s *State) noteReadyUpdate(state domain.UpdateState) {
	if s.updateUI != nil {
		s.updateUI.NoteReadyUpdate(state)
	}
}
func (s *State) updateCheckButton(c *ui.Context) {
	if s.updateUI != nil {
		s.updateUI.UpdateCheckButton(c)
	}
}
func (s *State) updateCheckDialogView(c *ui.Context) {
	if s.updateUI != nil {
		s.updateUI.UpdateCheckDialogView(c)
	}
}
func updateMenuLabel(state domain.UpdateState) (string, bool) { return updateui.MenuLabel(state) }
func updateVersion(version string) string                     { return updateui.Version(version) }

// updateMenuItems is the tray menu's updating line: one item while an update
// is ready, and nothing at all otherwise.
//
// The click only starts a goroutine, because a menu callback runs on the main
// thread and the install writes files and restarts the application.
func (s *State) updateMenuItems() []*mygo.MenuItem {
	if s.updates == nil {
		return nil
	}
	label, show := updateMenuLabel(s.updateState())
	if !show {
		return nil
	}
	return []*mygo.MenuItem{{
		Label: label,
		Click: func(*mygo.MenuItem, *mygo.Window) { s.installUpdate() },
	}}
}
