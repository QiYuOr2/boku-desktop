package app

import (
	"fmt"
	"time"

	"boku/internal/domain"
	"github.com/egoist/mygo/ui"
)

const (
	bilibiliPrimary = "#FB7299"
	bilibiliTint    = "#FFF0F3"
	bilibiliBorder  = "#FCD5E0"
)

// bilibiliUpdates is absent until at least one explicit UP is configured, and
// the block is drawn even when no answer is on screen yet: a creator list is
// something the user configured, so it has a shape before the first reply.
func (s *State) bilibiliUpdates(c *ui.Context) {
	if s.bilibiliService == nil || !s.bilibiliService.Enabled() {
		return
	}
	uids := s.bilibiliService.UIDs()
	if len(uids) == 0 {
		return
	}
	items, _, loading, err := s.bilibiliService.View()
	quickDivider(c)
	quickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(7)).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				// The count belongs to the title, so it sits beside it rather
				// than being pushed to the right edge by the actions.
				ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					quickTile(c, panelMark{svg: s.bilibiliMark, tile: ui.Hex(bilibiliTint), ink: ui.Hex(bilibiliPrimary)})
					ui.Text(c, "关注的 UP").FontSize(sizeQuickName).FontWeight(600).TextColor(panelInk).SingleLine()
					ui.Box(c).Background(ui.Hex(bilibiliTint)).Radius(float32(10)).Padding(float32(2), float32(6)).Children(func() {
						ui.Text(c, fmt.Sprint(len(uids))).FontSize(sizeQuickTiny).FontWeight(600).TextColor(ui.Hex(bilibiliPrimary)).SingleLine()
					})
				})
				ui.Spacer(c)
				// The refresh mark is the only loading indicator: it already
				// turns while a request is in flight, as it does on the
				// animation block.
				if button := s.refreshButton(c, loading, err != nil && len(items) > 0); button.Clicked() && !loading {
					s.refreshBilibiliAsync(true)
				}
			})
			if err != nil {
				notice(c, noteDanger, friendlyError(err))
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
func (s *State) bilibiliPlaceholderRow(c *ui.Context, uid int64) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Radius(radiusTile).Padding(float32(6), float32(8)).Background(panelField).Border(1, ui.Hex("#F1F0EF")).Children(func() {
		s.bilibiliPortrait(c, s.bilibiliAvatars[uid])
		ui.Column(c).Grow(1).Gap(float32(4)).Children(func() {
			ui.Text(c, fmt.Sprintf("UID %d", uid)).FontSize(sizeQuickFine).FontWeight(600).TextColor(panelFaint).SingleLine()
			skeletonBar(c, 120, 0.62)
		})
	})
}

// bilibiliPortrait is one creator's round avatar, falling back to the block's
// own mark when the image is missing or could not be decoded.
func (s *State) bilibiliPortrait(c *ui.Context, avatar *ui.Bitmap) {
	ui.Box(c).Size(float32(32), float32(32)).Shrink(0).Radius(float32(16)).Background(ui.Hex(bilibiliTint)).Border(1, ui.Hex(bilibiliBorder)).Center().Children(func() {
		if avatar != nil {
			ui.Image(c, avatar).Fill().Fit(ui.Cover).Radius(float32(16))
			return
		}
		ui.Icon(c, s.bilibiliMark).Size(float32(14), float32(14)).TextColor(ui.Hex(bilibiliPrimary))
	})
}

