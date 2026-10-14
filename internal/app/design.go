package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
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
	// colorSidebar is the face of the settings window's sidebar, the one
	// region of a window that is not the window's own white: the design gives
	// the navigation a face of its own, so that the page beside it reads as
	// the content and the column as the way around it.
	colorSidebar = ui.Hex("#FAFAF9")

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

	// colorReadingInk is how a reading above a word is written in the
	// translation window: the design's stone-500.
	colorReadingInk = ui.Hex("#78716C")
	// colorSkeleton is the face of a line that has not arrived yet.
	colorSkeleton = ui.Hex("#F5F5F4")

	// --- The translation card's own palette ------------------------------

	// The design is a stone scale rather than the slate one the rest of the
	// application uses, because the card is a piece of the page's own text
	// rather than a form: every color below is the one its class names.
	colorSourceInk = ui.Hex("#1C1917") // stone-900, the source text
	colorParticle  = ui.Hex("#A8A29E") // stone-400, a particle between words
	colorEyebrow   = ui.Hex("#A8A29E") // stone-400, the "原文" label
	colorBodyInk   = ui.Hex("#292524") // stone-800, the translation
	colorFooterInk = ui.Hex("#A8A29E") // stone-400, the line at the foot
	colorLoanword  = colorBrand        // the brand green, for a loanword

	// Each language's badge: the face and the ink the design gives it.
	badgeStoneFill, badgeStoneInk     = ui.Hex("#F5F5F4"), ui.Hex("#78716C") // 日本語
	badgeBlueFill, badgeBlueInk       = ui.Hex("#EFF6FF"), ui.Hex("#2563EB") // English
	badgeVioletFill, badgeVioletInk   = ui.Hex("#F5F3FF"), ui.Hex("#7C3AED") // 한국어
	badgeEmeraldFill, badgeEmeraldInk = ui.Hex("#ECFDF5"), colorBrand        // 中文

	// The foot of the card is the design's stone-50 at half strength over
	// the card's white, which is what the eye is shown: #FAFAF9 at 50%
	// over #FFFFFF.
	colorCardFoot = ui.Hex("#FCFCFC")
	// colorCardLine is the hairline the design draws inside the card:
	// stone-100 between the two blocks, stone-200/80 around it.
	colorCardLine = ui.Hex("#F5F5F4")
	// The annotation chips: stone-50 on a stone-100 edge, with the word in
	// stone-700 and its reading — or the loanword's own word — above it.
	colorChip    = ui.Hex("#FAFAF9") // stone-50
	colorChipInk = ui.Hex("#44403C") // stone-700

	// --- The quick window's own scale ------------------------------------
	//
	// The flyout hangs over the desktop rather than sitting on a page, and
	// the design draws it in stone: its chrome is not the slate the settings
	// window uses. Each name below is the design's own class, and the piece
	// that reads it is the one the design gives that value to.
	panelInk    = ui.Hex("#292524") // stone-800, a block's name
	panelNumber = ui.Hex("#1C1917") // stone-900, the number a block is about
	panelFaint  = ui.Hex("#A8A29E") // stone-400, times, labels, quiet actions
	panelBody   = ui.Hex("#78716C") // stone-500, the clipboard line
	panelLine   = ui.Hex("#F5F5F4") // stone-100, dividers and the footer's edge
	panelField  = ui.Hex("#FAFAF9") // stone-50, the clipboard row's face
	panelBorder = ui.Hex("#E7E5E4") // stone-200, its hairline
	panelHover  = ui.Hex("#F5F5F4") // stone-100, what a quiet action takes
	panelPress  = ui.Hex("#E7E5E4") // stone-200, and what it takes pressed
)

