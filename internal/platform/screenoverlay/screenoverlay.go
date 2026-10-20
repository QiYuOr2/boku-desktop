// Package screenoverlay gives Boku's region-selection overlay the native
// window properties MyGo's window options cannot express.
//
// The overlay must cover a whole display, including the menu bar and the Dock.
// On macOS AppKit refuses that to a titled window: a frame whose title bar
// would be off screen is shifted down by the menu bar's height as the window
// is shown, and MyGo's floating window level stays below the menu bar and the
// Dock, which keep drawing over it. The overlay is therefore made borderless
// and put at the screen saver's level. It is also given no window animation:
// an NSPanel fades out after it closes, and the pixel read that follows a
// selection would capture the fading mask instead of the desktop. MyGo exposes
// none of these, so this package reaches the native window through the handle
// MyGo does expose. The rest of Boku sees only Cover; when MyGo grows the
// options, the macOS implementation is deleted and Cover becomes a no-op (or
// the call goes away).
package screenoverlay

// Cover prepares a window that was created hidden and not yet placed to cover
// a whole display: it removes the title bar, raises the window above the menu
// bar and the Dock, and turns its open/close animation off so that the window
// is gone as soon as it is closed. Call it before the window is given its
// bounds and shown: the title bar is what made AppKit constrain the frame.
//
// It reports the platform's failure to change the window. A platform whose
// window options already suffice returns nil without doing anything.
func Cover(window uintptr) error { return cover(window) }
