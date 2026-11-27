package app

import (
	"testing"

	"github.com/egoist/mygo"
)

// 只有用户能切走再切回的那类窗口才让 App 出现在 Cmd+Tab 中；
// 快捷窗口、翻译卡片和闪念输入行都是临时性的，不计入。
func TestActivationPolicyFollowsTheWindows(t *testing.T) {
	cases := []struct {
		settings, history, flashHistory, diary bool
		want                                   mygo.ActivationPolicy
	}{
		{false, false, false, false, mygo.ActivationPolicyAccessory},
		{true, false, false, false, mygo.ActivationPolicyRegular},
		{false, true, false, false, mygo.ActivationPolicyRegular},
		{false, false, true, false, mygo.ActivationPolicyRegular},
		{false, false, false, true, mygo.ActivationPolicyRegular},
		{true, true, true, true, mygo.ActivationPolicyRegular},
	}
	for _, tt := range cases {
		if got := activationPolicyFor(tt.settings, tt.history, tt.flashHistory, tt.diary); got != tt.want {
			t.Errorf("activationPolicyFor(%v, %v, %v, %v) = %q, want %q", tt.settings, tt.history, tt.flashHistory, tt.diary, got, tt.want)
		}
	}
}
