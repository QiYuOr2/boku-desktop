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

// bridge resolves CoreGraphics and the two permission functions once. They
// arrived with macOS 10.15, which every supported macOS has.
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
		// Without the permission functions there is no switch to turn on.
		return true
	}
	return preflight()
}

// settingsURL is System Settings' Screen Recording pane.
func settingsURL() string {
	return "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
}

func request() bool {
	bridgeOnce.Do(bridge)
	if !bridgeOK {
		// Without the permission functions there is no switch to turn on,
		// and the capture reports whatever went wrong itself.
		return true
	}
	// Asked only when the permission is not there: the request presents the
	// system's prompt, and there is nothing to ask once the answer is known.
	if preflight() {
		return true
	}
	return ask()
}
