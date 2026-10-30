package presentation

import "github.com/egoist/mygo/ui"

// ShortcutField is the shared recorder: a fixed 140-point field, a click that
// waits for the next combination, and an optional clear mark inside the field.
// What the combination does after it is read stays with the feature.
type ShortcutField struct {
	Accelerator string
	Recording   bool
	ClearMark   *ui.SVG
	ClearLabel  string
	OnInput     func(ui.InputEvent) bool
	OnPress     func()
	OnClear     func()
}

// View builds the recorder. The clear mark only appears when a binding is
// present; its column is reserved in the label so the last letter is not
// drawn under the icon.
func (field ShortcutField) View(c *ui.Context) ui.Element {
	label := CompactAccelerator(field.Accelerator)
	if label == "" {
		label = "点击录制"
	}
	if field.Recording {
		label = "按下组合键…"
	}
	labelPad := float32(12)
	showClear := field.ClearMark != nil && field.Accelerator != ""
	if showClear {
		labelPad = 24
	}
	control := ui.Box(c).Width(float32(140)).Height(HeightControl - 4)
	control.Children(func() {
		button := ui.ButtonBase(c).Label("快捷键 "+FriendlyAccelerator(field.Accelerator)).
			Absolute().Fill().
			Padding(0, labelPad, 0, 12).
			Radius(RadiusField).
			Border(1, ColorBorder).
			Background(ColorSurface).
			Focusable().
			Cursor(ui.CursorPointer)
		switch {
		case field.Recording:
			button.Background(ColorBrandTintStrong).Border(1, ColorBrand)
		case button.Hovered() || button.Focused():
			button.Background(ColorHover)
		}
		button.Children(func() {
			text := ui.Text(c, label).
				FontSize(SizeTiny).
				Font("monospace").
				LetterSpacing(float32(-0.25)).
				SingleLine()
			if field.Recording {
				text.TextColor(ColorBrand)
			} else {
				text.TextColor(ColorTextSoft)
			}
		})
		if field.OnInput != nil {
			button.HandleInput(field.OnInput)
		}
		if field.Recording {
			button.AutoFocus()
		}
		if button.Clicked() && field.OnPress != nil {
			field.OnPress()
		}
		if showClear {
			if clear := CardIconButton(c, field.ClearMark, field.ClearLabel).Absolute().Right(float32(2)).Top(shortcutClearTop()); clear.Clicked() && field.OnClear != nil {
				field.OnClear()
			}
		}
	})
	return control
}

func shortcutClearTop() float32 {
	fieldHeight := HeightControl - 4
	iconTarget := SizeCardMark + 2*CardMarkPad
	return float32((fieldHeight - iconTarget) / 2)
}
