package app

import "github.com/egoist/mygo/ui"

type quickLayoutEntry struct {
	id       string
	title    string
	subtitle string
	mark     panelMark
}

func (s *State) quickLayoutEntries() []quickLayoutEntry {
	entries := make([]quickLayoutEntry, 0, len(defaultQuickBlockOrder))
	for _, id := range s.quickWindowLayout.Order() {
		switch id {
		case quickBlockDeepSeek:
			entries = append(entries, quickLayoutEntry{id: id, title: "DeepSeek 额度", subtitle: "API 余额", mark: s.providerMark(id)})
		case quickBlockCodex:
			entries = append(entries, quickLayoutEntry{id: id, title: "Codex 额度", subtitle: "本机账号用量", mark: s.providerMark(id)})
		case quickBlockBilibili:
			entries = append(entries, quickLayoutEntry{id: id, title: "B站 UP 监听", subtitle: "关注主播动态 / 开播", mark: panelMark{svg: s.bilibiliMark, tile: ui.Hex(bilibiliTint), ink: ui.Hex(bilibiliPrimary)}})
		case quickBlockAnime:
			entries = append(entries, quickLayoutEntry{id: id, title: "动画更新", subtitle: "Bangumi 今日放送", mark: panelMark{svg: s.tvMark, tile: ui.Hex(animeTint), ink: ui.Hex(animePrimary)}})
		}
	}
	return entries
}

func (s *State) quickLayoutSection(c *ui.Context, head *ui.Handle) {
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, head, s.layoutMark, colorBrand, "悬浮窗面板", "拖拽排序 · 点击眼睛控制显示")
			notice(c, notePlain, "以下区块会出现在托盘悬浮窗中。拖动卡片调整上下顺序；点击右侧眼睛图标可隐藏或显示对应区块。隐藏后该区块不占位。")
			ui.Column(c).FillWidth().Children(func() {
				for _, entry := range s.quickLayoutEntries() {
					s.quickLayoutDropZone(c, entry.id)
					s.quickLayoutCard(c, entry)
				}
				s.quickLayoutDropZone(c, "")
			})
			if s.layoutStatus != "" {
				notice(c, notePlain, s.layoutStatus)
			}
		})
	})
}

func (s *State) quickLayoutDropZone(c *ui.Context, before string) {
	zone := ui.Box(c).FillWidth().Height(float32(6)).Radius(radiusField)
	if _, over := ui.DragOver[string](zone); over {
		zone = zone.Height(float32(26)).Background(colorBrandTint).Border(1, colorBrand).Center()
		zone.Children(func() {
			ui.Text(c, "放置到此处").FontSize(sizeQuickTiny).TextColor(colorBrand)
		})
	}
	if moved, dropped := ui.Drop[string](zone); dropped {
		if before == "" {
			s.moveQuickBlockToEnd(moved)
			return
		}
		s.moveQuickBlockBefore(moved, before)
	}
}

func (s *State) quickLayoutCard(c *ui.Context, entry quickLayoutEntry) {
	visible := s.quickWindowLayout.Visible(entry.id)
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Padding(float32(10), float32(10)).Radius(radiusField).Background(colorSurface).Border(1, colorBorder).Drag(entry.id)
	if !visible {
		row = row.Opacity(0.55)
	}
	row.Children(func() {
		ui.Icon(c, s.gripMark).Size(float32(16), float32(16)).Shrink(0).TextColor(colorTextFaint)
		ui.Box(c).Size(float32(28), float32(28)).Shrink(0).Radius(float32(6)).Background(entry.mark.tile).Center().Children(func() {
			ui.Icon(c, entry.mark.svg).Size(float32(14), float32(14)).TextColor(entry.mark.ink)
		})
		ui.Column(c).Grow(1).Gap(float32(1)).Children(func() {
			ink := colorTextSoft
			if !visible {
				ink = colorTextFaint
			}
			ui.Text(c, entry.title).FontSize(sizeSmall).FontWeight(500).TextColor(ink).SingleLine()
			ui.Text(c, entry.subtitle).FontSize(sizeQuickTiny).TextColor(colorTextMuted).SingleLine()
		})
		eye, label := s.eyeMark, "隐藏区块"
		if !visible {
			eye, label = s.eyeOffMark, "显示区块"
		}
		if button := cardIconButton(c, eye, label); button.Clicked() {
			s.setQuickBlockVisible(entry.id, !visible)
		}
	})
}

