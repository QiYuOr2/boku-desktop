//go:build darwin

package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
)

// CoreGraphics reads screen points, so a Retina display's scale must not be
// applied to the rectangle: the reader returns the pixels itself.
func TestNativeRegionRectKeepsScreenPointsOnMacOS(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: 1707, Y: -337, Width: 853, Height: 1369}, ScaleFactor: 2}
	got := nativeRegionRect(display, image.Rect(20, 30, 120, 230))
	want := image.Rect(1727, -307, 1827, -107)
	if got != want {
		t.Fatalf("nativeRegionRect() = %v, want %v", got, want)
	}
}
