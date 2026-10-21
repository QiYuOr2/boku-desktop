package presentation

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

// palette is one appearance of the design system: every color the
// application draws with, chosen together.
//
// There are two, and they are the same design rather than two designs. A
// name means the same thing in both — colorSurface is the window's face in
// either appearance — and only its value changes. The light palette is the
// one the design's class names spell; the dark one is that design on a dark
// page: the same slate/stone relationships, the same brand green, with the
// surfaces inverted and the inks raised until they read.
//
// A third appearance would be a third palette. Nothing in the views knows
// which one is installed; they ask for a name, and design.go decides what
// that name is worth in the appearance the user chose.
type Palette struct {
	Anime, Bilibili     ReadingAccent
	CodexTile, CodexInk ui.Color
	// Faces.
	surface, sidebar, field, muted, hover, active, rowAlt ui.Color
	border, Hairline                                      ui.Color

	// Text.
	text, textSoft, textBody, textMuted, textFaint, inputValue ui.Color

	// Brand and status. onBrand is the ink on a brand fill; onMark is the
	// white a provider's drawing is made of inside its own colored tile.
	brand, brandHover, brandPressed, onBrand, onMark ui.Color
	// brandTint and brandTintStrong are the brand at the two strengths a
	// selected row is washed in.
	brandTint, brandTintStrong ui.Color

	success, warning, danger                                 ui.Color
	warningFill, warningBorder, warningHover, warningPressed ui.Color

	// The colors a recorded copy is drawn in: a tile and its ink.
	skyTile, skyInk, blueTile, blueInk           ui.Color
	violetTile, violetInk, amberTile, amberInk   ui.Color
	emeraldTile, emeraldInk, stoneTile, stoneInk ui.Color

	readingInk, skeleton ui.Color
	// tileEdge outlines a provider's own tile when its brand color would
	// otherwise sit against a surface of the same darkness. It is nothing
	// in the light appearance.
	tileEdge ui.Color

	// The translation card's own palette.
	sourceInk, particle, Eyebrow, bodyInk, footerInk ui.Color
	BadgeStoneFill, BadgeStoneInk                    ui.Color
	BadgeBlueFill, BadgeBlueInk                      ui.Color
	BadgeVioletFill, BadgeVioletInk                  ui.Color
	BadgeEmeraldFill, BadgeEmeraldInk                ui.Color
	cardFoot, cardLine, chip, chipInk                ui.Color

	// The quick window's own scale.
	PanelInk, PanelNumber, PanelFaint, PanelBody ui.Color
	PanelLine, PanelField, PanelBorder           ui.Color
	PanelHover, PanelPress                       ui.Color
}

// lightPalette is the design as its own classes write it: white pages, slate
// borders, stone inks and the brand green on top.
var LightPalette = Palette{
	CodexTile: ui.Hex("#111111"), CodexInk: ui.Hex("#FFFFFF"),
	Anime: ReadingAccent{
		Ink: ui.Hex("#B74F5B"), Tint: ui.Hex("#FFF5F6"), Border: ui.Hex("#F9DDE0"),
		Hover: ui.Hex("#FFECEF"), HoverBorder: ui.Hex("#F4B8BE"),
		Tile: ui.Hex("#EA9199"), TileInk: ui.Hex("#FFFFFF"), BadgeFill: ui.Hex("#B74F5B"), BadgeInk: ui.Hex("#FFFFFF"),
	},
	Bilibili: ReadingAccent{
		Ink: ui.Hex("#BF315C"), Tint: ui.Hex("#FFF0F3"), Border: ui.Hex("#FCD5E0"),
		Hover: ui.Hex("#FFE8EF"), HoverBorder: ui.Hex("#F4B0C5"),
		Tile: ui.Hex("#FFF0F3"), TileInk: ui.Hex("#BF315C"), BadgeFill: ui.Hex("#C93666"), BadgeInk: ui.Hex("#FFFFFF"),
	},
	surface: ui.Hex("#FFFFFF"),
	sidebar: ui.Hex("#FAFAF9"),
	field:   ui.Hex("#FAFAF9"),
	muted:   ui.Hex("#F5F5F4"),
	hover:   ui.Hex("#F5F5F4"),
	active:  ui.Hex("#E7E5E4"),
	rowAlt:  ui.Hex("#F8FAFC"),

	border:   ui.Hex("#E2E8F0"),
	Hairline: ui.Hex("#F5F5F4"),

	text:       ui.Hex("#0F172A"),
	textSoft:   ui.Hex("#1C1917"),
	textBody:   ui.Hex("#44403C"),
	textMuted:  ui.Hex("#64748B"),
	textFaint:  ui.Hex("#78716C"),
	inputValue: ui.Hex("#57534E"),

	brand:           ui.Hex("#3B693D"),
	brandHover:      ui.Hex("#2F5631"),
	brandPressed:    ui.Hex("#294A2B"),
	onBrand:         ui.Hex("#FFFFFF"),
	onMark:          ui.Hex("#FFFFFF"),
	brandTint:       ui.RGBA(59, 105, 61, 0.06),
	brandTintStrong: ui.RGBA(59, 105, 61, 0.10),

	success:        ui.Hex("#059669"),
	warning:        ui.Hex("#B45309"),
	danger:         ui.Hex("#DC2626"),
	warningFill:    ui.Hex("#FFFBEB"),
	warningBorder:  ui.Hex("#FDE68A"),
	warningHover:   ui.Hex("#FEF3C7"),
	warningPressed: ui.Hex("#FDE68A"),

	skyTile:     ui.Hex("#F0F9FF"),
	skyInk:      ui.Hex("#0EA5E9"),
	blueTile:    ui.Hex("#EFF6FF"),
	blueInk:     ui.Hex("#3B82F6"),
	violetTile:  ui.Hex("#F5F3FF"),
	violetInk:   ui.Hex("#8B5CF6"),
	amberTile:   ui.Hex("#FFFBEB"),
	amberInk:    ui.Hex("#D97706"),
	emeraldTile: ui.Hex("#ECFDF5"),
	emeraldInk:  ui.Hex("#047857"),
	stoneTile:   ui.Hex("#F5F5F4"),
	stoneInk:    ui.Hex("#57534E"),

	readingInk: ui.Hex("#78716C"),
	skeleton:   ui.Hex("#F5F5F4"),
	tileEdge:   ui.Transparent,

	sourceInk: ui.Hex("#1C1917"),
	particle:  ui.Hex("#A8A29E"),
	Eyebrow:   ui.Hex("#A8A29E"),
	bodyInk:   ui.Hex("#292524"),
	footerInk: ui.Hex("#A8A29E"),

	BadgeStoneFill:   ui.Hex("#F5F5F4"),
	BadgeStoneInk:    ui.Hex("#78716C"),
	BadgeBlueFill:    ui.Hex("#EFF6FF"),
	BadgeBlueInk:     ui.Hex("#2563EB"),
	BadgeVioletFill:  ui.Hex("#F5F3FF"),
	BadgeVioletInk:   ui.Hex("#7C3AED"),
	BadgeEmeraldFill: ui.Hex("#ECFDF5"),
	BadgeEmeraldInk:  ui.Hex("#3B693D"),

	cardFoot: ui.Hex("#FCFCFC"),
	cardLine: ui.Hex("#F5F5F4"),
	chip:     ui.Hex("#FAFAF9"),
	chipInk:  ui.Hex("#44403C"),

	PanelInk:    ui.Hex("#292524"),
	PanelNumber: ui.Hex("#1C1917"),
	PanelFaint:  ui.Hex("#A8A29E"),
	PanelBody:   ui.Hex("#78716C"),
	PanelLine:   ui.Hex("#F5F5F4"),
	PanelField:  ui.Hex("#FAFAF9"),
	PanelBorder: ui.Hex("#E7E5E4"),
	PanelHover:  ui.Hex("#F5F5F4"),
	PanelPress:  ui.Hex("#E7E5E4"),
}

