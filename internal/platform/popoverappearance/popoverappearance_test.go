package popoverappearance

import "testing"

// A zero handle is not a popover window: an open quick window whose native
// popover has not been adopted yet must not be given an appearance, and the
// caller uses that answer to try again.
func TestAZeroWindowIsNotAPopover(t *testing.T) {
	if New().SetDark(0) {
		t.Error("a zero handle reported that it took the dark appearance")
	}
}

// Availability is read before App.Run, when main decides what to inject; it
// must not need a running application just to resolve the appearance.
func TestAvailableIsSafeWithoutARunningApp(t *testing.T) {
	_ = Available()
}
