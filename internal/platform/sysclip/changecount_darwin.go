//go:build darwin

package sysclip

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The pasteboard's changeCount is the write counter the whole platform uses:
// it is raised by every write, by every application, and comparing it before
// and after a copy is how a reader tells "nothing was copied" from "the same
// value was copied again".
//
// MyGo does not expose it, so it is read here, through the same pure-Go
// Objective-C bridge MyGo itself uses. There is no cgo in this build.
var (
	pasteboardOnce sync.Once
	pasteboard     objc.ID
	changeCountSel objc.SEL
)

func clipboardChangeCount() uint32 {
	pasteboardOnce.Do(func() {
		if _, err := purego.Dlopen(
			"/System/Library/Frameworks/AppKit.framework/AppKit",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		); err != nil {
			return
		}
		class := objc.GetClass("NSPasteboard")
		if class == 0 {
			return
		}
		pasteboard = objc.Send[objc.ID](objc.ID(class), objc.RegisterName("generalPasteboard"))
		changeCountSel = objc.RegisterName("changeCount")
	})
	if pasteboard == 0 || changeCountSel == 0 {
		return 0
	}
	return uint32(objc.Send[int](pasteboard, changeCountSel))
}
