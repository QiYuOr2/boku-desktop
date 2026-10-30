package app

import (
	"testing"

	"github.com/egoist/mygo"
)

// Only the windows a user can switch away from and come back to put the app
// in Cmd+Tab. The quick window, the translation card, and the flash capture
// line are transient and do not.
func TestActivationPolicyFollowsTheWindows(t *testing.T) {
	cases := []struct {
		settings, history, flashHistory bool
		want                            mygo.ActivationPolicy
	}{
		{false, false, false, mygo.ActivationPolicyAccessory},
		{true, false, false, mygo.ActivationPolicyRegular},
		{false, true, false, mygo.ActivationPolicyRegular},
		{false, false, true, mygo.ActivationPolicyRegular},
		{true, true, true, mygo.ActivationPolicyRegular},
	}
	for _, tt := range cases {
		if got := activationPolicyFor(tt.settings, tt.history, tt.flashHistory); got != tt.want {
			t.Errorf("activationPolicyFor(%v, %v, %v) = %q, want %q", tt.settings, tt.history, tt.flashHistory, got, tt.want)
		}
	}
}
