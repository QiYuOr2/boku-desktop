package translate

import (
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
			in:   Settings{Custom: CustomSettings{BaseURL: "api.example.com/v1/"}},
			want: Settings{Custom: CustomSettings{BaseURL: "https://api.example.com/v1"}},
		},
		{
			name: "another scheme is dropped",
			in:   Settings{Custom: CustomSettings{BaseURL: "file:///etc/passwd"}},
			want: Settings{Custom: CustomSettings{BaseURL: ""}},
		},
		{
			name: "a long name is cut",
			in: Settings{Custom: CustomSettings{
				DisplayName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
			}},
			want: Settings{Custom: CustomSettings{
				DisplayName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN",
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
			if got.Custom != tc.want.Custom {
				t.Errorf("custom = %+v, want %+v", got.Custom, tc.want.Custom)
			}
		})
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
