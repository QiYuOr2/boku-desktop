package app

import "boku/internal/presentation"

func (s *State) clipboardDesign() presentation.ClipboardDesign {
	return presentation.ClipboardDesign{
		ReadingDesign: s.readingDesign(s.clipboardMark),
		WindowFrame:   presentation.WindowFrame, Hairline: presentation.Hairline, RowDivider: presentation.RowDivider,
		Content: presentation.Content, KindTile: presentation.KindTile, KindBadge: presentation.KindBadge, Segmented: presentation.Segmented,
		PageEdge: presentation.PageEdge, SizeSection: presentation.SizeSection, SizeFine: presentation.SizeFine,
		ColorActive: presentation.ColorActive, ColorRowAlt: presentation.ColorRowAlt, ColorSurface: presentation.ColorSurface, ColorBorder: presentation.ColorBorder, ColorHover: presentation.ColorHover,
		ColorVioletTile: presentation.ColorVioletTile, ColorVioletInk: presentation.ColorVioletInk, ColorBlueTile: presentation.ColorBlueTile, ColorBlueInk: presentation.ColorBlueInk,
		ColorStoneTile: presentation.ColorStoneTile, ColorStoneInk: presentation.ColorStoneInk, ColorEmeraldTile: presentation.ColorEmeraldTile, ColorEmeraldInk: presentation.ColorEmeraldInk,
		ColorAmberTile: presentation.ColorAmberTile, ColorAmberInk: presentation.ColorAmberInk, ColorSkyTile: presentation.ColorSkyTile, ColorSkyInk: presentation.ColorSkyInk,
		PanelBody: presentation.PanelBody, PanelBorder: presentation.PanelBorder,
		HistoryText: s.historyText, HistoryImage: s.historyImage, HistoryLink: s.historyLink, HistoryCode: s.historyCode, Dark: s.dark,
	}
}
