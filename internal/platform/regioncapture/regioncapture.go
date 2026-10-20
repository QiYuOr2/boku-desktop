// Package regioncapture reads pixels from a rectangle of the desktop.
//
// It deliberately does not present any UI and never touches the clipboard.
// The application owns the native selection overlay; this package owns only
// the operating-system pixel read and PNG encoding.
package regioncapture

type Device struct{}

func New() *Device { return &Device{} }