// darkPalette is the same design at night. The page is the darkest surface
// and every other face is a step above it — a field, a tile, a hover — so
// that the light design's "one surface, lines drawn on it" still reads: the
// lines are one step lighter than the page, and nothing is a bright band.
//
// The brand green is the lighter one the framework's dark accent already
// used (#6FA776), because #3B693D would sink into a #18181B page; the ink on
// it turns dark for the same reason. The kind tiles are the light families
// at their 950 end, and their inks at the 300 end, so a recorded copy keeps
// the color that says what it is without lighting up the list.
var DarkPalette = Palette{
	CodexTile: ui.Hex("#E7E5E4"), CodexInk: ui.Hex("#18181B"),
	Anime: ReadingAccent{
		Ink: ui.Hex("#F0A9B2"), Tint: ui.Hex("#2B2228"), Border: ui.Hex("#49313B"),
		Hover: ui.Hex("#392932"), HoverBorder: ui.Hex("#80505E"),
		Tile: ui.Hex("#49313B"), TileInk: ui.Hex("#F0A9B2"), BadgeFill: ui.Hex("#713444"), BadgeInk: ui.Hex("#FFE4E9"),
	},
	Bilibili: ReadingAccent{
		Ink: ui.Hex("#F694B2"), Tint: ui.Hex("#2C2128"), Border: ui.Hex("#50313F"),
		Hover: ui.Hex("#3B2631"), HoverBorder: ui.Hex("#91516B"),
		Tile: ui.Hex("#50313F"), TileInk: ui.Hex("#F694B2"), BadgeFill: ui.Hex("#81304D"), BadgeInk: ui.Hex("#FFE4EC"),
	},
	surface: ui.Hex("#18181B"),
	sidebar: ui.Hex("#1E1E22"),
	field:   ui.Hex("#202024"),
	muted:   ui.Hex("#27272A"),
	hover:   ui.Hex("#27272A"),
	active:  ui.Hex("#303035"),
	rowAlt:  ui.Hex("#1F1F23"),

	border:   ui.Hex("#34343A"),
	Hairline: ui.Hex("#27272A"),

	text:       ui.Hex("#F4F4F5"),
	textSoft:   ui.Hex("#FAFAF9"),
	textBody:   ui.Hex("#D4D4D8"),
	textMuted:  ui.Hex("#A1A1AA"),
	textFaint:  ui.Hex("#A8A29E"),
	inputValue: ui.Hex("#D6D3D1"),

	brand:           ui.Hex("#6FA776"),
	brandHover:      ui.Hex("#84BA8A"),
	brandPressed:    ui.Hex("#5C8F63"),
	onBrand:         ui.Hex("#10240F"),
	onMark:          ui.Hex("#FFFFFF"),
	brandTint:       ui.RGBA(111, 167, 118, 0.16),
	brandTintStrong: ui.RGBA(111, 167, 118, 0.22),

	success:        ui.Hex("#34D399"),
	warning:        ui.Hex("#FBBF24"),
	danger:         ui.Hex("#F87171"),
	warningFill:    ui.Hex("#2A2113"),
	warningBorder:  ui.Hex("#5C4318"),
	warningHover:   ui.Hex("#332818"),
	warningPressed: ui.Hex("#3D301C"),

	skyTile:     ui.Hex("#082F49"),
	skyInk:      ui.Hex("#38BDF8"),
	blueTile:    ui.Hex("#172554"),
	blueInk:     ui.Hex("#93C5FD"),
	violetTile:  ui.Hex("#2E1065"),
	violetInk:   ui.Hex("#C4B5FD"),
	amberTile:   ui.Hex("#451A03"),
	amberInk:    ui.Hex("#FCD34D"),
	emeraldTile: ui.Hex("#022C22"),
	emeraldInk:  ui.Hex("#6EE7B7"),
	stoneTile:   ui.Hex("#292524"),
	stoneInk:    ui.Hex("#D6D3D1"),

	readingInk: ui.Hex("#A8A29E"),
	skeleton:   ui.Hex("#27272A"),
	tileEdge:   ui.Hex("#3F3F46"),

	sourceInk: ui.Hex("#FAFAF9"),
	particle:  ui.Hex("#78716C"),
	Eyebrow:   ui.Hex("#A8A29E"),
	bodyInk:   ui.Hex("#E7E5E4"),
	footerInk: ui.Hex("#A8A29E"),

	BadgeStoneFill:   ui.Hex("#292524"),
	BadgeStoneInk:    ui.Hex("#D6D3D1"),
	BadgeBlueFill:    ui.Hex("#172554"),
	BadgeBlueInk:     ui.Hex("#93C5FD"),
	BadgeVioletFill:  ui.Hex("#2E1065"),
	BadgeVioletInk:   ui.Hex("#C4B5FD"),
	BadgeEmeraldFill: ui.Hex("#022C22"),
	BadgeEmeraldInk:  ui.Hex("#6FA776"),

	cardFoot: ui.Hex("#1F1F23"),
	cardLine: ui.Hex("#27272A"),
	chip:     ui.Hex("#202024"),
	chipInk:  ui.Hex("#D6D3D1"),

	PanelInk:    ui.Hex("#E7E5E4"),
	PanelNumber: ui.Hex("#FAFAF9"),
	PanelFaint:  ui.Hex("#A8A29E"),
	PanelBody:   ui.Hex("#A8A29E"),
	PanelLine:   ui.Hex("#292524"),
	PanelField:  ui.Hex("#22201E"),
	PanelBorder: ui.Hex("#3A3735"),
	PanelHover:  ui.Hex("#292524"),
	PanelPress:  ui.Hex("#3A3735"),
}

