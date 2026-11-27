//go:build darwin

package sysclip

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// 粘贴板的 changeCount 是整个平台通用的写计数器：任何应用每次写入都会递增，
// 在复制前后比较它，读取方才能区分「没有新复制」与「同一个值又被复制了一次」。
//
// MyGo 没有暴露它，这里通过 MyGo 自身使用的纯 Go Objective-C 桥读取；
// 本构建不含 cgo。
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
