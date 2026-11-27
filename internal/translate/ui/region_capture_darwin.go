//go:build darwin

package ui

import (
	"image"

	"github.com/egoist/mygo"
)

// 返回 CoreGraphics 读取选区用的矩形。屏幕截取以点为单位，与 NSScreen、MyGo 显示器和覆盖窗口同一空间，
// 因此 DIP 矩形原样传入：返回的图像是显示器像素，Retina 下宽高各翻倍。若这里再缩放一次，会读到偏移翻倍的区域。
func nativeRegionRect(display mygo.Display, local image.Rectangle) image.Rectangle {
	return desktopRect(display, local, 1)
}
