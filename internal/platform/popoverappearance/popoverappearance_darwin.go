//go:build darwin

package popoverappearance

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	bridgeOnce sync.Once
	bridgeOK   bool
	dark       objc.ID
	setAppear  objc.SEL
	responds   objc.SEL
)

// bridge resolves the vibrant dark appearance once. It is the appearance the
// popover's material already reports in a dark desktop; using it rather than
// a plain window appearance keeps the material's own variant.
func bridge() {
	if _, err := purego.Dlopen(
		"/System/Library/Frameworks/AppKit.framework/AppKit",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	); err != nil {
		return
	}
	class := objc.GetClass("NSAppearance")
	if class == 0 {
		return
	}
	named := objc.RegisterName("appearanceNamed:")
	dark = namedAppearance(objc.ID(class), named, "NSAppearanceNameVibrantDark")
	if dark == 0 {
		dark = namedAppearance(objc.ID(class), named, "NSAppearanceNameDarkAqua")
	}
	setAppear = objc.RegisterName("setAppearance:")
	responds = objc.RegisterName("respondsToSelector:")
	bridgeOK = dark != 0 && setAppear != 0 && responds != 0
}

// namedAppearance asks NSAppearance for one of its named appearances. The
// name is made with alloc/init and released again, so this does not depend on
// an autorelease pool being in place at the call site.
func namedAppearance(class objc.ID, named objc.SEL, name string) objc.ID {
	bytes := append([]byte(name), 0)
	nsstringClass := objc.GetClass("NSString")
	if nsstringClass == 0 {
		return 0
	}
	alloc := objc.Send[objc.ID](objc.ID(nsstringClass), objc.RegisterName("alloc"))
	str := objc.Send[objc.ID](alloc, objc.RegisterName("initWithUTF8String:"), unsafe.Pointer(&bytes[0]))
	if str == 0 {
		return 0
	}
	appearance := objc.Send[objc.ID](class, named, str)
	objc.Send[objc.ID](str, objc.RegisterName("release"))
	if appearance != 0 {
		// The cache holds it for as long as the process runs.
		objc.Send[objc.ID](appearance, objc.RegisterName("retain"))
	}
	return appearance
}

func available() bool {
	bridgeOnce.Do(bridge)
	return bridgeOK
}

// SetDark gives the native window handle the vibrant dark appearance. It
// reports whether it was set: a zero handle, an object that does not answer
// setAppearance:, and a platform without AppKit are all false.
func (Controller) SetDark(window uintptr) bool {
	if window == 0 {
		return false
	}
	bridgeOnce.Do(bridge)
	if !bridgeOK {
		return false
	}
	win := objc.ID(window)
	if !objc.Send[bool](win, responds, setAppear) {
		return false
	}
	objc.Send[objc.ID](win, setAppear, dark)
	return true
}
