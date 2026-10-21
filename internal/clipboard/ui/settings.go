package ui

import (
	"boku/internal/clipboard"
	"boku/internal/presentation"
	"context"
	"github.com/egoist/mygo/ui"
	"strconv"
	"time"
)

func (s *State) SettingsView(c *ui.Context, head *ui.Handle, design presentation.ClipboardDesign) {
	s.design = design
	if s.clipboard == nil {
		return
	}
	settings := s.clipboard.Settings()
	s.design.Inset(c, func() {
		s.design.Stacked(c, float32(16), func() {
			s.design.SectionHead(c, head, s.design.Mark, s.design.ColorStoneInk, "剪贴板", "监听与预览设置")

			s.design.Stacked(c, float32(12), func() {
				s.design.Notice(c, false, "剪贴板内容可能包含密码、验证码与私人对话；默认不读取。")

				monitor := settings.Monitoring
				ui.Checkbox(c, &monitor, "监听剪贴板（每 "+intervalLabel(settings)+" 读取一次）").
					FontSize(s.design.SizeBody).
					OnClick(func() { s.setClipboardMonitoring(monitor) })

				visible := settings.RegionVisible
				ui.Checkbox(c, &visible, "在快捷窗口底部显示剪贴板内容").
					FontSize(s.design.SizeBody).
					OnClick(func() { s.setClipboardRegionVisible(visible) })

				// The record range: how much of what was copied is kept. It
				// is a choice among three rather than a number, and the line
				// under it says what shortening it costs, because the entries
				// that no longer fit are dropped rather than hidden.
				sizes := clipboard.HistorySizes()
				selected := 0
				for index, size := range sizes {
					if size == settings.KeepsHistory() {
						selected = index
					}
				}
				s.design.Stacked(c, float32(8), func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						s.design.SectionLabel(c, "记录范围")
						ui.Spacer(c)
						picker := s.design.Segmented(c, &selected, historySizeLabels(sizes)...)
						if picker.Changed() {
							s.setClipboardHistorySize(sizes[selected])
						}
					})
					s.design.Notice(c, false, "只保留最近 "+strconv.Itoa(settings.KeepsHistory())+
						" 条；缩短范围会立刻丢弃更早的记录。")
					if !settings.Monitoring {
						s.design.Notice(c, false, "关闭监听后不再读取剪贴板，历史里已有的记录仍然保留。")
					}
				})

				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					if button := s.historyButton(c); button.Clicked() {
						s.openHistory()
					}
				})

				if text := s.clipboardStatusLine(); text != "" {
					s.design.Notice(c, false, text)
				}
			})
		})
	})
}

// historyButton is the compact trailing action that opens clipboard history.
func (s *State) historyButton(c *ui.Context) ui.Element {
	b := ui.ButtonBase(c).Label("打开剪贴板历史").
		Height(s.design.HeightControl).
		Padding(0, float32(12)).
		Radius(s.design.RadiusField).
		Background(s.design.ColorSurface).
		Border(1, s.design.ColorBorder).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(s.design.ColorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(s.design.ColorHover)
	}
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
			ui.Icon(c, s.design.Mark).
				Size(float32(14), float32(14)).
				Shrink(0).
				TextColor(s.design.ColorTextMuted)
			ui.Text(c, "打开剪贴板历史").FontSize(s.design.SizeFine).FontWeight(500).SingleLine()
		})
	})
	return b
}

// intervalLabel names the poll interval the way the switch reads.
func intervalLabel(settings clipboard.Settings) string {
	interval := settings.Interval()
	if interval%time.Second == 0 {
		return strconv.Itoa(int(interval/time.Second)) + " 秒"
	}
	return strconv.Itoa(int(interval/time.Millisecond)) + " 毫秒"
}

// setClipboardMonitoring flips the switch and says what happened, so the
// user is not left guessing whether it worked.
func (s *State) setClipboardMonitoring(enabled bool) {
	err := s.clipboard.SetMonitoring(context.Background(), enabled)
	settings := s.clipboard.Settings()
	switch {
	case err != nil:
		s.clipboardStatus = "开关没能写进设置文件：" + s.design.FriendlyError(err) + "（本次运行内已生效）"
	case settings.Monitoring:
		s.clipboardStatus = "已开始读取剪贴板。"
	default:
		s.clipboardStatus = "已停止读取剪贴板。"
	}
	s.host.Refresh()
}

// setClipboardRegionVisible flips the visibility of the quick window's
// clipboard line.
func (s *State) setClipboardRegionVisible(visible bool) {
	err := s.clipboard.SetRegionVisible(context.Background(), visible)
	settings := s.clipboard.Settings()
	switch {
	case err != nil:
		s.clipboardStatus = "开关没能写进设置文件：" + s.design.FriendlyError(err) + "（本次运行内已生效）"
	case settings.RegionVisible:
		s.clipboardStatus = "快捷窗口底部会显示剪贴板内容。"
	default:
		s.clipboardStatus = "快捷窗口底部不再显示剪贴板内容。"
	}
	s.host.Refresh()
}

// historySizeLabels names the record ranges the way the segments read.
func historySizeLabels(sizes []int) []string {
	labels := make([]string, 0, len(sizes))
	for _, size := range sizes {
		labels = append(labels, strconv.Itoa(size)+" 条")
	}
	return labels
}

// setClipboardHistorySize chooses the record range: what no longer fits is
// dropped here, not at the next copy.
func (s *State) setClipboardHistorySize(size int) {
	if s.clipboard == nil {
		return
	}
	err := s.clipboard.SetHistorySize(context.Background(), size)
	if err != nil {
		s.clipboardStatus = "没能写进设置文件：" + s.design.FriendlyError(err) + "（本次运行内已生效）"
	}
	s.host.Refresh()
}

// clipboardStatusLine is the last thing the clipboard switches said.
func (s *State) clipboardStatusLine() string { return s.clipboardStatus }
