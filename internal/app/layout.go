package app

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

// brandedMark is a provider's logo and the color it is drawn in: the mark is
// the provider's own, so the color comes with it and not from the theme.
type brandedMark struct {
	svg   *ui.SVG
	color ui.Color
}

// content is the region a window's sections live in: the page's own edge above
// the first section and below the last, and the one line that divides the
// sections drawn by the caller.
//
// The region itself keeps only that vertical edge: a section is responsible for
// its own horizontal margin, because the hairline that divides two sections
// runs past it — a line divides the window, not the column of text in it.
func content(c *ui.Context, fill bool, sections func()) {
	e := ui.Column(c).FillWidth().PaddingY(pageEdge).AlignItems(ui.Start)
	if fill {
		e = e.Grow(1)
	}
	e.Children(sections)
}

// inset is one section's own room inside the page's margin: what a section's
// rows are laid out in, and what the section's head uses.
func inset(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().PaddingX(pageEdge).Children(body)
}

// betweenSections divides one section of a window from the next, as the design
// has it: the sections of a window are peers, and a line between them is what
// says so.
//
// The line is drawn inside the page's margin, like everything else a section
// holds, and clear of both sections: it separates two sections of the page
// rather than two halves of the window, and a line that ran to the window's
// edge would put the page's edge somewhere the content never goes.
func betweenSections(c *ui.Context) {
	inset(c, func() {
		ui.Column(c).FillWidth().PaddingY(sectionGap).Children(func() {
			hairline(c)
		})
	})
}

// quickWindowFooter is the quick window's own foot: the version and the actions
// that are always available, on the window's own face, so that nothing looks
// bolted onto the bottom of the card.
//
// The version and the actions keep their own width: the actions are what the
// footer is for, and a footer that squeezed them into an ellipsis would be a
// footer nobody could use.
func quickWindowFooter(c *ui.Context, version string, actions func(c *ui.Context)) {
	ui.Row(c).FillWidth().Shrink(0).Height(float32(36)).
		PaddingX(float32(12)).
		AlignItems(ui.Center).
		Font("monospace").
		Children(func() {
			ui.Text(c, version).
				FontSize(sizeSmall).
				TextColor(colorTextMuted).
				SingleLine().
				Shrink(0)
			ui.Spacer(c)
			if actions != nil {
				actions(c)
			}
		})
}

// chipButton is a text button with a face: a small label on a rounded
// background, which darkens under the pointer. It is for a row that is about
// a value rather than about actions — the clipboard line — where a control
// with no face is easy to miss next to the text it sits beside.
func chipButton(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Radius(radiusField).
		Padding(float32(2), float32(8))
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(colorHover)
	default:
		b = b.Background(colorTextFill)
	}
	b.Children(func() {
		text := ui.Text(c, label).FontSize(sizeSmall).SingleLine()
		if b.Hovered() || b.Focused() {
			text.TextColor(colorText)
		} else {
			text.TextColor(colorTextMuted)
		}
	})
	return b
}
