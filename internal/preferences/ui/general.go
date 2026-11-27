package ui

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) GeneralView(c *ui.Context, head *ui.Handle, props GeneralProps) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHead(c, head, props.Mark, presentation.ColorStoneInk, "通用", "启动与外观")

			presentation.Stacked(c, presentation.RowGap, func() {
				presentation.Stacked(c, presentation.FieldGap, func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						presentation.SectionLabel(c, "主题")
						ui.Spacer(c)
						AppearancePicker(c, props.Appearance, props.SetAppearance)
					})
					if props.AppearanceStatus != "" {
						presentation.Notice(c, presentation.NotePlain, props.AppearanceStatus)
					}
				})

				props.Panels(c)
				if props.Status != "" {
					presentation.Notice(c, presentation.NotePlain, props.Status)
				}

				s.autostartSwitch(c)
			})

			presentation.Stacked(c, presentation.FieldGap, func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					presentation.SectionLabel(c, "数据目录")
					ui.Spacer(c)
					if button := presentation.DesignButton(c, "在文件管理器中显示", presentation.ButtonSecondary).Shrink(0); button.Clicked() {
						props.RevealDataPath()
					}
				})
				ui.Text(c, props.DataPath).
					FontSize(presentation.SizeSmall).
					TextColor(presentation.ColorInputValue).
					Font("monospace").
					Wrap()
			})
		})
	})
}
