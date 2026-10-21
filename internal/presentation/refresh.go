package presentation

import (
	"github.com/egoist/mygo/ui"
	"time"
)

// spinPeriod is how long the refresh mark takes to turn once, and spinKey
// tells its animation apart from any other on the element.
const (
	spinPeriod = 900 * time.Millisecond
	spinKey    = "refresh"
)

// refreshButton is the panel's one action, drawn as the mark for fetching
// again rather than as a word: the line it sits on is already carrying the
// age of the numbers, and a mark says "fetch this again" without taking a
// corner of the line for it. What it is, for a pointer that rests on it and
// for assistive technology, is its label.
//
// While a fetch is running the mark turns. That is the whole of what the
// click answers with: the numbers stay as they are until an answer arrives,
// and a still mark beside them would leave the user wondering whether the
// click landed.
func RefreshButton(c *ui.Context, markSVG *ui.SVG, loading, retry bool) ui.Element {
	label := "刷新"
	if retry {
		// One action, two situations: the reason the last attempt failed is
		// written under the numbers, and this is what trying again is
		// called when there is something to try again about.
		label = "重试"
	}
	// The mark is built here and filled in below, inside the button's own
	// children: the refresh action draws a mark that moves, which the
	// design system's icon button — a still mark — does not. Everything
	// else about it, down to the face under the pointer, is the design's.
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
	// Turning the mark is what the click answers while a fetch runs: a still
	// mark beside unchanged numbers would leave the user wondering whether
	// the click landed.
	if loading && mark.Valid() {
		mark.Rotate(mark.Loop(spinKey, spinPeriod, ui.Linear) * 360)
	}
	return button
}
