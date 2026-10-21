package presentation

import (
	"reflect"
	"strings"
	"testing"
)

// Both appearances have to choose every color. A palette field left out of
// one of the two literals is a zero color, which draws as nothing: the page
// would lose a line, an ink or a face without any compile error. This is the
// one check the compiler cannot make for a table of colors.
//
// The light tile edge is deliberately absent: the light appearance has no
// edge around a provider's tile, so ui.Transparent is its value.
func TestBothPalettesChooseEveryColor(t *testing.T) {
	for _, test := range []struct {
		name  string
		p     Palette
		clear string
	}{
		{name: "light", p: LightPalette, clear: "tileEdge"},
		{name: "dark", p: DarkPalette},
	} {
		value := reflect.ValueOf(test.p)
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index).Name
			if field == test.clear {
				continue
			}
			// ui.Color's fourth byte is its alpha. A color without one draws
			// as nothing whatever its RGB says.
			if alpha := value.Field(index).Field(3).Uint(); alpha == 0 {
				t.Errorf("%s palette: %s has no alpha", test.name, field)
			}
		}
	}
}

// Both appearances have to name the application's font: it is what keeps a
// Chinese glyph the same shape whatever language the system is in, and a
// theme that leaves it empty draws in the framework's per-script fallback.
// The two themes differ in color, not in type.
func TestBothAppearancesNameTheApplicationFont(t *testing.T) {
	for _, dark := range []bool{false, true} {
		if got := Theme(dark).Font; got != UiFontFamily {
			t.Errorf("theme(dark=%v).Font = %q, want %q", dark, got, UiFontFamily)
		}
	}
}

// The family list ends at sans-serif: on a system with none of the named
// faces a character is drawn in whatever the platform offers rather than
// being refused. And the CJK face is named before the list ends, which is the
// whole point of the task.
func TestUIFontFamilyEndsAtTheSystemFallback(t *testing.T) {
	families := strings.Split(UiFontFamily, ", ")
	for i, family := range families {
		families[i] = strings.Trim(strings.TrimSpace(family), `"`)
	}
	if last := families[len(families)-1]; last != "sans-serif" {
		t.Errorf("the family list ends at %q, want sans-serif", last)
	}
	// A CJK face has to be named before the generic fallback: the families
	// after the generic one are never reached.
	for _, want := range []string{"Microsoft YaHei", "PingFang SC", "Noto Sans CJK SC"} {
		if !slicesContains(families, want) {
			t.Errorf("the family list does not name %q: %q", want, families)
		}
	}
}

// slicesContains is a small helper so the test reads as the check it is.
func slicesContains(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