// The colors every view asks for, as the current appearance spells them.
//
// They are package values rather than parameters because the design system
// is one namespace that every window draws from, and MyGo builds a window's
// view as one synchronous pass on the main thread: applyDesign installs the
// appearance at the top of that pass, and every name below is read after it.
// A window therefore cannot be half light and half dark, and a background
// goroutine never reads these — views are the only readers.
var (
	// --- Faces -----------------------------------------------------------

	// colorSurface is the window's own face. Every window is drawn on it —
	// content, header lines and footer alike — so a window reads as one
	// surface with lines drawn on it rather than as a stack of bands.
	ColorSurface = LightPalette.surface
	// colorField is the face of an input, and colorMuted the face of a tile
	// that is not colored by what it stands for.
	ColorField = LightPalette.field
	ColorMuted = LightPalette.muted
	// colorHover and colorActive are what a row or a button takes under the
	// pointer and while it is pressed.
	ColorHover  = LightPalette.hover
	ColorActive = LightPalette.active
	ColorRowAlt = LightPalette.rowAlt
	// colorSidebar is the face of the settings window's sidebar, the one
	// region of a window that is not the window's own white: the design gives
	// the navigation a face of its own, so that the page beside it reads as
	// the content and the column as the way around it.
	ColorSidebar = LightPalette.sidebar

	ColorBorder = LightPalette.border
	// colorHairline is the line between two entries of a list: fainter than
	// a border, because a list is one thing rather than a row of them.
	ColorHairline = LightPalette.Hairline

	// --- Text ------------------------------------------------------------

	ColorText       = LightPalette.text
	ColorTextSoft   = LightPalette.textSoft
	ColorTextBody   = LightPalette.textBody
	ColorTextMuted  = LightPalette.textMuted
	ColorTextFaint  = LightPalette.textFaint
	ColorInputValue = LightPalette.inputValue

	// --- Brand and status ------------------------------------------------

	// colorBrand is the green of the one action a form is for, and of every
	// other place the application asks for attention. It is the theme's
	// accent too, so a checked box and a picked segment come along with it.
	ColorBrand        = LightPalette.brand
	ColorBrandHover   = LightPalette.brandHover
	ColorBrandPressed = LightPalette.brandPressed
	ColorOnBrand      = LightPalette.onBrand
	// colorOnMark is the white of a provider's own mark inside its tile. It
	// stays white in both appearances: the tile is the brand's color, and
	// the drawing on it is not text on the application's green.
	ColorOnMark          = LightPalette.onMark
	ColorBrandTint       = LightPalette.brandTint
	ColorBrandTintStrong = LightPalette.brandTintStrong

	ColorSuccess = LightPalette.success
	ColorWarning = LightPalette.warning
	ColorDanger  = LightPalette.danger

	// The warning panel: the one note on a page the user should not be able
	// to miss.
	ColorWarningFill    = LightPalette.warningFill
	ColorWarningBorder  = LightPalette.warningBorder
	ColorWarningHover   = LightPalette.warningHover
	ColorWarningPressed = LightPalette.warningPressed

	// --- The colors a recorded copy is drawn in --------------------------

	// Each is a pair: the face of the tile at the head of the row, and the
	// color the mark and the kind's badge are drawn in.
	ColorSkyTile, ColorSkyInk         = LightPalette.skyTile, LightPalette.skyInk
	ColorBlueTile, ColorBlueInk       = LightPalette.blueTile, LightPalette.blueInk
	ColorVioletTile, ColorVioletInk   = LightPalette.violetTile, LightPalette.violetInk
	ColorAmberTile, ColorAmberInk     = LightPalette.amberTile, LightPalette.amberInk
	ColorEmeraldTile, ColorEmeraldInk = LightPalette.emeraldTile, LightPalette.emeraldInk
	ColorStoneTile, ColorStoneInk     = LightPalette.stoneTile, LightPalette.stoneInk

	// colorReadingInk is how a reading above a word is written in the
	// translation window: the design's stone-500.
	ColorReadingInk = LightPalette.readingInk
	// colorSkeleton is the face of a line that has not arrived yet.
	ColorSkeleton = LightPalette.skeleton
	// colorTileEdge outlines a provider's tile where its own color would
	// otherwise disappear into the page. The light appearance has none.
	ColorTileEdge = LightPalette.tileEdge

	// --- The translation card's own palette ------------------------------

	// The design is a stone scale rather than the slate one the rest of the
	// application uses, because the card is a piece of the page's own text
	// rather than a form: every color below is the one its class names.
	ColorSourceInk = LightPalette.sourceInk // stone-900, the source text
	ColorParticle  = LightPalette.particle  // stone-400, a particle between words
	ColorEyebrow   = LightPalette.Eyebrow   // stone-400, the "原文" label
	ColorBodyInk   = LightPalette.bodyInk   // stone-800, the translation
	ColorFooterInk = LightPalette.footerInk // stone-400, the line at the foot
	ColorLoanword  = LightPalette.brand     // the brand green, for a loanword

	// Each language's badge: the face and the ink the design gives it.
	BadgeStoneFill, BadgeStoneInk     = LightPalette.BadgeStoneFill, LightPalette.BadgeStoneInk     // 日本語
	BadgeBlueFill, BadgeBlueInk       = LightPalette.BadgeBlueFill, LightPalette.BadgeBlueInk       // English
	BadgeVioletFill, BadgeVioletInk   = LightPalette.BadgeVioletFill, LightPalette.BadgeVioletInk   // 한국어
	BadgeEmeraldFill, BadgeEmeraldInk = LightPalette.BadgeEmeraldFill, LightPalette.BadgeEmeraldInk // 中文

	// The foot of the card is the design's stone-50 at half strength over
	// the card's white, which is what the eye is shown: #FAFAF9 at 50%
	// over #FFFFFF.
	ColorCardFoot = LightPalette.cardFoot
	// colorCardLine is the hairline the design draws inside the card:
	// stone-100 between the two blocks, stone-200/80 around it.
	ColorCardLine = LightPalette.cardLine
	// The annotation chips: stone-50 on a stone-100 edge, with the word in
	// stone-700 and its reading — or the loanword's own word — above it.
	ColorChip    = LightPalette.chip    // stone-50
	ColorChipInk = LightPalette.chipInk // stone-700

	// --- The quick window's own scale ------------------------------------
	//
	// The flyout hangs over the desktop rather than sitting on a page, and
	// the design draws it in stone: its chrome is not the slate the settings
	// window uses. Each name below is the design's own class, and the piece
	// that reads it is the one the design gives that value to.
	PanelInk    = LightPalette.PanelInk    // stone-800, a block's name
	PanelNumber = LightPalette.PanelNumber // stone-900, the number a block is about
	PanelFaint  = LightPalette.PanelFaint  // stone-400, times, labels, quiet actions
	PanelBody   = LightPalette.PanelBody   // stone-500, the clipboard line
	PanelLine   = LightPalette.PanelLine   // stone-100, dividers and the footer's edge
	PanelField  = LightPalette.PanelField  // stone-50, the clipboard row's face
	PanelBorder = LightPalette.PanelBorder // stone-200, its hairline
	PanelHover  = LightPalette.PanelHover  // stone-100, what a quiet action takes
	PanelPress  = LightPalette.PanelPress  // stone-200, and what it takes pressed

)

