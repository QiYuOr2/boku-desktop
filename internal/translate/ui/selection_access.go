package ui

import (
	"log/slog"
	"strings"
)

// RefreshSelectionAccess 更新平台取词权限是否已授予，变化时刷新已打开的窗口。
// 调用时机为应用激活、设置窗口获得焦点或打开、以及一次授权请求之后：
// 这些时刻用户才可能在系统设置里翻了开关。
func (s *State) RefreshSelectionAccess() {
	granted := true
	if s.translate != nil {
		granted = s.translate.SelectionAccessGranted()
	}
	if granted == s.selectionGranted {
		return
	}
	s.selectionGranted = granted
	slog.Info("selection access changed", "granted", granted)
	if granted && strings.HasPrefix(s.translateNotice, "已请求辅助功能权限") {
		// 提示对应的请求已经有结果了。
		s.translateNotice = ""
	}
	s.Refresh()
}
