package domain

import (
	"testing"
)

// 类型会写入历史并再读出，因此 Valid 防止某行声称应用无法绘制的形状。
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

// 是否为新内容靠与上一次读取比较判断。两张同尺寸截图是两张不同的图，
// 只比尺寸会把它们当成同一张，导致第二张不被记录。
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

	// 位图未能读取时没有哈希，只能退回按尺寸比较，因为没有更好的依据。
	unread := ClipContent{Kind: ClipKindImage, HasImage: true, ImageBytes: 4096}
	if screenshot.Unchanged(unread) {
		t.Error("a picture with a hash is the same as one without")
	}
	if !unread.Unchanged(ClipContent{Kind: ClipKindImage, HasImage: true, ImageBytes: 4096}) {
		t.Error("two unread pictures of the same size are not the same reading")
	}
}
