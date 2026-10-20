package app

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/translate"
)

// The global shortcut is the tool's way in, and the rules around it are worth
// testing where neither a running application nor a graphical session exists:
// a callback that returns at once, a failure that is shown rather than
// swallowed, a binding that is released before it is taken again, and a
// binding that is put back when the platform refuses the new one.

func TestRegisterShortcutsBindsTheDefault(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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
	// The recorder previews the binding itself, so a successful registration
	// needs no duplicate status line below it.
	if text, kind := state.shortcutLine(); text != "" || kind != notePlain {
		t.Errorf("the bound shortcut still draws %q (kind %d)", text, kind)
	}
}

// The key press runs on the main thread, with the whole application waiting on
// it, so it must return at once: the translation itself is a network request,
// and a request is never waited out on the thread that draws. The assertion is
// a timeout, which is what makes it a test of the rule rather than of the code
// that happens to be there.
func TestShortcutCallbackReturnsAtOnce(t *testing.T) {
	state, translate, card := translateState(t)
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
	// What the callback left behind is the window asking for a translation.
	// The answer itself arrives on the thread that owns the state, and a fake
	// translator can answer before this line runs: loading and ready are both
	// the state the callback is allowed to leave. Waiting for a particular one
	// made this a test of the fake's speed rather than of the callback.
	switch state.translation.status {
	case translateLoading, translateReady:
	default:
		t.Errorf("the window is in state %d, want loading or ready", state.translation.status)
	}
	if !card.visible() {
		t.Error("the card the callback asked for was not shown")
	}
}

// A registration the platform refuses is visible and does not stop the
// application: §6 of the tool design.
func TestRegisterShortcutsReportsAFailure(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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
	if kind != noteWarning {
		t.Errorf("the line is drawn as %d, want a warning", kind)
	}
}

// Rebinding releases what is held before it takes the new combination: the
// platform has one call that releases them all, and a binding left behind
// belongs to a process that no longer answers.
func TestRebindingReleasesBeforeItRegisters(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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
	if note, kind := state.shortcutNote(); note != "" || kind != notePlain {
		t.Errorf("an accepted change still draws %q (kind %d)", note, kind)
	}
	// What was bound is remembered, so that the next launch registers it.
	settings, err := state.translateSettings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.AcceleratorOr() != "Ctrl+Alt+J" {
		t.Errorf("the stored shortcut is %q, want the new one", settings.AcceleratorOr())
	}
}

// A combination another application holds leaves the binding that was there:
// a user who tries one has not asked to lose the shortcut they had.
func TestRebindingPutsTheOldBindingBackOnFailure(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.RegisterShortcuts()
	// The first registration is the one that is already there; everything
	// after it is refused, which is the shape of a combination another
	// application holds.
	shortcuts.refuseAfter(0)
	state.applyShortcut("Ctrl+Alt+J")

	// The new combination was refused, and the binding that was there was put
	// back and refused too: nothing is bound, and the window says so rather
	// than claiming a binding nobody has.
	if shortcuts.refused() == 0 {
		t.Fatal("the platform never refused anything, so the rollback was not exercised")
	}
	if got := shortcuts.bound(); got != "" {
		t.Errorf("bound %q although every registration was refused", got)
	}
	if !strings.Contains(state.shortcut.note, "用不了") {
		t.Errorf("the note is %q, want it to say the combination cannot be used", state.shortcut.note)
	}
	if note, kind := state.shortcutNote(); note == "" || kind != noteWarning {
		t.Errorf("a refused combination draws %q (kind %d), want a warning", note, kind)
	}
	if state.shortcut.accelerator != translate.DefaultAccelerator {
		t.Errorf("the window shows %q, want the binding that was there", state.shortcut.accelerator)
	}
}

