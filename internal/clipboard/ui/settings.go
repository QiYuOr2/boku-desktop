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
		s.design.Stacked(c, presentation.GroupGap, func() {
			s.design.SectionHead(c, head, s.design.Mark, s.design.ColorStoneInk, "剪贴板", "监听与预览设置")

			s.design.Stacked(c, presentation.RowGap, func() {
				s.design.Notice(c, false, "剪贴板内容可能包含密码、验证码与私人对话；默认不读取。")

				monitor := settings.Monitoring
				ui.Checkbox(c, &monitor, "监听剪贴板（每 "+intervalLabel(settings)+" 读取一次）").
					FontSize(s.design.SizeBody).
					OnClick(func() { s.setClipboardMonitoring(monitor) })

				visible := settings.RegionVisible
				ui.Checkbox(c, &visible, "在快捷窗口底部显示剪贴板内容").
					FontSize(s.design.SizeBody).
					OnClick(func() { s.setClipboardRegionVisible(visible) })

				// 记录范围是三个选项而非常量，选更短会立刻丢弃装不下的旧记录，
				// 所以下方说明强调这一点。
				sizes := clipboard.HistorySizes()
				selected := 0
				for index, size := range sizes {
					if size == settings.KeepsHistory() {
						selected = index
					}
				}
				s.design.Stacked(c, presentation.FieldGap, func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						s.design.SectionLabel(c, "记录范围")
						ui.Spacer(c)
						picker := s.design.Segmented(c, &selected, historySizeLabels(sizes)...)
						if picker.Changed() {
							s.setClipboardHistorySize(sizes[selected])
						}
					})
					s.design.Notice(c, false, "缩短范围会立刻丢弃更早的记录。")
					if !settings.Monitoring {
						s.design.Notice(c, false, "已有记录仍然保留。")
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

// historyButton 是打开剪贴板历史的紧凑尾部动作。
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

// intervalLabel 按开关的读法格式化轮询间隔。
func intervalLabel(settings clipboard.Settings) string {
	interval := settings.Interval()
	if interval%time.Second == 0 {
		return strconv.Itoa(int(interval/time.Second)) + " 秒"
	}
	return strconv.Itoa(int(interval/time.Millisecond)) + " 毫秒"
}

// setClipboardMonitoring 切换监听开关。复选框会显示结果，这里只报告写入失败。
func (s *State) setClipboardMonitoring(enabled bool) {
	err := s.clipboard.SetMonitoring(context.Background(), enabled)
	if err != nil {
		s.clipboardStatus = "开关没能写进设置文件：" + s.design.FriendlyError(err) + "（本次运行内已生效）"
	} else {
		s.clipboardStatus = ""
	}
	s.host.Refresh()
}

// setClipboardRegionVisible 切换快捷窗口底部剪贴板行的可见性。
func (s *State) setClipboardRegionVisible(visible bool) {
	err := s.clipboard.SetRegionVisible(context.Background(), visible)
	if err != nil {
		s.clipboardStatus = "开关没能写进设置文件：" + s.design.FriendlyError(err) + "（本次运行内已生效）"
	} else {
		s.clipboardStatus = ""
	}
	s.host.Refresh()
}

// historySizeLabels 按分段控件的读法格式化记录范围。
func historySizeLabels(sizes []int) []string {
	labels := make([]string, 0, len(sizes))
	for _, size := range sizes {
		labels = append(labels, strconv.Itoa(size)+" 条")
	}
	return labels
}

// setClipboardHistorySize 选择记录范围：装不下的记录在此刻立即丢弃，不留到下次复制。
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

// clipboardStatusLine 返回剪贴板开关最近一次的结果提示。
func (s *State) clipboardStatusLine() string { return s.clipboardStatus }
