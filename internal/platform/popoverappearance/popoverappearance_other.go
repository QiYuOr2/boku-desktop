//go:build !darwin

package popoverappearance

func available() bool { return false }

// SetDark 在无 NSPopover 的平台上为空操作。
func (Controller) SetDark(uintptr) bool { return false }
