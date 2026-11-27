//go:build windows

package autostart

// New 返回本桌面的登录启动。这是 main 唯一询问平台使用哪种机制的地方，返回的是值而非装配中的行为分支。
func New() Manager { return NewWindows() }
