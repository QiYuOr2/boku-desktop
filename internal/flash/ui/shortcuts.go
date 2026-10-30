package ui

import (
	"context"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/presentation"
)

// shortcutButton is the same recorder the translation settings use: the binding
// sits in a fixed field, a click waits for the next combination, and the clear
// mark removes it. Flash has no default, so there is no reset.
func (s *State) shortcutButton(c *ui.Context) ui.Element {
	accelerator := s.accelerator
	label := compactAccelerator(accelerator)
	if label == "" {
		label = "点击录制"
	}
	if s.recording {
		label = "按下组合键…"
	}
	control := ui.Box(c).Width(float32(140)).Height(presentation.HeightControl - 4)
	labelPad := float32(12)
	if accelerator != "" && s.closeMark != nil {
		labelPad = 24
	}
	control.Children(func() {
		button := ui.ButtonBase(c).Label("快捷键 "+friendlyAccelerator(accelerator)).
			Absolute().Fill().
			Padding(0, labelPad, 0, 12).
			Radius(presentation.RadiusField).
			Border(1, presentation.ColorBorder).
			Background(presentation.ColorSurface).
			Focusable().
			Cursor(ui.CursorPointer)
		switch {
		case s.recording:
			button.Background(presentation.ColorBrandTintStrong).Border(1, presentation.ColorBrand)
		case button.Hovered() || button.Focused():
			button.Background(presentation.ColorHover)
		}
		button.Children(func() {
			text := ui.Text(c, label).
				FontSize(presentation.SizeTiny).
				Font("monospace").
				LetterSpacing(float32(-0.25)).
				SingleLine()
			if s.recording {
				text.TextColor(presentation.ColorBrand)
			} else {
				text.TextColor(presentation.ColorTextSoft)
			}
		})
		button.HandleInput(s.captureShortcut)
		if s.recording {
			button.AutoFocus()
		}
		if button.Clicked() {
			if s.recording {
				s.cancelShortcutCapture()
			} else {
				s.beginShortcutCapture()
			}
		}
		if accelerator != "" && s.closeMark != nil {
			if clear := presentation.CardIconButton(c, s.closeMark, "清除闪念快捷键").Absolute().Right(float32(2)).Top(shortcutClearTop()); clear.Clicked() {
				s.clearAccelerator()
			}
		}
	})
	return control
}