// The application's metrics, in points, taken from the design: every window
// keeps a 20-point page edge, a section keeps 12 points between its rows, and
// every control on a line is 36 points high.
const (
	// pageEdge is the window's own margin: the room the page keeps on every
	// side, and the room the footer's version keeps inside it.
	pageEdge = 14.0
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

	// The quick window's block, as the design draws it: 16 beside the block,
	// 14 above it and 12 below, with a 20-point mark at its head.
	quickEdge     = 16.0
	quickTop      = 14.0
	quickBottom   = 12.0
	quickTileSize = 20.0
	quickMarkSize = 12.0
	// And its own type: a 13-point name, 11-point times and labels, a
	// 22-point number, and the 10-point line over a bar.
	sizeQuickName   = 13.0
	sizeQuickFine   = 11.0
	sizeQuickNumber = 22.0
	sizeQuickTiny   = 10.0

	// The API key section is the page's dense one: the design packs a service
	// into two lines, so its head and the row inside each card are a size
	// down from the rest of the page. Every number below is that design's.
	sizeTileCompact     = 28.0 // its mark: w-7
	sizeTitleCompact    = 14.0 // the head's name
	sizeSubtitleCompact = 11.0 // the head's one line of explanation
	sizeCardName        = 13.0 // the name at the head of a card
	sizeFine            = 11.0 // a card's actions and the warning above them
	heightCardControl   = 32.0 // h-8: a card's field and the actions beside it
	cardActionPad       = 10.0 // px-2.5: the room beside an action's label

	// Typography: a section's name, its one line of explanation, and the
	// body text under them. They are the design's own sizes rather than
	// multiples of the theme's, because the two windows are dense.
	sizeTitle    = 15.0
	sizeSection  = 14.0
	sizeSubtitle = 12.0
	sizeBody     = 14.0
	sizeSmall    = 12.0
	sizeTiny     = 10.0

	// The translation card's own scale, read off the design's classes: the
	// source and the translation are 13px, and the labels and badges 10px.
	// Its actions are 14px marks in 4px of padding, which is what makes a
	// 22-point target out of a 14-point glyph.
	sizeCardLabel  = 10.0
	sizeCardSource = 13.0
	sizeCardTarget = 13.0
	sizeCardFoot   = 11.0
	sizeCardMark   = 14.0
	// The annotation chips: the word is 12px, the reading or the loanword's
	// own word above it 10px. The chip is stone-50 with a stone-100 edge,
	// cornered at 8, and 10 by 6 of its own padding.
	sizeChipWord = 12.0
	sizeChipNote = 10.0
	chipRadius   = 8.0
	chipPadX     = 10.0
	chipPadY     = 6.0
	// cardEdge is the design's px-3.5 and the vertical room of its blocks:
	// 14 on each side, 12 above the source block and 10 under it.
	cardEdge    = 14.0
	cardHeadTop = 12.0
	cardHeadEnd = 10.0
	cardBodyTop = 10.0
	cardBodyEnd = 10.0
	// cardFootTop and cardFootEnd are the room above and below the foot's own
	// line: the design's px-3 py-1.5 is 6, raised to 10 so that the line does
	// not sit on the card's bottom edge (the user asked for more room there,
	// and it is the settings window's own footer padding).
	cardFootTop = 10.0
	cardFootEnd = 10.0
	// cardMarkPad is the design's p-1 around an action's mark: 4 points.
	// With the 14-point glyph it makes the design's own 22-point target.
	cardMarkPad = 4.0

	// sizeIcon is the glyph of an action a card carries.
	sizeIcon = 20.0
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

// headSize is how large a section introduces itself: the tile its mark sits
// in, the room that mark keeps inside the tile, the room between the tile and
// the name beside it, and the two sizes of type.
type headSize struct {
	tile     float32
	padX     float32
	padY     float32
	gap      float32
	title    float32
	subtitle float32
}

// The two heads the settings page draws: the design's own, which every
// section but one wears, and the smaller one the API key section wears
// because the design packed each of its cards into two lines.
//
// The small head's mark is 28 where the design's is 32, and the room after it
// is 14 where that one's is 10: a smaller mark must not move the name, since
// the names of the sections are one column down the page (the property
// TestDesignSectionsArePeers keeps).
var (
	headRegular = headSize{tile: sizeTile, padX: 6, padY: 4, gap: 10, title: sizeTitle, subtitle: sizeSubtitle}
	headSmall   = headSize{tile: sizeTileCompact, padX: 5, padY: 3.5, gap: 14, title: sizeTitleCompact, subtitle: sizeSubtitleCompact}
)

// sectionHead is how a section of the settings window introduces itself: the
// mark of what it is about, its name, and one line saying what the section is
// for. It binds head to the box it drew, which is what tells the sidebar
// where the section's heading is.
//
// A section's mark is drawn in the color its own thing is drawn in and is not
// dressed up: it keeps its own shape and proportions wherever it appears. A
// section with nothing of its own — the clipboard, a group still to come —
// carries the application's mark in the muted stone instead.
func sectionHead(c *ui.Context, head *ui.Handle, mark *ui.SVG, ink ui.Color, title, subtitle string) {
	sectionHeadSized(c, head, mark, ink, title, subtitle, headRegular)
}

// sectionHeadSmall is the same head a size down: a 28-point tile and the
// design's own 14/11 pair. The API key section wears it because its cards are
// dense, and a head a size above them would be the largest thing on the page.
func sectionHeadSmall(c *ui.Context, head *ui.Handle, mark *ui.SVG, ink ui.Color, title, subtitle string) {
	sectionHeadSized(c, head, mark, ink, title, subtitle, headSmall)
}

// sectionHeadSized draws the head itself, at one of the two sizes.
func sectionHeadSized(c *ui.Context, head *ui.Handle, mark *ui.SVG, ink ui.Color, title, subtitle string, size headSize) {
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(size.gap)
	if head != nil {
		row.Bind(head)
	}
	row.Children(func() {
		if mark != nil {
			ui.Box(c).Children(func() {
				ui.Icon(c, mark).
					Height(size.tile).
					Width(size.tile).
					Padding(size.padY, size.padX).
					Shrink(0).
					TextColor(ink)
			}).Background(colorMuted).Radius(radiusTile)
		}
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, title).FontSize(size.title).FontWeight(600).TextColor(colorTextSoft).SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(size.subtitle).TextColor(colorTextMuted).SingleLine()
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

// fieldFrame is inputFrame at the size a card's own row has room for: 32
// points high with 10 beside the text, which is what leaves the rest of the
// line to the actions that act on the value in it.
//
// The field is the thing a dense card is about, so it takes the room that is
// left rather than a width of its own: the caller grows it.
func fieldFrame(c *ui.Context, input ui.Element) ui.Element {
	return input.
		Height(heightCardControl).
		Padding(0, float32(10)).
		Radius(radiusField).
		Background(colorField).
		Border(1, colorBorder).
		FontSize(sizeSmall).
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

// buttonSize is how much room a button takes: the height every control on a
// form row shares, the room beside its label, and the size of that label.
type buttonSize struct {
	height float32
	pad    float32
	font   float32
}

// The two sizes the page draws: the design's own, and the one the API key
// cards use, where a field and two actions share one 32-point line.
var (
	buttonRegular = buttonSize{height: heightControl, pad: 16, font: sizeBody}
	buttonDense   = buttonSize{height: heightCardControl, pad: cardActionPad, font: sizeFine}
)

// designButton builds an action in one of the tones. Every tone keeps the
// whole behavior of a button — the pointer, the keyboard, the focus ring and
// what assistive technology reads — since both are the framework's ButtonBase
// underneath.
func designButton(c *ui.Context, label string, tone buttonTone) ui.Element {
	return buttonSized(c, label, tone, buttonRegular)
}

// cardButton is designButton in the size a dense card's row has room for: the
// design's h-8 with 10 points beside the label. It is what stands next to a
// card's field, where the three things on the line have to be one row.
func cardButton(c *ui.Context, label string, tone buttonTone) ui.Element {
	return buttonSized(c, label, tone, buttonDense)
}

// buttonSized draws the button itself, at one of the two sizes.
func buttonSized(c *ui.Context, label string, tone buttonTone, size buttonSize) ui.Element {
	b := ui.ButtonBase(c).Label(label)
	switch tone {
	case buttonPrimary:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(radiusField).
			Background(colorBrand).
			TextColor(colorOnBrand).
			FontSize(size.font).
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
			Height(size.height).
			Padding(0, size.pad).
			Radius(radiusField).
			Background(colorSurface).
			Border(1, colorBorder).
			TextColor(colorText).
			FontSize(size.font).
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
	b.Children(func() { ui.Text(c, label).FontSize(size.font).FontWeight(500).SingleLine() })
	return b
}

// textAction is an action written as a word rather than drawn as a button: the
// quiet "删除" at the end of a card's head. It wears no face of its own,
// because the two buttons under it are what the card is for, and a third one
// beside the name would read as another thing the card does; its ink says what
// it does. It answers the pointer over its own room, as every action here does.
func textAction(c *ui.Context, label string, ink ui.Color) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Radius(float32(4)).
		Padding(float32(2), float32(6)).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(colorHover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(sizeFine).FontWeight(500).TextColor(ink).SingleLine()
	})
	return b
}

// quietIconButton is an action drawn as a mark: one glyph with a square of the
// content's own room around it, drawn in the faint ink the design gives a
// secondary action and in the body ink under the pointer. The room is what
// makes it a target rather than a picture; its label is what it means, for the
// pointer that rests on it and for assistive technology.
//
// It is the quick window's footer action: the design's p-1 around a 14-point
// mark, on the stone face the rest of that window is drawn on.
func quietIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(cardMarkPad).
		Padding(cardMarkPad).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(panelPress)
	case b.Hovered() || b.Focused():
		b = b.Background(panelHover)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(sizeCardMark), float32(sizeCardMark)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(colorTextSoft)
		} else {
			icon.TextColor(panelFaint)
		}
	})
	return b
}

