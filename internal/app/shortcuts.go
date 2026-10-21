package app

import (
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/translate"
)

// A global shortcut is what starts a translation while another application
// has the keyboard. It is registered through an interface rather than called
// directly, for one reason: registering one needs a running application and
// a graphical session, and the rules around it — a callback that returns at
// once, a failure that is shown rather than swallowed, an unregistration on
// the way out — are worth testing where neither exists.
type ShortcutRegistrar interface {
	// Register binds an accelerator, such as "Ctrl+Shift+T", to fn. It
	// returns an error when the accelerator is malformed or another
	// application already holds it.
	Register(accelerator string, fn func()) error
	// UnregisterAll releases every binding this application holds.
	UnregisterAll()
}

// mygoShortcuts is the real thing: the framework's own global shortcut
// module. Its callbacks run on the main thread, so nothing bound through it
// may wait on anything.
type mygoShortcuts struct{}

// MyGoShortcuts returns the registrar the assembled application uses. It is
// named here rather than in main so that the framework's module is reached
// from one place, and so that a test can stand in for it.
func MyGoShortcuts() ShortcutRegistrar { return mygoShortcuts{} }

func (mygoShortcuts) Register(accelerator string, fn func()) error {
	return mygo.GlobalShortcut.Register(accelerator, fn)
}

func (mygoShortcuts) UnregisterAll() { mygo.GlobalShortcut.UnregisterAll() }

// RegisterShortcuts binds the translation shortcut. main.go calls it once the
// application is ready: a global shortcut needs a running application.
func (s *State) RegisterShortcuts() { s.registerShortcuts() }

// UnregisterShortcuts releases it. main.go calls it on the way out.
func (s *State) UnregisterShortcuts() { s.unregisterShortcuts() }

// shortcutBinding is the one global shortcut the tool has, as something that
// can be registered: a combination to bind and what to run.
//
// There is exactly one binding in this build — 划词后弹出翻译. The design's
// other keys (朗读原文、固定浮窗、关闭浮窗) are not implemented, so they are
// not registered and the settings window does not offer to record them. It is
// a struct rather than two variables so that "a binding nobody could take" is
// a value that can be stored and shown.
type shortcutBinding struct {
	accelerator string
	fn          func()
}

// shortcutState is what became of the translation shortcut. It is what the
// settings window reads to say either the binding or why there is none:
// §6 of the tool design requires a registration failure to be visible rather than
// silent.
type shortcutState struct {
	// accelerator is the binding the platform holds, which is what the
	// settings window's button shows.
	accelerator string
	// registered is whether the platform took it.
	registered bool
	// err is why it did not, in the platform's own words.
	err error
	// recording is whether the window is waiting for the next key
	// combination. Nothing is registered or released until one is complete:
	// a user who is halfway through pressing keys has not asked for
	// anything yet.
	recording bool
	// note is why the last change could not be made, or what the recorder
	// still needs. An accepted change says nothing: the button already shows
	// the new binding.
	note string
}

type shortcutKind uint8

const (
	shortcutText shortcutKind = iota
	shortcutScreenshot
)

// registerShortcuts binds the translation shortcut and remembers what
// happened. It runs once, from WhenReady: a shortcut needs the running
// application, and a tray application that cannot bind one still starts.
//
// A tool that is switched off gets no binding at all. The switch is what the
// user turned off, and a combination that is still held by this process is a
// combination every other application is still denied: "off" has to mean the
// key is free again, not merely that pressing it draws an apology.
func (s *State) registerShortcuts() {
	accelerator := s.translateSettingsOr().AcceleratorOr()
	s.shortcut = shortcutState{accelerator: accelerator}
	s.screenshotShortcut = shortcutState{accelerator: s.translateSettingsOr().ScreenshotAcceleratorOr()}
	s.bindShortcutForSwitch()
}

// bindShortcutForSwitch takes or releases the translation binding, following
// the tool's switch. It is what the launch and the switch itself both call.
func (s *State) bindShortcutForSwitch() {
	if !s.translateSettingsOr().Enabled {
		s.unregisterShortcuts()
		s.shortcut.registered = false
		s.shortcut.err = nil
		return
	}
	accelerator := s.shortcut.accelerator
	if accelerator == "" {
		accelerator = s.translateSettingsOr().AcceleratorOr()
	}
	if err := s.bindShortcut(shortcutBinding{accelerator: accelerator, fn: s.onTranslateShortcut}); err != nil {
		s.shortcut.err = err
		s.shortcut.registered = false
		return
	}
	s.shortcut.accelerator = accelerator
	s.shortcut.registered = true
	s.shortcut.err = nil
}

