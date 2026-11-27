package ui

import (
	"boku/internal/clipboard"
	"boku/internal/domain"
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
	"strconv"
	"time"
)

// QuickView 绘制快捷窗口底部的一行剪贴板内容。有实际内容时才绘制。
func (s *State) QuickView(c *ui.Context, design presentation.ClipboardDesign) {
	s.design = design
	content, ok := s.clipboardLatest()

	if content.Display == "" || !ok {
		return
	}

	ui.Column(c).FillWidth().
		Padding(float32(4), float32(12), float32(10), float32(12)).
		Children(func() {
			ui.Row(c).FillWidth().
				Padding(float32(6), float32(10)).
				Radius(float32(8)).
				Border(1, s.design.PanelBorder).
				Background(s.design.PanelField).
				AlignItems(ui.Center).
				Gap(float32(8)).
				Children(func() {
					ui.Icon(c, s.design.Mark).Size(float32(14), float32(14)).Shrink(0).TextColor(s.design.PanelFaint)
					ui.Text(c, content.Display).FontSize(s.design.SizeSmall).TextColor(s.design.PanelBody).SingleLine()
					ui.Spacer(c)
					if button := s.quickHistoryButton(c); button.Clicked() {
						s.openHistory()
					}
				})
		})
}

// quickHistoryButton 是从剪贴板行进入历史的入口。
func (s *State) quickHistoryButton(c *ui.Context) ui.Element {
	return s.design.WordButton(c, "历史")
}

// clipboardLatest 返回最近一次观测到的记录，以及是否有内容。
func (s *State) clipboardLatest() (domain.ClipRecord, bool) {
	if s.clipboard == nil {
		return domain.ClipRecord{}, false
	}
	return s.clipboard.Latest()
}

// clipboardHistorySize 返回用户选择的记录范围。
func (s *State) clipboardHistorySize() int {
	if s.clipboard == nil {
		return clipboard.DefaultHistorySize
	}
	return s.clipboard.Settings().KeepsHistory()
}

// clipboardHistory 返回记录中的复制内容，新的在前，最多为用户选择的条数。
func (s *State) clipboardHistory() []domain.ClipboardEntry {
	if s.clipboard == nil {
		return nil
	}
	return s.clipboard.History(s.clipboardHistorySize())
}

// clipboardHistoryError 返回读取历史失败的原因（若有）。
func (s *State) clipboardHistoryError() error {
	if s.clipboard == nil {
		return nil
	}
	return s.clipboard.HistoryError()
}

// HistoryView 列出记录中的复制内容。它读取的是工具已持有的数据，
// 绘制时不复制任何内容。
//
// 打开时会异步读一次剪贴板：剪贴板读取可能被他方占用而阻塞，不能
// 在窗口显示后空等；读完再请求一帧刷新，避免窗口已绘制但仍是空的。
func (s *State) HistoryView(c *ui.Context, design presentation.ClipboardDesign) {
	s.design = design
	s.design.WindowFrame(c).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).
			Padding(s.design.PageEdge/2, s.design.PageEdge).
			AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, "最近 "+strconv.Itoa(s.clipboardHistorySize())+" 条").
					FontSize(s.design.SizeSection).FontWeight(600).TextColor(s.design.ColorTextBody).SingleLine()
				ui.Spacer(c)
				ui.Text(c, "点击即复制").FontSize(s.design.SizeSmall).TextColor(s.design.ColorTextMuted).SingleLine()
			})
		s.design.Hairline(c)

		if err := s.clipboardHistoryError(); err != nil {
			s.design.Content(c, true, func() {
				s.design.Inset(c, func() {
					s.design.Notice(c, true, "读不到上次的历史："+s.design.FriendlyError(err))
					s.design.Notice(c, false, "新复制的仍然会记下来。")
				})
			})
			return
		}

		entries := s.clipboardHistory()
		if len(entries) == 0 {
			s.design.Content(c, true, func() {
				s.design.Inset(c, func() {
					ui.Text(c, "还没有记录。").
						FontSize(s.design.SizeBody).
						TextColor(s.design.ColorTextMuted)
					ui.Text(c, "复制一段内容，再打开这里。").
						FontSize(s.design.SizeSmall).
						TextColor(s.design.ColorTextMuted)
				})
			})
			return
		}

		list := ui.Scroll(c).FillWidth().Grow(1)
		list.Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for index, entry := range entries {
					if index > 0 {
						s.design.RowDivider(c)
					}
					s.historyRow(c, entry)
				}
			})
		})
	})
}

