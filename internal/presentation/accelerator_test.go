package presentation

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAcceleratorOf(t *testing.T) {
	cases := []struct {
		name string
		ev   ui.InputEvent
		want string
		ok   bool
	}{
		{"ctrl shift t", ui.InputEvent{Key: ui.KeyT, Mods: ui.Ctrl | ui.Shift}, "Ctrl+Shift+T", true},
		{"ctrl shift n", ui.InputEvent{Key: ui.KeyN, Mods: ui.Ctrl | ui.Shift}, "Ctrl+Shift+N", true},
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
			got, ok := AcceleratorOf(tc.ev)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("AcceleratorOf = %q, %v, want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestFriendlyAccelerator(t *testing.T) {
	cases := map[string]string{
		"Ctrl+Shift+T": "Ctrl + Shift + T",
		"Ctrl+Alt+N":   "Ctrl + Alt + N",
		"":             "",
		"  Alt+S  ":    "Alt + S",
	}
	for in, want := range cases {
		if got := FriendlyAccelerator(in); got != want {
			t.Errorf("FriendlyAccelerator(%q) = %q, want %q", in, got, want)
		}
	}
}

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
		if got := FriendlyAcceleratorFor(tt.in, tt.goos); got != tt.want {
			t.Errorf("FriendlyAcceleratorFor(%q, %s) = %q, want %q", tt.in, tt.goos, got, tt.want)
		}
	}
}

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
		{"Super+N", "linux", "Super+N"},
		{"", "darwin", ""},
		{"   ", "darwin", ""},
	}
	for _, tt := range cases {
		if got := CompactAcceleratorFor(tt.in, tt.goos); got != tt.want {
			t.Errorf("CompactAcceleratorFor(%q, %s) = %q, want %q", tt.in, tt.goos, got, tt.want)
		}
	}
	if CompactAccelerator("Super+N") == "" {
		t.Fatal("compact accelerator was empty")
	}
}
