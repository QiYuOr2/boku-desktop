package ui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/presentation"
)

func (s *State) shortcutButton(c *ui.Context) ui.Element {
	return presentation.ShortcutField{
		Accelerator: s.accelerator,
		Recording:   s.recording,
		ClearMark:   s.closeMark,
		ClearLabel:  "清除闪念快捷键",
		OnInput:     s.captureShortcut,
		OnPress: func() {
			if s.recording {
				s.cancelShortcutCapture()
			} else {
				s.beginShortcutCapture()
			}
		},
		OnClear: s.clearAccelerator,
	}.View(c)
}

func (s *State) beginShortcutCapture() {
	s.recording = true
	s.shortcutNote = ""
	s.host.Refresh()
}

func (s *State) cancelShortcutCapture() {
	s.recording = false
	s.shortcutNote = ""
	s.host.Refresh()
}

// captureShortcut 在录制时读取组合键。单独的修饰键、以及不带修饰键的按键
// 都会被拒绝：前者不是快捷键，后者会把这颗键从所有应用那里抢走。
func (s *State) captureShortcut(ev ui.InputEvent) bool {
	if !s.recording || ev.Kind != ui.InputKeyDown {
		return false
	}
	if ev.Key == ui.KeyEscape {
		s.cancelShortcutCapture()
		return true
	}
	if presentation.ModifierKey(ev.Key) {
		if !ev.Repeat {
			s.shortcutNote = "修饰键要和一个字母或功能键一起按。"
			s.host.Refresh()
		}
		return true
	}
	accelerator, ok := presentation.AcceleratorOf(ev)
	if !ok {
		s.shortcutNote = presentation.ModifierHint()
		s.host.Refresh()
		return true
	}
	s.applyAccelerator(accelerator)
	return true
}

// applyAccelerator 保存组合键并让 shell 重新注册所有全局快捷键。注册被拒时
// 恢复上一个绑定：尝试新快捷键不能把用户弄成没有快捷键。
func (s *State) applyAccelerator(accelerator string) {
	previous := s.Accelerator()
	s.recording = false
	s.shortcutNote = ""
	if s.flashes == nil {
		s.shortcutNote = "闪念没有装上"
		s.host.Refresh()
		return
	}
	if err := s.flashes.SetAccelerator(context.Background(), accelerator); err != nil {
		s.shortcutNote = presentation.ModifierHint()
		s.host.Refresh()
		return
	}
	s.rebind()
	if strings.TrimSpace(accelerator) != "" && !s.shortcutOn {
		reason := s.shortcutErr
		if reason == "" {
			reason = "快捷键没有绑上"
		}
		kept := "未绑定"
		if err := s.flashes.SetAccelerator(context.Background(), previous); err != nil {
			s.shortcutNote = "用不了 " + presentation.FriendlyAccelerator(accelerator) + "：" + reason + "。"
		} else {
			s.rebind()
			if strings.TrimSpace(previous) != "" {
				kept = presentation.FriendlyAccelerator(previous)
			}
			s.shortcutNote = "用不了 " + presentation.FriendlyAccelerator(accelerator) + "：" + reason + "，仍然是 " + kept + "。"
		}
	}
	s.host.Refresh()
}

func (s *State) clearAccelerator() {
	s.recording = false
	s.shortcutNote = ""
	if s.flashes == nil {
		s.shortcutNote = "闪念没有装上"
		s.host.Refresh()
		return
	}
	if err := s.flashes.SetAccelerator(context.Background(), ""); err != nil {
		s.shortcutNote = "没有清除快捷键"
		s.host.Refresh()
		return
	}
	s.rebind()
	s.host.Refresh()
}

func (s *State) rebind() {
	if s.rebindAll != nil {
		s.rebindAll()
		return
	}
	s.accelerator = s.Accelerator()
	s.shortcutErr = ""
	s.shortcutOn = strings.TrimSpace(s.accelerator) != ""
}
