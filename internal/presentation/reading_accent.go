package presentation

import "github.com/egoist/mygo/ui"

// ReadingAccent 让带色调的阅读卡片与主题其余部分保持同一表面层级：
// Ink 用于文字/图标，TileInk 与 BadgeInk 只能配各自对应的实心背景。
type ReadingAccent struct {
	Ink, Tint, Border, Hover, HoverBorder ui.Color
	Tile, TileInk, BadgeFill, BadgeInk    ui.Color
}

var (
	AnimeAccent    = LightPalette.Anime
	BilibiliAccent = LightPalette.Bilibili
	CodexTile      = LightPalette.CodexTile
	CodexInk       = LightPalette.CodexInk
	SteamTile      = LightPalette.SteamTile
	SteamInk       = LightPalette.SteamInk
)
