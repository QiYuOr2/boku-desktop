package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

// syncActivationPolicy 让 App 在 Dock 与 Cmd+Tab 中的存在感和当前打开的窗口保持一致。
//
// 托盘 App 作为 accessory：无 Dock 图标、无 Cmd+Tab 条目，这正是菜单栏工具只剩托盘图标时应有的样子。
// 但设置窗口和剪贴板历史窗口是普通窗口：用户会从它们切走，并期望 Cmd+Tab 能切回来。
// 只要其中任一打开，App 就是 regular，进入任务切换器；两者都关闭后恢复 accessory。
//
// 快捷窗口和翻译卡片刻意不在此列：它们是临时性的，不是用来切回的对象。
func (s *State) syncActivationPolicy() {
	if runtime.GOOS != "darwin" || s.app == nil {
		return
	}
	s.app.SetActivationPolicy(activationPolicyFor(s.settingsWindow != nil, s.historyWindow != nil, s.flashHistory != nil, s.diaryWindow != nil))
}

// activationPolicyFor 是规则本身，与窗口分离，使得「哪些窗口会把 App 放进 Cmd+Tab」
// 无需会话即可测试。闪念输入行和翻译卡片一样是临时性的，不算在内。
func activationPolicyFor(settingsOpen, historyOpen, flashHistoryOpen, diaryOpen bool) mygo.ActivationPolicy {
	if settingsOpen || historyOpen || flashHistoryOpen || diaryOpen {
		return mygo.ActivationPolicyRegular
	}
	return mygo.ActivationPolicyAccessory
}
