package screenoverlay

import "testing"

// 调用方可能在 MyGo 创建原生窗口前到达这里；零句柄永远不是窗口，不能报告为已覆盖。
func TestAZeroHandleIsNotAWindow(t *testing.T) {
	if err := Cover(0); err == nil {
		t.Error("a zero handle was covered")
	}
}
