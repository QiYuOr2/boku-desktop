package app

import (
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
// The read and the write are both local — one registry value, or one small
// file — so the switch answers in the frame it was pressed in rather than
// through a background goroutine the way the network-written settings do.

// autostartStatus is what the settings page says under the switch: what the
// last press did, or why there is nothing to press.
type autostartStatus struct {
	text string
	kind noteKind
}

// autostartLauncher builds the login start the switch would write.
//
// The program is the one BinaryPath answers with — the installer's shortcut
// on Windows, the executable inside the bundle on macOS — and the argument is
// the framework's own "started by the system" marker, so that a start nobody
// asked for does not put a window on screen.
func (s *State) autostartLauncher() (autostart.Launcher, error) {
	exe, err := autostart.BinaryPath()
	if err != nil {
		return autostart.Launcher{}, err
	}
	return autostart.Launcher{
		Label: autostartIdentifier,
		Name:  appName,
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
		return false, false, autostartStatus{text: "找不到程序本身：" + friendlyError(err), kind: noteDanger}
	}
	if !autostart.Available(s.autostart, launcher) {
		return false, false, autostartStatus{text: autostartUnsupportedNote, kind: notePlain}
	}
	on, err = s.autostart.Read(launcher)
	if err != nil {
		return false, false, autostartStatus{text: "读不到开机启动的状态：" + friendlyError(err), kind: noteDanger}
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
		s.autostartStatus = autostartStatus{text: "没能改成开机启动：" + friendlyError(err), kind: noteDanger}
		s.Refresh()
		return
	}
	slog.Info("the login start was changed", "on", on)
	s.autostartStatus = autostartStatus{}
	s.Refresh()
}

// autostartPathLine names where the start lives — the launch agent's path on
// macOS, the registry value on Windows. It is asked of the platform rather
// than written down here, so that the line and the file cannot disagree.
func (s *State) autostartPathLine() string {
	if s.autostart == nil {
		return ""
	}
	launcher, err := s.autostartLauncher()
	if err != nil {
		return ""
	}
	doc, err := s.autostart.Document(launcher)
	if err != nil {
		return ""
	}
	return doc.Key
}

// autostartUnsupportedNote is what the section says on a desktop with no
// login start Boku can register. It is a fact about the platform, not a
// failure of the user's, so it is drawn quietly.
const autostartUnsupportedNote = "这个平台没有 Boku 能登记的开机启动方式。"

// autostartHelps is the line under a working switch: where the start is
// unregistered from, which is what a user does when the switch does not tell
// them what they want to know — "did it work?" is answered by the system's own
// list, not by this page.
func autostartHelps() string {
	return "关闭后取消登记；系统中也可以随时查看或停用这一项。"
}

// autostartLabel is what the switch says.
const autostartLabel = "开机自动启动"

// autostartIdentifier is the name the login start is recorded under where the
// platform names it rather than reading it back: the launch agent's file on
// macOS. It is the application's own identifier — com.boku.app, and
// com.boku.app.dev for the development build — so that the two builds keep
// two login starts, the way they keep two settings files and two single-
// instance locks.
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
		ui.Checkbox(c, &on, autostartLabel).FontSize(sizeBody).OnClick(func() { s.setAutostart(on) })
		notice(c, notePlain, autostartHelps())
		if path := s.autostartPathLine(); path != "" {
			ui.Text(c, path).
				FontSize(sizeTiny).
				TextColor(colorTextMuted).
				Font("monospace").
				Wrap()
		}
	}
	if status.text != "" {
		notice(c, status.kind, status.text)
	}
}