// declinesInput takes no input at all, and is what a card's actions are given.
//
// The foot around them takes every pointer event as it comes, to be the handle
// the card is dragged by, and the framework gives a press to the innermost
// element that handles input as it comes — so an action without this would
// have its clicks swallowed by the row it sits on, and the button would never
// press. Saying nothing and letting the event go on is the framework's own
// "shield", and it is what makes an action inside a drag handle work.
func declinesInput(ui.InputEvent) bool { return false }

// cardIconButton is the action a translation card carries: a 14-point mark in
// 4 points of padding, which is what makes a 22-point target of it — the
// design's p-1 and w-3.5 h-3.5.
//
// Its two states are the design's: under the pointer it takes a faint stone
// face and the ink deepens. The glyph keeps the muted ink of a secondary
// action otherwise.
func cardIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(cardMarkPad).
		Cursor(ui.CursorPointer).
		HandleInput(declinesInput)
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(colorCardLine)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(sizeCardMark), float32(sizeCardMark)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(colorTextSoft)
		} else {
			icon.TextColor(colorEyebrow)
		}
	})
	return b
}

// cardIconToggle is cardIconButton with an on state: the mark keeps the
// theme's green while what it stands for is on, which is the whole of what it
// has to say. A pin that is pressed looks different from one that is not.
//
// The green is read from the theme rather than named here, because that is
// what it is: the theme's accent, which the design happens to spell as the
// same #3B693D the rest of the application calls the brand green.
func cardIconToggle(c *ui.Context, mark *ui.SVG, label string, on bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(cardMarkPad).
		Cursor(ui.CursorPointer).
		HandleInput(declinesInput)
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case on:
		b = b.Background(colorCardLine)
	case b.Hovered() || b.Focused():
		b = b.Background(colorCardLine)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(sizeCardMark), float32(sizeCardMark)).Shrink(0)
		if on {
			icon.TextColor(c.Theme().Accent)
		} else if b.Hovered() || b.Focused() {
			icon.TextColor(colorTextSoft)
		} else {
			icon.TextColor(colorEyebrow)
		}
	})
	return b
}

