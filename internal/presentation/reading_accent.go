package presentation

import "github.com/egoist/mygo/ui"

// ReadingAccent keeps tinted reading cards in the same surface hierarchy as
// the rest of the theme. Ink is for text/icons; TileInk and BadgeInk belong
// only on their corresponding filled backgrounds.
type ReadingAccent struct {
	Ink, Tint, Border, Hover, HoverBorder ui.Color
	Tile, TileInk, BadgeFill, BadgeInk    ui.Color
}

var (
	AnimeAccent    = LightPalette.Anime
	BilibiliAccent = LightPalette.Bilibili
	CodexTile      = LightPalette.CodexTile
	CodexInk       = LightPalette.CodexInk
)
