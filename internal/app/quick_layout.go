package app

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) quickLayoutBlocks(c *ui.Context) {
	first := true
	for _, id := range s.quickWindowLayout.Order() {
		if !s.quickWindowLayout.Visible(id) || !s.quickBlockAvailable(id) {
			continue
		}
		if !first {
			presentation.QuickDivider(c)
		}
		first = false
		switch id {
		case "deepseek", "codex", "grok":
			panelID, _ := s.quickPanelID(id)
			s.panelUI.Block(c, panelID, s.dark)
		case "anime":
			s.animeUpdates(c)
		case "bilibili":
			s.bilibiliUpdates(c)
		case "playtime":
			s.playtimePanel.Draw(c, s.playtime)
		}
	}
}

func (s *State) quickBlockAvailable(id string) bool {
	switch id {
	case "deepseek", "codex", "grok":
		_, found := s.quickPanelID(id)
		return found
	case "anime":
		if s.animeService == nil || !s.animeService.Enabled() || s.animeService.Username() == "" {
			return false
		}
		items, _, loading, _ := s.animeService.View()
		return len(items) > 0 || loading
	case "bilibili":
		return s.bilibiliService != nil && s.bilibiliService.Enabled() && len(s.bilibiliService.UIDs()) > 0
	case "playtime":
		return s.playtime != nil && s.playtime.Available() && s.playtimePanel != nil
	default:
		return false
	}
}

// quickPanelID 把布局里稳定的 provider 身份解析为面板稳定的实体 ID。
// 二者刻意不同：一个 provider 可以支撑某个面板，但不一定成为面板的持久标识。
func (s *State) quickPanelID(providerID string) (string, bool) {
	if s.panelService == nil {
		return "", false
	}
	for _, item := range s.panelService.List() {
		if item.ProviderID == providerID {
			return item.ID, true
		}
	}
	return "", false
}
