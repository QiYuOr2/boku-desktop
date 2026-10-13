package app

import (
	"github.com/egoist/mygo/ui"
)

// This file is Boku's design system: the one place the application's look is
// decided, and the one place a color or a metric is written down. Every
// window is assembled from the pieces here, so that the quick window, the
// settings window and the clipboard history read as one application.
//
// The values are the ones the design gives, not the framework's defaults:
// shadcn's slate border (#E2E8F0), its stone page (#FAFAF9), the brand green
// #3B693D, and the same radius, control height and section rhythm in all
// three windows. A screen says what a thing is — "this is a section's head",
// "this is the main action" — and takes what that looks like from here.
//
// Correctness: a view is built several times per frame, and MyGo builds it
// again for every pointer or keyboard state, so a control can ask whether it
// is hovered, pressed or focused while it is built. That is how the states
// below are expressed — a background chosen from what the frame already
// knows, never a value kept between frames. Nothing here keeps state, so
// building a piece twice costs nothing.
var (
	// --- Faces -----------------------------------------------------------

	// colorSurface is the window's own face. Every window is drawn on it —
	// content, header lines and footer alike — so a window reads as one
	// surface with lines drawn on it rather than as a stack of bands.
	colorSurface = ui.Hex("#FFFFFF")
	// colorField is the face of an input, and colorMuted the face of a tile
	// that is not colored by what it stands for.
	colorField = ui.Hex("#FAFAF9")
	colorMuted = ui.Hex("#F5F5F4")
	// colorHover and colorActive are what a row or a button takes under the
	// pointer and while it is pressed.
	colorHover  = ui.Hex("#F5F5F4")
	colorActive = ui.Hex("#E7E5E4")
	colorRowAlt = ui.Hex("#F8FAFC")

	colorBorder = ui.Hex("#E2E8F0")
	// colorHairline is the line between two entries of a list: fainter than
	// a border, because a list is one thing rather than a row of them.
	colorHairline = ui.Hex("#F5F5F4")

	// --- Text ------------------------------------------------------------

	colorText       = ui.Hex("#0F172A")
	colorTextSoft   = ui.Hex("#1C1917")
	colorTextBody   = ui.Hex("#44403C")
	colorTextMuted  = ui.Hex("#64748B")
	colorTextFaint  = ui.Hex("#78716C")
	colorInputValue = ui.Hex("#57534E")

	// --- Brand and status ------------------------------------------------

	// colorBrand is the green of the one action a form is for, and of every
	// other place the application asks for attention. It is the theme's
	// accent too, so a checked box and a picked segment come along with it.
	colorBrand        = ui.Hex("#3B693D")
	colorBrandHover   = ui.Hex("#2F5631")
	colorBrandPressed = ui.Hex("#294A2B")
	colorOnBrand      = ui.Hex("#FFFFFF")

	colorSuccess = ui.Hex("#059669")
	colorWarning = ui.Hex("#B45309")
	colorDanger  = ui.Hex("#DC2626")

	// The warning panel: the one note on a page the user should not be able
	// to miss.
	colorWarningFill   = ui.Hex("#FFFBEB")
	colorWarningBorder = ui.Hex("#FDE68A")

	// --- The colors a recorded copy is drawn in --------------------------

	// Each is a pair: the face of the tile at the head of the row, and the
	// color the mark and the kind's badge are drawn in.
	colorSkyTile, colorSkyInk         = ui.Hex("#F0F9FF"), ui.Hex("#0EA5E9")
	colorBlueTile, colorBlueInk       = ui.Hex("#EFF6FF"), ui.Hex("#3B82F6")
	colorVioletTile, colorVioletInk   = ui.Hex("#F5F3FF"), ui.Hex("#8B5CF6")
	colorAmberTile, colorAmberInk     = ui.Hex("#FFFBEB"), ui.Hex("#D97706")
	colorEmeraldTile, colorEmeraldInk = ui.Hex("#ECFDF5"), ui.Hex("#047857")
	colorStoneTile, colorStoneInk     = ui.Hex("#F5F5F4"), ui.Hex("#57534E")

	// The tints behind a quiet button and a badge.
	colorTextFill = ui.RGBA(15, 23, 42, 0.06)
)

