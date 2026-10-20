package translate

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// The translation defaults are what a fresh installation has: the tool on,
// the built-in translator, both Japanese annotations, unpinned, closing soon
// after it loses the keyboard.
func TestTheDefaultsAreWhatAFreshInstallationHas(t *testing.T) {
	got := Defaults()
	if !got.Enabled {
		t.Error("translation starts off")
	}
	if got.ProviderOr() != DefaultProvider {
		t.Errorf("provider = %q, want %q", got.ProviderOr(), DefaultProvider)
	}
	if got.AcceleratorOr() != DefaultAccelerator {
		t.Errorf("shortcut = %q, want %q", got.AcceleratorOr(), DefaultAccelerator)
	}
	if got.ScreenshotAcceleratorOr() != DefaultScreenshotAccelerator {
		t.Errorf("screenshot shortcut = %q, want %q", got.ScreenshotAcceleratorOr(), DefaultScreenshotAccelerator)
	}
	if !got.Furigana || !got.LoanwordGloss {
		t.Error("the Japanese annotations do not start on")
	}
	if got.Pinned {
		t.Error("the window starts pinned")
	}
	if got.CloseDelayOr() != DefaultCloseDelay {
		t.Errorf("close delay = %s, want %s", got.CloseDelayOr(), DefaultCloseDelay)
	}
}

func TestScreenshotShortcutMayBeClearedAndPersists(t *testing.T) {
	section := NewSection()
	if err := section.Decode(map[string]json.RawMessage{"translateScreenshotAccelerator": json.RawMessage(`""`)}); err != nil {
		t.Fatal(err)
	}
	if got := section.Current().ScreenshotAcceleratorOr(); got != "" {
		t.Fatalf("shortcut = %q, want cleared", got)
	}
	encoded, err := section.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := encoded["translateScreenshotAccelerator"]; got != "" {
		t.Fatalf("stored = %#v, want empty", got)
	}
}

