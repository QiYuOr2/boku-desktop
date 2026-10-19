package app

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
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

// UpdateService is everything the presentation knows about updates.
//
// Each method is called on behalf of the main thread, and each answers in
// its own way:
//
//   - State is read while a frame is being built, so it does not block: it
//     returns the last state the service reached rather than one it waits
//     for.
//   - Check starts a check and the download behind it and returns at once.
//     It is not a handle to the download: the progress arrives through
//     OnChange. An error means the check could not be started or could not
//     finish, and is reported as well as returned.
//   - Install puts a ready update in place and restarts into it. It blocks
//     for as long as that takes, which is why every caller here starts it
//     off the main thread.
//
// OnChange is installed once, before the service is used, and is called from
// whichever goroutine the service answers on — not only the main thread. It
// may be called more often than the state changes, so a listener decides for
// itself what is new.
type UpdateService interface {
	State() domain.UpdateState
	Check(ctx context.Context) error
	Install(ctx context.Context) error
	OnChange(fn func())
}

// updateNotifier is how the presentation puts a notice on the desktop. It is
// an interface for one reason: the system's notification needs a running
// application, and what to say — and how often — is worth testing where
// there is none.
type updateNotifier interface {
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

// updateNoticeID is the notice's ID. A fixed one is deliberate: a second
// notice about the same version replaces the one already in Notification
// Center rather than piling up beside it.
const updateNoticeID = "boku-update-ready"

// downloadingLine says what is coming down and how far along it is: the
// version, and the share once the total is known.
func downloadingLine(state domain.UpdateState) string {
	head := "正在下载"
	if version := updateVersion(state.Version); version != "" {
		head += " " + version
	}
	percent, known := state.Percent()
	// A share that rounds to nothing is left out rather than written as 0%:
	// the files an earlier run finished are counted here before anything is
	// fetched, and "0%" on a download that is in fact well under way reads
	// as nothing happening.
	if !known || percent <= 0 {
		return head + "…"
	}
	return head + "… " + strconv.Itoa(percent) + "%"
}

// updateMenuLabel is the line the tray menu gains while an update is ready,
// and whether it is there at all. Only a ready update earns one: any other
// status would be an item that cannot do anything.
func updateMenuLabel(state domain.UpdateState) (label string, show bool) {
	if state.Status != domain.UpdateReady {
		return "", false
	}
	if version := updateVersion(state.Version); version != "" {
		return "重启并更新到 " + version, true
	}
	return "重启并更新", true
}

// updateNotice is what the desktop notice says about a ready update.
func updateNotice(state domain.UpdateState) (title, body string) {
	title = "Boku 有新版本可以用了"
	if version := updateVersion(state.Version); version != "" {
		return title, "新版本 " + version + " 已经下载好，重启 Boku 就会装上它。"
	}
	return title, "重启 Boku 就会装上新版本。"
}

// updateVersion is a version as the interface writes it: a release's tag has
// a "v" that belongs to the repository rather than to the number, and a row
// that said "新版本 v0.1.0" would be one "v" too many.
func updateVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// prepareUpdates gives the state its update service: the notifier, and the
// listener that turns the service's own answers into frames.
//
// A test builds a State over a fake service through this same call, so that
// what the tests drive is what the assembled application runs.
func (s *State) prepareUpdates(updates UpdateService) {
	if updates == nil {
		return
	}
	s.updates = updates
	if s.updateNotifier == nil {
		s.updateNotifier = mygoNotifier{}
	}
	// The service may call this from any goroutine, for as long as it lives:
	// what it is given is the state's own method, which touches nothing that
	// is the main thread's.
	updates.OnChange(s.onUpdateChanged)
}

// onUpdateChanged is the service saying that its state changed. It is called
// on whatever goroutine the service answers on, so it reads nothing and
// decides nothing: the repaint below carries both to the main thread, which
// is where the state may be touched and where a notice may be shown.
//
// The work travels with the repaint rather than after it, so that a change
// arriving while no window is open still announces itself: with nothing to
// repaint there is nothing to carry it, and the next frame's own check takes
// it. See refreshReadyUpdate and noteReadyUpdate for why both paths are the
// same decision.
func (s *State) onUpdateChanged() { s.refresh(s.refreshReadyUpdate) }

// refreshReadyUpdate is the half of the update check that runs on the main
// thread: it reads what the service reports, and shows the notice once for a
// version that has just become ready.
func (s *State) refreshReadyUpdate() {
	state := s.updateState()
	s.noteReadyUpdate(state)
}

// updateState is what the views draw: the service's answer, or a status of
// its own when there is no service. It is read once per frame and waits for
// nothing.
func (s *State) updateState() domain.UpdateState {
	if s.updates == nil {
		return domain.UpdateState{Status: domain.UpdateUnsupported}
	}
	return s.updates.State()
}

// startUpdateCheck asks the service for a check, off the main thread.
//
// It is what a click and the launch call, both of which run on the main
// thread and neither of which may wait for a feed: the call returns at once,
// and the answer arrives through OnChange, which is the repaint. A check
// already running is the service's to make a no-op.
func (s *State) startUpdateCheck() {
	updates := s.updates
	if updates == nil {
		return
	}
	go func() {
		if err := updates.Check(context.Background()); err != nil {
			if errors.Is(err, domain.ErrUpdateUnsupported) {
				// Development and package-managed builds are expected not to
				// update themselves. The dialog renders that settled state; a
				// log line would only make normal local development look broken.
				return
			}
			// The service has already written the state the dialog renders.
			// What the dialog cannot say is which call it was, and that is the
			// log's.
			slog.Info("the update check did not finish", "error", err)
		}
	}()
}

// installUpdate puts a ready update in place and restarts into it.
//
// Like the check, it leaves the main thread at once: the swap writes files
// and the restart ends the process, and neither belongs in a frame. That it
// could not be started is written down, because the row has nothing else to
// say about it.
func (s *State) installUpdate() {
	updates := s.updates
	if updates == nil {
		return
	}
	go func() {
		if err := updates.Install(context.Background()); err != nil {
			slog.Warn("the update could not be installed", "error", err)
			s.Refresh()
		}
	}()
}

// noteReadyUpdate shows the desktop notice the first time an update becomes
// ready in this run, and not again for that version.
//
// It runs on the main thread, and it has two callers for one decision: the
// quick window's frame, and the state change the service reports. Either can
// be the first to see a version become ready — an update finishes downloading
// whether or not the user ever opens the quick window — and the version it
// remembers makes the second caller a no-op. That it runs on one thread is
// what lets that memory be a plain field with no lock.
//
// Showing a notice is not drawing: MyGo posts the request to the main thread
// and returns, so a frame is a place it can be asked for from. The version is
// recorded before the request, so a desktop that refuses the notice does not
// get asked again on every frame.
//
// The notice is skipped silently where the desktop cannot show one, and where
// there is no service at all: the check dialog is the place that says what
// happened.
func (s *State) noteReadyUpdate(state domain.UpdateState) {
	if s.updates == nil || state.Status != domain.UpdateReady {
		return
	}
	version := updateVersion(state.Version)
	// Each version is told once. A ready update with no version at all can
	// be told once and no more, since there is then nothing to tell a
	// second notice apart from the first.
	notified := version
	if notified == "" {
		notified = updateNotifiedWithoutVersion
	}
	if s.updateNotified == notified {
		return
	}
	s.updateNotified = notified
	if s.updateNotifier == nil || !s.updateNotifier.Supported() {
		return
	}
	title, body := updateNotice(state)
	s.updateNotifier.Notify(updateNoticeID, title, body, s.showUpdateSettings)
}

// updateNotifiedWithoutVersion stands in updateNotified for a notice about a
// ready update that named no version. It cannot collide with a real one: the
// version goes through updateVersion, which trims and never invents this.
const updateNotifiedWithoutVersion = "\x00"

// showUpdateSettings is what a click on the notice does: the settings window,
// taken to the section that has the row and the button on it. It runs on the
// main thread, and opens the window the way the tray icon does.
func (s *State) showUpdateSettings() {
	s.openSettings()
	s.openSettingsSection(sectionAbout)
}

// updateCheckButton draws the about box's standing action: a check on
// demand, which is how the user asks again without waiting for the next one
// the application makes on its own.
func (s *State) updateCheckButton(c *ui.Context) {
	if s.updates == nil {
		return
	}
	if button := designButton(c, "检查更新", buttonSecondary); button.Clicked() {
		s.openUpdateCheckDialog()
	}
}

// openUpdateCheckDialog opens the result of a check before asking for it.
// Check itself stays asynchronous: a button callback is on the main thread,
// while the dialog reads the service's state on subsequent frames.
func (s *State) openUpdateCheckDialog() {
	if s.updates == nil {
		return
	}
	s.updateCheckDialog = true
	s.startUpdateCheck()
}

// updateDialogInfo is the small, presentation-only summary in the explicit
// check dialog. Keeping the words out of the view makes the version mapping
// testable without testing pixels or a MyGo window.
type updateDialogInfo struct {
	latest  string
	status  string
	loading bool
}

func updateDialogState(state domain.UpdateState, current string) updateDialogInfo {
	current = updateVersion(current)
	if current == "" {
		current = "dev"
	}
	version := updateVersion(state.Version)
	switch state.Status {
	case domain.UpdateChecking:
		return updateDialogInfo{latest: "正在获取", status: "正在获取最新版本…", loading: true}
	case domain.UpdateDownloading:
		if version == "" {
			version = "正在准备"
		}
		return updateDialogInfo{latest: version, status: downloadingLine(state), loading: true}
	case domain.UpdateUpToDate:
		return updateDialogInfo{latest: current, status: "已是最新版本"}
	case domain.UpdateReady:
		if version == "" {
			version = "已找到新版本"
		}
		return updateDialogInfo{latest: version, status: "新版本已准备就绪"}
	case domain.UpdateFailed:
		if state.Detail != "" {
			return updateDialogInfo{latest: "未能获取", status: "检查更新失败：" + state.Detail}
		}
		return updateDialogInfo{latest: "未能获取", status: "检查更新失败"}
	case domain.UpdateUnsupported:
		return updateDialogInfo{latest: "不可用", status: "当前构建不支持自动更新"}
	default:
		return updateDialogInfo{latest: "尚未获取", status: "等待检查更新"}
	}
}

// updateCheckDialogView draws the dialog opened by the About section's
// "检查更新" button. It is deliberately separate from updateRow: the row is
// passive status for scheduled checks, whereas this dialog is an answer to a
// request the user just made.
func (s *State) updateCheckDialogView(c *ui.Context) {
	ui.Modal(c, &s.updateCheckDialog, func() {
		state := s.updateState()
		info := updateDialogState(state, s.version)
		ui.Column(c).Width(float32(340)).Gap(float32(12)).Children(func() {
			ui.Text(c, "检查更新").FontSize(sizeTitle).FontWeight(600).TextColor(colorText)
			ui.Column(c).FillWidth().
				Background(colorField).
				Border(1, colorBorder).
				Radius(radiusField).
				Padding(float32(10)).
				Gap(float32(8)).
				Children(func() {
					updateDialogVersionLine(c, "最新版本", info.latest)
					updateDialogVersionLine(c, "当前版本", updateVersionOrDev(s.version))
				})
			ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if info.loading {
					ui.Spinner(c)
				}
				ui.Text(c, info.status).FontSize(sizeSmall).TextColor(colorTextBody).Wrap()
			})
			ui.Row(c).FillWidth().Justify(ui.End).Gap(float32(8)).Children(func() {
				if state.Status == domain.UpdateReady {
					if button := designButton(c, "重启并更新", buttonPrimary); button.Clicked() {
						s.updateCheckDialog = false
						s.installUpdate()
					}
				}
				if state.Status == domain.UpdateFailed {
					if button := designButton(c, "重试", buttonSecondary); button.Clicked() {
						s.startUpdateCheck()
					}
				}
				if button := designButton(c, "关闭", buttonSecondary); button.Clicked() {
					s.updateCheckDialog = false
				}
			})
		})
	})
}

func updateDialogVersionLine(c *ui.Context, label, version string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).TextColor(colorTextMuted)
		ui.Spacer(c)
		ui.Text(c, version).FontSize(sizeSmall).Font("monospace").TextColor(colorText).SingleLine()
	})
}

func updateVersionOrDev(version string) string {
	if version = updateVersion(version); version != "" {
		return version
	}
	return "dev"
}

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
