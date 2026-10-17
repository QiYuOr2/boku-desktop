package app

import (
	"log/slog"
	"strings"
)

// RefreshSelectionAccess updates whether the platform's selection permission
// is already granted, and refreshes the open windows if that changed. It is
// called when the app becomes active, when the settings window is focused or
// opened, and after a request: those are the moments a user can have flipped
// the switch in System Settings.
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
		// The request the note was about has been answered.
		s.translateNotice = ""
	}
	s.Refresh()
}
