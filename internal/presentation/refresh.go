package presentation

import (
	"github.com/egoist/mygo/ui"
	"time"
)

// spinPeriod 是刷新图标转一圈的时长；spinKey 用于与元素上其他动画区分。
const (
	spinPeriod = 900 * time.Millisecond
	spinKey    = "refresh"
)

// RefreshButton 是面板唯一的操作，以图标而非文字呈现：所在行已显示数据新旧，
// 图标不占行内一角；其含义通过 label 提供给悬停提示与辅助技术。
// 刷新进行中图标旋转，否则用户无法确认点击是否生效。
func RefreshButton(c *ui.Context, markSVG *ui.SVG, loading, retry bool) ui.Element {
	label := "刷新"
	if retry {
		// 同一操作两种情境：上次失败的原因写在数据下方，重试时用「重试」。
		label = "重试"
	}
	var mark ui.Element
	button := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(float32(2)).
		Cursor(ui.CursorPointer)
	switch {
	case button.Pressed():
		button = button.Background(PanelPress)
	case button.Hovered() || button.Focused():
		button = button.Background(PanelHover)
	}
	button.Children(func() {
		mark = ui.Icon(c, markSVG).Size(float32(QuickMarkSize), float32(QuickMarkSize)).Shrink(0)
		if button.Hovered() || button.Focused() {
			mark.TextColor(PanelInk)
		} else {
			mark.TextColor(PanelFaint)
		}
	})
	if loading && mark.Valid() {
		mark.Rotate(mark.Loop(spinKey, spinPeriod, ui.Linear) * 360)
	}
	return button
}
