//go:build darwin

package screenrecording

import (
	"sync"

	"github.com/ebitengine/purego"
)

var (
	bridgeOnce sync.Once
	bridgeOK   bool
	preflight  func() bool
	ask        func() bool
)

// bridge 一次性解析 CoreGraphics 和两个权限函数；它们随 macOS 10.15 提供，
// 所有受支持的 macOS 都有。
func bridge() {
	lib, err := purego.Dlopen(
		"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_LAZY|purego.RTLD_LOCAL,
	)
	if err != nil {
		return
	}
	preflightAddr, err := purego.Dlsym(lib, "CGPreflightScreenCaptureAccess")
	if err != nil {
		return
	}
	requestAddr, err := purego.Dlsym(lib, "CGRequestScreenCaptureAccess")
	if err != nil {
		return
	}
	purego.RegisterFunc(&preflight, preflightAddr)
	purego.RegisterFunc(&ask, requestAddr)
	bridgeOK = true
}

func granted() bool {
	bridgeOnce.Do(bridge)
	if !bridgeOK {
		// 没有权限函数就说明没有可开启的开关。
		return true
	}
	return preflight()
}

// settingsURL 指向系统设置中的「屏幕录制」面板。
func settingsURL() string {
	return "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
}

func request() bool {
	bridgeOnce.Do(bridge)
	if !bridgeOK {
		// 没有权限函数就没有可开启的开关，采集失败会自行上报错误。
		return true
	}
	// 仅在尚无权限时询问：该请求会弹出系统提示，答案已知后无需再问。
	if preflight() {
		return true
	}
	return ask()
}
