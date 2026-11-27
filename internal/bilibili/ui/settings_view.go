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
			s.design.SectionHead(c, head, s.design.Mark, presentation.BilibiliAccent.Ink, "B站 UP 监听", "")
			on := s.service.Enabled()
			presentation.SettingsPanel(c, func() {
				presentation.SettingsSwitchRow(c, "在悬浮窗显示关注的 UP", "", &on, func(v bool) { s.saveBilibili(v, s.service.UIDs()) })

				presentation.SettingsDivider(c)

				presentation.SettingsRow(c, func() {
					s.design.SectionLabel(c, "关注列表（最多 8 个）")
					for _, id := range s.service.UIDs() {
						uid := id
						row := ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(float32(4), float32(8)).Radius(presentation.RadiusField)
						hot := row.Hovered()
						if hot {
							row.Background(presentation.ColorHover)
						}
						row.Children(func() {
							ui.Text(c, fmt.Sprintf("UID %d", uid)).FontSize(presentation.SizeSmall).Font("monospace").TextColor(presentation.ColorText)
							ui.Spacer(c)
							actions := ui.Row(c).Shrink(0)
							if !hot {
								actions = actions.Opacity(0).PassThrough()
							}
							actions.Children(func() {
								if presentation.TextAction(c, "删除", presentation.ColorDanger).Clicked() {
									s.removeBilibiliUID(on, uid)
								}
							})
						})
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
						uidField := ui.TextInput(c, &s.bilibiliUIDInput).Placeholder("UID 或空间链接，如 672328094").SingleLine()
						uidField.OnSubmit(func() { s.addBilibiliUID(on) })
						presentation.InputFrame(c, uidField).Grow(1)
						if s.design.PrimaryButton(c, "添加").Clicked() {
							s.addBilibiliUID(on)
						}
					})
				})

				presentation.SettingsDivider(c)

				presentation.SettingsControlRow(c, "刷新间隔（分钟）", "", func() {
					field := ui.TextInput(c, &s.bilibiliRefreshInput).SingleLine()
					field.OnSubmit(func() { s.saveBilibili(on, s.service.UIDs()) })
					presentation.InputFrame(c, field).Width(float32(88)).Font("monospace")
				})

				presentation.SettingsDivider(c)

				presentation.SettingsControlRow(c, "动态拉取条数", "每位 UP 展示最新 1–5 条动态", func() {
					field := ui.TextInput(c, &s.bilibiliDynamicInput).SingleLine()
					field.OnSubmit(func() { s.saveBilibili(on, s.service.UIDs()) })
					presentation.InputFrame(c, field).Width(float32(88)).Font("monospace")
				})

				presentation.SettingsDivider(c)

				presentation.SettingsSwitchRow(c, "开播时系统通知", "", &s.bilibiliLiveNotice, func(bool) { s.saveBilibili(on, s.service.UIDs()) })

				presentation.SettingsDivider(c)

				presentation.SettingsSwitchRow(c, "有新动态时系统通知", "", &s.bilibiliDynamicNotice, func(bool) { s.saveBilibili(on, s.service.UIDs()) })
			})
		})
	})
}
