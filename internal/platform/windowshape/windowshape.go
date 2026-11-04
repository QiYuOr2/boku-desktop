// Package windowshape clips a window to the rectangle at its top.
//
// The live caption keeps one window height for as long as it is open.
// The card eases downward inside that window instead of resizing it. The
// area below the card is transparent and would otherwise take clicks meant
// for the window behind it. MyGo cannot clip a window to part of its frame, so
// this package reaches the native window through the handle MyGo exposes.
package windowshape

// ClipTop limits the window to its top dip device-independent
// pixels. dip is the card's height. It reports whether the clip is in
// place. A non-positive dip or a window that is not on screen yet reports
// false, so the caller can try again. A dip that covers the whole window
// clears the clip. A platform that cannot clip part of a window reports
// true and does nothing.
func ClipTop(window uintptr, dip int) bool { return clipTop(window, dip) }
