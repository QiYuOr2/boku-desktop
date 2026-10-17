package app

import (
	"testing"

	"github.com/egoist/mygo"
)

// Only the windows a user can switch away from and come back to put the app
// in Cmd+Tab. The quick window and the translation card are transient and do
// not.
func TestActivationPolicyFollowsTheWindows(t *testing.T) {
	cases := []struct {
		settings, history bool
		want              mygo.ActivationPolicy
	}{
		{false, false, mygo.ActivationPolicyAccessory},
		{true, false, mygo.ActivationPolicyRegular},
		{false, true, mygo.ActivationPolicyRegular},
		{true, true, mygo.ActivationPolicyRegular},
	}
	for _, tt := range cases {
		if got := activationPolicyFor(tt.settings, tt.history); got != tt.want {
			t.Errorf("activationPolicyFor(%v, %v) = %q, want %q", tt.settings, tt.history, got, tt.want)
		}
	}
}
