//go:build !darwin

package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
)

// GDI reads physical virtual-desktop pixels, so each display's origin and the
// selection are scaled by that display's factor.
func TestNativeRegionRectScalesPhysicalPixels(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: 1707, Y: -337, Width: 853, Height: 1369}, ScaleFactor: 1.5}
	got := nativeRegionRect(display, image.Rect(20, 30, 120, 230))
	want := image.Rect(2591, -461, 2741, -161)
	if got != want {
		t.Fatalf("nativeRegionRect() = %v, want %v", got, want)
	}
}
