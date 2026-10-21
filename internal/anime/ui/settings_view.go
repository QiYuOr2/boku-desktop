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
		s.design.Stacked(c, float32(16), func() {
			s.design.SectionHead(c, head, s.design.Mark, ui.Hex(animePrimary), "动画更新", "Bangumi「在看」今日放送")
			on := s.service.Enabled()
			ui.Checkbox(c, &on, "在悬浮窗显示今日动画更新").FontSize(s.design.SizeBody).OnClick(func() { s.saveAnime(on) })
			s.design.Notice(c, false, "仅展示「在看」且今日有新集的动画；无更新时不占位。")
			s.design.Stacked(c, float32(8), func() {
				s.design.SectionLabel(c, "Bangumi 用户名")
				field := ui.TextInput(c, &s.animeUsername).Placeholder("用户名或 UID，如 sai").FillWidth().Height(s.design.HeightControl)
				field.OnSubmit(func() { s.saveAnime(on) })
				s.design.Notice(c, false, "读取公开「在看」列表；不会保存或请求 Bangumi 密码、Token。")
			})
			s.design.Stacked(c, float32(8), func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					s.design.SectionLabel(c, "Access Token（可选）")
					ui.Spacer(c)
					if link := s.design.WordButton(c, "获取 Token"); link.Clicked() {
						c.OpenURL("https://next.bgm.tv/demo/access-token")
					}
				})
				ui.Row(c).FillWidth().Gap(float32(6)).Children(func() {
					ui.TextInput(c, &s.animeTokenInput).Placeholder("仅私有收藏或写接口时需要").FillWidth().Grow(1).Height(s.design.HeightControl)
					if button := s.design.SecondaryButton(c, "保存"); button.Clicked() {
						s.saveAnimeToken()
					}
				})
				s.design.CardHint(c, "公开在看列表无需 Token；私有收藏会以 Authorization: Bearer 发送。")
			})
			s.design.Stacked(c, float32(8), func() {
				s.design.SectionLabel(c, "刷新间隔（分钟）")
				ui.TextInput(c, &s.animeRefreshInput).FillWidth().Height(s.design.HeightControl)
				s.design.CardHint(c, "后台轮询在看列表与章节放送日")
			})
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				if button := s.design.SecondaryButton(c, "保存").Shrink(0); button.Clicked() {
					s.saveAnime(on)
				}
			})
		})
	})
}
