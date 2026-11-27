//go:build !darwin

package ui

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
)

// GDI 按物理虚拟桌面像素寻址，显示器原点和选区均须按其缩放因子换算。
func TestNativeRegionRectScalesPhysicalPixels(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: 1707, Y: -337, Width: 853, Height: 1369}, ScaleFactor: 1.5}
	got := nativeRegionRect(display, image.Rect(20, 30, 120, 230))
	want := image.Rect(2591, -461, 2741, -161)
	if got != want {
		t.Fatalf("nativeRegionRect() = %v, want %v", got, want)
	}
}
