package ui

import (
	"boku/internal/domain"
	"boku/internal/presentation"
	"context"
	"errors"
	"github.com/egoist/mygo/ui"
	"log/slog"
)

// NoticeID is the notice's ID. A fixed one is deliberate: a second
// notice about the same version replaces the one already in Notification
// Center rather than piling up beside it.
const NoticeID = "boku-update-ready"

// downloadingLine says what is coming down and how far along it is: the
// version, and the share once the total is known.
func downloadingLine(state domain.UpdateState) string {
	return DownloadingLine(state)
}

// updateMenuLabel is the line the tray menu gains while an update is ready,
// and whether it is there at all. Only a ready update earns one: any other
// Status would be an item that cannot do anything.
func updateMenuLabel(state domain.UpdateState) (label string, show bool) {
	return MenuLabel(state)
}

// updateNotice is what the desktop notice says about a ready update.
func updateNotice(state domain.UpdateState) (title, body string) {
	return Notice(state)
}

// updateVersion is a version as the interface writes it: a release's tag has
// a "v" that belongs to the repository rather than to the number, and a row
// that said "新版本 v0.1.0" would be one "v" too many.
func updateVersion(version string) string {
	return Version(version)
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
func (s *State) onUpdateChanged() { s.host.OnMain(s.RefreshReadyUpdate) }

// refreshReadyUpdate is the half of the update check that runs on the main
// thread: it reads what the service reports, and shows the notice once for a
// version that has just become ready.
func (s *State) RefreshReadyUpdate() {
	state := s.UpdateState()
	s.NoteReadyUpdate(state)
}

// updateState is what the views draw: the service's answer, or a Status of
// its own when there is no service. It is read once per frame and waits for
// nothing.
func (s *State) UpdateState() domain.UpdateState {
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
func (s *State) StartUpdateCheck() {
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
func (s *State) InstallUpdate() {
	updates := s.updates
	if updates == nil {
		return
	}
	go func() {
		if err := updates.Install(context.Background()); err != nil {
			slog.Warn("the update could not be installed", "error", err)
			s.host.Refresh()
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
func (s *State) NoteReadyUpdate(state domain.UpdateState) {
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
	if s.notifier == nil || !s.notifier.Supported() {
		return
	}
	title, body := updateNotice(state)
	s.notifier.Notify(NoticeID, title, body, s.openSettings)
}

// updateNotifiedWithoutVersion stands in updateNotified for a notice about a
// ready update that named no version. It cannot collide with a real one: the
// version goes through updateVersion, which trims and never invents this.
const updateNotifiedWithoutVersion = "\x00"

// updateCheckButton draws the about box's standing action: a check on
// demand, which is how the user asks again without waiting for the next one
// the application makes on its own.
func (s *State) UpdateCheckButton(c *ui.Context) {
	if s.updates == nil {
		return
	}
	if button := presentation.DesignButton(c, "检查更新", presentation.ButtonSecondary); button.Clicked() {
		s.OpenUpdateCheckDialog()
	}
}

// openUpdateCheckDialog opens the result of a check before asking for it.
// Check itself stays asynchronous: a button callback is on the main thread,
// while the dialog reads the service's state on subsequent frames.
func (s *State) OpenUpdateCheckDialog() {
	if s.updates == nil {
		return
	}
	s.updateCheckDialog = true
	s.StartUpdateCheck()
}

// DialogInfo is the small, presentation-only summary in the explicit
// check dialog. Keeping the words out of the view makes the version mapping
// testable without testing pixels or a MyGo window.
type DialogInfo struct {
	Latest  string
	Status  string
	Loading bool
}

func DialogState(state domain.UpdateState, current string) DialogInfo {
	current = updateVersion(current)
	if current == "" {
		current = "dev"
	}
	version := updateVersion(state.Version)
	switch state.Status {
	case domain.UpdateChecking:
		return DialogInfo{Latest: "正在获取", Status: "正在获取最新版本…", Loading: true}
	case domain.UpdateDownloading:
		if version == "" {
			version = "正在准备"
		}
		return DialogInfo{Latest: version, Status: downloadingLine(state), Loading: true}
	case domain.UpdateUpToDate:
		return DialogInfo{Latest: current, Status: "已是最新版本"}
	case domain.UpdateReady:
		if version == "" {
			version = "已找到新版本"
		}
		return DialogInfo{Latest: version, Status: "新版本已准备就绪"}
	case domain.UpdateFailed:
		if state.Detail != "" {
			return DialogInfo{Latest: "未能获取", Status: "检查更新失败：" + state.Detail}
		}
		return DialogInfo{Latest: "未能获取", Status: "检查更新失败"}
	case domain.UpdateUnsupported:
		return DialogInfo{Latest: "不可用", Status: "当前构建不支持自动更新"}
	default:
		return DialogInfo{Latest: "尚未获取", Status: "等待检查更新"}
	}
}

// updateCheckDialogView draws the dialog opened by the About section's
// "检查更新" button. It is deliberately separate from updateRow: the row is
// passive Status for scheduled checks, whereas this dialog is an answer to a
// request the user just made.
func (s *State) UpdateCheckDialogView(c *ui.Context) {
	ui.Modal(c, &s.updateCheckDialog, func() {
		state := s.UpdateState()
		info := DialogState(state, s.version)
		ui.Column(c).Width(float32(340)).Gap(float32(12)).Children(func() {
			ui.Text(c, "检查更新").FontSize(presentation.SizeTitle).FontWeight(600).TextColor(presentation.ColorText)
			ui.Column(c).FillWidth().
				Background(presentation.ColorField).
				Border(1, presentation.ColorBorder).
				Radius(presentation.RadiusField).
				Padding(float32(10)).
				Gap(float32(8)).
				Children(func() {
					updateDialogVersionLine(c, "最新版本", info.Latest)
					updateDialogVersionLine(c, "当前版本", updateVersionOrDev(s.version))
				})
			ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if info.Loading {
					ui.Spinner(c)
				}
				ui.Text(c, info.Status).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextBody).Wrap()
			})
			ui.Row(c).FillWidth().Justify(ui.End).Gap(float32(8)).Children(func() {
				if state.Status == domain.UpdateReady {
					if button := presentation.DesignButton(c, "重启并更新", presentation.ButtonPrimary); button.Clicked() {
						s.updateCheckDialog = false
						s.InstallUpdate()
					}
				}
				if state.Status == domain.UpdateFailed {
					if button := presentation.DesignButton(c, "重试", presentation.ButtonSecondary); button.Clicked() {
						s.StartUpdateCheck()
					}
				}
				if button := presentation.DesignButton(c, "关闭", presentation.ButtonSecondary); button.Clicked() {
					s.updateCheckDialog = false
				}
			})
		})
	})
}

func updateDialogVersionLine(c *ui.Context, label, version string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted)
		ui.Spacer(c)
		ui.Text(c, version).FontSize(presentation.SizeSmall).Font("monospace").TextColor(presentation.ColorText).SingleLine()
	})
}

func updateVersionOrDev(version string) string {
	if version = updateVersion(version); version != "" {
		return version
	}
	return "dev"
}
