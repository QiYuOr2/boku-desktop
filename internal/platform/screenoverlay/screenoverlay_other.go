//go:build !darwin

package screenoverlay

import "fmt"

// cover 只检查句柄：本平台的窗口选项已足以让遮罩覆盖整个屏幕。
func cover(window uintptr) error {
	if window == 0 {
		return fmt.Errorf("screen overlay: no window")
	}
	return nil
}