// applyDesign installs one appearance for the pass that is about to build a
// window. It is the only writer of the colors above.
func ApplyDesign(dark bool) {
	p := LightPalette
	if dark {
		p = DarkPalette
	}
	ColorSurface = p.surface
	AnimeAccent, BilibiliAccent = p.Anime, p.Bilibili
	CodexTile, CodexInk = p.CodexTile, p.CodexInk
	ColorSidebar = p.sidebar
	ColorField = p.field
	ColorMuted = p.muted
	ColorHover = p.hover
	ColorActive = p.active
	ColorRowAlt = p.rowAlt
	ColorBorder = p.border
	ColorHairline = p.Hairline

	ColorText = p.text
	ColorTextSoft = p.textSoft
	ColorTextBody = p.textBody
	ColorTextMuted = p.textMuted
	ColorTextFaint = p.textFaint
	ColorInputValue = p.inputValue

	ColorBrand = p.brand
	ColorBrandHover = p.brandHover
	ColorBrandPressed = p.brandPressed
	ColorOnBrand = p.onBrand
	ColorOnMark = p.onMark
	ColorBrandTint = p.brandTint
	ColorBrandTintStrong = p.brandTintStrong
	ColorSuccess = p.success
	ColorWarning = p.warning
	ColorDanger = p.danger
	ColorWarningFill = p.warningFill
	ColorWarningBorder = p.warningBorder
	ColorWarningHover = p.warningHover
	ColorWarningPressed = p.warningPressed

	ColorSkyTile, ColorSkyInk = p.skyTile, p.skyInk
	ColorBlueTile, ColorBlueInk = p.blueTile, p.blueInk
	ColorVioletTile, ColorVioletInk = p.violetTile, p.violetInk
	ColorAmberTile, ColorAmberInk = p.amberTile, p.amberInk
	ColorEmeraldTile, ColorEmeraldInk = p.emeraldTile, p.emeraldInk
	ColorStoneTile, ColorStoneInk = p.stoneTile, p.stoneInk
	ColorReadingInk = p.readingInk
	ColorSkeleton = p.skeleton
	ColorTileEdge = p.tileEdge

	ColorSourceInk = p.sourceInk
	ColorParticle = p.particle
	ColorEyebrow = p.Eyebrow
	ColorBodyInk = p.bodyInk
	ColorFooterInk = p.footerInk
	ColorLoanword = p.brand
	BadgeStoneFill, BadgeStoneInk = p.BadgeStoneFill, p.BadgeStoneInk
	BadgeBlueFill, BadgeBlueInk = p.BadgeBlueFill, p.BadgeBlueInk
	BadgeVioletFill, BadgeVioletInk = p.BadgeVioletFill, p.BadgeVioletInk
	BadgeEmeraldFill, BadgeEmeraldInk = p.BadgeEmeraldFill, p.BadgeEmeraldInk
	ColorCardFoot = p.cardFoot
	ColorCardLine = p.cardLine
	ColorChip = p.chip
	ColorChipInk = p.chipInk

	PanelInk = p.PanelInk
	PanelNumber = p.PanelNumber
	PanelFaint = p.PanelFaint
	PanelBody = p.PanelBody
	PanelLine = p.PanelLine
	PanelField = p.PanelField
	PanelBorder = p.PanelBorder
	PanelHover = p.PanelHover
	PanelPress = p.PanelPress
}

