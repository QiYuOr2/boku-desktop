//go:build windows

package windowshape

import (
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	getClientRect   = user32.NewProc("GetClientRect")
	getDpiForWindow = user32.NewProc("GetDpiForWindow")
	setWindowRgn    = user32.NewProc("SetWindowRgn")
	createRectRgn   = gdi32.NewProc("CreateRectRgn")
	deleteObject    = gdi32.NewProc("DeleteObject")
)

type rect struct {
	Left, Top, Right, Bottom int32
}

// clipTop sets the window region to the top dip of its client area.
// SetWindowRgn takes ownership of the region when it succeeds. The redraw
// flag stays off: the page is already painting the card, and redrawing the
// whole window flashed.
func clipTop(window uintptr, dip int) bool {
	if window == 0 || dip <= 0 {
		return false
	}
	dpi, _, _ := getDpiForWindow.Call(window)
	if dpi == 0 {
		dpi = 96
	}
	var r rect
	getClientRect.Call(window, uintptr(unsafe.Pointer(&r)))
	width, height := int(r.Right), int(r.Bottom)
	if width <= 0 || height <= 0 {
		return false
	}
	card := (dip*int(dpi) + 48) / 96
	if card >= height {
		setWindowRgn.Call(window, 0, 0)
		return true
	}
	if card < 1 {
		card = 1
	}
	region, _, _ := createRectRgn.Call(0, 0, uintptr(width), uintptr(card))
	if region == 0 {
		return false
	}
	if ok, _, _ := setWindowRgn.Call(window, region, 0); ok == 0 {
		deleteObject.Call(region)
		return false
	}
	return true
}
