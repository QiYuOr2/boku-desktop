package ui

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) GeneralView(c *ui.Context, head *ui.Handle, props GeneralProps) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHead(c, head, props.Mark, presentation.ColorTextMuted, "通用", "")

			presentation.SettingsPanel(c, func() {
				presentation.SettingsControlRow(c, "主题", props.AppearanceStatus, func() {
					AppearancePicker(c, props.Appearance, props.SetAppearance)
				})

				if props.Panels != nil {
					props.Panels(c)
				}

				s.autostartSwitch(c)

				presentation.SettingsDivider(c)

				presentation.SettingsRow(c, func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.GroupGap).Children(func() {
						presentation.SectionLabel(c, "数据目录")
						ui.Spacer(c)
						if button := presentation.DesignButton(c, "在文件管理器中显示", presentation.ButtonSecondary).Shrink(0); button.Clicked() {
							props.RevealDataPath()
						}
					})
					ui.Text(c, props.DataPath).
						FontSize(presentation.SizeSmall).
						TextColor(presentation.ColorTextMuted).
						Font("monospace").
						Wrap()
				})
			})

			if props.Status != "" {
				presentation.Notice(c, presentation.NotePlain, props.Status)
			}
		})
	})
}
