package presentation

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// AcceleratorOf spells a key press the way the platform shortcut API reads it.
// Shift alone is refused: Shift+A is how a capital letter is typed, and taking
// it would take that key from every other application.
func AcceleratorOf(ev ui.InputEvent) (string, bool) {
	name, ok := acceleratorNames[ev.Key]
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

// ModifierKey reports a modifier pressed on its own. The framework reports it
// as an unknown key with its bit set, rather than as a key of its own.
func ModifierKey(key ui.Key) bool { return key == ui.KeyUnknown }

// acceleratorNames is what each key is called in an accelerator. It is written
// down rather than derived, because the framework's own table is internal, and
// a binding spelled the way its parser does not read fails in front of the user.
// A key that is not here cannot be bound.
var acceleratorNames = map[ui.Key]string{
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

// ModifierHint is what the recorder says when a key press has no usable modifier.
func ModifierHint() string {
	if runtime.GOOS == "darwin" {
		return "需要一个带 Command（⌘）或 Control 的组合键。"
	}
	return "需要一个带 Ctrl / Alt / Super 的组合键。"
}

// ShortcutPlatformNote is what settings say about Linux, where the binding is
// made by the desktop portal rather than by the application itself.
func ShortcutPlatformNote() string {
	if runtime.GOOS == "linux" {
		return "Linux / Wayland 下快捷键由桌面门户绑定，可能需要系统确认。"
	}
	return ""
}

// FriendlyAccelerator writes a binding the way a person reads it. Command on
// macOS, Ctrl on the others, and "CmdOrCtrl" resolves to whichever the platform uses.
func FriendlyAccelerator(accelerator string) string {
	return FriendlyAcceleratorFor(accelerator, runtime.GOOS)
}

// FriendlyAcceleratorFor is FriendlyAccelerator with the platform passed in.
func FriendlyAcceleratorFor(accelerator, goos string) string {
	return strings.Join(acceleratorParts(accelerator, goos), " + ")
}

// CompactAccelerator is the same names without spaces around the plus, so a
// three-key combination fits in the recorder field.
func CompactAccelerator(accelerator string) string {
	return CompactAcceleratorFor(accelerator, runtime.GOOS)
}

// CompactAcceleratorFor is CompactAccelerator with the platform passed in.
func CompactAcceleratorFor(accelerator, goos string) string {
	return strings.Join(acceleratorParts(accelerator, goos), "+")
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
			// The framework reads Command as Super; a person reads it as Command.
			part = "Command"
		}
		parts[index] = part
	}
	return parts
}
