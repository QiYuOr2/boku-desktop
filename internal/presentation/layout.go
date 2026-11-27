package presentation

import (
	"github.com/egoist/mygo/ui"
)

// Content 是窗口内容区：只在上下保留页边距，横向边距由各 section 自己负责，
// 因为分隔线要越过 section 的横向边距——线分隔的是窗口，而非其中的文字栏。
func Content(c *ui.Context, fill bool, sections func()) {
	e := ui.Column(c).FillWidth().PaddingY(PageEdge).AlignItems(ui.Start)
	if fill {
		e = e.Grow(1)
	}
	e.Children(sections)
}

func Inset(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().PaddingX(PageEdge).Children(body)
}

// BetweenSections 分隔窗口中相邻的两个 section；分隔线画在页边距之内。
func BetweenSections(c *ui.Context) {
	Inset(c, func() {
		ui.Column(c).FillWidth().PaddingY(SectionGap).Children(func() {
			Hairline(c)
		})
	})
}

// QuickWindowFooter 是快速窗口的底栏：版本号与常驻操作；版本与操作都不压缩，
// 操作被省略号截断会使底栏不可用。
func QuickWindowFooter(c *ui.Context, version string, actions func(c *ui.Context)) {
	ui.Row(c).FillWidth().Shrink(0).
		Padding(float32(6), float32(14)).
		AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, version).
				FontSize(SizeQuickTiny).
				TextColor(PanelFaint).
				SingleLine().
				Shrink(0)
			ui.Spacer(c)
			if actions != nil {
				actions(c)
			}
		})
}

func QuickEdgeLine(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(PanelLine)
}

// WordButton 是一个可点击的词（如剪贴板行进入历史的入口），悬停时使用品牌色。
func WordButton(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Radius(float32(4)).
		Padding(0, float32(4)).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(PanelPress)
	case b.Hovered() || b.Focused():
		b = b.Background(PanelField)
	}
	b.Children(func() {
		text := ui.Text(c, label).FontSize(SizeQuickFine).SingleLine()
		if b.Hovered() || b.Focused() {
			text.TextColor(ColorBrand)
		} else {
			text.TextColor(PanelFaint)
		}
	})
	return b
}