func TestResetShortcutPutsTheDefaultBack(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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

// The application releases its binding on the way out.
func TestUnregisterShortcutsReleasesEverything(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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

// Recording reads a complete combination and binds it. A modifier on its own
// and a key with no modifier are refused with a word rather than registered:
// one is not a combination, and the other would take that key from every
// application on the machine.
func TestRecordingBindsACombination(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.RegisterShortcuts()
	state.beginShortcutCapture()

	// A modifier on its own: not a combination.
	if !state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUnknown, Mods: ui.Ctrl}) {
		t.Error("the modifier press was not taken, so it would act on the window behind")
	}
	if !strings.Contains(state.shortcut.note, "修饰键") {
		t.Errorf("the note is %q, want it to explain the modifier", state.shortcut.note)
	}
	if !state.shortcut.recording {
		t.Error("refusing a modifier stopped the recording")
	}

	// A key with no modifier: refused, and still recording.
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

	// A whole combination: bound.
	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyT, Mods: ui.Ctrl | ui.Alt})
	if state.shortcut.recording {
		t.Error("the recording did not stop when it had a combination")
	}
	if got := shortcuts.bound(); got != "Ctrl+Alt+T" {
		t.Errorf("bound %q, want Ctrl+Alt+T", got)
	}
}

// Escape cancels the recording without binding anything.
func TestRecordingCancelsOnEscape(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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

// Nothing is read while the window is not recording: a key pressed elsewhere
// in the settings window is not a binding.
func TestCaptureIgnoresKeysWhileNotRecording(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.RegisterShortcuts()

	if state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyT, Mods: ui.Ctrl}) {
		t.Error("a key was taken while the window was not recording")
	}
	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Errorf("bound %q, want the binding untouched", got)
	}
}