// annotationChips is the annotation under the translation: one chip per word
// that carries something, in the order the words appear, packed from the left
// and wrapping.
//
// A chip is the word with the reading over it — or, for a loanword, the word
// it came from, in the brand green. It is a chip rather than a line of ruby
// over the whole sentence because what is drawn is not the sentence: the
// sentence is above, and these are the words a reader cannot read. The face
// and the corner are the design's stone-50 on stone-100 at 8.
func annotationChips(c *ui.Context, words []domain.Segment, showReading, showGloss bool) {
	ui.Row(c).FillWidth().Wrap().Justify(ui.Start).Gap(float32(6)).Children(func() {
		for _, segment := range words {
			if strings.TrimSpace(segment.Text) == "" {
				continue
			}
			note, loanword := annotationNote(segment, showReading, showGloss)
			annotationChip(c, segment.Text, note, loanword)
		}
	})
}

// annotationChip is one of those chips: the word, and above it what there is
// to say about it — a reading in stone-500, or a loanword's own word in the
// brand green.
func annotationChip(c *ui.Context, word, note string, loanword bool) {
	ui.Column(c).Shrink(0).AlignItems(ui.Center).
		Background(colorChip).
		Border(1, colorCardLine).
		Radius(chipRadius).
		Padding(chipPadY, chipPadX, chipPadY, chipPadX).
		Children(func() {
			if note != "" {
				text := ui.Text(c, note).
					FontSize(sizeChipNote).
					TextColor(colorReadingInk).
					SingleLine()
				if loanword {
					text.TextColor(colorLoanword).FontWeight(500)
				}
				text.FixedLineHeight(float32(sizeChipNote + 2)).Shrink(0)
				ui.Spacer(c).Height(float32(4)).Shrink(0)
			}
			ui.Text(c, word).
				FontSize(sizeChipWord).
				TextColor(colorChipInk).
				SingleLine().
				Shrink(0)
		})
}

