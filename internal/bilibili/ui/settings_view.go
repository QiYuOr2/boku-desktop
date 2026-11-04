package ui

import (
	"boku/internal/presentation"
	"fmt"
	"github.com/egoist/mygo/ui"
)

func (s *ViewModel) SettingsView(c *ui.Context, head *ui.Handle, design presentation.ReadingDesign) {
	s.design = design
	if s.service == nil {
		return
	}
	s.design.Inset(c, func() {
		s.design.Stacked(c, presentation.GroupGap, func() {
			s.design.SectionHead(c, head, s.design.Mark, presentation.BilibiliAccent.Ink, "B站 UP 监听", "指定主播最新动态 + 开播状态")
			s.design.Stacked(c, presentation.RowGap, func() {
				on := s.service.Enabled()
				ui.Checkbox(c, &on, "在悬浮窗显示关注的 UP").FontSize(s.design.SizeBody).OnClick(func() { s.saveBilibili(on, s.service.UIDs()) })
				s.design.Stacked(c, presentation.FieldGap, func() {
					s.design.SectionLabel(c, "关注列表（最多 8 个）")
					for _, id := range s.service.UIDs() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
							ui.Text(c, fmt.Sprintf("UID %d", id)).FontSize(s.design.SizeSmall).TextColor(s.design.ColorTextBody)
							ui.Spacer(c)
							if s.design.WordButton(c, "删除").Clicked() {
								s.removeBilibiliUID(on, id)
							}
						})
					}
					ui.Row(c).FillWidth().Gap(float32(6)).Children(func() {
						ui.TextInput(c, &s.bilibiliUIDInput).Placeholder("UID 或空间链接，如 672328094").Grow(1).Height(s.design.HeightControl).FixedLineHeight(presentation.InputLineHeight(presentation.SizeBody))
						if s.design.PrimaryButton(c, "添加").Clicked() {
							s.addBilibiliUID(on)
						}
					})
				})
				s.design.Stacked(c, presentation.FieldGap, func() {
					s.design.SectionLabel(c, "刷新间隔（分钟）")
					ui.TextInput(c, &s.bilibiliRefreshInput).FillWidth().Height(s.design.HeightControl)
				})
				s.design.Stacked(c, presentation.FieldGap, func() {
					s.design.SectionLabel(c, "动态拉取条数")
					ui.TextInput(c, &s.bilibiliDynamicInput).FillWidth().Height(s.design.HeightControl)
					s.design.CardHint(c, "每位 UP 展示最新 1–5 条动态")
				})
				ui.Checkbox(c, &s.bilibiliLiveNotice, "开播时系统通知").FontSize(s.design.SizeBody).OnClick(func() { s.saveBilibili(on, s.service.UIDs()) })
				ui.Checkbox(c, &s.bilibiliDynamicNotice, "有新动态时系统通知").FontSize(s.design.SizeBody).OnClick(func() { s.saveBilibili(on, s.service.UIDs()) })
			})
		})
	})
}
