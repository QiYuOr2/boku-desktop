//go:build darwin

package ui

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
)

// CoreGraphics 读取的是屏幕点，因此不能对矩形施加 Retina 缩放：像素由读取方自行返回。
func TestNativeRegionRectKeepsScreenPointsOnMacOS(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: 1707, Y: -337, Width: 853, Height: 1369}, ScaleFactor: 2}
	got := nativeRegionRect(display, image.Rect(20, 30, 120, 230))
	want := image.Rect(1727, -307, 1827, -107)
	if got != want {
		t.Fatalf("nativeRegionRect() = %v, want %v", got, want)
	}
}
