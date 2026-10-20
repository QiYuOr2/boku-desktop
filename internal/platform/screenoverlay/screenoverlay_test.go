package screenoverlay

import "testing"

// The caller may reach this before MyGo has a native window; a zero handle is
// never one, and must not be reported as covered.
func TestAZeroHandleIsNotAWindow(t *testing.T) {
	if err := Cover(0); err == nil {
		t.Error("a zero handle was covered")
	}
}
