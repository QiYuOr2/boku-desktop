package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/flash"
)

func TestRecordingBindsACombination(t *testing.T) {
	state, service := flashState(t)
	var bound string
	state.rebindAll = func() {
		state.Rebind(func(accelerator string, _ func()) error {
			bound = accelerator
			return nil
		}, "Ctrl+Shift+T", "Ctrl+Alt+T")
	}
	state.beginShortcutCapture()

	if !state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUnknown, Mods: ui.Ctrl}) {
		t.Fatal("modifier was not taken")
	}
	if !strings.Contains(state.shortcutNote, "修饰键") || !state.recording {
		t.Fatalf("note %q recording %v", state.shortcutNote, state.recording)
	}

	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyN})
	if !state.recording || state.shortcutNote == "" {
		t.Fatalf("bare key note %q recording %v", state.shortcutNote, state.recording)
	}
	if service.Accelerator() != "" {
		t.Fatalf("stored %q", service.Accelerator())
	}

	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyN, Mods: ui.Ctrl | ui.Alt})
	if state.recording {
		t.Fatal("recording stayed open")
	}
	if service.Accelerator() != "Ctrl+Alt+N" || bound != "Ctrl+Alt+N" || !state.shortcutOn {
		t.Fatalf("stored %q bound %q on %v err %q", service.Accelerator(), bound, state.shortcutOn, state.shortcutErr)
	}
}

func TestRecordingCancelsOnEscape(t *testing.T) {
	state, service := flashState(t)
	if err := service.SetAccelerator(context.Background(), "Ctrl+Shift+N"); err != nil {
		t.Fatal(err)
	}
	state.accelerator = service.Accelerator()
	state.beginShortcutCapture()
	if !state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}) {
		t.Fatal("escape was not taken")
	}
	if state.recording || service.Accelerator() != "Ctrl+Shift+N" {
		t.Fatalf("recording %v stored %q", state.recording, service.Accelerator())
	}
}

func TestCaptureIgnoresKeysWhileNotRecording(t *testing.T) {
	state, service := flashState(t)
	if state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyN, Mods: ui.Ctrl}) {
		t.Fatal("key taken while idle")
	}
	if service.Accelerator() != "" {
		t.Fatalf("stored %q", service.Accelerator())
	}
}

func TestRecordingRestoresPreviousWhenRegistrationFails(t *testing.T) {
	state, service := flashState(t)
	if err := service.SetAccelerator(context.Background(), "Ctrl+Shift+N"); err != nil {
		t.Fatal(err)
	}
	state.accelerator = service.Accelerator()
	state.rebindAll = func() {
		state.Rebind(func(accelerator string, _ func()) error {
			if accelerator == "Ctrl+Alt+N" {
				return errors.New("taken")
			}
			return nil
		}, "Ctrl+Shift+T", "")
	}
	state.beginShortcutCapture()
	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyN, Mods: ui.Ctrl | ui.Alt})
	if service.Accelerator() != "Ctrl+Shift+N" {
		t.Fatalf("stored %q", service.Accelerator())
	}
	if !strings.Contains(state.shortcutNote, "仍然是") || !state.shortcutOn {
		t.Fatalf("note %q on %v err %q", state.shortcutNote, state.shortcutOn, state.shortcutErr)
	}
}

func TestRecordingRejectsTranslationShortcut(t *testing.T) {
	state, service := flashState(t)
	state.rebindAll = func() {
		state.Rebind(func(string, func()) error { return nil }, "Ctrl+Shift+T", "")
	}
	state.beginShortcutCapture()
	state.captureShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyT, Mods: ui.Ctrl | ui.Shift})
	if service.Accelerator() != "" {
		t.Fatalf("stored %q", service.Accelerator())
	}
	if !strings.Contains(state.shortcutNote, "不能与翻译快捷键相同") {
		t.Fatalf("note %q", state.shortcutNote)
	}
}

func TestClearAccelerator(t *testing.T) {
	state, service := flashState(t)
	if err := service.SetAccelerator(context.Background(), "Ctrl+Shift+N"); err != nil {
		t.Fatal(err)
	}
	state.accelerator = service.Accelerator()
	state.shortcutOn = true
	var bound string
	state.rebindAll = func() {
		state.Rebind(func(accelerator string, _ func()) error {
			bound = accelerator
			return nil
		}, "", "")
	}
	state.clearAccelerator()
	if service.Accelerator() != "" || state.shortcutOn || bound != "" {
		t.Fatalf("stored %q on %v bound %q", service.Accelerator(), state.shortcutOn, bound)
	}
}

func flashState(t *testing.T) (*State, *flash.Service) {
	t.Helper()
	section := flash.NewSection()
	service, err := flash.Open(filepath.Join(t.TempDir(), "flashes.json"), section, memSettings{}, domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Service: service}), service
}

type memSettings struct{}

func (memSettings) Update(_ context.Context, _ string, change func() error) error {
	return change()
}
