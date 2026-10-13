package domain

import "testing"

// A kind is written to the history and read back from it, so Valid is what
// keeps a row from claiming a shape the application cannot draw.
func TestClipKindValid(t *testing.T) {
	for _, kind := range []ClipKind{
		ClipKindText, ClipKindURL, ClipKindJSON,
		ClipKindCode, ClipKindFile, ClipKindImage,
	} {
		if !kind.Valid() {
			t.Errorf("%q is not valid", kind)
		}
	}
	for _, kind := range []ClipKind{"", "html", "TEXT"} {
		if kind.Valid() {
			t.Errorf("%q should not be valid", kind)
		}
	}
}
