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
			s.design.SectionHead(c, head, s.design.Mark, presentation.ColorTextMuted, "剪贴板", "")

			presentation.SettingsPanel(c, func() {
				monitor := settings.Monitoring
				monitorNote := "每 " + intervalLabel(settings) + " 读取一次；可能包含密码与验证码，默认不读取。"
				presentation.SettingsSwitchRow(c, "监听剪贴板", monitorNote, &monitor, s.setClipboardMonitoring)

				presentation.SettingsDivider(c)

				visible := settings.RegionVisible
				presentation.SettingsSwitchRow(c, "在快捷窗口底部显示剪贴板内容", "", &visible, s.setClipboardRegionVisible)

				presentation.SettingsDivider(c)

				// 记录范围是三个选项而非常量，选更短会立刻丢弃装不下的旧记录。
				sizes := clipboard.HistorySizes()
				selected := 0
				for index, size := range sizes {
					if size == settings.KeepsHistory() {
						selected = index
					}
				}
				historyNote := "缩短会立刻丢弃更早的记录。"
				if !settings.Monitoring {
					historyNote = "缩短会立刻丢弃更早的记录；关闭监听仍保留历史。"
				}
				presentation.SettingsControlRow(c, "记录范围", historyNote, func() {
					picker := s.design.Segmented(c, &selected, historySizeLabels(sizes)...)
					if picker.Changed() {
						s.setClipboardHistorySize(sizes[selected])
					}
				})

				presentation.SettingsDivider(c)

				presentation.SettingsControlRow(c, "历史记录", "", func() {
					if button := s.historyButton(c); button.Clicked() {
						s.openHistory()
					}
				})
			})

			if text := s.clipboardStatusLine(); text != "" {
				s.design.Notice(c, false, text)
			}
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
		ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Icon(c, s.design.Mark).
				Size(float32(16), float32(16)).
				Shrink(0).
				TextColor(s.design.ColorTextMuted)
			ui.Text(c, "打开剪贴板历史").FontSize(s.design.SizeFine).FontWeight(500).TextColor(presentation.ColorText).SingleLine()
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