// annotationBlock is the card's third block, under the translation: the row
// that opens the annotation, and the chips it holds once it is open.
//
// It is its own block — its own hairline above it — because it is its own
// thing: something the user asks for, that costs a request, and that is not
// part of the translation above it.
//
// The row runs the card's own width, edge to edge, so that the face it takes
// under the pointer covers the row rather than the label in the middle of it.
// The label and the chevron keep the card's 14 points of edge inside it, which
// is what the design's own closed row does with `w-full … px-3.5`.
func annotationBlock(c *ui.Context, open bool, label string, toggle func()) {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(open).
		FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Radius(0).
		Padding(float32(8), float32(cardEdge), float32(8), float32(cardEdge)).
		HandleInput(declinesInput)
	switch {
	case button.Pressed():
		button = button.Background(colorActive)
	case button.Hovered():
		button = button.Background(colorChip)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(sizeCardFoot).TextColor(colorReadingInk).SingleLine()
		disclosureArrow(c, arrowTurn(open), colorReadingInk)
	})
	if button.Clicked() && toggle != nil {
		toggle()
	}
}

// arrowTurn is how far the annotation row's chevron is turned: pointing down
// while closed, up while open, which is the design's own pair.
func arrowTurn(open bool) float32 {
	if open {
		return -90
	}
	return 90
}

// langBadge is the small label that names a language: the one piece of the
// translation window that says what is being read. It wears the design's own
// pair for the language — 日本語 stone, English blue, 한국어 violet, 中文
// emerald — in 10-point type with 1 point of vertical and 6 of horizontal
// room.
func langBadge(c *ui.Context, name string, code domain.Language) {
	if strings.TrimSpace(name) == "" {
		return
	}
	fill, ink := langBadgeColors(code)
	ui.Box(c).Shrink(0).
		Padding(float32(1), float32(6)).
		Radius(float32(4)).
		Background(fill).
		Children(func() {
			ui.Text(c, name).FontSize(sizeCardLabel).FontWeight(500).TextColor(ink).SingleLine()
		})
}

// langBadgeColors is the pair a language's badge is drawn with, as the design
// writes it.
func langBadgeColors(code domain.Language) (fill, ink ui.Color) {
	switch code {
	case domain.LangChinese:
		return badgeEmeraldFill, badgeEmeraldInk
	case domain.LangEnglish:
		return badgeBlueFill, badgeBlueInk
	case domain.LangJapanese:
		return badgeStoneFill, badgeStoneInk
	case domain.LangKorean:
		return badgeVioletFill, badgeVioletInk
	default:
		return badgeStoneFill, badgeStoneInk
	}
}

// eyebrow is the small label above a block of text: "原文" or "译文". It is
// written the way the design writes it — 10 points, medium, uppercase and
// letter-spaced, in stone-400 — so that it names the block without competing
// with it.
func eyebrow(c *ui.Context, label string) {
	ui.Text(c, strings.ToUpper(label)).
		FontSize(sizeCardLabel).
		FontWeight(500).
		TextColor(colorEyebrow).
		SingleLine().
		Shrink(0)
}

