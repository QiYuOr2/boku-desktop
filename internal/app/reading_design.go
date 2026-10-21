package app

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) readingDesign(mark *ui.SVG) presentation.ReadingDesign {
	return presentation.ReadingDesign{
		QuickDivider: presentation.QuickDivider, QuickBlock: presentation.QuickBlock,
		QuickTile: func(c *ui.Context, svg *ui.SVG, tile, ink ui.Color) {
			presentation.QuickTile(c, presentation.PanelMark{SVG: svg, Tile: tile, Ink: ink})
		},
		RefreshButton: s.refreshButton,
		Notice: func(c *ui.Context, danger bool, text string) {
			kind := presentation.NotePlain
			if danger {
				kind = presentation.NoteDanger
			}
			presentation.Notice(c, kind, text)
		},
		FriendlyError: friendlyError, SkeletonBar: presentation.SkeletonBar, Inset: presentation.Inset, Stacked: presentation.Stacked,
		SectionHead: presentation.SectionHead, SectionLabel: presentation.SectionLabel, CardHint: presentation.CardHint, WordButton: presentation.WordButton,
		PrimaryButton: func(c *ui.Context, label string) ui.Element {
			return presentation.DesignButton(c, label, presentation.ButtonPrimary)
		},
		SecondaryButton: func(c *ui.Context, label string) ui.Element {
			return presentation.DesignButton(c, label, presentation.ButtonSecondary)
		},
		SizeQuickName: presentation.SizeQuickName, SizeQuickTiny: presentation.SizeQuickTiny, SizeQuickFine: presentation.SizeQuickFine, SizeBody: presentation.SizeBody, SizeSmall: presentation.SizeSmall,
		RadiusTile: presentation.RadiusTile, RadiusField: presentation.RadiusField, HeightControl: presentation.HeightControl,
		PanelInk: presentation.PanelInk, PanelFaint: presentation.PanelFaint, PanelField: presentation.PanelField, PanelLine: presentation.PanelLine,
		ColorOnMark: presentation.ColorOnMark, ColorTextMuted: presentation.ColorTextMuted, ColorTextBody: presentation.ColorTextBody,
		Mark: mark, Chevron: s.chevronRightMark,
	}
}
