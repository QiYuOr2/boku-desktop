package ui

import (
	"boku/internal/presentation"
	"errors"
	"log/slog"

	"github.com/egoist/mygo/ui"

	"boku/internal/platform/autostart"
)

// This file is start-at-login: the switch in Settings → 通用, and the state
// that section is drawn from.
//
// The switch is not a setting of Boku's own, and it has no key in
// settings.json. Whether the system starts the application at login is a fact
// the system keeps; a copy of it in Boku's own file would be a second truth,
// and it would drift the first time the user turned the start off in Task
// Manager, in System Settings, or in any tool that lists their login items.
// What the section shows is the platform's own answer, read when it is drawn.
//
// The read and the write are both local — one registry value, or one system
// login item — so the switch answers in the frame it was pressed in rather
// than through a background goroutine the way the network-written settings do.

// autostartStatus is what the settings page says under the switch: what the
// last press did, or why there is nothing to press.
type autostartStatus struct {
	text string
	kind presentation.NoteKind
}

// autostartLauncher builds the login start the switch would write.
//
// Windows names the program and the framework's own "started by the system"
// marker; macOS registers the running application bundle and ignores both.
// The label is still passed because it is how the macOS manager finds the
// launch agent written by the previous mechanism.
func (s *State) autostartLauncher() (autostart.Launcher, error) {
	exe, err := autostart.BinaryPath()
	if err != nil {
		return autostart.Launcher{}, err
	}
	return autostart.Launcher{
		Label: autostartIdentifier,
		Name:  "Boku",
		Exe:   exe,
		Arg:   autostart.OpenedAtLoginArg,
	}, nil
}

// autostartState is what the general section draws: whether the switch is on,
// whether it can be drawn at all, and what to say under it.
//
// It is read once per frame rather than remembered: the answer is local, and
// a page still showing the state from when it was opened would be exactly the
// second truth this design does not keep.
func (s *State) autostartState() (on, enabled bool, status autostartStatus) {
	if s.autostart == nil {
		return false, false, autostartStatus{}
	}
	launcher, err := s.autostartLauncher()
	if err != nil {
		return false, false, autostartStatus{text: "找不到程序本身：" + presentation.FriendlyError(err), kind: presentation.NoteDanger}
	}
	if !autostart.Available(s.autostart, launcher) {
		return false, false, autostartStatus{text: autostartUnsupportedNote, kind: presentation.NotePlain}
	}
	on, err = s.autostart.Read(launcher)
	if errors.Is(err, autostart.ErrNeedsApproval) {
		return false, true, autostartStatus{text: autostartApprovalNote, kind: presentation.NotePlain}
	}
	if err != nil {
		return false, false, autostartStatus{text: "读不到开机启动的状态：" + presentation.FriendlyError(err), kind: presentation.NoteDanger}
	}
	return on, true, s.autostartStatus
}

// setAutostart turns the login start on or off and says what the system
// answered. The switch is drawn from the same source on the next frame, so a
// write the system refused shows as the old state with a line saying why —
// rather than as a switch that moved and a start that did not happen.
func (s *State) setAutostart(on bool) {
	if s.autostart == nil {
		return
	}
	launcher, err := s.autostartLauncher()
	if err == nil {
		if on {
			err = s.autostart.Start(launcher)
		} else {
			err = s.autostart.Stop(launcher)
		}
	}
	if err != nil {
		slog.Warn("the login start could not be changed", "on", on, "error", err)
		switch {
		case errors.Is(err, autostart.ErrNeedsApproval):
			s.autostartStatus = autostartStatus{text: autostartApprovalNote, kind: presentation.NotePlain}
		case errors.Is(err, autostart.ErrNotInstalled):
			s.autostartStatus = autostartStatus{text: autostartNotInstalledNote, kind: presentation.NoteDanger}
		default:
			s.autostartStatus = autostartStatus{text: "没能改成开机启动：" + presentation.FriendlyError(err), kind: presentation.NoteDanger}
		}
		s.host.Refresh()
		return
	}
	slog.Info("the login start was changed", "on", on)
	s.autostartStatus = autostartStatus{}
	s.host.Refresh()
}

// autostartUnsupportedNote is what the section says on a desktop with no
// login start Boku can register. It is a fact about the platform, not a
// failure of the user's, so it is drawn quietly.
const autostartUnsupportedNote = "这个平台没有 Boku 能登记的开机启动方式。"

// autostartApprovalNote is what the section says when the system has the
// login item but the user has it switched off there. The switch stays off —
// that is the system's answer — and the line says where to turn it back on.
const autostartApprovalNote = "系统里的登录项已被关闭；请在「系统设置 > 通用 > 登录项与扩展」中允许 Boku。"

// autostartNotInstalledNote is what the section says when the application is
// not in an Applications folder: a login item would name a program that is
// not there at the next login, so it is refused.
const autostartNotInstalledNote = "请先把 Boku 放进「应用程序」文件夹，再打开开机启动。"

// autostartLabel is what the switch says.
const autostartLabel = "开机自动启动"

// autostartIdentifier is the application's identifier: the label the previous
// macOS mechanism wrote its launch agent under. The current manager keeps it
// only to find and remove that file on the first read, so an upgrade does not
// leave two login starts behind.
//
// Windows does not use it: a Run value is named after the program, which is
// already Boku or Boku Dev.
const autostartIdentifier = "com.boku.app"

// autostartSwitch draws the switch and the lines under it.
//
// There is no disabled state to draw: where the platform has no login start,
// the line says so and there is no checkbox at all. A checkbox that cannot be
// clicked is a claim that there is something to click, and there is not.
func (s *State) autostartSwitch(c *ui.Context) {
	on, enabled, status := s.autostartState()
	if enabled {
		ui.Checkbox(c, &on, autostartLabel).FontSize(presentation.SizeBody).OnClick(func() { s.setAutostart(on) })
	}
	if status.text != "" {
		presentation.Notice(c, status.kind, status.text)
	}
}
