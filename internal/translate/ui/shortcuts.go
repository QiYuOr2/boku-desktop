package ui

import (
	"boku/internal/presentation"
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
		s.finishExtras("", "")
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
		s.finishExtras("", "")
		return err
	}
	if screenshot == "" || s.screenshot == nil {
		s.finishExtras(text, "")
		return nil
	}
	if strings.EqualFold(screenshot, text) {
		s.finishExtras(text, "")
		return fmt.Errorf("截图翻译快捷键不能与划词翻译快捷键相同")
	}
	if err := s.shortcuts.Register(screenshot, s.onScreenshotShortcut); err != nil {
		s.finishExtras(text, "")
		return err
	}
	s.screenshotShortcut = shortcutState{accelerator: screenshot, registered: true}
	s.finishExtras(text, screenshot)
	return nil
}

// SetExtraRebind is how the shell puts another global shortcut back in the
// same pass that rebinds translation. nil leaves translation on its own.
func (s *State) SetExtraRebind(rebind func(register func(string, func()) error, text, screenshot string)) {
	s.extraRebind = rebind
}

func (s *State) finishExtras(text, screenshot string) {
	if s.extraRebind == nil || s.shortcuts == nil {
		return
	}
	s.extraRebind(s.shortcuts.Register, text, screenshot)
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
	if presentation.ModifierKey(ev.Key) {
		if !ev.Repeat {
			s.shortcutStateFor(kind).note = "修饰键要和一个字母或功能键一起按。"
			s.Refresh()
		}
		return true
	}
	accelerator, ok := presentation.AcceleratorOf(ev)
	if !ok {
		s.shortcutStateFor(kind).note = presentation.ModifierHint()
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
		s.screenshotShortcut = shortcutState{accelerator: previous, registered: restored == nil, err: restored, note: "用不了 " + presentation.FriendlyAccelerator(accelerator) + "：" + presentation.FriendlyError(err) + "，仍然是 " + presentation.FriendlyAccelerator(previous) + "。"}
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
			note: "用不了 " + presentation.FriendlyAccelerator(accelerator) + "：" + presentation.FriendlyError(err) +
				"，仍然是 " + presentation.FriendlyAccelerator(previous) + "。",
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
func (s *State) shortcutLine() (string, presentation.NoteKind) {
	accelerator := s.shortcut.accelerator
	if accelerator == "" {
		accelerator = s.translateSettingsOr().AcceleratorOr()
	}
	switch {
	case s.shortcut.recording:
		return "正在录制：按下新的组合键，Esc 取消。", presentation.NotePlain
	case s.shortcut.registered:
		return "", presentation.NotePlain
	case s.shortcut.err != nil:
		return "快捷键 " + presentation.FriendlyAccelerator(accelerator) + " 无法注册：" +
			presentation.FriendlyError(s.shortcut.err) + "。请换一个组合键。", presentation.NoteWarning
	default:
		return "快捷键尚未注册。", presentation.NoteWarning
	}
}

// shortcutNote is the last thing a change said, or nothing.
func (s *State) shortcutNote() (string, presentation.NoteKind) {
	if strings.TrimSpace(s.shortcut.note) == "" {
		return "", presentation.NotePlain
	}
	return s.shortcut.note, presentation.NoteWarning
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
