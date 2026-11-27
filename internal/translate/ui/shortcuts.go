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

type ShortcutRegistrar interface {
	// Register 把诸如 "Ctrl+Shift+T" 的快捷键绑定到 fn。快捷键格式错误或已被其他应用占用时返回错误。
	Register(accelerator string, fn func()) error
	// UnregisterAll 释放本应用持有的所有绑定。
	UnregisterAll()
}

// mygoShortcuts 是真实实现：框架自带的全局快捷键模块。其回调在主线程运行，
// 所以通过它绑定的任何东西都不能等待。
type mygoShortcuts struct{}

func MyGoShortcuts() ShortcutRegistrar { return mygoShortcuts{} }

func (mygoShortcuts) Register(accelerator string, fn func()) error {
	return mygo.GlobalShortcut.Register(accelerator, fn)
}

func (mygoShortcuts) UnregisterAll() { mygo.GlobalShortcut.UnregisterAll() }

// 应用就绪后注册全局快捷键。
func (s *State) RegisterShortcuts() { s.registerShortcuts() }

func (s *State) UnregisterShortcuts() { s.unregisterShortcuts() }

type shortcutBinding struct {
	accelerator string
	fn          func()
}

type shortcutState struct {
	accelerator string
	registered  bool
	err         error
	// recording 表示窗口正在等待下一个组合键。未组成完整组合前不注册也不释放：
	// 用户还没按完键就不算提出了任何请求。
	recording bool
	note      string
}

type shortcutKind uint8

const (
	shortcutText shortcutKind = iota
	shortcutScreenshot
)

// 关闭工具必须释放全局快捷键，避免继续占用其它应用可用的组合。
func (s *State) registerShortcuts() {
	accelerator := s.translateSettingsOr().AcceleratorOr()
	s.shortcut = shortcutState{accelerator: accelerator}
	s.screenshotShortcut = shortcutState{accelerator: s.translateSettingsOr().ScreenshotAcceleratorOr()}
	s.bindShortcutForSwitch()
}

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

// MyGo 只能一次释放全部绑定；新组合注册失败时须恢复旧组合。
func (s *State) bindShortcut(binding shortcutBinding) error {
	return s.bindShortcutSet(binding.accelerator, s.translateSettingsOr().ScreenshotAcceleratorOr())
}

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

// SetExtraRebind 让外壳在重绑翻译的同一趟里放回另一个全局快捷键。nil 表示不额外处理。
func (s *State) SetExtraRebind(rebind func(register func(string, func()) error, text, screenshot string)) {
	s.extraRebind = rebind
}

func (s *State) finishExtras(text, screenshot string) {
	if s.extraRebind == nil || s.shortcuts == nil {
		return
	}
	s.extraRebind(s.shortcuts.Register, text, screenshot)
}

// unregisterShortcuts 释放绑定。应用在退出时调用：遗留的绑定属于一个已不存在的进程，
// 下次启动会发现自己想用的键被自己占着。
func (s *State) unregisterShortcuts() {
	if s.shortcuts != nil {
		s.shortcuts.UnregisterAll()
	}
}

// onTranslateShortcut 是按键触发时执行的逻辑。它运行在主线程、整个应用都在等它，
// 所以只做一件事：看点在哪，然后把活交给 goroutine——翻译是网络请求，
// 决不能在绘制线程上等它完成。
func (s *State) onTranslateShortcut() {
	slog.Info("translation shortcut invoked", "event", "text_translation_shortcut_invoked")
	s.startTranslation(s.cursorPoint())
}

func (s *State) onScreenshotShortcut() {
	slog.Info("screenshot shortcut invoked", "event", "screenshot_translation_shortcut_invoked")
	s.startScreenshotTranslation()
}

// clearScreenshotShortcut 持久化显式的禁用状态，并先释放两个绑定、再只恢复文字快捷键。
// 写入失败要记日志而非吞掉：窗口已说快捷键已清除，没写进文件的设置会在下次启动时复活，
// 却没人知道原因。绑定本身无论如何都释放——用户关心的是这一次运行，记住的值只关乎下一次。
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

func (s *State) cancelShortcutCapture() {
	s.shortcut.recording = false
	s.screenshotShortcut.recording = false
	slog.Info("shortcut recording cancelled", "event", "shortcut_recording_cancelled")
	s.Refresh()
}

// captureShortcut 是录制按钮接收的输入：持有键盘期间到达的每个按键。
// 它等待一个完整组合（一个有实际字符的按键或功能键，且带至少一个 Ctrl、Alt 或 Super）再绑定。
// 单独按下的修饰键不是组合；完全没有修饰键的按键会从机器上每个应用手里夺走那个键：
// 两者都给出说明而不是注册。无论接受与否都要吃掉事件，以免录制时按的键又作用到按钮后面的窗口。
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

// applyShortcut 绑定组合并交代结果。重点处理被其他应用占用的组合：放回原有绑定，
// 窗口说明哪个仍在使用。用户尝试一个组合，不等于想丢掉原来的快捷键。
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
		// 绑定要被记住，供下次启动注册同一个。写失败不会拿走当前可用的绑定——
		// 已绑的就是已绑的——但下次启动会回到旧的，这点值得说出来。
		if err := s.translate.SetAccelerator(backgroundContext(), accelerator); err != nil {
			slog.Warn("the shortcut could not be remembered for the next launch",
				"accelerator", accelerator, "error", err)
		}
	}
	s.Refresh()
}

func (s *State) resetShortcut() {
	s.applyShortcut(translate.DefaultAccelerator)
}

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

func (s *State) shortcutNote() (string, presentation.NoteKind) {
	if strings.TrimSpace(s.shortcut.note) == "" {
		return "", presentation.NotePlain
	}
	return s.shortcut.note, presentation.NoteWarning
}

// selectionPlatformNote 是设置窗口关于本平台取词的说明。macOS 取词需要权限，
// 不告知用户，就会看到工具静默改为读取剪贴板而不知原因。
func selectionPlatformNote() string {
	if runtime.GOOS == "darwin" {
		return "macOS 取词需要在「系统设置 > 隐私与安全性 > 辅助功能」中允许 Boku；点下方按钮可让系统列出 Boku 并弹出授权窗口。未授权时会改为读取剪贴板。"
	}
	return ""
}
