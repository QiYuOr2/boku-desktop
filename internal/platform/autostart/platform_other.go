//go:build !windows && !darwin

package autostart

// New returns this desktop's login start. There is none: the desktops that
// are not Windows or macOS would each have their own (an XDG autostart entry,
// a session manager's list), and the application does not ship to them. The
// settings page then says the switch is not available rather than drawing one
// that could not do anything.
func New() Manager { return Unsupported{} }
