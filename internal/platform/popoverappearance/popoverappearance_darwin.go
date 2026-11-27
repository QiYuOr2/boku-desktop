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

// bridge 只解析一次 vibrant dark 外观。这是弹窗材质在深色桌面下本就报告的外观，
// 用它而非普通窗口外观，可保留材质自身的变体。
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

// namedAppearance 向 NSAppearance 请求指定命名的外观。名称用 alloc/init 创建后再
// release，因此调用点不依赖 autorelease pool。
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
		// 缓存会持有它直到进程结束。
		objc.Send[objc.ID](appearance, objc.RegisterName("retain"))
	}
	return appearance
}

func available() bool {
	bridgeOnce.Do(bridge)
	return bridgeOK
}

// SetDark 给原生窗口句柄设置 vibrant dark 外观。返回值表示是否设置成功：
// 句柄为零、对象不响应 setAppearance:、平台无 AppKit 时均返回 false。
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