// The application's metrics, in points, taken from the design: every window
// keeps a 20-point page edge, a section keeps 12 points between its rows, and
// every control on a line is 36 points high.
const (
	// pageEdge is the window's own margin: the room the page keeps on every
	// side, and the room the footer's version keeps inside it.
	pageEdge = 20.0
	// sectionGap is the room a section keeps above and below itself, which is
	// what keeps the line dividing two of them from touching either.
	sectionGap = 16.0

	radiusCard = 12.0
	// radiusTile rounds the small square at the head of a list row.
	radiusTile  = 6.0
	radiusField = 6.0

	// heightControl is the height every control shares: an input, a button
	// and a segmented control sit on one line as one row of things.
	heightControl = 36.0
	// sizeTile is the mark at the head of a section: 32 points, rounded.
	sizeTile = 32.0

	// Typography: a section's name, its one line of explanation, and the
	// body text under them. They are the design's own sizes rather than
	// multiples of the theme's, because the two windows are dense.
	sizeTitle    = 15.0
	sizeSection  = 14.0
	sizeSubtitle = 12.0
	sizeBody     = 14.0
	sizeSmall    = 12.0
	sizeTiny     = 10.0
)

// theme colours MyGo's own widgets: its check boxes, inputs, focus rings,
// scrollbars and dividers take these, so that a widget the framework draws
// and a piece this file composes read as the same design.
func theme(dark bool) *ui.Theme {
	if dark {
		base := ui.DarkTheme()
		base.Accent = ui.Hex("#6FA776")
		base.AccentHover = ui.Hex("#84BA8A")
		base.AccentPressed = ui.Hex("#5C8F63")
		base.AccentText = ui.Hex("#10240F")
		base.Focus = ui.RGBA(111, 167, 118, 0.6)
		base.Selection = ui.RGBA(111, 167, 118, 0.35)
		base.Radius = radiusField
		return base
	}
	t := ui.LightTheme()
	t.Background = colorSurface
	t.Surface = colorField
	t.SurfaceHover = colorHover
	t.SurfacePressed = colorActive
	t.Border = colorBorder
	t.Text = colorText
	t.TextMuted = colorTextMuted
	t.Accent = colorBrand
	t.AccentHover = colorBrandHover
	t.AccentPressed = colorBrandPressed
	t.AccentText = colorOnBrand
	t.Danger = colorDanger
	t.Warning = colorWarning
	t.Success = colorSuccess
	t.Selection = ui.RGBA(59, 105, 61, 0.22)
	t.Focus = ui.RGBA(59, 105, 61, 0.55)
	t.Radius = radiusField
	return t
}

// useDesign points the frame being built at Boku's theme. A view calls it
// once, before it builds anything: the theme is read while an element is
// constructed, so a control built before this call keeps the framework's
// colors.
func useDesign(c *ui.Context) {
	c.SetTheme(theme(false))
}

// windowFrame is the column every window is built in: the content area's face
// from edge to edge, which the regions inside it divide.
func windowFrame(c *ui.Context) ui.Element {
	return ui.Column(c).Fill().Background(colorSurface)
}

// footer is the quiet line at the bottom of a window: the version, on the
// window's own face, under one hairline. It is the last line of the window
// rather than another region of it, so it wears no band of its own.
//
// The hairline is the window's own line rather than one between two sections:
// it runs from edge to edge, and what it says is where the window ends. The
// version sits inside it, in the room a footer action keeps.
func footer(c *ui.Context, version string) {
	hairline(c)
	ui.Row(c).FillWidth().Shrink(0).Font("monospace").
		PaddingX(float32(12)).
		PaddingY(pageEdge / 2).
		AlignItems(ui.Center).
		Children(func() { versionLine(c, version) })
}

// versionLine is the quiet version at the bottom of a window.
func versionLine(c *ui.Context, version string) {
	ui.Text(c, version).FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine()
}

