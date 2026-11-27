//go:build darwin

package screenoverlay

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	// AppKit 的 NSWindowStyleMaskTitled。AppKit 会保证标题栏留在屏幕内，
	// 因此会把整屏遮罩向下挤开菜单栏的高度。
	styleTitled = 1 << 0
	// AppKit 的 NSScreenSaverWindowLevel，高于菜单栏和 Dock 的窗口，
	// 使整屏窗口能覆盖两者。
	screenSaverLevel = 1000
	// AppKit 的 NSWindowAnimationBehaviorNone。NSPanel 默认使用工具窗口动画：
	// 关闭后会在不到一秒内淡出，期间紧接着的选区像素读取会拍到 Boku 自己的
	// 遮罩而非桌面。遮罩必须在关闭瞬间就消失。
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

// bridge 只解析一次 AppKit 及其 selector。运行中的应用里 MyGo 已加载 AppKit，
// Dlopen 返回的是同一个映像。
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
