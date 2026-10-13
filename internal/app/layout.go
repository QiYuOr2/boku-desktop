package app

import (
	"github.com/egoist/mygo/ui"
)

// Every screen of Boku is built from the same regions: a header that names
// it, a body that shows what it is about, and a footer bar that carries the
// version and the actions which are always available. A screen without much
// to introduce — the quick window, whose one panel names itself — leaves the
// header off; each region is drawn between dividers, so a screen reads as a
// column of sections.
//
// The quick window and the settings window use them, so the application
// looks like one thing rather than two designs. Regions are plain functions
// of the frame: they hold no state, and building them twice costs nothing.
type page struct {
	// showHeader turns the header on: the mark, the title and the one line
	// that says what the screen is. A screen nobody needs introducing —
	// the quick window, whose whole content is a panel that names itself —
	// leaves it off, and its content starts at the top of the window.
	showHeader bool
	// mark is the logo a screen about one provider shows in its header,
	// drawn in the brand color the mark carries.
	mark brandedMark
	// title and subtitle are the header: what this screen is, said in one
	// bold line and one quiet one.
	title    string
	subtitle string
	// body draws the screen's own blocks, between the header's divider and
	// the footer's, filling the room that is left.
	body func(c *ui.Context)
	// actions draws the right side of the footer, or is nil for a screen
	// whose actions are elsewhere.
	actions func(c *ui.Context)
}

// page lays the regions out in a column. The body takes the space that is
// left, so the footer stays at the bottom whatever the window's height is.
func (s *State) page(c *ui.Context, p page) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		if p.showHeader {
			header(c, p)
			ui.Divider(c)
		}
		body := ui.Column(c).Fill().
			Padding(t.Space(4), t.Space(4), t.Space(3), t.Space(4)).
			Gap(t.Space(3))
		body.Children(func() {
			if p.body != nil {
				p.body(c)
			}
		})
		ui.Divider(c)
		footer(c, s.versionLine(), p.actions)
	})
}

// brandedMark is a provider's logo and the color it is drawn in: the mark is
// the provider's own, so the color comes with it and not from the theme.
type brandedMark struct {
	svg   *ui.SVG
	color ui.Color
}

// block is one section of a window: an optional title and its own content.
//
// Both windows are built from blocks rather than from one long form, because
// both of them grow: the quick window will show more panels next to this one,
// and the settings window will show more groups than the one provider it
// configures today. A block is what a new panel or a new group adds.
//
// A block has no face of its own — no box around it. What marks where one
// ends and the next begins is a divider, the same hairline that separates
// the content from the footer, so a window reads as a column of sections
// rather than as a wall of boxes.
func block(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(t.Rem(0.95)).Bold()
		}
		body()
	})
}

// metricRows draws the small numbers under a block's headline: one line each,
// label on the left and value on the right, close together so that a block
// stays a block instead of turning into a page.
func metricRows(c *ui.Context, rows []metricRow) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		for _, row := range rows {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, row.label).FontSize(t.Rem(0.78)).TextColor(t.TextMuted)
				ui.Spacer(c)
				ui.Text(c, row.value).FontSize(t.Rem(0.82)).SingleLine()
			})
		}
	})
}

// metricRow is one line of a block: what a number is, and the number.
type metricRow struct {
	label string
	value string
}

// header is the top region: a mark, a title and one line of explanation,
// in the order the eye reads them.
func header(c *ui.Context, p page) {
	t := c.Theme()
	ui.Row(c).FillWidth().
		Padding(t.Space(4), t.Space(4), t.Space(3), t.Space(4)).
		Gap(t.Space(3)).
		AlignItems(ui.Start).
		Children(func() {
			if p.mark.svg != nil {
				// The mark decides its own width: its aspect ratio comes
				// from the drawing, and a logo squeezed into a square box
				// is a different logo.
				ui.Image(c, p.mark.svg).
					Height(t.Space(10)).
					TextColor(p.mark.color)
			}
			ui.Column(c).Fill().Gap(t.Space(1)).Children(func() {
				ui.Text(c, p.title).FontSize(t.Rem(1.2)).Bold()
				if p.subtitle != "" {
					ui.Text(c, p.subtitle).
						FontSize(t.Rem(0.8)).
						TextColor(t.TextMuted).
						Wrap().
						LineHeight(1.35)
				}
			})
		})
}

// footer is the bottom region: the version on the left, the actions that are
// always available on the right. It is deliberately quiet — a hairline above
// it, no face of its own — so it reads as the edge of the window rather than
// as another row of content.
func footer(c *ui.Context, version string, actions func(c *ui.Context)) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(t.Space(9)).
		PaddingX(t.Space(3)).
		AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, version).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).SingleLine()
			ui.Spacer(c)
			if actions != nil {
				actions(c)
			}
		})
}

// textButton is one footer action: a label that acts like a button without a
// button's face and padding, so the footer stays a bar instead of a row of
// controls. It keeps the keyboard and focus behaviour of a button.
func textButton(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	button := ui.ButtonBase(c)
	button.Radius(t.Radius).Padding(t.Space(1), t.Space(2))
	button.Children(func() {
		text := ui.Text(c, label).FontSize(t.Rem(0.8)).SingleLine()
		if button.Hovered() || button.Focused() {
			text.TextColor(t.Text)
		} else {
			text.TextColor(t.TextMuted)
		}
	})
	return button
}
