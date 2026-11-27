package popoverappearance

import "testing"

// 零句柄不是弹窗窗口：原生弹窗尚未接管时，打开的快捷窗口不得被设置外观，
// 调用方依据返回值重试。
func TestAZeroWindowIsNotAPopover(t *testing.T) {
	if New().SetDark(0) {
		t.Error("a zero handle reported that it took the dark appearance")
	}
}

// 可用性在 App.Run 之前、main 决定注入什么时读取；解析外观不得要求应用已在运行。
func TestAvailableIsSafeWithoutARunningApp(t *testing.T) {
	_ = Available()
}
