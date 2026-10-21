package presentation

import (
	"github.com/egoist/mygo/ui"
)

// The pieces a window is assembled from, beyond the ones design.go composes:
// the region a window's content lives in, the version line its footer carries,
// and the two actions the quick window keeps at its foot.
//
// Every screen of Boku is the same shape: a title bar, content, and a footer,
// with the content divided into sections by a hairline. A section is a plain
// group of rows on the content's own face — the sections of the settings
// window are peers, so nothing nests inside anything else and no section wears
// a card of its own.

// content is the region a window's sections live in: the page's own edge above
// the first section and below the last, and the one line that divides the
// sections drawn by the caller.
//
// The region itself keeps only that vertical edge: a section is responsible for
// its own horizontal margin, because the hairline that divides two sections
// runs past it — a line divides the window, not the column of text in it.
func Content(c *ui.Context, fill bool, sections func()) {
	e := ui.Column(c).FillWidth().PaddingY(PageEdge).AlignItems(ui.Start)
	if fill {
		e = e.Grow(1)
	}
	e.Children(sections)
}

// inset is one section's own room inside the page's margin: what a section's
// rows are laid out in, and what the section's head uses.
func Inset(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().PaddingX(PageEdge).Children(body)
}

// betweenSections divides one section of a window from the next, as the design
// has it: the sections of a window are peers, and a line between them is what
// says so.
//
// The line is drawn inside the page's margin, like everything else a section
// holds, and clear of both sections: it separates two sections of the page
// rather than two halves of the window, and a line that ran to the window's
// edge would put the page's edge somewhere the content never goes.
func BetweenSections(c *ui.Context) {
	Inset(c, func() {
		ui.Column(c).FillWidth().PaddingY(SectionGap).Children(func() {
			Hairline(c)
		})
	})
}

// quickWindowFooter is the quick window's own foot: the version and the
// actions that are always available, above the window's own edge.
//
// The design gives it 14 beside and 6 above and below, the version in 10
// points of faint ink, and the two actions as marks: two words at the bottom
// of a card read as content, while two marks read as what they are.
//
// The version and the actions keep their own width: the actions are what the
// footer is for, and a footer that squeezed them into an ellipsis would be a
// footer nobody could use.
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

// quickEdgeLine is the line the quick window draws above its foot: it runs the
// window's whole width — it says where the window ends rather than where one
// block does — and it is the stone that window is drawn on.
func QuickEdgeLine(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(PanelLine)
}

// wordButton is a word that acts: the way into the history from the clipboard
// line. It wears no face of its own, because the line it sits on is about a
// value rather than about actions, and it takes the brand green under the
// pointer — the color the application means "this is the one to press" with.
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
