package ui

import (
	"boku/internal/presentation"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/translate"
)

// 全局快捷键是工具的入口，其规则值得在没有运行应用也没有图形会话的环境里测：
// 回调立刻返回、失败要显示而非吞掉、重新绑定前先释放旧绑定、平台拒绝新绑定时恢复旧绑定。

func TestRegisterShortcutsBindsTheDefault(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()

	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Errorf("bound %q, want the default %q", got, translate.DefaultAccelerator)
	}
	registrations, _ := shortcuts.counts()
	if registrations != 1 {
		t.Errorf("registered %d times, want once", registrations)
	}
	if !state.shortcut.registered {
		t.Error("the window does not know the shortcut is bound")
	}
	if text, kind := state.shortcutLine(); text != "" || kind != presentation.NotePlain {
		t.Errorf("the bound shortcut still draws %q (kind %d)", text, kind)
	}
}

// 按键回调在主线程执行，整个应用都在等它，所以必须立刻返回：翻译本身是网络请求，
// 绝不能在绘制线程上等它完成。断言用超时，这样测的是规则本身，而非碰巧存在的代码。
func TestShortcutCallbackReturnsAtOnce(t *testing.T) {
	state, translate, card := translateState(t)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	translate.reply.gate = gate
	card.updates = make(chan func(), 2)
	state.RegisterShortcuts()
	translate.device.set(domain.ClipContent{
		Kind:    domain.ClipKindText,
		Text:    "A compiler converts source code.",
		Display: "A compiler converts source code.",
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		state.onTranslateShortcut()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the shortcut callback did not return: something in it waits")
	}
	if state.translation.status != translateLoading {
		t.Errorf("the window is in state %d, want loading while the request is blocked", state.translation.status)
	}
	if !card.visible() {
		t.Error("the card the callback asked for was not shown")
	}
	release()
	for range 2 {
		select {
		case update := <-card.updates:
			update()
		case <-time.After(2 * time.Second):
			t.Fatal("the translation did not hand its result back to the window")
		}
	}
	if state.translation.status != translateReady || state.translation.result.TranslatedText != "译文" {
		t.Fatalf("translation = %+v, want the completed result", state.translation)
	}
}

// 平台拒绝的注册要可见，且不阻断应用：工具设计 §6。
func TestRegisterShortcutsReportsAFailure(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	shortcuts.failWith(errors.New("already registered by another application"))
	state.RegisterShortcuts()

	if state.shortcut.registered {
		t.Error("the shortcut is reported as bound although registration failed")
	}
	if state.shortcut.err == nil {
		t.Fatal("the failure was swallowed")
	}
	text, kind := state.shortcutLine()
	if !strings.Contains(text, "无法注册") {
		t.Errorf("the line is %q, want it to say the registration failed", text)
	}
	if kind != presentation.NoteWarning {
		t.Errorf("the line is drawn as %d, want a warning", kind)
	}
}

// 重新绑定会在占用新组合前释放已有绑定：平台只有一个释放全部的调用，遗留的绑定会
// 属于一个不再响应的进程。
func TestRebindingReleasesBeforeItRegisters(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	state.applyShortcut("Ctrl+Alt+J")

	if got := shortcuts.bound(); got != "Ctrl+Alt+J" {
		t.Errorf("bound %q, want the new combination", got)
	}
	registrations, releases := shortcuts.counts()
	if registrations != 2 {
		t.Errorf("registered %d times, want the new combination after the first", registrations)
	}
	if releases == 0 {
		t.Error("the old binding was not released")
	}
	if state.shortcut.note != "" {
		t.Errorf("an accepted change still has a note: %q", state.shortcut.note)
	}
	if note, kind := state.shortcutNote(); note != "" || kind != presentation.NotePlain {
		t.Errorf("an accepted change still draws %q (kind %d)", note, kind)
	}
	// 绑定会被记住，供下次启动注册。
	settings, err := state.translateSettings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.AcceleratorOr() != "Ctrl+Alt+J" {
		t.Errorf("the stored shortcut is %q, want the new one", settings.AcceleratorOr())
	}
}

// 别的应用占用的组合会保留原有绑定：用户尝试一下，不等于想丢掉原来的快捷键。
func TestRebindingPutsTheOldBindingBackOnFailure(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	shortcuts.refuseAfter(0)
	state.applyShortcut("Ctrl+Alt+J")

	// 新组合被拒，原有的绑定又被放回也被拒：什么都没绑定，窗口如实说明，
	// 而不是谎称有一个没人持有的绑定。
	if shortcuts.refused() == 0 {
		t.Fatal("the platform never refused anything, so the rollback was not exercised")
	}
	if got := shortcuts.bound(); got != "" {
		t.Errorf("bound %q although every registration was refused", got)
	}
	if !strings.Contains(state.shortcut.note, "用不了") {
		t.Errorf("the note is %q, want it to say the combination cannot be used", state.shortcut.note)
	}
	if note, kind := state.shortcutNote(); note == "" || kind != presentation.NoteWarning {
		t.Errorf("a refused combination draws %q (kind %d), want a warning", note, kind)
	}
	if state.shortcut.accelerator != translate.DefaultAccelerator {
		t.Errorf("the window shows %q, want the binding that was there", state.shortcut.accelerator)
	}
}

func TestResetShortcutPutsTheDefaultBack(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	state.applyShortcut("Ctrl+Alt+J")
	state.resetShortcut()

	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Errorf("bound %q, want the default back", got)
	}
	settings, err := state.translateSettings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.AcceleratorOr() != translate.DefaultAccelerator {
		t.Errorf("the stored shortcut is %q, want the default", settings.AcceleratorOr())
	}
}

