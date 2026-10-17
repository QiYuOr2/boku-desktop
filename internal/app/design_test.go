package app

import (
	"reflect"
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
		p     palette
		clear string
	}{
		{name: "light", p: lightPalette, clear: "tileEdge"},
		{name: "dark", p: darkPalette},
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
