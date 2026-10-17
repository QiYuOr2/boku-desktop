//go:build !darwin

package popoverappearance

func available() bool { return false }

// SetDark is a no-op where the platform has no NSPopover.
func (Controller) SetDark(uintptr) bool { return false }