// 应用退出时释放其绑定。
func TestUnregisterShortcutsReleasesEverything(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	state.UnregisterShortcuts()

	if got := shortcuts.bound(); got != "" {
		t.Errorf("bound %q after the application said it was leaving", got)
	}
	_, releases := shortcuts.counts()
	if releases == 0 {
		t.Error("nothing was released on the way out")
	}
}

// 录制会读取一个完整组合并绑定。单独的修饰键、以及无修饰键的按键，都会给出说明而不是注册：
// 前者不是组合，后者会从机器上每个应用手里夺走那个键。
func TestRecordingBindsACombination(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	state.beginShortcutCapture()

	if !state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUnknown, Mods: ui.Ctrl}) {
		t.Error("the modifier press was not taken, so it would act on the window behind")
	}
	if !strings.Contains(state.shortcut.note, "修饰键") {
		t.Errorf("the note is %q, want it to explain the modifier", state.shortcut.note)
	}
	if !state.shortcut.recording {
		t.Error("refusing a modifier stopped the recording")
	}

	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyT})
	modifier := "Ctrl"
	if runtime.GOOS == "darwin" {
		modifier = "Command"
	}
	if !strings.Contains(state.shortcut.note, modifier) {
		t.Errorf("the note is %q, want it to ask for a modifier", state.shortcut.note)
	}
	if !state.shortcut.recording {
		t.Error("refusing a bare key stopped the recording")
	}

	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyT, Mods: ui.Ctrl | ui.Alt})
	if state.shortcut.recording {
		t.Error("the recording did not stop when it had a combination")
	}
	if got := shortcuts.bound(); got != "Ctrl+Alt+T" {
		t.Errorf("bound %q, want Ctrl+Alt+T", got)
	}
}

func TestRecordingCancelsOnEscape(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	state.beginShortcutCapture()

	if !state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}) {
		t.Error("Escape was not taken by the recording")
	}
	if state.shortcut.recording {
		t.Error("Escape did not stop the recording")
	}
	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Errorf("bound %q, want the binding untouched", got)
	}
}

// 不在录制时什么都不读：设置窗口别处按下的键不是绑定。
func TestCaptureIgnoresKeysWhileNotRecording(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()

	if state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyT, Mods: ui.Ctrl}) {
		t.Error("a key was taken while the window was not recording")
	}
	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Errorf("bound %q, want the binding untouched", got)
	}
}

// 启动即关闭的工具不占用任何快捷键：全局快捷键是本进程对机器上其他所有应用持有的键，
// 为已关闭的工具绑定它会让用户白白失去该组合——所以「关」意味着键根本没被占用。
func TestNoShortcutIsBoundWhileTheToolStartsOff(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	if err := state.translate.SetEnabled(t.Context(), false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	state.RegisterShortcuts()

	if got := shortcuts.bound(); got != "" {
		t.Errorf("bound %q for a tool that is off, want nothing", got)
	}
	if state.shortcut.registered {
		t.Error("the window thinks a shortcut is bound")
	}
}

// 关闭工具释放按键，再打开则重新占用。这让开关名副其实：
// 用户只有两种状态——「关，且键空闲」和「开，且键能唤起」。
func TestTheSwitchTakesAndReleasesTheShortcut(t *testing.T) {
	state, _, shortcuts := testStateWith(t)
	state.RegisterShortcuts()
	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Fatalf("the shortcut did not start bound: %q", got)
	}

	state.setTranslationEnabled(false)
	if got := shortcuts.bound(); got != "" {
		t.Errorf("the key is still held after the tool was turned off: %q", got)
	}
	if state.shortcut.registered {
		t.Error("the window still thinks the shortcut is bound")
	}
	// 此时按下也不会有动作，因为没有绑定可触发。
	if fn := shortcuts.callback(); fn != nil {
		t.Error("a callback survived the release")
	}

	state.setTranslationEnabled(true)
	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Errorf("the key was not taken again: bound %q", got)
	}
	if !state.shortcut.registered {
		t.Error("the window does not know the shortcut is bound again")
	}
	if fn := shortcuts.callback(); fn == nil {
		t.Error("the key is bound to nothing")
	}
}

// 关闭的工具只画开关：为未运行的工具铺满设置会诱使用户去配置一件不会发生的事。
func TestASwitchedOffToolDrawsOnlyItsSwitch(t *testing.T) {
	state, fixture, _ := translateState(t)
	if err := fixture.service.SetEnabled(t.Context(), false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	settings := state.translateSettingsOr()
	if settings.Enabled {
		t.Fatal("the tool is on, so this test is about the wrong state")
	}
	// 该 section 的规则：除开关外的一切都藏在其后。外观由运行时验收，这里只测这个判断。
	if showsTranslationControls(settings) {
		t.Error("a switched-off tool still draws the controls that configure it")
	}
	if err := fixture.service.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	if !showsTranslationControls(state.translateSettingsOr()) {
		t.Error("a switched-on tool draws none of its controls")
	}
}