// historyRow 绘制一条记录：整行都是按钮（扩大点击区域）；按下时与
// 复制后短暂保留按下态，让点击反馈落在行本身。
func (s *State) historyRow(c *ui.Context, entry domain.ClipboardEntry) {
	tile, ink, mark := s.historyKind(entry)
	label := historyLabel(entry)
	row := ui.ButtonBase(c).FillWidth().
		Padding(float32(10), float32(12)).
		AlignItems(ui.Center).
		Gap(float32(12))
	justCopied := entry.ID == s.copiedEntry
	// 复制后行会保留淡色高亮：浅色页面上 10% 足够，深色页面需更浓才看得出。
	wash := float32(0.10)
	if s.design.Dark {
		wash = 0.20
	}
	switch {
	case justCopied:
		row.Background(ink.Alpha(wash))
	case row.Pressed():
		row.Background(s.design.ColorActive)
	case row.Hovered() || row.Focused():
		row.Background(s.design.ColorRowAlt)
	}
	row.Children(func() {
		s.design.KindTile(c, mark, tile, ink)
		ui.Text(c, entry.Line()).
			FontSize(s.design.SizeBody).
			TextColor(s.inkText(entry)).
			Font(fontOf(entry)).
			SingleLine().
			Grow(1).
			MinWidth(0)
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
			if entry.Note != "" {
				// 这条记录的读取本身注意到的信息（如内容不完整），照存储原样展示。
				s.design.Notice(c, true, entry.Note)
			}
			if label != "" {
				s.design.KindBadge(c, tile, ink, label)
			}
			ui.Text(c, copiedClock(entry.CopiedAt)).
				FontSize(s.design.SizeSmall).
				TextColor(s.design.ColorTextMuted).
				SingleLine()
		})
	})
	if row.Clicked() {
		s.copyEntry(entry)
	}
}

// inkText 返回记录文本的颜色：链接用链接色，其余用正文色。
func (s *State) inkText(entry domain.ClipboardEntry) ui.Color {
	if entry.Kind == domain.ClipKindURL {
		return s.design.ColorBlueInk
	}
	return s.design.ColorTextBody
}

// fontOf 返回记录文本的字体：代码用等宽字体以便逐字符辨认。
func fontOf(entry domain.ClipboardEntry) string {
	if entry.Kind == domain.ClipKindCode {
		return "monospace"
	}
	return ""
}

// historyKind 返回记录所用的底色、墨色与图标（图片用图片图标）。
func (s *State) historyKind(entry domain.ClipboardEntry) (tile, ink ui.Color, mark *ui.SVG) {
	switch entry.Kind {
	case domain.ClipKindImage:
		return s.design.ColorVioletTile, s.design.ColorVioletInk, s.design.HistoryImage
	case domain.ClipKindURL:
		return s.design.ColorBlueTile, s.design.ColorBlueInk, s.design.HistoryLink
	case domain.ClipKindCode:
		return s.design.ColorStoneTile, s.design.ColorStoneInk, s.design.HistoryCode
	case domain.ClipKindJSON:
		return s.design.ColorEmeraldTile, s.design.ColorEmeraldInk, s.design.HistoryText
	case domain.ClipKindFile:
		return s.design.ColorAmberTile, s.design.ColorAmberInk, s.design.HistoryText
	default:
		return s.design.ColorSkyTile, s.design.ColorSkyInk, s.design.HistoryText
	}
}

// historyLabel 返回列表旁显示的一个词：类型；图片则还带大小。
func historyLabel(entry domain.ClipboardEntry) string { return Label(entry) }

// copiedClock 返回用于列表显示的时间：今天显示时刻，更早显示日期。
func copiedClock(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	local := at.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	return local.Format("01-02 15:04")
}
