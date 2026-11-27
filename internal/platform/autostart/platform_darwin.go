//go:build darwin

package autostart

// New 返回本桌面的登录启动。这是 main 唯一询问平台使用哪种机制的地方，返回的是值而非装配中的行为分支。
//
// 它是应用自己的登录项而非 launch agent：只有登录项会出现在"系统设置 > 通用 > 登录项与扩展"，
// 用户正是在那里查找开机启动项。
func New() Manager { return NewSMAppService() }
