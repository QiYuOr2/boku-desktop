package domain

import (
	"testing"
)

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

// A reading is compared with the one before it to decide whether the user
// copied something new. Two screenshots of the same size are two pictures: the
// size alone said they were one, so the second was never recorded.
func TestABitmapIsComparedByItsBytesNotItsSize(t *testing.T) {
	screenshot := ClipContent{
		Kind: ClipKindImage, HasImage: true, ImageBytes: 4096, ImageRef: "sha256:one",
	}
	another := ClipContent{
		Kind: ClipKindImage, HasImage: true, ImageBytes: 4096, ImageRef: "sha256:two",
	}
	if screenshot.Unchanged(another) {
		t.Error("two pictures of the same size are the same reading")
	}
	same := ClipContent{
		Kind: ClipKindImage, HasImage: true, ImageBytes: 4096, ImageRef: "sha256:one",
	}
	if !screenshot.Unchanged(same) {
		t.Error("the same picture read twice is not the same reading")
	}

	// A reading whose bitmap could not be read carries no hash, and falls back
	// to the size: there is nothing better to compare it by.
	unread := ClipContent{Kind: ClipKindImage, HasImage: true, ImageBytes: 4096}
	if screenshot.Unchanged(unread) {
		t.Error("a picture with a hash is the same as one without")
	}
	if !unread.Unchanged(ClipContent{Kind: ClipKindImage, HasImage: true, ImageBytes: 4096}) {
		t.Error("two unread pictures of the same size are not the same reading")
	}
}
