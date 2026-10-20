//go:build !darwin

package screenoverlay

import "fmt"

// cover does nothing beyond checking the handle: the window options already
// let the overlay cover a whole display on this platform.
func cover(window uintptr) error {
	if window == 0 {
		return fmt.Errorf("screen overlay: no window")
	}
	return nil
}
