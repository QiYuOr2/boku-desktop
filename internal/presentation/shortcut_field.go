package presentation

import "github.com/egoist/mygo/ui"

// ShortcutField 是共享的快捷键录制控件；组合键读取后的行为由功能自己处理。
type ShortcutField struct {
	Accelerator string
	Recording   bool
	ClearMark   *ui.SVG
	ClearLabel  string
	OnInput     func(ui.InputEvent) bool
	OnPress     func()
	OnClear     func()
}

// View 构建录制控件。清除图标仅在已有绑定时出现，其占位在 label 中预留，
// 避免最后一个字母被图标遮住。
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
		labelPad = 28
	}
	control := ui.Box(c).Width(float32(164)).Height(HeightCardControl)
	control.Children(func() {
		button := ui.ButtonBase(c).Label("快捷键 "+FriendlyAccelerator(field.Accelerator)).
			Absolute().Fill().
			Padding(0, labelPad, 0, 12).
			Radius(RadiusField).
			Border(1, ColorBorder).
			Background(ColorField).
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
				FontSize(SizeSmall).
				Font("monospace").
				LetterSpacing(float32(-0.25)).
				SingleLine()
			if field.Recording {
				text.TextColor(ColorBrand)
			} else {
				text.TextColor(ColorText)
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
			if clear := CardIconButton(c, field.ClearMark, field.ClearLabel).Absolute().Right(float32(4)).Top(shortcutClearTop()); clear.Clicked() && field.OnClear != nil {
				field.OnClear()
			}
		}
	})
	return control
}

func shortcutClearTop() float32 {
	iconTarget := SizeCardMark + 2*CardMarkPad
	return float32((HeightCardControl - iconTarget) / 2)
}
