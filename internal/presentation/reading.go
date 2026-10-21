package presentation

import "github.com/egoist/mygo/ui"

// ReadingDesign supplies shared components and current theme tokens to compact
// reading features. Components build immediately; no Element is retained.
type ReadingDesign struct {
	QuickDivider                                                                            func(*ui.Context)
	QuickBlock                                                                              func(*ui.Context, func())
	QuickTile                                                                               func(*ui.Context, *ui.SVG, ui.Color, ui.Color)
	RefreshButton                                                                           func(*ui.Context, bool, bool) ui.Element
	Notice                                                                                  func(*ui.Context, bool, string)
	FriendlyError                                                                           func(error) string
	SkeletonBar                                                                             func(*ui.Context, float32, float32)
	Inset                                                                                   func(*ui.Context, func())
	Stacked                                                                                 func(*ui.Context, float32, func())
	SectionHead                                                                             func(*ui.Context, *ui.Handle, *ui.SVG, ui.Color, string, string)
	SectionLabel                                                                            func(*ui.Context, string)
	CardHint                                                                                func(*ui.Context, string)
	WordButton                                                                              func(*ui.Context, string) ui.Element
	PrimaryButton                                                                           func(*ui.Context, string) ui.Element
	SecondaryButton                                                                         func(*ui.Context, string) ui.Element
	SizeQuickName, SizeQuickTiny, SizeQuickFine, SizeBody, SizeSmall                        float32
	RadiusTile, RadiusField, HeightControl                                                  float32
	PanelInk, PanelFaint, PanelField, PanelLine, ColorOnMark, ColorTextMuted, ColorTextBody ui.Color
	Mark, Chevron                                                                           *ui.SVG
}
