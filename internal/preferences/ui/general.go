package ui

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) GeneralView(c *ui.Context, head *ui.Handle, props GeneralProps) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, float32(16), func() {
			presentation.SectionHead(c, head, props.Mark, presentation.ColorStoneInk, "通用", "启动与外观")

			presentation.Stacked(c, float32(10), func() {
				// The appearance is the section's first choice because it is
				// the one the user sees before reading anything: three words
				// in the same segmented control the clipboard's record range
				// uses, so a choice between a few fixed things looks the same
				// wherever it appears.
				presentation.Stacked(c, float32(8), func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						presentation.SectionLabel(c, "主题")
						ui.Spacer(c)
						AppearancePicker(c, props.Appearance, props.SetAppearance)
					})
					presentation.Notice(c, presentation.NotePlain, "自动跟随系统外观；浅色与深色固定使用。")
					if props.AppearanceStatus != "" {
						presentation.Notice(c, presentation.NotePlain, props.AppearanceStatus)
					}
				})

				// The panels that read something of the user's own — today
				// the Codex quota, which comes from the login the command
				// line already made — are switches here. What the switch
				// says is the application's words; which panels exist is the
				// assembly's.
				props.Panels(c)
				if props.Status != "" {
					presentation.Notice(c, presentation.NotePlain, props.Status)
				}

				s.autostartSwitch(c)
			})

			presentation.Stacked(c, float32(8), func() {
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
