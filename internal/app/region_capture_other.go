//go:build !darwin

package app

import (
	"image"

	"github.com/egoist/mygo"
)

// nativeRegionRect returns the rectangle the platform pixel reader takes for a
// selection in display. GDI and the other readers address physical
// virtual-desktop pixels, where each monitor's origin is scaled by its own
// factor.
func nativeRegionRect(display mygo.Display, local image.Rectangle) image.Rectangle {
	return desktopRect(display, local, display.ScaleFactor)
}
