//go:build !darwin

package ui

import (
	"image"

	"github.com/egoist/mygo"
)

// GDI 等取像素器按物理虚拟桌面像素寻址，显示器原点和选区均须按缩放因子换算。
func nativeRegionRect(display mygo.Display, local image.Rectangle) image.Rectangle {
	return desktopRect(display, local, display.ScaleFactor)
}
