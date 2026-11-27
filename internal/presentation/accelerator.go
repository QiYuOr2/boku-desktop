package presentation

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// AcceleratorOf 按平台快捷键 API 的读法拼写按键。单独的 Shift 会被拒绝：
// Shift+A 是输入大写字母的方式，把它当成快捷键会从其他应用手里抢走该键。
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

// ModifierKey 判断是否单独按下了修饰键。框架把它报告成一个置位的未知键，
// 而不是独立的按键。
func ModifierKey(key ui.Key) bool { return key == ui.KeyUnknown }

// acceleratorNames 是每个键在快捷键里的名字。这里手写而非推导，因为框架自己的
// 表是内部的，而拼成解析器读不懂的写法会在用户面前出错。不在表里的键无法绑定。
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

// ModifierHint 是录键器在按键没有可用修饰键时给出的提示。
func ModifierHint() string {
	if runtime.GOOS == "darwin" {
		return "需要一个带 Command（⌘）或 Control 的组合键。"
	}
	return "需要一个带 Ctrl / Alt / Super 的组合键。"
}

// ShortcutPlatformNote 是设置里关于 Linux 的说明：那里由桌面门户而非应用自身
// 完成绑定。
func ShortcutPlatformNote() string {
	if runtime.GOOS == "linux" {
		return "Linux / Wayland 下快捷键由桌面门户绑定，可能需要系统确认。"
	}
	return ""
}

// FriendlyAccelerator 按人阅读的方式书写绑定：macOS 上是 Command，其它平台是
// Ctrl，"CmdOrCtrl" 解析为当前平台对应的那个。
func FriendlyAccelerator(accelerator string) string {
	return FriendlyAcceleratorFor(accelerator, runtime.GOOS)
}

// FriendlyAcceleratorFor 是显式传入平台的 FriendlyAccelerator。
func FriendlyAcceleratorFor(accelerator, goos string) string {
	return strings.Join(acceleratorParts(accelerator, goos), " + ")
}

// CompactAccelerator 是同样的名字但加号两边不带空格，好让三键组合塞进录键器
// 输入框。
func CompactAccelerator(accelerator string) string {
	return CompactAcceleratorFor(accelerator, runtime.GOOS)
}

// CompactAcceleratorFor 是显式传入平台的 CompactAccelerator。
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
			// 框架把 Command 读作 Super；人则把它读作 Command。
			part = "Command"
		}
		parts[index] = part
	}
	return parts
}