// The application's metrics, in points, taken from the design: every window
// keeps a 20-point page edge, a section keeps 12 points between its rows, and
// every control on a line is 36 points high.
const (
	// pageEdge is the window's own margin: the room the page keeps on every
	// side, and the room the footer's version keeps inside it.
	PageEdge = 14.0
	// sectionGap is the room a section keeps above and below itself, which is
	// what keeps the line dividing two of them from touching either.
	SectionGap = 16.0

	RadiusCard = 12.0
	// radiusTile rounds the small square at the head of a list row.
	RadiusTile  = 6.0
	RadiusField = 6.0

	// heightControl is the height every control shares: an input, a button
	// and a segmented control sit on one line as one row of things.
	HeightControl = 36.0
	// sizeTile is the mark at the head of a section: 32 points, rounded.
	SizeTile = 32.0

	// The quick window's block, as the design draws it: 16 beside the block,
	// 14 above it and 12 below, with a 20-point mark at its head.
	QuickEdge     = 16.0
	QuickTop      = 14.0
	QuickBottom   = 12.0
	QuickTileSize = 20.0
	QuickMarkSize = 12.0
	// And its own type: a 13-point name, 11-point times and labels, a
	// 22-point number, and the 10-point line over a bar.
	SizeQuickName   = 13.0
	SizeQuickFine   = 11.0
	SizeQuickNumber = 22.0
	SizeQuickTiny   = 10.0

	// The API key section is the page's dense one: the design packs a service
	// into two lines, so its head and the row inside each card are a size
	// down from the rest of the page. Every number below is that design's.
	SizeTileCompact     = 28.0 // its mark: w-7
	SizeTitleCompact    = 14.0 // the head's name
	SizeSubtitleCompact = 11.0 // the head's one line of explanation
	SizeCardName        = 13.0 // the name at the head of a card
	SizeFine            = 11.0 // a card's actions and the warning above them
	HeightCardControl   = 32.0 // h-8: a card's field and the actions beside it
	CardActionPad       = 10.0 // px-2.5: the room beside an action's label

	// Typography: a section's name, its one line of explanation, and the
	// body text under them. They are the design's own sizes rather than
	// multiples of the theme's, because the two windows are dense.
	SizeTitle    = 15.0
	SizeSection  = 14.0
	SizeSubtitle = 12.0
	SizeBody     = 14.0
	SizeSmall    = 12.0
	SizeTiny     = 10.0

	// The translation card's own scale, read off the design's classes: the
	// source and the translation are 13px, and the labels and badges 10px.
	// Its actions are 14px marks in 4px of padding, which is what makes a
	// 22-point target out of a 14-point glyph.
	SizeCardLabel  = 10.0
	SizeCardSource = 13.0
	SizeCardTarget = 13.0
	SizeCardFoot   = 11.0
	SizeCardMark   = 14.0
	// The annotation chips: the word is 12px, the reading or the loanword's
	// own word above it 10px. The chip is stone-50 with a stone-100 edge,
	// cornered at 8, and 10 by 6 of its own padding.
	SizeChipWord = 12.0
	SizeChipNote = 10.0
	ChipRadius   = 8.0
	ChipPadX     = 10.0
	ChipPadY     = 6.0
	// cardEdge is the design's px-3.5 and the vertical room of its blocks:
	// 14 on each side, 12 above the source block and 10 under it.
	CardEdge    = 14.0
	CardHeadTop = 12.0
	CardHeadEnd = 10.0
	CardBodyTop = 10.0
	CardBodyEnd = 10.0
	// cardFootTop and cardFootEnd are the room above and below the foot's own
	// line: the design's px-3 py-1.5 is 6, raised to 10 so that the line does
	// not sit on the card's bottom edge (the user asked for more room there,
	// and it is the settings window's own footer padding).
	CardFootTop = 10.0
	CardFootEnd = 10.0
	// cardMarkPad is the design's p-1 around an action's mark: 4 points.
	// With the 14-point glyph it makes the design's own 22-point target.
	CardMarkPad = 4.0

	// sizeIcon is the glyph of an action a card carries.
	SizeIcon = 20.0
)

// theme colours MyGo's own widgets: its check boxes, inputs, focus rings,
// scrollbars and dividers take these, so that a widget the framework draws
// and a piece this file composes read as the same design.
//
// It is built from the same palette as the views rather than from the
// framework's own dark theme: a checked box is drawn by MyGo, and it has to
// be the brand green on the same surface as the button beside it. The
// framework's palette is the starting point for the properties Boku does not
// name (scrollbar, inverse, spacing), not for a second set of colors.
func Theme(dark bool) *ui.Theme {
	p := LightPalette
	base := ui.LightTheme()
	if dark {
		p = DarkPalette
		base = ui.DarkTheme()
	}
	base.Background = p.surface
	base.Surface = p.field
	base.SurfaceHover = p.hover
	base.SurfacePressed = p.active
	base.Border = p.border
	base.Text = p.text
	base.TextMuted = p.textMuted
	base.Accent = p.brand
	base.AccentHover = p.brandHover
	base.AccentPressed = p.brandPressed
	base.AccentText = p.onBrand
	base.Danger = p.danger
	base.Warning = p.warning
	base.Success = p.success
	// The light appearance's selection and focus are the ones it always had.
	base.Selection = p.brand.Alpha(0.22)
	base.Focus = p.brand.Alpha(0.55)
	if dark {
		// A dark page needs the accent's wash a little stronger to be seen.
		base.Selection = p.brand.Alpha(0.35)
		base.Focus = p.brand.Alpha(0.60)
	}
	base.Radius = RadiusField
	// The family every window's text is drawn in. It names the CJK faces
	// after the Latin ones on every desktop, so that a Chinese glyph is the
	// same shape whatever language the system is in: without it the text
	// system falls back per script, and an English or Japanese Windows picks
	// a different face than a Chinese one does.
	base.Font = UiFontFamily
	return base
}

// uiFontFamily is the family the application's text is drawn in: the design's
// own stack, which asks for the Latin faces of the system the way it always
// did and then names the CJK face before giving up. MyGo takes a
// comma-separated list and draws each character in the first family that has
// it, so the reading order below is also the fallback order.
//
// The design's stack is the source of this list (docs/ui.md §7); changing it
// there changes it here.
const UiFontFamily = "-apple-system, BlinkMacSystemFont, \"Segoe UI\", " +
	"\"PingFang SC\", \"Hiragino Sans GB\", \"Microsoft YaHei UI\", \"Microsoft YaHei\", " +
	"\"Noto Sans CJK JP\", \"Noto Sans CJK SC\", \"Yu Gothic\", sans-serif"

// useDesign points the frame being built at one appearance of Boku's design:
// it installs the palette the helpers below read, and the matching theme for
// the widgets MyGo draws instead of this file.
//
// A view calls it once, before it builds anything: the theme is read while an
// element is constructed, so a control built before this call keeps the
// framework's colors. The state owns the appearance because it is a choice
// the user made in the settings window; a view only says "draw the
// application as it stands".

// windowFrame is the column every window is built in: the content area's face
// from edge to edge, which the regions inside it divide.
func WindowFrame(c *ui.Context) ui.Element {
	return ui.Column(c).Fill().Background(ColorSurface)
}

// footer is the quiet line at the bottom of a window: the version, on the
// window's own face, under one hairline. It is the last line of the window
// rather than another region of it, so it wears no band of its own.
//
// The hairline is the window's own line rather than one between two sections:
// it runs from edge to edge, and what it says is where the window ends. The
// version sits inside it, in the room a footer action keeps.
func Footer(c *ui.Context, version string) {
	Hairline(c)
	ui.Row(c).FillWidth().Shrink(0).Font("monospace").
		PaddingX(float32(12)).
		PaddingY(PageEdge / 2).
		AlignItems(ui.Center).
		Children(func() { VersionLine(c, version) })
}

// versionLine is the quiet version at the bottom of a window.
func VersionLine(c *ui.Context, version string) {
	ui.Text(c, version).FontSize(SizeSmall).TextColor(ColorTextMuted).SingleLine()
}

