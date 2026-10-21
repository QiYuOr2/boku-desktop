package ui

import (
	"boku/internal/presentation"
	"fmt"
	"github.com/egoist/mygo/ui"
	"log/slog"
)

// animeUpdates draws today's releases. It is absent when disabled,
// unconfigured, or there is no release. Once a request has settled, an empty
// today stays absent even when the request failed: an optional schedule must
// not leave an error-shaped blank region in the compact flyout.
func (s *ViewModel) QuickView(c *ui.Context, design presentation.ReadingDesign) {
	s.design = design
	if s.service == nil || !s.service.Enabled() || s.service.Username() == "" {
		return
	}
	items, _, loading, err := s.service.View()
	if len(items) == 0 && !loading {
		return
	}
	updates := Group(items)
	s.design.QuickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(7)).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					s.design.QuickTile(c, s.design.Mark, presentation.AnimeAccent.Tile, presentation.AnimeAccent.TileInk)
					ui.Text(c, "今日动画更新").FontSize(s.design.SizeQuickName).FontWeight(600).TextColor(s.design.PanelInk).SingleLine()
					// Keep the count with the thing it counts. The right edge is
					// reserved for the refresh control, as it is on quota panels.
					ui.Box(c).Background(presentation.AnimeAccent.Tint).Radius(float32(10)).Padding(float32(2), float32(6)).Children(func() {
						ui.Text(c, fmt.Sprintf("%d", len(updates))).FontSize(s.design.SizeQuickTiny).FontWeight(600).TextColor(presentation.AnimeAccent.Ink).SingleLine()
					})
				})
				ui.Spacer(c)
				// The refresh mark is the only loading indicator in this compact
				// header. It already turns while a request is in flight; a second
				// spinner beside it only makes the state look duplicated.
				if button := s.design.RefreshButton(c, loading, err != nil && len(items) > 0); button.Clicked() && !loading {
					s.RefreshAsync(true)
				}
			})
			if err != nil {
				s.design.Notice(c, true, s.design.FriendlyError(err))
			}
			for _, update := range updates {
				row := ui.ButtonBase(c).Label(update.SubjectName).FillWidth().Cursor(ui.CursorPointer).Radius(s.design.RadiusTile).Padding(float32(6), float32(8)).Background(presentation.AnimeAccent.Tint).Border(1, presentation.AnimeAccent.Border)
				if row.Hovered() || row.Focused() {
					row = row.Background(presentation.AnimeAccent.Hover).Border(1, presentation.AnimeAccent.HoverBorder)
				}
				row.Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
						var cover *ui.Bitmap
						if s != nil {
							cover = s.Cover(update.SubjectID)
						}
						thumbnail := ui.Box(c).Size(float32(32), float32(32)).Shrink(0).Radius(s.design.RadiusField).Background(presentation.AnimeAccent.Tint).Border(1, presentation.AnimeAccent.Border).Center()
						thumbnail.Children(func() {
							if cover != nil {
								ui.Image(c, cover).Fill().Fit(ui.Cover).Radius(s.design.RadiusField)
								return
							}
							ui.Icon(c, s.design.Mark).Size(float32(14), float32(14)).TextColor(presentation.AnimeAccent.Ink)
						})
						ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
							ui.Text(c, update.SubjectName).FontSize(s.design.SizeQuickFine).FontWeight(600).TextColor(s.design.PanelInk).SingleLine()
							ui.Text(c, EpisodeSummary(update.Episodes)).FontSize(s.design.SizeQuickTiny).TextColor(s.design.PanelFaint).SingleLine()
						})
						ui.Icon(c, s.design.Chevron).Size(float32(14), float32(14)).Shrink(0).TextColor(s.design.ColorTextMuted)
					})
				})
				if row.Clicked() {
					c.OpenURL(fmt.Sprintf("https://bgm.tv/subject/%d", update.SubjectID))
				}
			}
		})
	})
}

// cacheAnimeCovers converts the feature's already-fetched image bytes into
// MyGo bitmaps on the presentation thread. It is called after a refresh, never
// from the view, so drawing remains I/O- and decoding-free.
//
// A cover that cannot be turned into a bitmap is not a failed refresh: the
// release list is what the user came for, and the row falls back to its mark.
// It is logged because a cover that never appears is otherwise unexplained —
// the view draws the same fallback whether the bytes were missing, not an
// image, or a format this build cannot decode.
func (s *ViewModel) PrepareCovers() {
	if s.service == nil {
		return
	}
	// A failed refresh still answers with the releases of the last one that
	// worked, and those covers are worth caching: the view keeps drawing them.
	// The error is logged because the covers of what is on screen will not
	// change until a refresh succeeds, which is otherwise unexplained.
	items, _, _, viewErr := s.service.View()
	if viewErr != nil {
		slog.Warn("the animation releases could not be re-read; their covers are unchanged", "event", "anime_covers_unreadable", "error", viewErr)
	}
	if s != nil {
		s.CacheCovers(items)
	}
}
