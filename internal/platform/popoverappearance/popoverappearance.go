// Package popoverappearance is the macOS bridge a forced dark appearance
// needs until MyGo exposes the popover's own appearance.
//
// An NSPopover's material follows the view it is positioned against — for
// Boku, the menu bar button — rather than the application's forced
// appearance. When the user forces dark on a light desktop, the page is dark
// but the popover stays light. MyGo does not expose NSPopover.appearance, so
// this package reaches the popover's native window through the handle MyGo
// does expose and gives that window the vibrant dark appearance.
//
// It is temporary and deliberately isolated. The rest of Boku sees only the
// small interface internal/app declares; main injects this implementation.
// When MyGo grows an option for the popover's appearance, main injects a
// different implementation (or none) and this package is deleted.
package popoverappearance

// Controller gives an already-created native popover the dark appearance.
type Controller struct{}

// New returns the platform's controller. Available reports whether it can do
// anything on this platform.
func New() *Controller { return &Controller{} }

// Available reports whether the platform can set a popover's appearance.
func Available() bool { return available() }