// bindShortcut takes a binding, releasing what is held first.
//
// The framework has no call that releases one shortcut: it has one that
// releases them all. Re-binding therefore goes back to nothing and forwards
// again, and because that leaves a moment with no shortcut, a combination the
// platform refuses is answered by putting the previous one back.
func (s *State) bindShortcut(binding shortcutBinding) error {
	return s.bindShortcutSet(binding.accelerator, s.translateSettingsOr().ScreenshotAcceleratorOr())
}

// bindShortcutSet registers the two independent shortcuts as one operation:
// MyGo can release all bindings but not a single binding.
func (s *State) bindShortcutSet(text, screenshot string) error {
	if s.shortcuts == nil {
		return fmt.Errorf("没有可用的全局快捷键实现")
	}
	s.shortcuts.UnregisterAll()
	if err := s.shortcuts.Register(text, s.onTranslateShortcut); err != nil {
		return err
	}
	if screenshot == "" || s.screenshot == nil {
		return nil
	}
	if strings.EqualFold(screenshot, text) {
		return fmt.Errorf("截图翻译快捷键不能与划词翻译快捷键相同")
	}
	if err := s.shortcuts.Register(screenshot, s.onScreenshotShortcut); err != nil {
		return err
	}
	s.screenshotShortcut = shortcutState{accelerator: screenshot, registered: true}
	return nil
}

// unregisterShortcuts releases the binding. The application calls it on the
// way out: a shortcut left bound belongs to a process that no longer exists,
// and the next launch would find it taken by itself.
func (s *State) unregisterShortcuts() {
	if s.shortcuts != nil {
		s.shortcuts.UnregisterAll()
	}
}

// onTranslateShortcut is what the key press runs.
//
// It runs on the main thread, with the whole application waiting on it, so
// it does nothing but look at where the pointer is and hand the work to a
// goroutine: a translation is a network request, and a request must never be
// waited out on the thread that draws.
func (s *State) onTranslateShortcut() {
	slog.Info("translation shortcut invoked", "event", "text_translation_shortcut_invoked")
	s.startTranslation(s.cursorPoint())
}

func (s *State) onScreenshotShortcut() {
	slog.Info("screenshot shortcut invoked", "event", "screenshot_translation_shortcut_invoked")
	s.startScreenshotTranslation()
}

// clearScreenshotShortcut persists the explicit disabled state and releases
// both bindings before restoring the text shortcut alone.
//
// A write that fails is logged rather than swallowed: the window has already
// said the shortcut is cleared, and a setting that did not reach the file
// comes back at the next launch with nobody told why. The binding itself is
// released either way — this run is what the user asked about, and the
// remembered value is only about the next one.
func (s *State) clearScreenshotShortcut() {
	slog.Info("screenshot shortcut cleared", "event", "screenshot_shortcut_cleared")
	if s.translate != nil {
		if err := s.translate.SetScreenshotAccelerator(backgroundContext(), ""); err != nil {
			slog.Warn("the cleared screenshot shortcut could not be remembered", "event", "screenshot_shortcut_clear_failed", "error", err)
		}
	}
	s.screenshotShortcut = shortcutState{}
	if s.translateSettingsOr().Enabled {
		s.bindShortcutForSwitch()
	}
	s.Refresh()
}

// beginShortcutCapture starts recording the next key combination.
func (s *State) beginShortcutCapture(kinds ...shortcutKind) {
	kind := shortcutText
	if len(kinds) > 0 {
		kind = kinds[0]
	}
	s.shortcut.recording = kind == shortcutText
	s.screenshotShortcut.recording = kind == shortcutScreenshot
	s.shortcut.note = ""
	s.screenshotShortcut.note = ""
	slog.Info("shortcut recording started", "event", "shortcut_recording_started", "kind", kind)
	s.Refresh()
}

// cancelShortcutCapture stops recording without changing anything.
func (s *State) cancelShortcutCapture() {
	s.shortcut.recording = false
	s.screenshotShortcut.recording = false
	slog.Info("shortcut recording cancelled", "event", "shortcut_recording_cancelled")
	s.Refresh()
}

// captureShortcut is what the recording button is given: every key that
// arrives while it has the keyboard.
//
// It waits for a complete combination — a key that types something, or a
// function key, with at least one of Ctrl, Alt or Super — and binds it. A
// modifier pressed on its own is not a combination, and a key with no
// modifier at all would take that key from every application on the machine:
// both are refused with a word rather than registered. The event is taken
// either way, so that what the user presses while recording does not also act
// on the window behind the button.
func (s *State) captureShortcut(ev ui.InputEvent) bool {
	kind := shortcutText
	if s.screenshotShortcut.recording {
		kind = shortcutScreenshot
	} else if !s.shortcut.recording {
		return false
	}
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	if ev.Key == ui.KeyEscape {
		s.cancelShortcutCapture()
		s.Refresh()
		return true
	}
	if isModifierKey(ev.Key) {
		if !ev.Repeat {
			s.shortcutStateFor(kind).note = "修饰键要和一个字母或功能键一起按。"
			s.Refresh()
		}
		return true
	}
	accelerator, ok := acceleratorOf(ev)
	if !ok {
		s.shortcutStateFor(kind).note = modifierHint()
		s.Refresh()
		return true
	}
	if kind == shortcutScreenshot {
		s.applyScreenshotShortcut(accelerator)
	} else {
		s.applyShortcut(accelerator)
	}
	return true
}