// sectionHead is how a section of the settings window introduces itself: the
// mark of what it is about, its name, and one line saying what the section is
// for.
//
// A provider's mark is drawn in the provider's own color and is not dressed
// up: it is the logo the user knows, and it keeps its own shape, color and
// proportions wherever it appears. A section with nothing to show — the
// clipboard, a group still to come — carries the application's own mark in
// the muted stone instead.
func sectionHead(c *ui.Context, mark *ui.SVG, ink ui.Color, title, subtitle string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(10)).Children(func() {
		if mark != nil {
			ui.Box(c).Children(func() {
				ui.Icon(c, mark).Height(float32(32)).Width(float32(32)).Padding(float32(4), float32(6)).Shrink(0).TextColor(ink)
			}).Background(colorMuted).Radius(radiusTile)
		}
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, title).FontSize(sizeTitle).FontWeight(600).TextColor(colorTextSoft).SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(sizeSubtitle).TextColor(colorTextMuted).SingleLine()
			}
		})
	})
}

// sectionLabel is the label above a control: what the value under it is.
func sectionLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(sizeSection).FontWeight(500).TextColor(colorTextBody).SingleLine()
}

// inputFrame gives a text input the look of a field: the field's own face, a
// hairline, the height every control shares, and the monospace type the
// design gives a value that is read rather than written here.
func inputFrame(c *ui.Context, input ui.Element) ui.Element {
	return input.
		FillWidth().
		Height(heightControl).
		Padding(0, float32(12)).
		Radius(radiusField).
		Background(colorField).
		Border(1, colorBorder).
		FontSize(sizeBody).
		TextColor(colorInputValue)
}

// stacked is a plain column of the pieces inside a section, so that a section
// body does not repeat the same gap in every call.
func stacked(c *ui.Context, gap float32, body func()) {
	ui.Column(c).FillWidth().Gap(gap).Children(body)
}

// buttonTone is what a button is for, which is all a caller has to say about
// how it looks.
type buttonTone int

const (
	// buttonPrimary is the one action a form is for: filled, in the brand
	// green, with the light text on it.
	buttonPrimary buttonTone = iota
	// buttonSecondary is every other action: the content's own face, a
	// hairline, and a hover that tints it.
	buttonSecondary
)

// designButton builds an action in one of the tones. Every tone keeps the
// whole behavior of a button — the pointer, the keyboard, the focus ring and
// what assistive technology reads — since both are the framework's ButtonBase
// underneath.
func designButton(c *ui.Context, label string, tone buttonTone) ui.Element {
	b := ui.ButtonBase(c).Label(label)
	switch tone {
	case buttonPrimary:
		b = b.
			Height(heightControl).
			Padding(0, float32(16)).
			Radius(radiusField).
			Background(colorBrand).
			TextColor(colorOnBrand).
			FontSize(sizeBody).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(colorBrandPressed)
		case b.Hovered() || b.Focused():
			b = b.Background(colorBrandHover)
		}
	case buttonSecondary:
		b = b.
			Height(heightControl).
			Padding(0, float32(16)).
			Radius(radiusField).
			Background(colorSurface).
			Border(1, colorBorder).
			TextColor(colorText).
			FontSize(sizeBody).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(colorActive)
		case b.Hovered() || b.Focused():
			b = b.Background(colorHover)
		}
	}
	b.Children(func() { ui.Text(c, label).FontSize(sizeBody).FontWeight(500).SingleLine() })
	return b
}

// quietIconButton is an action drawn as a mark: one glyph with a square of the
// content's own room around it, drawn in the muted ink the design gives a
// secondary action and in the body ink under the pointer. The room is what
// makes it a target rather than a picture; its label is what it means, for the
// pointer that rests on it and for assistive technology.
func quietIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(radiusField).
		Padding(float32(4)).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(colorHover)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(16), float32(16)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(colorText)
		} else {
			icon.TextColor(colorTextMuted)
		}
	})
	return b
}