func shortcutClearTop() float32 {
	fieldHeight := presentation.HeightControl - 4
	iconTarget := presentation.SizeCardMark + 2*presentation.CardMarkPad
	return float32((fieldHeight - iconTarget) / 2)
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

// captureShortcut reads the combination while the field is recording.
// A lone modifier, or a key with no modifier, is refused: the first is not a
// shortcut, and the second would take that key from every application.
func (s *State) captureShortcut(ev ui.InputEvent) bool {
	if !s.recording || ev.Kind != ui.InputKeyDown {
		return false
	}
	if ev.Key == ui.KeyEscape {
		s.cancelShortcutCapture()
		return true
	}
	if isModifierKey(ev.Key) {
		if !ev.Repeat {
			s.shortcutNote = "修饰键要和一个字母或功能键一起按。"
			s.host.Refresh()
		}
		return true
	}
	accelerator, ok := acceleratorOf(ev)
	if !ok {
		s.shortcutNote = modifierHint()
		s.host.Refresh()
		return true
	}
	s.applyAccelerator(accelerator)
	return true
}

// applyAccelerator stores the combination and asks the shell to register every
// global shortcut again. A refusal puts the previous binding back: trying a
// new one must not leave the user with none.
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
		s.shortcutNote = acceleratorHint()
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
			s.shortcutNote = "用不了 " + friendlyAccelerator(accelerator) + "：" + reason + "。"
		} else {
			s.rebind()
			if strings.TrimSpace(previous) != "" {
				kept = friendlyAccelerator(previous)
			}
			s.shortcutNote = "用不了 " + friendlyAccelerator(accelerator) + "：" + reason + "，仍然是 " + kept + "。"
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

func acceleratorHint() string {
	if runtime.GOOS == "darwin" {
		return "需要一个带 Command（⌘）或 Control 的组合键。"
	}
	return "需要一个带 Ctrl / Alt / Super 的组合键。"
}

func modifierHint() string { return acceleratorHint() }

func shortcutPlatformNote() string {
	if runtime.GOOS == "linux" {
		return "Linux / Wayland 下快捷键由桌面门户绑定，可能需要系统确认。"
	}
	return ""
}

// acceleratorOf spells a key press the way the platform shortcut API reads it.
// Shift alone is refused: Shift+A is how a capital letter is typed.
func acceleratorOf(ev ui.InputEvent) (string, bool) {
	name, ok := keyNames[ev.Key]
	if !ok {
		return "", false
	}
	if ev.Mods&(ui.Ctrl|ui.Alt|ui.Super) == 0 {
		return "", false
	}
	parts := make([]string, 0, 4)
	if ev.Mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if ev.Mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if ev.Mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if ev.Mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	return strings.Join(append(parts, name), "+"), true
}

func isModifierKey(key ui.Key) bool { return key == ui.KeyUnknown }

var keyNames = map[ui.Key]string{
	ui.KeyEnter:        "Enter",
	ui.KeyBackspace:    "Backspace",
	ui.KeyTab:          "Tab",
	ui.KeySpace:        "Space",
	ui.KeyDelete:       "Delete",
	ui.KeyInsert:       "Insert",
	ui.KeyHome:         "Home",
	ui.KeyEnd:          "End",
	ui.KeyPageUp:       "PageUp",
	ui.KeyPageDown:     "PageDown",
	ui.KeyUp:           "Up",
	ui.KeyDown:         "Down",
	ui.KeyLeft:         "Left",
	ui.KeyRight:        "Right",
	ui.KeyMinus:        "-",
	ui.KeyEqual:        "=",
	ui.KeyComma:        ",",
	ui.KeyPeriod:       ".",
	ui.KeySlash:        "/",
	ui.KeySemicolon:    ";",
	ui.KeyQuote:        "'",
	ui.KeyBracketLeft:  "[",
	ui.KeyBracketRight: "]",
	ui.KeyBackslash:    "\\",
	ui.KeyBackquote:    "`",
	ui.KeyF1:           "F1",
	ui.KeyF2:           "F2",
	ui.KeyF3:           "F3",
	ui.KeyF4:           "F4",
	ui.KeyF5:           "F5",
	ui.KeyF6:           "F6",
	ui.KeyF7:           "F7",
	ui.KeyF8:           "F8",
	ui.KeyF9:           "F9",
	ui.KeyF10:          "F10",
	ui.KeyF11:          "F11",
	ui.KeyF12:          "F12",
	ui.Key0:            "0",
	ui.Key1:            "1",
	ui.Key2:            "2",
	ui.Key3:            "3",
	ui.Key4:            "4",
	ui.Key5:            "5",
	ui.Key6:            "6",
	ui.Key7:            "7",
	ui.Key8:            "8",
	ui.Key9:            "9",
	ui.KeyA:            "A",
	ui.KeyB:            "B",
	ui.KeyC:            "C",
	ui.KeyD:            "D",
	ui.KeyE:            "E",
	ui.KeyF:            "F",
	ui.KeyG:            "G",
	ui.KeyH:            "H",
	ui.KeyI:            "I",
	ui.KeyJ:            "J",
	ui.KeyK:            "K",
	ui.KeyL:            "L",
	ui.KeyM:            "M",
	ui.KeyN:            "N",
	ui.KeyO:            "O",
	ui.KeyP:            "P",
	ui.KeyQ:            "Q",
	ui.KeyR:            "R",
	ui.KeyS:            "S",
	ui.KeyT:            "T",
	ui.KeyU:            "U",
	ui.KeyV:            "V",
	ui.KeyW:            "W",
	ui.KeyX:            "X",
	ui.KeyY:            "Y",
	ui.KeyZ:            "Z",
}

func friendlyAccelerator(accelerator string) string {
	return strings.Join(acceleratorParts(accelerator, runtime.GOOS), " + ")
}

func compactAccelerator(accelerator string) string {
	return strings.Join(acceleratorParts(accelerator, runtime.GOOS), "+")
}

func acceleratorParts(accelerator, goos string) []string {
	trimmed := strings.TrimSpace(accelerator)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "+")
	for index, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.EqualFold(part, "CmdOrCtrl"):
			part = "Ctrl"
			if goos == "darwin" {
				part = "Command"
			}
		case strings.EqualFold(part, "Super") && goos == "darwin":
			part = "Command"
		}
		parts[index] = part
	}
	return parts
}