// headSize is how large a section introduces itself: the tile its mark sits
// in, the room that mark keeps inside the tile, the room between the tile and
// the name beside it, and the two sizes of type.
type HeadSize struct {
	Tile     float32
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
	HeadRegular = HeadSize{Tile: SizeTile, padX: 6, padY: 4, gap: 10, title: SizeTitle, subtitle: SizeSubtitle}
	HeadSmall   = HeadSize{Tile: SizeTileCompact, padX: 5, padY: 3.5, gap: 14, title: SizeTitleCompact, subtitle: SizeSubtitleCompact}
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
func SectionHead(c *ui.Context, head *ui.Handle, mark *ui.SVG, Ink ui.Color, title, subtitle string) {
	SectionHeadSized(c, head, mark, Ink, title, subtitle, HeadRegular)
}

// sectionHeadSmall is the same head a size down: a 28-point tile and the
// design's own 14/11 pair. The API key section wears it because its cards are
// dense, and a head a size above them would be the largest thing on the page.
func SectionHeadSmall(c *ui.Context, head *ui.Handle, mark *ui.SVG, Ink ui.Color, title, subtitle string) {
	SectionHeadSized(c, head, mark, Ink, title, subtitle, HeadSmall)
}

// sectionHeadSized draws the head itself, at one of the two sizes.
func SectionHeadSized(c *ui.Context, head *ui.Handle, mark *ui.SVG, Ink ui.Color, title, subtitle string, size HeadSize) {
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(size.gap)
	if head != nil {
		row.Bind(head)
	}
	row.Children(func() {
		if mark != nil {
			ui.Box(c).Children(func() {
				ui.Icon(c, mark).
					Height(size.Tile).
					Width(size.Tile).
					Padding(size.padY, size.padX).
					Shrink(0).
					TextColor(Ink)
			}).Background(ColorMuted).Radius(RadiusTile)
		}
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, title).FontSize(size.title).FontWeight(600).TextColor(ColorTextSoft).SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(size.subtitle).TextColor(ColorTextMuted).SingleLine()
			}
		})
	})
}

// sectionLabel is the label above a control: what the value under it is.
func SectionLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(SizeSection).FontWeight(500).TextColor(ColorTextBody).SingleLine()
}

// inputFrame gives a text input the look of a field: the field's own face, a
// hairline, the height every control shares, and the monospace type the
// design gives a value that is read rather than written here.
func InputFrame(c *ui.Context, input ui.Element) ui.Element {
	return input.
		FillWidth().
		Height(HeightControl).
		Padding(0, float32(12)).
		Radius(RadiusField).
		Background(ColorField).
		Border(1, ColorBorder).
		FontSize(SizeBody).
		TextColor(ColorInputValue)
}

// fieldFrame is inputFrame at the size a card's own row has room for: 32
// points high with 10 beside the text, which is what leaves the rest of the
// line to the actions that act on the value in it.
//
// The field is the thing a dense card is about, so it takes the room that is
// left rather than a width of its own: the caller grows it.
func FieldFrame(c *ui.Context, input ui.Element) ui.Element {
	return input.
		Height(HeightCardControl).
		Padding(0, float32(10)).
		Radius(RadiusField).
		Background(ColorField).
		Border(1, ColorBorder).
		FontSize(SizeSmall).
		TextColor(ColorInputValue)
}

// stacked is a plain column of the pieces inside a section, so that a section
// body does not repeat the same gap in every call.
func Stacked(c *ui.Context, gap float32, body func()) {
	ui.Column(c).FillWidth().Gap(gap).Children(body)
}

// buttonTone is what a button is for, which is all a caller has to say about
// how it looks.
type ButtonTone int

const (
	// buttonPrimary is the one action a form is for: filled, in the brand
	// green, with the light text on it.
	ButtonPrimary ButtonTone = iota
	// buttonSecondary is every other action: the content's own face, a
	// hairline, and a hover that tints it.
	ButtonSecondary
	// buttonWarning is an action that changes something back: the warning
	// face and border, with the warning ink, rather than the ordinary
	// secondary face.
	ButtonWarning
)

// buttonSize is how much room a button takes: the height every control on a
// form row shares, the room beside its label, and the size of that label.
type ButtonSize struct {
	height float32
	pad    float32
	font   float32
}

// The two sizes the page draws: the design's own, and the one the API key
// cards use, where a field and two actions share one 32-point line.
var (
	ButtonRegular = ButtonSize{height: HeightControl, pad: 16, font: SizeBody}
	ButtonDense   = ButtonSize{height: HeightCardControl, pad: CardActionPad, font: SizeFine}
)

// designButton builds an action in one of the tones. Every tone keeps the
// whole behavior of a button — the pointer, the keyboard, the focus ring and
// what assistive technology reads — since both are the framework's ButtonBase
// underneath.
func DesignButton(c *ui.Context, label string, tone ButtonTone) ui.Element {
	return ButtonSized(c, label, tone, ButtonRegular)
}

// cardButton is designButton in the size a dense card's row has room for: the
// design's h-8 with 10 points beside the label. It is what stands next to a
// card's field, where the three things on the line have to be one row.
func CardButton(c *ui.Context, label string, tone ButtonTone) ui.Element {
	return ButtonSized(c, label, tone, ButtonDense)
}