func (s *State) shortcutStateFor(kind shortcutKind) *shortcutState {
	if kind == shortcutScreenshot {
		return &s.screenshotShortcut
	}
	return &s.shortcut
}

func (s *State) applyScreenshotShortcut(accelerator string) {
	previous := s.screenshotShortcut.accelerator
	if previous == "" {
		previous = s.translateSettingsOr().ScreenshotAcceleratorOr()
	}
	s.screenshotShortcut.recording = false
	text := s.shortcut.accelerator
	if text == "" {
		text = s.translateSettingsOr().AcceleratorOr()
	}
	if err := s.bindShortcutSet(text, accelerator); err != nil {
		restored := s.bindShortcutSet(text, previous)
		s.screenshotShortcut = shortcutState{accelerator: previous, registered: restored == nil, err: restored, note: "用不了 " + friendlyAccelerator(accelerator) + "：" + friendlyError(err) + "，仍然是 " + friendlyAccelerator(previous) + "。"}
		s.Refresh()
		return
	}
	s.screenshotShortcut = shortcutState{accelerator: accelerator, registered: true}
	slog.Info("screenshot shortcut changed", "event", "screenshot_shortcut_changed", "accelerator", accelerator)
	if s.translate != nil {
		if err := s.translate.SetScreenshotAccelerator(backgroundContext(), accelerator); err != nil {
			slog.Warn("the screenshot shortcut could not be remembered", "accelerator", accelerator, "error", err)
		}
	}
	s.Refresh()
}

// applyShortcut binds a combination and says what happened.
//
// A combination another application holds is the case this is written for:
// the binding that was there is put back, and the window says which one is
// still in use. A user who tries a combination has not asked to lose the
// shortcut they had.
func (s *State) applyShortcut(accelerator string) {
	previous := s.shortcut.accelerator
	if previous == "" {
		previous = s.translateSettingsOr().AcceleratorOr()
	}
	s.shortcut.recording = false
	if err := s.bindShortcut(shortcutBinding{accelerator: accelerator, fn: s.onTranslateShortcut}); err != nil {
		restored := s.bindShortcut(shortcutBinding{accelerator: previous, fn: s.onTranslateShortcut})
		s.shortcut = shortcutState{
			accelerator: previous,
			registered:  restored == nil,
			err:         restored,
			note: "用不了 " + friendlyAccelerator(accelerator) + "：" + friendlyError(err) +
				"，仍然是 " + friendlyAccelerator(previous) + "。",
		}
		s.Refresh()
		return
	}
	s.shortcut = shortcutState{
		accelerator: accelerator,
		registered:  true,
	}
	slog.Info("translation shortcut changed", "event", "text_translation_shortcut_changed", "accelerator", accelerator)
	if s.translate != nil {
		// The binding is remembered, so that the next launch registers the
		// same one. Failing to write it does not take the working binding
		// away: what is bound is bound — but the next launch will come back
		// with the old one, which is worth saying.
		if err := s.translate.SetAccelerator(backgroundContext(), accelerator); err != nil {
			slog.Warn("the shortcut could not be remembered for the next launch",
				"accelerator", accelerator, "error", err)
		}
	}
	s.Refresh()
}

// resetShortcut puts the default combination back.
func (s *State) resetShortcut() {
	s.applyShortcut(translate.DefaultAccelerator)
}

// shortcutLine is what the settings window writes beside the shortcut: the
// binding, or why there is none.
func (s *State) shortcutLine() (string, noteKind) {
	accelerator := s.shortcut.accelerator
	if accelerator == "" {
		accelerator = s.translateSettingsOr().AcceleratorOr()
	}
	switch {
	case s.shortcut.recording:
		return "正在录制：按下新的组合键，Esc 取消。", notePlain
	case s.shortcut.registered:
		return "", notePlain
	case s.shortcut.err != nil:
		return "快捷键 " + friendlyAccelerator(accelerator) + " 无法注册：" +
			friendlyError(s.shortcut.err) + "。请换一个组合键。", noteWarning
	default:
		return "快捷键尚未注册。", noteWarning
	}
}