// segmented is the choice among a few short options: a bordered track, and
// the chosen segment filled in the brand green.
//
// Only the look is this file's: the choice itself, the arrows that move it
// and what assistive technology reads come from the framework's
// SegmentedBase, whose Track reports a new choice with Changed.
func segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.
		Padding(float32(2)).
		Radius(radiusField).
		Background(colorSurface).
		Border(1, colorBorder)
	parts.Track.Children(func() {
		for index := range labels {
			segment := parts.Segment(index).
				Padding(float32(4), float32(10)).
				Radius(float32(5)).
				FontSize(sizeSmall).
				FontWeight(500)
			if index == *selected {
				segment.Background(colorBrand).TextColor(colorOnBrand)
			} else {
				segment.TextColor(colorTextFaint)
				if segment.Hovered() {
					segment.Background(colorHover)
				}
			}
			segment.Children(func() { ui.Text(c, labels[index]).SingleLine() })
		}
	})
	return parts.Track
}

// noteKind is what a note is telling the user: something that went wrong,
// something that went right, or nothing in particular.
type noteKind int

const (
	notePlain noteKind = iota
	noteWarning
	noteDanger
	noteSuccess
)

// noteColor is the color a kind of note is written in, and of the mark that
// stands for it.
func noteColor(kind noteKind) ui.Color {
	switch kind {
	case noteWarning:
		return colorWarning
	case noteDanger:
		return colorDanger
	case noteSuccess:
		return colorSuccess
	default:
		return colorTextMuted
	}
}

// notice draws one quiet line: a note under a control ("已保存。"), or the
// explanation a section keeps under its switches. It is text rather than a
// panel, because a sentence is not furniture.
func notice(c *ui.Context, kind noteKind, text string) {
	if text == "" {
		return
	}
	ui.Text(c, text).
		FontSize(sizeSmall).
		TextColor(noteColor(kind)).
		Wrap().
		LineHeight(1.5)
}

// noticePanel is the one note on a page that must not be missed: a tinted,
// bordered panel in the warning colors, whose body is a line of its own.
func noticePanel(c *ui.Context, title, body string) {
	ui.Column(c).FillWidth().
		Background(colorWarningFill).
		Border(1, colorWarningBorder).
		Radius(radiusField).
		Padding(float32(10), float32(12)).
		Children(func() {
			if title != "" {
				ui.Text(c, title).FontSize(sizeTiny).FontWeight(600).TextColor(colorWarning)
			}
			if body != "" {
				ui.Text(c, body).
					FontSize(sizeTiny).
					TextColor(colorWarning).
					Wrap().
					LineHeight(1.6)
			}
		})
}

// statusNote is the line that says whether a credential is already stored: a
// dot in the success color and what is stored, in the same color.
func statusNote(c *ui.Context, text string) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
		ui.Box(c).Size(float32(6), float32(6)).Radius(999).Shrink(0).Background(colorSuccess)
		ui.Text(c, text).FontSize(sizeBody).TextColor(colorSuccess).SingleLine()
	})
}

// kindBadge is the small label a list row carries: what the copy is, in the
// color its own mark is drawn in.
func kindBadge(c *ui.Context, tile, ink ui.Color, label string) {
	ui.Box(c).Shrink(0).
		Padding(float32(1), float32(6)).
		Radius(float32(4)).
		Background(tile).
		Children(func() {
			ui.Text(c, label).FontSize(sizeTiny).FontWeight(500).TextColor(ink).SingleLine()
		})
}

// kindTile is the rounded square at the head of a list row: the mark of what
// the copy is, on the tint of its kind.
func kindTile(c *ui.Context, mark *ui.SVG, tile, ink ui.Color) {
	ui.Box(c).Size(float32(28), float32(28)).Shrink(0).
		Radius(radiusTile).
		Background(tile).
		Center().
		Children(func() {
			ui.Icon(c, mark).Size(float32(14), float32(14)).Shrink(0).TextColor(ink)
		})
}

// hairline is the line a window draws between two of its regions, and between
// two sections of one: a single point of the border color.
//
// It stays inside the region it is drawn in, the way every other thing on the
// page does: a line that ran to the window's edge would step outside the
// margin the rest of the page keeps, and the page would have two edges.
func hairline(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(colorBorder)
}

// rowDivider is the fainter line between two entries of a list.
func rowDivider(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(colorHairline)
}