// buttonSized draws the button itself, at one of the two sizes.
func ButtonSized(c *ui.Context, label string, tone ButtonTone, size ButtonSize) ui.Element {
	b := ui.ButtonBase(c).Label(label)
	switch tone {
	case ButtonPrimary:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(RadiusField).
			Background(ColorBrand).
			TextColor(ColorOnBrand).
			FontSize(size.font).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(ColorBrandPressed)
		case b.Hovered() || b.Focused():
			b = b.Background(ColorBrandHover)
		}
	case ButtonSecondary:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(RadiusField).
			Background(ColorSurface).
			Border(1, ColorBorder).
			TextColor(ColorText).
			FontSize(size.font).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(ColorActive)
		case b.Hovered() || b.Focused():
			b = b.Background(ColorHover)
		}
	case ButtonWarning:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(RadiusField).
			Background(ColorWarningFill).
			Border(1, ColorWarningBorder).
			TextColor(ColorWarning).
			FontSize(size.font).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(ColorWarningPressed)
		case b.Hovered() || b.Focused():
			b = b.Background(ColorWarningHover)
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
func TextAction(c *ui.Context, label string, Ink ui.Color) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Radius(float32(4)).
		Padding(float32(2), float32(6)).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(ColorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(ColorHover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(SizeFine).FontWeight(500).TextColor(Ink).SingleLine()
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
func QuietIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(CardMarkPad).
		Padding(CardMarkPad).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(PanelPress)
	case b.Hovered() || b.Focused():
		b = b.Background(PanelHover)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(SizeCardMark), float32(SizeCardMark)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(ColorTextSoft)
		} else {
			icon.TextColor(PanelFaint)
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
func DeclinesInput(ui.InputEvent) bool { return false }

// cardIconButton is the action a translation card carries: a 14-point mark in
// 4 points of padding, which is what makes a 22-point target of it — the
// design's p-1 and w-3.5 h-3.5.
//
// Its two states are the design's: under the pointer it takes a faint stone
// face and the ink deepens. The glyph keeps the muted ink of a secondary
// action otherwise.
func CardIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(CardMarkPad).
		Cursor(ui.CursorPointer).
		HandleInput(DeclinesInput)
	switch {
	case b.Pressed():
		b = b.Background(ColorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(ColorCardLine)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(SizeCardMark), float32(SizeCardMark)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(ColorTextSoft)
		} else {
			icon.TextColor(ColorEyebrow)
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
func CardIconToggle(c *ui.Context, mark *ui.SVG, label string, on bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(CardMarkPad).
		Cursor(ui.CursorPointer).
		HandleInput(DeclinesInput)
	switch {
	case b.Pressed():
		b = b.Background(ColorActive)
	case on:
		b = b.Background(ColorCardLine)
	case b.Hovered() || b.Focused():
		b = b.Background(ColorCardLine)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(SizeCardMark), float32(SizeCardMark)).Shrink(0)
		if on {
			icon.TextColor(c.Theme().Accent)
		} else if b.Hovered() || b.Focused() {
			icon.TextColor(ColorTextSoft)
		} else {
			icon.TextColor(ColorEyebrow)
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
func AnnotationChips(c *ui.Context, words []domain.Segment, showReading, showGloss bool) {
	ui.Row(c).FillWidth().Wrap().Justify(ui.Start).Gap(float32(6)).Children(func() {
		for _, segment := range words {
			if strings.TrimSpace(segment.Text) == "" {
				continue
			}
			note, loanword := AnnotationNote(segment, showReading, showGloss)
			AnnotationChip(c, segment.Text, note, loanword)
		}
	})
}

// annotationChip is one of those chips: the word, and above it what there is
// to say about it — a reading in stone-500, or a loanword's own word in the
// brand green.
func AnnotationChip(c *ui.Context, word, note string, loanword bool) {
	ui.Column(c).Shrink(0).AlignItems(ui.Center).
		Background(ColorChip).
		Border(1, ColorCardLine).
		Radius(ChipRadius).
		Padding(ChipPadY, ChipPadX, ChipPadY, ChipPadX).
		Children(func() {
			if note != "" {
				text := ui.Text(c, note).
					FontSize(SizeChipNote).
					TextColor(ColorReadingInk).
					SingleLine()
				if loanword {
					text.TextColor(ColorLoanword).FontWeight(500)
				}
				text.FixedLineHeight(float32(SizeChipNote + 2)).Shrink(0)
				ui.Spacer(c).Height(float32(4)).Shrink(0)
			}
			ui.Text(c, word).
				FontSize(SizeChipWord).
				TextColor(ColorChipInk).
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
func AnnotationBlock(c *ui.Context, open bool, label string, toggle func()) {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(open).
		FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Radius(0).
		Padding(float32(8), float32(CardEdge), float32(8), float32(CardEdge)).
		HandleInput(DeclinesInput)
	switch {
	case button.Pressed():
		button = button.Background(ColorActive)
	case button.Hovered():
		button = button.Background(ColorChip)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(SizeCardFoot).TextColor(ColorReadingInk).SingleLine()
		DisclosureArrow(c, ArrowTurn(open), ColorReadingInk)
	})
	if button.Clicked() && toggle != nil {
		toggle()
	}
}

// arrowTurn is how far the annotation row's chevron is turned: pointing down
// while closed, up while open, which is the design's own pair.
func ArrowTurn(open bool) float32 {
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
func LangBadge(c *ui.Context, name string, code domain.Language) {
	if strings.TrimSpace(name) == "" {
		return
	}
	fill, Ink := LangBadgeColors(code)
	ui.Box(c).Shrink(0).
		Padding(float32(1), float32(6)).
		Radius(float32(4)).
		Background(fill).
		Children(func() {
			ui.Text(c, name).FontSize(SizeCardLabel).FontWeight(500).TextColor(Ink).SingleLine()
		})
}

// langBadgeColors is the pair a language's badge is drawn with, as the design
// writes it.
func LangBadgeColors(code domain.Language) (fill, Ink ui.Color) {
	switch code {
	case domain.LangChinese:
		return BadgeEmeraldFill, BadgeEmeraldInk
	case domain.LangEnglish:
		return BadgeBlueFill, BadgeBlueInk
	case domain.LangJapanese:
		return BadgeStoneFill, BadgeStoneInk
	case domain.LangKorean:
		return BadgeVioletFill, BadgeVioletInk
	default:
		return BadgeStoneFill, BadgeStoneInk
	}
}

// eyebrow is the small label above a block of text: "原文" or "译文". It is
// written the way the design writes it — 10 points, medium, uppercase and
// letter-spaced, in stone-400 — so that it names the block without competing
// with it.
func Eyebrow(c *ui.Context, label string) {
	ui.Text(c, strings.ToUpper(label)).
		FontSize(SizeCardLabel).
		FontWeight(500).
		TextColor(ColorEyebrow).
		SingleLine().
		Shrink(0)
}

// skeletonBar is a placeholder line: the shape of the text that is on its
// way, so that a window waiting for an answer looks like a window rather than
// like a mistake. It is measured in the card's own width, which the card
// passes in.
func SkeletonBar(c *ui.Context, width, fraction float32) {
	ui.Box(c).
		Height(float32(12)).
		Width(width * fraction).
		Radius(float32(4)).
		Background(ColorSkeleton)
}

// segmented is the choice among a few short options: a bordered track, and
// the chosen segment filled in the brand green.
//
// Only the look is this file's: the choice itself, the arrows that move it
// and what assistive technology reads come from the framework's
// SegmentedBase, whose Track reports a new choice with Changed.
func Segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.
		Padding(float32(2)).
		Radius(RadiusField).
		Background(ColorSurface).
		Border(1, ColorBorder)
	parts.Track.Children(func() {
		for index := range labels {
			segment := parts.Segment(index).
				Padding(float32(4), float32(10)).
				Radius(float32(5)).
				FontSize(SizeSmall).
				FontWeight(500)
			if index == *selected {
				segment.Background(ColorBrand).TextColor(ColorOnBrand)
			} else {
				segment.TextColor(ColorTextFaint)
				if segment.Hovered() {
					segment.Background(ColorHover)
				}
			}
			segment.Children(func() { ui.Text(c, labels[index]).SingleLine() })
		}
	})
	return parts.Track
}

// noteKind is what a note is telling the user: something that went wrong,
// something that went right, or nothing in particular.
type NoteKind int

const (
	NotePlain NoteKind = iota
	NoteWarning
	NoteDanger
	NoteSuccess
)

// noteColor is the color a kind of note is written in, and of the mark that
// stands for it.
func NoteColor(kind NoteKind) ui.Color {
	switch kind {
	case NoteWarning:
		return ColorWarning
	case NoteDanger:
		return ColorDanger
	case NoteSuccess:
		return ColorSuccess
	default:
		return ColorTextMuted
	}
}

// notice draws one quiet line: a note under a control ("已保存。"), or the
// explanation a section keeps under its switches. It is text rather than a
// panel, because a sentence is not furniture.
func Notice(c *ui.Context, kind NoteKind, text string) {
	if text == "" {
		return
	}
	ui.Text(c, text).
		FontSize(SizeSmall).
		TextColor(NoteColor(kind)).
		Wrap().
		LineHeight(1.5)
}

// noticePanel is the one note on a page that must not be missed: a tinted,
// bordered strip in the warning colors, with a single small line in it. It is
// a panel rather than another line of body text, and the design draws it tight
// — 6 above and below — because the rows under it are what the page is for.
func NoticePanel(c *ui.Context, text string) {
	ui.Box(c).FillWidth().
		Background(ColorWarningFill).
		Border(1, ColorWarningBorder).
		Radius(RadiusField).
		Padding(float32(6), float32(10)).
		Children(func() {
			ui.Text(c, text).
				FontSize(SizeFine).
				TextColor(ColorWarning).
				Wrap().
				LineHeight(1.4)
		})
}

// cardHint is the smallest line a card carries: where the value above it comes
// from. It is quieter than a notice, which is about what just happened.
func CardHint(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(SizeTiny).TextColor(ColorTextMuted).SingleLine()
}

// cardNote is the line a block's own head carries when one more line is needed
// there: what an account is, when its window resets. The design gives that line
// 11 points of faint ink, and the quick window's own stone.
func CardNote(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(SizeQuickFine).TextColor(PanelFaint).SingleLine()
}

// barHeight is how tall a proportion bar is: the design's h-1.5, which is a
// line rather than a block.
const BarHeight = 6.0

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
func ProgressBar(c *ui.Context, percent int64, fill ui.Color, label, name string) {
	ui.Column(c).FillWidth().Gap(float32(4)).AlignItems(ui.End).Children(func() {
		// ui.Text(c, label).FontSize(sizeQuickTiny).FontWeight(500).TextColor(fill).SingleLine()
		ui.Box(c).FillWidth().
			Height(float32(BarHeight)).
			Radius(999).
			Background(PanelLine).
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
type PanelMark struct {
	SVG  *ui.SVG
	Tile ui.Color
	Ink  ui.Color
	Bar  ui.Color
	// Scale enlarges detailed product marks without changing the tile size.
	// Zero uses the standard size.
	Scale float32
}

// quickBlock is one block of the quick window: the design's own room around
// it, which is what makes a block read as one thing rather than as rows that
// happen to follow each other.
func QuickBlock(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().
		Padding(QuickTop, QuickEdge, QuickBottom, QuickEdge).
		Children(body)
}

// quickDivider is the line between two blocks of the quick window: one point
// of stone-100, inside the same edge the blocks keep.
func QuickDivider(c *ui.Context) {
	ui.Column(c).FillWidth().PaddingX(QuickEdge).Children(func() {
		ui.Row(c).FillWidth().Height(1).Shrink(0).Background(PanelLine)
	})
}

// quickTile is the mark at the head of a block: the provider's drawing in a
// tile of its own, so that a block is recognised before its name is read. A
// block whose provider has no drawing gets the tile alone, so that two blocks'
// names still start at the same place.
func QuickTile(c *ui.Context, mark PanelMark) {
	box := ui.Box(c).Size(QuickTileSize, QuickTileSize).Shrink(0).
		Radius(RadiusTile).
		Background(mark.Tile).
		Center()
	// A brand whose tile is as dark as the page keeps an edge, so that the
	// block still starts with a mark rather than with the name beside a hole.
	// The light appearance has no edge to add.
	if ColorTileEdge.A > 0 {
		box.Border(1, ColorTileEdge)
	}
	box.Children(func() {
		if mark.SVG == nil {
			return
		}
		scale := mark.Scale
		if scale <= 0 {
			scale = 1
		}
		size := min(float32(QuickMarkSize)*scale, float32(QuickTileSize)-4)
		ui.Icon(c, mark.SVG).Size(size, size).Shrink(0).TextColor(mark.Ink)
	})
}

// statusNote is the line that says whether a credential is already stored: a
// dot in the success color and what is stored, in the same color.
func StatusNote(c *ui.Context, text string) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
		ui.Box(c).Size(float32(6), float32(6)).Radius(999).Shrink(0).Background(ColorSuccess)
		ui.Text(c, text).FontSize(SizeBody).TextColor(ColorSuccess).SingleLine()
	})
}

// kindBadge is the small label a list row carries: what the copy is, in the
// color its own mark is drawn in.
func KindBadge(c *ui.Context, Tile, Ink ui.Color, label string) {
	ui.Box(c).Shrink(0).
		Padding(float32(1), float32(6)).
		Radius(float32(4)).
		Background(Tile).
		Children(func() {
			ui.Text(c, label).FontSize(SizeTiny).FontWeight(500).TextColor(Ink).SingleLine()
		})
}

// kindTile is the rounded square at the head of a list row: the mark of what
// the copy is, on the tint of its kind.
func KindTile(c *ui.Context, mark *ui.SVG, Tile, Ink ui.Color) {
	ui.Box(c).Size(float32(28), float32(28)).Shrink(0).
		Radius(RadiusTile).
		Background(Tile).
		Center().
		Children(func() {
			ui.Icon(c, mark).Size(float32(14), float32(14)).Shrink(0).TextColor(Ink)
		})
}

// hairline is the line a window draws between two of its regions, and between
// two sections of one: a single point of the border color.
//
// It stays inside the region it is drawn in, the way every other thing on the
// page does: a line that ran to the window's edge would step outside the
// margin the rest of the page keeps, and the page would have two edges.
func Hairline(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(ColorBorder)
}

// rowDivider is the fainter line between two entries of a list.
func RowDivider(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(ColorHairline)
}
