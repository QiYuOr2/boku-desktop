//go:build !darwin

package screenrecording

// granted 和 request 恒报告已授权：这些平台读取屏幕无需权限。
func granted() bool { return true }
func request() bool { return true }

// settingsURL 为空：这些平台没有「屏幕录制」开关。
func settingsURL() string { return "" }