// shortcutNote is the last thing a change said, or nothing.
func (s *State) shortcutNote() (string, noteKind) {
	if strings.TrimSpace(s.shortcut.note) == "" {
		return "", notePlain
	}
	return s.shortcut.note, noteWarning
}

// acceleratorOf turns a key press into the accelerator the platform's own API
// takes, and reports whether it is a combination at all.
//
// The modifiers are written in the order people say them, and the key in the
// spelling the framework's parser reads.
func acceleratorOf(ev ui.InputEvent) (string, bool) {
	name, ok := keyNames[ev.Key]
	if !ok {
		// A key this build has no name for is not something to bind: a
		// binding that cannot be written down cannot be stored either.
		return "", false
	}
	mods := ev.Mods
	if mods&(ui.Ctrl|ui.Alt|ui.Super) == 0 {
		// Shift alone is not enough: Shift+A is how a capital is typed, and
		// taking it would take the keyboard from every other application.
		return "", false
	}
	parts := make([]string, 0, 4)
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	return strings.Join(append(parts, name), "+"), true
}

// isModifierKey reports whether a key press is a modifier on its own.
//
// The framework reports a modifier as an unknown key with its bit set in the
// modifiers rather than as a key of its own: that is what makes a combination
// readable in one event, and what makes a lone modifier recognisable.
func isModifierKey(key ui.Key) bool {
	return key == ui.KeyUnknown
}

// keyNames is what each key is called in an accelerator. It is written down
// rather than derived, because the framework's own table is internal, and a
// binding spelled the way its parser does not read is a binding that fails in
// front of the user.
//
// A key that is not here cannot be bound, which is deliberate: the letters,
// the digits, the function keys and the named keys are here, and a key nobody
// binds is one whose name cannot be got wrong.
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

// modifierHint is what the recorder says when a key press has no modifier:
// the names are the platform's own, so a Mac user is not told to press Ctrl
// when the key is Command.
func modifierHint() string {
	if runtime.GOOS == "darwin" {
		return "需要一个带 Command（⌘）或 Control 的组合键。"
	}
	return "需要一个带 Ctrl / Alt / Super 的组合键。"
}

// friendlyAccelerator writes a binding the way a person reads it: the parts
// separated by spaces around the plus, which is how a shortcut is written
// down outside a configuration file. The platform names the modifiers it has:
// Command on macOS, Ctrl on the others, and "CmdOrCtrl" resolves to whichever
// the platform uses.
func friendlyAccelerator(accelerator string) string {
	return friendlyAcceleratorFor(accelerator, runtime.GOOS)
}

// friendlyAcceleratorFor is friendlyAccelerator with the platform passed in,
// so that both spellings can be tested on one machine.
func friendlyAcceleratorFor(accelerator, goos string) string {
	return strings.Join(acceleratorParts(accelerator, goos), " + ")
}

// acceleratorParts resolves a binding into the names the platform gives its
// modifiers and the key, in the order they were written.
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
			// The framework reads Command as Super; a person reads it as
			// Command, and the symbol is what the menu bar shows.
			part = "Command"
		}
		parts[index] = part
	}
	return parts
}

// compactAccelerator writes a binding for the recorder field: the same names
// the sentence below uses, with no spaces around the plus. The field is 140
// points wide and has to show the longest ordinary three-key combination
// beside the clear icon of the screenshot binding — "Command + Control + T"
// does not fit, "Command+Control+T" does. The long spelling still belongs to
// prose (friendlyAccelerator) and to what assistive technology reads.
func compactAccelerator(accelerator string) string {
	return compactAcceleratorFor(accelerator, runtime.GOOS)
}

// compactAcceleratorFor is compactAccelerator with the platform passed in.
func compactAcceleratorFor(accelerator, goos string) string {
	return strings.Join(acceleratorParts(accelerator, goos), "+")
}

// shortcutPlatformNote is what the settings window says about the platform:
// on Linux the binding is made by the desktop portal rather than by the
// application itself.
func shortcutPlatformNote() string {
	if runtime.GOOS == "linux" {
		return "Linux / Wayland 下快捷键由桌面门户绑定，可能需要系统确认。"
	}
	return ""
}

// selectionPlatformNote is what the settings window says about reading the
// selection on this platform. macOS needs a permission for it, and a user who
// is not told will see the tool fall back to the clipboard without knowing
// why.
func selectionPlatformNote() string {
	if runtime.GOOS == "darwin" {
		return "macOS 取词需要在「系统设置 > 隐私与安全性 > 辅助功能」中允许 Boku；点下方按钮可让系统列出 Boku 并弹出授权窗口。未授权时会改为读取剪贴板。"
	}
	return ""
}
