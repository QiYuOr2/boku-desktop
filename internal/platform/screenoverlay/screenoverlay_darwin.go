//go:build darwin

package screenoverlay

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	// AppKit's NSWindowStyleMaskTitled. AppKit keeps the title bar of such a
	// window on screen, which is what shifts a full-screen overlay down by
	// the menu bar's height.
	styleTitled = 1 << 0
	// AppKit's NSScreenSaverWindowLevel, above the windows of the menu bar
	// and the Dock, so that a display-sized window covers both.
	screenSaverLevel = 1000
	// AppKit's NSWindowAnimationBehaviorNone. An NSPanel's default behavior
	// is the utility-window animation: the panel fades out over a fraction of
	// a second after it closes, which is long enough for the pixel read that
	// follows the selection to capture Boku's own mask instead of the
	// desktop. The overlay must be gone the moment it is closed.
	animationBehaviorNone = 2
)

var (
	bridgeOnce sync.Once
	bridgeErr  error

	setStyleMask         objc.SEL
	styleMask            objc.SEL
	setLevel             objc.SEL
	setAnimationBehavior objc.SEL
	responds             objc.SEL
)

// bridge resolves AppKit and its selectors once. AppKit is already loaded by
// MyGo in the running application; Dlopen returns the same image.
func bridge() {
	if _, err := purego.Dlopen(
		"/System/Library/Frameworks/AppKit.framework/AppKit",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	); err != nil {
		bridgeErr = fmt.Errorf("screen overlay: load AppKit: %w", err)
		return
	}
	setStyleMask = objc.RegisterName("setStyleMask:")
	styleMask = objc.RegisterName("styleMask")
	setLevel = objc.RegisterName("setLevel:")
	setAnimationBehavior = objc.RegisterName("setAnimationBehavior:")
	responds = objc.RegisterName("respondsToSelector:")
	if setStyleMask == 0 || styleMask == 0 || setLevel == 0 || setAnimationBehavior == 0 || responds == 0 {
		bridgeErr = fmt.Errorf("screen overlay: resolve AppKit selectors")
	}
}

func cover(window uintptr) error {
	if window == 0 {
		return fmt.Errorf("screen overlay: no window")
	}
	bridgeOnce.Do(bridge)
	if bridgeErr != nil {
		return bridgeErr
	}
	win := objc.ID(window)
	if !objc.Send[bool](win, responds, setStyleMask) || !objc.Send[bool](win, responds, setLevel) {
		return fmt.Errorf("screen overlay: the window is not an AppKit window")
	}
	if !objc.Send[bool](win, responds, setAnimationBehavior) {
		return fmt.Errorf("screen overlay: the window cannot turn its animation off")
	}
	mask := objc.Send[int](win, styleMask)
	objc.Send[objc.ID](win, setStyleMask, int64(mask&^styleTitled))
	objc.Send[objc.ID](win, setLevel, int64(screenSaverLevel))
	objc.Send[objc.ID](win, setAnimationBehavior, int64(animationBehaviorNone))
	return nil
}