// skeletonBar is a placeholder line: the shape of the text that is on its
// way, so that a window waiting for an answer looks like a window rather than
// like a mistake. It is measured in the card's own width, which the card
// passes in.
func skeletonBar(c *ui.Context, width, fraction float32) {
	ui.Box(c).
		Height(float32(12)).
		Width(width * fraction).
		Radius(float32(4)).
		Background(colorSkeleton)
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
// bordered strip in the warning colors, with a single small line in it. It is
// a panel rather than another line of body text, and the design draws it tight
// — 6 above and below — because the rows under it are what the page is for.
func noticePanel(c *ui.Context, text string) {
	ui.Box(c).FillWidth().
		Background(colorWarningFill).
		Border(1, colorWarningBorder).
		Radius(radiusField).
		Padding(float32(6), float32(10)).
		Children(func() {
			ui.Text(c, text).
				FontSize(sizeFine).
				TextColor(colorWarning).
				Wrap().
				LineHeight(1.4)
		})
}

// cardHint is the smallest line a card carries: where the value above it comes
// from. It is quieter than a notice, which is about what just happened.
func cardHint(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(sizeTiny).TextColor(colorTextMuted).SingleLine()
}

// cardNote is the line a block's own head carries when one more line is needed
// there: what an account is, when its window resets. The design gives that line
// 11 points of faint ink, and the quick window's own stone.
func cardNote(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(sizeQuickFine).TextColor(panelFaint).SingleLine()
}

// barHeight is how tall a proportion bar is: the design's h-1.5, which is a
// line rather than a block.
const barHeight = 6.0

// progressBar draws a proportion: the whole of the room it is given as a
// track, the part of it that is filled, and the number it stands for over the
// track at its right end.
//
// The fill is painted rather than laid out: it is a fraction of whatever room
// the track ends up with, and only the paint knows that width — a fill laid
// out from the frame before would show the previous answer's share. What it
// is, for assistive technology, is its name, its range and its value.
//
// The color is the block's own, for the fill and for the number alike: a bar
// belongs to the block it is in, and the number over it is that same color —
// which is why it is one color and not two. It is drawn on the content's own
// face, so the color has to read there: a bar whose color is the tile's own
// white would be a bar with no number at all.
//
// A share it does not have — nothing left, nothing read yet — draws the track
// empty. An empty track is not "0%": the caller says which of the two it is in
// words.
func progressBar(c *ui.Context, percent int64, fill ui.Color, label, name string) {
	ui.Column(c).FillWidth().Gap(float32(4)).AlignItems(ui.End).Children(func() {
		// ui.Text(c, label).FontSize(sizeQuickTiny).FontWeight(500).TextColor(fill).SingleLine()
		ui.Box(c).FillWidth().
			Height(float32(barHeight)).
			Radius(999).
			Background(panelLine).
			Role(ui.RoleMeter).
			Range(0, 100, float64(percent)).
			Label(name).
			Draw(func(p *ui.Painter, r ui.Rect) {
				share := percent
				switch {
				case share <= 0:
					return
				case share > 100:
					share = 100
				}
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(share) / 100, H: r.H}, fill, 999)
			})
	})
}

// panelMark is how a block's mark is drawn: the provider's own drawing, the
// face of the tile it sits on, the color the drawing is made in, and the color
// of a proportion bar in that block.
//
// The tile's ink and the bar's color are two different things and are kept
// apart: the drawing on a tile is white (the tile is the provider's own color,
// and white is what reads on it), and a bar is drawn on the content's face,
// where white reads as nothing at all.
type panelMark struct {
	svg  *ui.SVG
	tile ui.Color
	ink  ui.Color
	bar  ui.Color
}

// quickBlock is one block of the quick window: the design's own room around
// it, which is what makes a block read as one thing rather than as rows that
// happen to follow each other.
func quickBlock(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().
		Padding(quickTop, quickEdge, quickBottom, quickEdge).
		Children(body)
}

// quickDivider is the line between two blocks of the quick window: one point
// of stone-100, inside the same edge the blocks keep.
func quickDivider(c *ui.Context) {
	ui.Column(c).FillWidth().PaddingX(quickEdge).Children(func() {
		ui.Row(c).FillWidth().Height(1).Shrink(0).Background(panelLine)
	})
}

// quickTile is the mark at the head of a block: the provider's drawing in a
// tile of its own, so that a block is recognised before its name is read. A
// block whose provider has no drawing gets the tile alone, so that two blocks'
// names still start at the same place.
func quickTile(c *ui.Context, mark panelMark) {
	ui.Box(c).Size(quickTileSize, quickTileSize).Shrink(0).
		Radius(radiusTile).
		Background(mark.tile).
		Center().
		Children(func() {
			if mark.svg == nil {
				return
			}
			ui.Icon(c, mark.svg).Size(quickMarkSize, quickMarkSize).Shrink(0).TextColor(mark.ink)
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
