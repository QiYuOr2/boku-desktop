//go:build darwin

package app

import (
	"image"

	"github.com/egoist/mygo"
)

// nativeRegionRect returns the rectangle CoreGraphics reads for a selection in
// display. Screen capture takes points, the same space NSScreen, MyGo's
// displays and the overlay window use, so the DIP rectangle is passed as it
// is: the image that comes back is in the display's pixels, twice the width
// and height on a Retina display. Scaling the rectangle here as well would
// read a region at twice the offset.
func nativeRegionRect(display mygo.Display, local image.Rectangle) image.Rectangle {
	return desktopRect(display, local, 1)
}
