//go:build !windows

package gamepad

// Connected is empty off Windows: the playtime mark only knows the HID
// enumeration implemented here.
func Connected() []Pad {
	note(nil)
	return nil
}
