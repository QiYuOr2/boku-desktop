package ui

import (
	"boku/internal/domain"
	"boku/internal/presentation"
	"fmt"
	"github.com/egoist/mygo/ui"
	"time"
)

// bilibiliUpdates is absent until at least one explicit UP is configured, and
// the block is drawn even when no answer is on screen yet: a creator list is
// something the user configured, so it has a shape before the first reply.
func (s *ViewModel) QuickView(c *ui.Context, design presentation.ReadingDesign) {
	s.design = design
	if s.service == nil || !s.service.Enabled() {
		return
	}
	uids := s.service.UIDs()
	if len(uids) == 0 {
		return
	}
	items, _, loading, err := s.service.View()
	s.design.QuickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(7)).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				// The count belongs to the title, so it sits beside it rather
				// than being pushed to the right edge by the actions.
				ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					s.design.QuickTile(c, s.design.Mark, presentation.BilibiliAccent.Tile, presentation.BilibiliAccent.TileInk)
					ui.Text(c, "关注的 UP").FontSize(s.design.SizeQuickName).FontWeight(600).TextColor(s.design.PanelInk).SingleLine()
					ui.Box(c).Background(presentation.BilibiliAccent.Tint).Radius(float32(10)).Padding(float32(2), float32(6)).Children(func() {
						ui.Text(c, fmt.Sprint(len(uids))).FontSize(s.design.SizeQuickTiny).FontWeight(600).TextColor(presentation.BilibiliAccent.Ink).SingleLine()
					})
				})
				ui.Spacer(c)
				// The refresh mark is the only loading indicator: it already
				// turns while a request is in flight, as it does on the
				// animation block.
				if button := s.design.RefreshButton(c, loading, err != nil && len(items) > 0); button.Clicked() && !loading {
					s.RefreshAsync(true)
				}
			})
			if err != nil {
				s.design.Notice(c, true, s.design.FriendlyError(err))
			}
			for _, item := range items {
				s.bilibiliRow(c, item)
			}
			if len(items) == 0 {
				// Nothing is on screen. The configured creators are still
				// shown as placeholders, so the block keeps its shape and
				// names what it is waiting for — whether the request failed,
				// is still in flight, or settled with nothing to show. The
				// reason for an empty answer is what the notice above says.
				for _, uid := range uids {
					s.bilibiliPlaceholderRow(c, uid)
				}
			}
		})
	})
}

// bilibiliPlaceholderRow is what a configured creator looks like before there
// is anything to say about them: a portrait if one was fetched, the UID as the
// name, and a placeholder line where the post will go.
func (s *ViewModel) bilibiliPlaceholderRow(c *ui.Context, uid int64) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Radius(s.design.RadiusTile).Padding(float32(6), float32(8)).Background(s.design.PanelField).Border(1, presentation.PanelBorder).Children(func() {
		s.bilibiliPortrait(c, s.bilibiliAvatar(uid))
		ui.Column(c).Grow(1).Gap(float32(4)).Children(func() {
			ui.Text(c, fmt.Sprintf("UID %d", uid)).FontSize(s.design.SizeQuickFine).FontWeight(600).TextColor(s.design.PanelFaint).SingleLine()
			s.design.SkeletonBar(c, 120, 0.62)
		})
	})
}

// bilibiliPortrait is one creator's round avatar, falling back to the block's
// own mark when the image is missing or could not be decoded.
func (s *ViewModel) bilibiliPortrait(c *ui.Context, avatar *ui.Bitmap) {
	ui.Box(c).Size(float32(32), float32(32)).Shrink(0).Radius(float32(16)).Background(presentation.BilibiliAccent.Tint).Border(1, presentation.BilibiliAccent.Border).Center().Children(func() {
		if avatar != nil {
			ui.Image(c, avatar).Fill().Fit(ui.Cover).Radius(float32(16))
			return
		}
		ui.Icon(c, s.design.Mark).Size(float32(14), float32(14)).TextColor(presentation.BilibiliAccent.Ink)
	})
}

func (s *ViewModel) bilibiliRow(c *ui.Context, item domain.FollowedUpdate) {
	background, border := s.design.PanelField, presentation.PanelBorder
	if item.Live {
		background, border = presentation.BilibiliAccent.Tint, presentation.BilibiliAccent.Border
	}
	row := ui.ButtonBase(c).Label(item.CreatorName).FillWidth().Cursor(ui.CursorPointer).Radius(s.design.RadiusTile).Padding(float32(6), float32(8)).Background(background).Border(1, border)
	if row.Hovered() || row.Focused() {
		row = row.Background(presentation.BilibiliAccent.Hover).Border(1, presentation.BilibiliAccent.HoverBorder)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			s.bilibiliPortrait(c, s.bilibiliAvatar(item.CreatorID))
			ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(float32(5)).Children(func() {
					ui.Text(c, item.CreatorName).FontSize(s.design.SizeQuickFine).FontWeight(600).TextColor(s.design.PanelInk).SingleLine()
					ui.Text(c, item.Kind).FontSize(s.design.SizeQuickTiny).TextColor(func() ui.Color {
						if item.Live {
							return presentation.BilibiliAccent.BadgeInk
						}
						return s.design.PanelFaint
					}()).Background(func() ui.Color {
						if item.Live {
							return presentation.BilibiliAccent.BadgeFill
						}
						return s.design.PanelLine
					}()).Radius(float32(3)).Padding(float32(1), float32(4)).SingleLine()
				})
				ui.Text(c, item.Summary).FontSize(s.design.SizeQuickTiny).TextColor(s.design.PanelFaint).SingleLine()
				ui.Text(c, bilibiliMeta(item)).FontSize(s.design.SizeQuickTiny).TextColor(s.design.PanelFaint).SingleLine()
			})
			ui.Icon(c, s.design.Chevron).Size(float32(14), float32(14)).Shrink(0).TextColor(func() ui.Color {
				if item.Live {
					return presentation.BilibiliAccent.Ink
				}
				return s.design.ColorTextMuted
			}())
		})
	})
	if row.Clicked() && item.URL != "" {
		c.OpenURL(item.URL)
	}
}

// cacheBilibiliAvatars turns the avatars the feature has fetched into UI
// bitmaps on the presentation thread. It runs after a refresh, never from the
// view, so drawing stays free of I/O and decoding.
//
// A creator whose avatar cannot be decoded keeps the row's mark: the name and
// the post are what the user came for, and a missing portrait is not a failed
// refresh.
func (s *ViewModel) CacheAvatars() {
	if s.service == nil {
		return
	}
	items, _, _, _ := s.service.View()
	seen := make(map[int64]bool, len(items))
	for _, item := range items {
		if seen[item.CreatorID] {
			continue
		}
		seen[item.CreatorID] = true
		if s != nil {
			s.CacheAvatar(item.CreatorID)
		}
	}
}

func bilibiliMeta(item domain.FollowedUpdate) string {
	if item.Live {
		if item.LiveAudience != "" {
			return item.LiveAudience + " 人气 · 直播中"
		}
		return "直播中"
	}
	return RelativeTime(item.PublishedAt, time.Now())
}

func (s *ViewModel) bilibiliAvatar(id int64) *ui.Bitmap {
	if s == nil {
		return nil
	}
	return s.Avatar(id)
}
