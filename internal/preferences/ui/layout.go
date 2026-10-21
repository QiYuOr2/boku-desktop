package ui

import (
	"boku/internal/preferences"
	"boku/internal/presentation"
	"context"
	"github.com/egoist/mygo/ui"
)

type quickLayoutEntry struct {
	id       string
	title    string
	subtitle string
	mark     presentation.PanelMark
}

func (s *LayoutState) SettingsView(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, float32(16), func() {
			presentation.SectionHead(c, head, s.layoutMark, presentation.ColorBrand, "悬浮窗面板", "拖拽排序 · 点击眼睛控制显示")
			presentation.Notice(c, presentation.NotePlain, "以下区块会出现在托盘悬浮窗中。拖动卡片调整上下顺序；点击右侧眼睛图标可隐藏或显示对应区块。隐藏后该区块不占位。")
			ui.Column(c).FillWidth().Children(func() {
				for _, entry := range s.entries() {
					s.quickLayoutDropZone(c, entry.id)
					s.quickLayoutCard(c, entry)
				}
				s.quickLayoutDropZone(c, "")
			})
			if s.layoutStatus != "" {
				presentation.Notice(c, presentation.NotePlain, s.layoutStatus)
			}
		})
	})
}

func (s *LayoutState) quickLayoutDropZone(c *ui.Context, before string) {
	c.Key("layout-drop-" + before)
	zone := ui.Box(c).FillWidth().Height(float32(6)).Radius(presentation.RadiusField)
	if _, over := ui.DragOver[string](zone); over {
		zone = zone.Height(float32(26)).Background(presentation.ColorBrandTint).Border(1, presentation.ColorBrand).Center()
		zone.Children(func() {
			ui.Text(c, "放置到此处").FontSize(presentation.SizeQuickTiny).TextColor(presentation.ColorBrand)
		})
	}
	if moved, dropped := ui.Drop[string](zone); dropped {
		s.host.OnMain(func() {
			if before == "" {
				s.moveQuickBlockToEnd(moved)
			} else {
				s.moveQuickBlockBefore(moved, before)
			}
		})
	}
}

func (s *LayoutState) quickLayoutCard(c *ui.Context, entry quickLayoutEntry) {
	c.Key("layout-card-" + entry.id)
	visible := s.layout.Visible(entry.id)
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Padding(float32(10), float32(10)).Radius(presentation.RadiusField).Background(presentation.ColorSurface).Border(1, presentation.ColorBorder).Drag(entry.id)
	if !visible {
		row = row.Opacity(0.55)
	}
	row.Children(func() {
		ui.Icon(c, s.gripMark).Size(float32(16), float32(16)).Shrink(0).TextColor(presentation.ColorTextFaint)
		ui.Box(c).Size(float32(28), float32(28)).Shrink(0).Radius(float32(6)).Background(entry.mark.Tile).Center().Children(func() {
			ui.Icon(c, entry.mark.SVG).Size(float32(14), float32(14)).TextColor(entry.mark.Ink)
		})
		ui.Column(c).Grow(1).Gap(float32(1)).Children(func() {
			ink := presentation.ColorTextSoft
			if !visible {
				ink = presentation.ColorTextFaint
			}
			ui.Text(c, entry.title).FontSize(presentation.SizeSmall).FontWeight(500).TextColor(ink).SingleLine()
			ui.Text(c, entry.subtitle).FontSize(presentation.SizeQuickTiny).TextColor(presentation.ColorTextMuted).SingleLine()
		})
		eye, label := s.eyeMark, "隐藏区块"
		if !visible {
			eye, label = s.eyeOffMark, "显示区块"
		}
		presentation.CardIconButton(c, eye, label).OnClick(func() {
			s.setQuickBlockVisible(entry.id, !visible)
		})
	})
}

func (s *LayoutState) setQuickBlockVisible(id string, visible bool) {
	s.layoutStatus = ""
	s.layout.SetVisible(id, visible)
	if err := s.layout.Save(context.Background(), s.store); err != nil {
		s.layoutStatus = "布局没能写进设置文件：" + presentation.FriendlyError(err) + "（本次运行内已生效）"
	}
	s.host.FitQuick()
	s.host.Refresh()
}

func (s *LayoutState) moveQuickBlockBefore(id, before string) {
	if id == before {
		return
	}
	s.layoutStatus = ""
	s.layout.MoveBefore(id, before)
	if err := s.layout.Save(context.Background(), s.store); err != nil {
		s.layoutStatus = "布局没能写进设置文件：" + presentation.FriendlyError(err) + "（本次运行内已生效）"
	}
	s.host.FitQuick()
	s.host.Refresh()
}

func (s *LayoutState) moveQuickBlockToEnd(id string) {
	s.layoutStatus = ""
	s.layout.MoveToEnd(id)
	if err := s.layout.Save(context.Background(), s.store); err != nil {
		s.layoutStatus = "布局没能写进设置文件：" + presentation.FriendlyError(err) + "（本次运行内已生效）"
	}
	s.host.FitQuick()
	s.host.Refresh()
}

type LayoutState struct {
	layout                                    *preferences.QuickWindowLayout
	store                                     preferences.LayoutStore
	host                                      presentation.Host
	layoutStatus                              string
	layoutMark, eyeMark, eyeOffMark, gripMark *ui.SVG
	marks                                     map[string]presentation.PanelMark
}

func NewLayout(layout *preferences.QuickWindowLayout, store preferences.LayoutStore, host presentation.Host, layoutMark, eye, eyeOff, grip *ui.SVG, marks map[string]presentation.PanelMark) *LayoutState {
	return &LayoutState{layout: layout, store: store, host: host, layoutMark: layoutMark, eyeMark: eye, eyeOffMark: eyeOff, gripMark: grip, marks: marks}
}
func (s *LayoutState) entries() []quickLayoutEntry {
	entries := make([]quickLayoutEntry, 0, 4)
	for _, id := range s.layout.Order() {
		var title, subtitle string
		switch id {
		case "deepseek":
			title, subtitle = "DeepSeek 额度", "API 余额"
		case "codex":
			title, subtitle = "Codex 额度", "本机账号用量"
		case "anime":
			title, subtitle = "动画更新", "Bangumi 今日放送"
		case "bilibili":
			title, subtitle = "B站 UP 监听", "关注主播动态 / 开播"
		}
		entries = append(entries, quickLayoutEntry{id: id, title: title, subtitle: subtitle, mark: s.marks[id]})
	}
	return entries
}
