// Package screenrecording 为区域截图向 macOS 申请「屏幕录制」权限。
//
// 自 macOS 10.15 起，用户未在「系统设置 > 隐私与安全性 > 屏幕录制」中允许应用前，
// 窗口服务器对 CGWindowListCreateImage 返回空白。macOS 只有在应用主动申请后才会
// 把应用加入该列表，因此 Boku 在启动时申请：首次启动应用就在列表中，用户有开关可开，
// 而不是一张失败且无从点击的截图。Windows 和 Linux 没有对应权限，一律报告为已授权。
package screenrecording

// Granted 报告权限是否已具备。它从不申请也从不弹窗，可安全用于任意线程，
// 包括像素读取前的一刻：未授权时 CGWindowListCreateImage 返回黑色而非错误，
// 因此没有该检查就不能开始读取。
func Granted() bool { return granted() }

// SettingsURL 是用户开启权限的位置；无此开关的平台返回 ""。
func SettingsURL() string { return settingsURL() }

// Request 为进程登记「屏幕录制」权限，并在首次调用时向用户申请。它返回当前是否已授权：
// 首次调用通常返回 false，因为用户还需允许应用。系统会记住答案，Boku 不会每次启动都重复
// 询问；被拒绝后则由截图自身的错误体现。需在主线程、首次截图前调用。
func Request() bool { return request() }
