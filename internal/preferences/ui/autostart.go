package ui

import (
	"boku/internal/presentation"
	"errors"
	"log/slog"

	"github.com/egoist/mygo/ui"

	"boku/internal/platform/autostart"
)

// 本文件实现开机自启：设置 → 通用里的开关及其状态来源。
//
// 它不属于 Boku 自己的设置，settings.json 里没有对应键：是否开机启动由系统掌握，
// 在 Boku 文件里再存一份会变成第二份真相并漂移。开关显示的始终是平台的回答，每帧读取。
// 读写都是本地的（一个注册表值或一个系统登录项），因此开关在按下的那一帧就地作答，
// 不经过网络设置那样的后台 goroutine。

// autostartStatus 是设置页在开关下方显示的内容：上次操作的结果，或没有开关可操作的原因。
type autostartStatus struct {
	text string
	kind presentation.NoteKind
}

// autostartLauncher 构造开关要写入的开机启动项。
//
// Windows 记下程序和「由系统启动」标记；macOS 登记应用包并忽略这两项。
// 仍传 Label，是为了让 macOS 管理器找到旧机制写下的 launch agent。
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

// autostartState 是通用区绘制的内容：开关是否打开、能否绘制、下方提示。
//
// 每帧读取而非缓存：答案是本地的，若显示打开时的旧状态，就会成为这套设计刻意不保留的第二份真相。
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

// setAutostart 打开或关闭开机自启，并给出系统的回答。下一帧开关仍从同一来源绘制，
// 因此系统拒绝的写入会显示为旧状态加一行原因，而不是开关动了、启动却没发生。
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

// 平台没有 Boku 能登记的开机启动方式时显示。这是平台事实而非用户失败，故以普通样式呈现。
const autostartUnsupportedNote = "这个平台没有 Boku 能登记的开机启动方式。"

// 系统里存在登录项但用户在其中关闭了它时显示。开关保持关闭（这是系统的回答），
// 提示行说明去哪里重新允许。
const autostartApprovalNote = "系统里的登录项已被关闭；请在「系统设置 > 通用 > 登录项与扩展」中允许 Boku。"

// 应用不在「应用程序」文件夹时显示：登录项会指向下一次登录时不存在的程序，故拒绝。
const autostartNotInstalledNote = "请先把 Boku 放进「应用程序」文件夹，再打开开机启动。"

// autostartLabel 是开关的文案。
const autostartLabel = "开机自动启动"

// autostartIdentifier 是应用标识，也是旧 macOS 机制写 launch agent 时用的 label。
// 现在的管理器只用它来在首次读取时找到并删除那个旧文件，避免升级后残留两个登录项。
//
// Windows 不用它：Run 值以程序命名，已经是 Boku 或 Boku Dev。
const autostartIdentifier = "com.boku.app"

// autostartSwitch 绘制开关及其下方提示行。
//
// 平台没有开机启动方式时不画禁用开关：提示行说明情况，且完全没有复选框。
// 一个点不动的复选框等于声称有东西可点，而实际上没有。
func (s *State) autostartSwitch(c *ui.Context) {
	on, enabled, status := s.autostartState()
	if enabled {
		ui.Checkbox(c, &on, autostartLabel).FontSize(presentation.SizeBody).OnClick(func() { s.setAutostart(on) })
	}
	if status.text != "" {
		presentation.Notice(c, status.kind, status.text)
	}
}