// The keys the recorder accepts are the ones the platform's parser reads, and
// the ones it does not accept cannot be bound rather than being bound wrongly.
func TestAcceleratorOf(t *testing.T) {
	cases := []struct {
		name string
		ev   ui.InputEvent
		want string
		ok   bool
	}{
		{"ctrl shift t", ui.InputEvent{Key: ui.KeyT, Mods: ui.Ctrl | ui.Shift}, "Ctrl+Shift+T", true},
		{"alt f5", ui.InputEvent{Key: ui.KeyF5, Mods: ui.Alt}, "Alt+F5", true},
		{"super space", ui.InputEvent{Key: ui.KeySpace, Mods: ui.Super}, "Super+Space", true},
		{"all modifiers", ui.InputEvent{Key: ui.KeyA, Mods: ui.Ctrl | ui.Shift | ui.Alt | ui.Super}, "Ctrl+Shift+Alt+Super+A", true},
		{"shift alone", ui.InputEvent{Key: ui.KeyT, Mods: ui.Shift}, "", false},
		{"no modifier", ui.InputEvent{Key: ui.KeyT}, "", false},
		{"a modifier on its own", ui.InputEvent{Key: ui.KeyUnknown, Mods: ui.Ctrl}, "", false},
		{"a key with no name", ui.InputEvent{Key: ui.Key(60000), Mods: ui.Ctrl}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := acceleratorOf(tc.ev)
			if ok != tc.ok {
				t.Fatalf("acceleratorOf = %q, %v, want ok=%v", got, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("acceleratorOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// A binding is written the way a person reads it, and the default the design
// names is one of them.
func TestFriendlyAccelerator(t *testing.T) {
	cases := map[string]string{
		"Ctrl+Shift+T": "Ctrl + Shift + T",
		"":             "",
		"  Alt+S  ":    "Alt + S",
	}
	for in, want := range cases {
		if got := friendlyAccelerator(in); got != want {
			t.Errorf("friendlyAccelerator(%q) = %q, want %q", in, got, want)
		}
	}
}

// The platform names the modifier it actually has: a Mac user's Command is
// the framework's Super, and the default is written so that each platform
// resolves it to its own key.
func TestFriendlyAcceleratorNamesThePlatformsModifier(t *testing.T) {
	cases := []struct {
		in, goos, want string
	}{
		{"CmdOrCtrl+Shift+T", "darwin", "Command + Shift + T"},
		{"CmdOrCtrl+Shift+T", "windows", "Ctrl + Shift + T"},
		{"Super+Space", "darwin", "Command + Space"},
		{"Super+Space", "linux", "Super + Space"},
	}
	for _, tt := range cases {
		if got := friendlyAcceleratorFor(tt.in, tt.goos); got != tt.want {
			t.Errorf("friendlyAcceleratorFor(%q, %s) = %q, want %q", tt.in, tt.goos, got, tt.want)
		}
	}
}

// The recorder field writes the same binding more tightly than prose does: no
// spaces around the plus, so the widest ordinary three-key combination fits
// beside the screenshot field's clear icon.
func TestCompactAccelerator(t *testing.T) {
	cases := []struct {
		in, goos, want string
	}{
		{"Ctrl+Shift+T", "windows", "Ctrl+Shift+T"},
		{"Shift + Command + T", "darwin", "Shift+Command+T"},
		{"CmdOrCtrl+Alt+T", "darwin", "Command+Alt+T"},
		{"CmdOrCtrl+Alt+T", "linux", "Ctrl+Alt+T"},
		{"Super+Space", "darwin", "Command+Space"},
		{"Super+Space", "linux", "Super+Space"},
		{"", "darwin", ""},
		{"   ", "darwin", ""},
	}
	for _, tt := range cases {
		if got := compactAcceleratorFor(tt.in, tt.goos); got != tt.want {
			t.Errorf("compactAcceleratorFor(%q, %s) = %q, want %q", tt.in, tt.goos, got, tt.want)
		}
	}
}

// waitForTranslation waits until the translation tool has reached a state,
// which is how a test waits for work the shortcut started: the answer arrives
// on the main thread, and a test window has no frame to carry it.
func waitForTranslation(t *testing.T, state *State, want translateStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if state.translation.status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the translation window stayed in state %d", state.translation.status)
}

// A tool that starts switched off takes no shortcut at launch.
//
// A global shortcut is a key this process holds against every other
// application on the machine. Binding one for a tool the user has turned off
// denies them the combination for a feature that will not run — so "off" has to
// mean the key was never taken.
func TestNoShortcutIsBoundWhileTheToolStartsOff(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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

// Turning the tool off releases the key, and turning it back on takes it again.
//
// It is what makes the switch mean what it says: the two states a user can be
// in are "off, and the key is free" and "on, and the key opens it".
func TestTheSwitchTakesAndReleasesTheShortcut(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.RegisterShortcuts()
	if got := shortcuts.bound(); got != translate.DefaultAccelerator {
		t.Fatalf("the shortcut did not start bound: %q", got)
	}

	// Off: the key goes back to the system.
	state.setTranslationEnabled(false)
	if got := shortcuts.bound(); got != "" {
		t.Errorf("the key is still held after the tool was turned off: %q", got)
	}
	if state.shortcut.registered {
		t.Error("the window still thinks the shortcut is bound")
	}
	// And pressing it does nothing, because there is nothing bound to press.
	if fn := shortcuts.callback(); fn != nil {
		t.Error("a callback survived the release")
	}

	// On again: the same key, bound to the same action.
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

// A switched-off tool draws the switch and nothing else: a page of controls for
// a tool that is not running invites the user to configure a thing that will
// not happen.
func TestASwitchedOffToolDrawsOnlyItsSwitch(t *testing.T) {
	state, fixture, _ := translateState(t)
	if err := fixture.service.SetEnabled(t.Context(), false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	settings := state.translateSettingsOr()
	if settings.Enabled {
		t.Fatal("the tool is on, so this test is about the wrong state")
	}
	// The rule the section draws by: everything but the switch is behind it.
	// What it looks like is accepted at runtime; this is the decision.
	if showsTranslationControls(settings) {
		t.Error("a switched-off tool still draws the controls that configure it")
	}
	// An enabled tool draws them.
	if err := fixture.service.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	if !showsTranslationControls(state.translateSettingsOr()) {
		t.Error("a switched-on tool draws none of its controls")
	}
}