func (s *State) bilibiliRow(c *ui.Context, item domain.FollowedUpdate) {
	background, border := panelField, ui.Hex("#F1F0EF")
	if item.Live {
		background, border = ui.Hex(bilibiliTint), ui.Hex(bilibiliBorder)
	}
	row := ui.ButtonBase(c).Label(item.CreatorName).FillWidth().Cursor(ui.CursorPointer).Radius(radiusTile).Padding(float32(6), float32(8)).Background(background).Border(1, border)
	if row.Hovered() || row.Focused() {
		row = row.Background(ui.Hex("#FFE8EF"))
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			s.bilibiliPortrait(c, s.bilibiliAvatars[item.CreatorID])
			ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(float32(5)).Children(func() {
					ui.Text(c, item.CreatorName).FontSize(sizeQuickFine).FontWeight(600).TextColor(panelInk).SingleLine()
					ui.Text(c, item.Kind).FontSize(sizeQuickTiny).TextColor(func() ui.Color {
						if item.Live {
							return colorOnMark
						}
						return panelFaint
					}()).Background(func() ui.Color {
						if item.Live {
							return ui.Hex(bilibiliPrimary)
						}
						return panelLine
					}()).Radius(float32(3)).Padding(float32(1), float32(4)).SingleLine()
				})
				ui.Text(c, item.Summary).FontSize(sizeQuickTiny).TextColor(panelFaint).SingleLine()
				ui.Text(c, bilibiliMeta(item)).FontSize(sizeQuickTiny).TextColor(panelFaint).SingleLine()
			})
			ui.Icon(c, s.chevronRightMark).Size(float32(14), float32(14)).Shrink(0).TextColor(func() ui.Color {
				if item.Live {
					return ui.Hex(bilibiliPrimary)
				}
				return colorTextMuted
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
func (s *State) cacheBilibiliAvatars() {
	if s.bilibiliService == nil {
		return
	}
	items, _, _, _ := s.bilibiliService.View()
	seen := make(map[int64]bool, len(items))
	for _, item := range items {
		if seen[item.CreatorID] {
			continue
		}
		seen[item.CreatorID] = true
		if _, exists := s.bilibiliAvatars[item.CreatorID]; exists {
			continue
		}
		data := s.bilibiliService.Avatar(item.CreatorID)
		if len(data) == 0 {
			continue
		}
		bitmap, err := ui.DecodeBitmap(data)
		if err != nil {
			continue
		}
		s.bilibiliAvatars[item.CreatorID] = bitmap
	}
}

func bilibiliMeta(item domain.FollowedUpdate) string {
	if item.Live {
		if item.LiveAudience != "" {
			return item.LiveAudience + " 人气 · 直播中"
		}
		return "直播中"
	}
	return relativeTime(item.PublishedAt, time.Now())
}

// NoteBilibiliChanges is called by the feature after a successful refresh.
// The first result only establishes the baseline; notifications describe a
// change since the user began monitoring, never every item already on screen.
func (s *State) NoteBilibiliChanges(previous, current []domain.FollowedUpdate) {
	if len(previous) == 0 || s.bilibiliService == nil {
		return
	}
	s.refresh(func() {
		liveNotice, dynamicNotice := s.bilibiliService.Notifications()
		if s.notifier == nil || !s.notifier.Supported() {
			return
		}
		old := make(map[int64]domain.FollowedUpdate, len(previous))
		for _, item := range previous {
			if _, seen := old[item.CreatorID]; !seen {
				old[item.CreatorID] = item
			}
		}
		seen := make(map[int64]bool, len(current))
		for _, item := range current {
			if seen[item.CreatorID] {
				continue
			}
			seen[item.CreatorID] = true
			before, ok := old[item.CreatorID]
			if !ok {
				continue
			}
			if liveNotice && item.Live && !before.Live {
				s.notifier.Notify(fmt.Sprintf("boku-bilibili-live-%d-%d", item.CreatorID, item.LiveRoomID), item.CreatorName+" 开播了", item.LiveTitle, s.Activate)
				continue
			}
			if dynamicNotice && !item.Live && item.URL != "" && item.URL != before.URL {
				s.notifier.Notify("boku-bilibili-dynamic-"+fmt.Sprint(item.CreatorID), item.CreatorName+" 发布了新动态", item.Summary, s.Activate)
			}
		}
	})
}
func relativeTime(at, now time.Time) string {
	d := now.Sub(at)
	if d < time.Minute {
		return "刚刚"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	}
	return fmt.Sprintf("%d 天前", int(d.Hours()/24))
}
