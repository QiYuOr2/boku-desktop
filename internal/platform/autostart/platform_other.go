//go:build !windows && !darwin

package autostart

// New 返回本桌面的登录启动：没有。非 Windows/macOS 的桌面各有自己的机制（XDG 启动项、会话管理器列表），
// 应用不发布到这些平台。设置页因此说明开关不可用，而不是绘制一个什么也做不了的开关。
func New() Manager { return Unsupported{} }
