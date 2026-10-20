// Package screenrecording asks macOS for the Screen Recording permission the
// region screenshot needs.
//
// Since macOS 10.15 the window server returns nothing to
// CGWindowListCreateImage until the user has allowed the app under System
// Settings > Privacy & Security > Screen Recording. macOS adds an app to that
// list only when the app asks for the permission, so Boku asks as it starts:
// the app is in the list from the first launch, and the user has a switch to
// turn on instead of a screenshot that fails with nothing to click. Windows
// and Linux have no equivalent permission, and report it as granted.
package screenrecording

// Granted reports whether the permission is already there. It never asks and
// never prompts, so it is safe on any thread, including immediately before a
// pixel read: an unauthorized CGWindowListCreateImage returns black, not an
// error, so the read must not start without this.
func Granted() bool { return granted() }

// SettingsURL is where the user turns the permission on, or "" on a platform
// that has no such switch.
func SettingsURL() string { return settingsURL() }

// Request registers the process for the Screen Recording permission and, the
// first time, asks the user for it. It reports whether the permission is
// granted now: a first call normally reports false, because the user still has
// to allow the app. The system remembers the answer, so Boku does not ask
// again on every launch; a denial then shows up as the screenshot's own
// error. Call it on the main thread, before the first capture.
func Request() bool { return request() }
