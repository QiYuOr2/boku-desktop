package presentation

import "github.com/egoist/mygo/ui"

// ClipboardDesign 是历史视图当前的主题与共享控件。
type ClipboardDesign struct {
	ReadingDesign
	WindowFrame                                                                                 func(*ui.Context) ui.Element
	Hairline, RowDivider                                                                        func(*ui.Context) ui.Element
	Content                                                                                     func(*ui.Context, bool, func())
	KindTile                                                                                    func(*ui.Context, *ui.SVG, ui.Color, ui.Color)
	KindBadge                                                                                   func(*ui.Context, ui.Color, ui.Color, string)
	Segmented                                                                                   func(*ui.Context, *int, ...string) ui.Element
	PageEdge, SizeSection, SizeFine                                                             float32
	ColorActive, ColorRowAlt, ColorSurface, ColorBorder, ColorHover                             ui.Color
	ColorVioletTile, ColorVioletInk, ColorBlueTile, ColorBlueInk, ColorStoneTile, ColorStoneInk ui.Color
	ColorEmeraldTile, ColorEmeraldInk, ColorAmberTile, ColorAmberInk, ColorSkyTile, ColorSkyInk ui.Color
	PanelBody, PanelBorder                                                                      ui.Color
	HistoryText, HistoryImage, HistoryLink, HistoryCode                                         *ui.SVG
	Dark                                                                                        bool
}
