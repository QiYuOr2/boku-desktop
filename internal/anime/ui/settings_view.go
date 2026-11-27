package ui

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *ViewModel) SettingsView(c *ui.Context, head *ui.Handle, design presentation.ReadingDesign) {
	s.design = design
	if s.service == nil {
		return
	}
	s.design.Inset(c, func() {
		s.design.Stacked(c, presentation.GroupGap, func() {
			s.design.SectionHead(c, head, s.design.Mark, presentation.AnimeAccent.Ink, "动画更新", "")
			on := s.service.Enabled()
			presentation.SettingsPanel(c, func() {
				presentation.SettingsSwitchRow(c, "在悬浮窗显示今日动画更新", "", &on, s.saveAnime)

				presentation.SettingsDivider(c)

				presentation.SettingsRow(c, func() {
					s.design.SectionLabel(c, "Bangumi 用户名")
					field := ui.TextInput(c, &s.animeUsername).Placeholder("用户名或 UID，如 sai").SingleLine()
					field.OnSubmit(func() { s.saveAnime(on) })
					presentation.InputFrame(c, field)
					s.design.Notice(c, false, "只读公开列表，不保存密码。")
				})

				presentation.SettingsDivider(c)

				presentation.SettingsRow(c, func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						s.design.SectionLabel(c, "Access Token（可选）")
						ui.Spacer(c)
						if link := s.design.WordButton(c, "获取 Token"); link.Clicked() {
							c.OpenURL("https://next.bgm.tv/demo/access-token")
						}
					})
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
						tokenField := ui.TextInput(c, &s.animeTokenInput).Placeholder("仅私有收藏或写接口时需要").SingleLine()
						presentation.InputFrame(c, tokenField).Grow(1).Font("monospace")
						if button := s.design.SecondaryButton(c, "保存"); button.Clicked() {
							s.saveAnimeToken()
						}
					})
				})

				presentation.SettingsDivider(c)

				presentation.SettingsControlRow(c, "刷新间隔（分钟）", "", func() {
					ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
						refreshField := ui.TextInput(c, &s.animeRefreshInput).SingleLine()
						refreshField.OnSubmit(func() { s.saveAnime(on) })
						presentation.InputFrame(c, refreshField).Width(float32(88)).Font("monospace")
						if button := s.design.SecondaryButton(c, "保存").Shrink(0); button.Clicked() {
							s.saveAnime(on)
						}
					})
				})
			})
		})
	})
}