func (s *State) setQuickBlockVisible(id string, visible bool) {
	s.layoutStatus = ""
	s.quickWindowLayout.SetVisible(id, visible)
	if err := s.saveQuickWindowLayout(); err != nil {
		s.layoutStatus = "布局没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	}
	s.fitQuickWindow()
	s.Refresh()
}

func (s *State) moveQuickBlockBefore(id, before string) {
	if id == before {
		return
	}
	s.layoutStatus = ""
	s.quickWindowLayout.MoveBefore(id, before)
	if err := s.saveQuickWindowLayout(); err != nil {
		s.layoutStatus = "布局没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	}
	s.fitQuickWindow()
	s.Refresh()
}

func (s *State) moveQuickBlockToEnd(id string) {
	s.layoutStatus = ""
	s.quickWindowLayout.MoveToEnd(id)
	if err := s.saveQuickWindowLayout(); err != nil {
		s.layoutStatus = "布局没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	}
	s.fitQuickWindow()
	s.Refresh()
}

func (s *State) saveQuickWindowLayout() error {
	if s.quickLayoutStore == nil {
		return nil
	}
	layout := s.quickWindowLayout
	order := layout.Order()
	hidden := make(map[string]bool, len(order))
	for _, id := range order {
		hidden[id] = !layout.Visible(id)
	}
	return s.quickLayoutStore.Update(backgroundContext(), layout.Name(), func() error {
		// Update re-reads the shared file before this closure. Put this
		// interaction's already-decided state back after that read, so another
		// feature's keys are preserved without losing this click or drop.
		layout.order = append([]string(nil), order...)
		layout.hidden = make(map[string]bool, len(hidden))
		for id, value := range hidden {
			layout.hidden[id] = value
		}
		return nil
	})
}

func (s *State) quickLayoutBlocks(c *ui.Context) {
	first := true
	for _, id := range s.quickWindowLayout.Order() {
		if !s.quickWindowLayout.Visible(id) || !s.quickBlockAvailable(id) {
			continue
		}
		if !first {
			quickDivider(c)
		}
		first = false
		switch id {
		case quickBlockDeepSeek, quickBlockCodex:
			panelID, _ := s.quickPanelID(id)
			s.panelBlock(c, s.panelService.View(panelID))
		case quickBlockAnime:
			s.animeUpdates(c)
		case quickBlockBilibili:
			s.bilibiliUpdates(c)
		}
	}
}

func (s *State) quickBlockAvailable(id string) bool {
	switch id {
	case quickBlockDeepSeek, quickBlockCodex:
		_, found := s.quickPanelID(id)
		return found
	case quickBlockAnime:
		if s.animeService == nil || !s.animeService.Enabled() || s.animeService.Username() == "" {
			return false
		}
		items, _, loading, _ := s.animeService.View()
		return len(items) > 0 || loading
	case quickBlockBilibili:
		return s.bilibiliService != nil && s.bilibiliService.Enabled() && len(s.bilibiliService.UIDs()) > 0
	default:
		return false
	}
}

// quickPanelID resolves the layout's stable provider identity to the panel's
// stable entity ID. They deliberately differ: a provider can back a panel
// without becoming its persistent panel identifier.
func (s *State) quickPanelID(providerID string) (string, bool) {
	for _, item := range s.panelService.List() {
		if item.ProviderID == providerID {
			return item.ID, true
		}
	}
	return "", false
}