func TestSettingsAreBroughtIntoTheirBounds(t *testing.T) {
	cases := []struct {
		name string
		in   Settings
		want Settings
	}{
		{
			name: "empty provider falls back",
			in:   Settings{Provider: "  "},
			want: Settings{Provider: DefaultProvider},
		},
		{
			name: "delay below zero falls back",
			in:   Settings{CloseDelay: -time.Second},
			want: Settings{CloseDelay: DefaultCloseDelay},
		},
		{
			name: "delay above the bound is clamped",
			in:   Settings{CloseDelay: time.Hour},
			want: Settings{CloseDelay: MaxCloseDelay},
		},
		{
			name: "zero delay is allowed",
			in:   Settings{CloseDelay: 0},
			want: Settings{CloseDelay: 0},
		},
		{
			name: "a bare host becomes https",
			in:   Settings{Custom: map[string]CustomSettings{"custom": {BaseURL: "api.example.com/v1/"}}},
			want: Settings{Custom: map[string]CustomSettings{"custom": {BaseURL: "https://api.example.com/v1"}}},
		},
		{
			name: "another scheme is dropped, and so is the service",
			in:   Settings{Custom: map[string]CustomSettings{"custom": {BaseURL: "file:///etc/passwd"}}},
			want: Settings{Custom: map[string]CustomSettings{}},
		},
		{
			name: "a long name is cut",
			in: Settings{Custom: map[string]CustomSettings{
				"custom": {
					BaseURL:     "https://api.example.com/v1",
					DisplayName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
				},
			}},
			want: Settings{Custom: map[string]CustomSettings{
				"custom": {
					BaseURL:     "https://api.example.com/v1",
					DisplayName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN",
				},
			}},
		},
		{
			name: "several services keep their own ids",
			in: Settings{Custom: map[string]CustomSettings{
				"custom":   {BaseURL: "http://127.0.0.1:11434/v1", DisplayName: "本地"},
				"custom-2": {BaseURL: "https://api.example.com/v1", DisplayName: "远端"},
			}},
			want: Settings{Custom: map[string]CustomSettings{
				"custom":   {BaseURL: "http://127.0.0.1:11434/v1", DisplayName: "本地"},
				"custom-2": {BaseURL: "https://api.example.com/v1", DisplayName: "远端"},
			}},
		},
		{
			name: "a service with no address is dropped rather than kept nameless",
			in: Settings{Custom: map[string]CustomSettings{
				"custom":   {BaseURL: "https://api.example.com/v1"},
				"custom-2": {DisplayName: "空地址"},
			}},
			want: Settings{Custom: map[string]CustomSettings{
				"custom": {BaseURL: "https://api.example.com/v1"},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			// Every case but the first says nothing about the provider, and
			// normalization names the default one whenever nothing is named.
			wantProvider := tc.want.Provider
			if wantProvider == "" {
				wantProvider = DefaultProvider
			}
			if got.Provider != wantProvider {
				t.Errorf("provider = %q, want %q", got.Provider, wantProvider)
			}
			if got.CloseDelay != tc.want.CloseDelay {
				t.Errorf("close delay = %s, want %s", got.CloseDelay, tc.want.CloseDelay)
			}
			if !sameServices(got.Custom, tc.want.Custom) {
				t.Errorf("custom = %+v, want %+v", got.Custom, tc.want.Custom)
			}
		})
	}
}

// sameServices compares two sets of the user's services, treating a nil map and
// an empty one as the same thing: what matters is which services there are, not
// whether the map was allocated.
func sameServices(got, want map[string]CustomSettings) bool {
	if len(got) != len(want) {
		return false
	}
	for id, service := range want {
		if got[id] != service {
			return false
		}
	}
	return true
}

// Adding a service takes the first id that is free, and the ids the file
// already holds are never handed out again.
func TestNextCustomServiceID(t *testing.T) {
	cases := []struct {
		name string
		have map[string]CustomSettings
		want string
	}{
		{"nothing stored", nil, "custom"},
		{"the first is taken", map[string]CustomSettings{"custom": {}}, "custom-2"},
		{"two are taken", map[string]CustomSettings{"custom": {}, "custom-2": {}}, "custom-3"},
		{"a gap in the middle is not reused past the end",
			map[string]CustomSettings{"custom": {}, "custom-3": {}}, "custom-2"},
		{"only a later id is taken", map[string]CustomSettings{"custom-2": {}}, "custom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := Settings{Custom: tc.have}
			if got := settings.NextCustomServiceID(); got != tc.want {
				t.Errorf("NextCustomServiceID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The ids a user's own services are stored under are recognised by their
// prefix, and nothing else is.
func TestIsCustomService(t *testing.T) {
	own := []string{"custom", "custom-2", "custom-10"}
	for _, id := range own {
		if !IsCustomService(id) {
			t.Errorf("%q is not recognised as the user's own service", id)
		}
	}
	others := []string{
		"", "deepseek", "free.microsoft", "free.bilibili",
		// Not the prefix with a separator: a translator that merely starts
		// with the word is a different thing.
		"customary", "customs-2", "Custom-2",
	}
	for _, id := range others {
		if IsCustomService(id) {
			t.Errorf("%q is treated as the user's own service", id)
		}
	}
}

// Every setting a user may leave unset has an answer for the empty case: a
// file written before a key existed reads as the default rather than as a zero.
//
// The close delay is the exception, and the reason the rule is written per
// setting rather than for the whole struct: zero means "close at once", which
// is a choice a user can make, so an absent delay reads as no delay rather
// than as the default one.
func TestTheEmptySettingsAnswerWithTheirDefaults(t *testing.T) {
	var empty Settings
	if empty.ProviderOr() != DefaultProvider {
		t.Errorf("ProviderOr of nothing = %q", empty.ProviderOr())
	}
	if empty.AcceleratorOr() != DefaultAccelerator {
		t.Errorf("AcceleratorOr of nothing = %q", empty.AcceleratorOr())
	}
	if empty.CloseDelayOr() != 0 {
		t.Errorf("CloseDelayOr of nothing = %s, want no delay: zero is a choice, not a gap",
			empty.CloseDelayOr())
	}
}

// Zero is a choice the user makes when they want the card to go at once, so a
// file that says zero has to read as zero. It used to read as "the key is not
// there" — which silently put every such user back on the default at the next
// start.
func TestAZeroCloseDelayReadsAsZero(t *testing.T) {
	section := NewSection()
	values := map[string]json.RawMessage{
		keyCloseDelay: json.RawMessage(`0`),
	}
	if err := section.Decode(values); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if got := section.Current().CloseDelay; got != 0 {
		t.Errorf("CloseDelay = %s, want 0: the file said zero", got)
	}
}

// And a file that does not mention it keeps the default: the two cases are
// different, and telling them apart is the whole of the fix.
func TestAnAbsentCloseDelayKeepsTheDefault(t *testing.T) {
	section := NewSection()
	if err := section.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if got := section.Current().CloseDelayOr(); got != DefaultCloseDelay {
		t.Errorf("CloseDelayOr = %s, want the default %s", got, DefaultCloseDelay)
	}
}

// A zero goes out to the file and comes back a zero: what the settings window
// shows after a restart is what the user chose.
func TestAZeroCloseDelaySurvivesTheFile(t *testing.T) {
	section := NewSection()
	settings := section.Current()
	settings.CloseDelay = 0
	section.Set(settings)
	encoded, err := section.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if got, want := fmt.Sprint(encoded[keyCloseDelay]), "0"; got != want {
		t.Errorf("%s = %s, want %s", keyCloseDelay, got, want)
	}
	values := map[string]json.RawMessage{}
	for key, value := range encoded {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		values[key] = raw
	}
	reread := NewSection()
	if err := reread.Decode(values); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if got := reread.Current().CloseDelay; got != 0 {
		t.Errorf("after the round trip CloseDelay = %s, want 0", got)
	}
}
