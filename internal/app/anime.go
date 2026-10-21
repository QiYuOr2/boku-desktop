package app

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"boku/internal/domain"
	"boku/internal/platform/logonce"
	"github.com/egoist/mygo/ui"
)

const (
	animePrimary = "#EA9199"
	animeTint    = "#FFF5F6"
	animeBorder  = "#F9DDE0"
	animeHover   = "#FFECEF"
)

type animeSubjectUpdate struct {
	SubjectID   int64
	SubjectName string
	Episodes    []domain.EpisodeRelease
}

// animeUpdates is absent when disabled, unconfigured, or there is no release:
// a zero-result day must not leave an empty section in the compact flyout.
func (s *State) animeUpdates(c *ui.Context) {
	if s.animeService == nil || !s.animeService.Enabled() || s.animeService.Username() == "" {
		return
	}
	items, _, loading, err := s.animeService.View()
	if len(items) == 0 && !loading && err == nil {
		return
	}
	quickDivider(c)
	updates := groupAnimeUpdates(items)
	quickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(7)).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					quickTile(c, panelMark{svg: s.tvMark, tile: ui.Hex(animePrimary), ink: colorOnMark})
					ui.Text(c, "今日动画更新").FontSize(sizeQuickName).FontWeight(600).TextColor(panelInk).SingleLine()
				})
				ui.Spacer(c)
				if loading {
					ui.Spinner(c)
				} else {
					ui.Box(c).Background(ui.Hex(animeTint)).Radius(float32(10)).Padding(float32(2), float32(6)).Children(func() {
						ui.Text(c, fmt.Sprintf("%d", len(updates))).FontSize(sizeQuickTiny).FontWeight(600).TextColor(ui.Hex(animePrimary)).SingleLine()
					})
				}
			})
			if err != nil {
				notice(c, noteDanger, friendlyError(err))
				return
			}
			for _, update := range updates {
				row := ui.ButtonBase(c).Label(update.SubjectName).FillWidth().Cursor(ui.CursorPointer).Radius(radiusTile).Padding(float32(6), float32(8)).Background(ui.Hex(animeTint)).Border(1, ui.Hex(animeBorder))
				if row.Hovered() || row.Focused() {
					row = row.Background(ui.Hex(animeHover)).Border(1, ui.Hex("#F4B8BE"))
				}
				row.Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
						cover := s.animeCovers[update.SubjectID]
						thumbnail := ui.Box(c).Size(float32(32), float32(32)).Shrink(0).Radius(radiusField).Background(ui.Hex("#FCE7E9")).Border(1, ui.Hex(animeBorder)).Center()
						thumbnail.Children(func() {
							if cover != nil {
								ui.Image(c, cover).Fill().Fit(ui.Cover).Radius(radiusField)
								return
							}
							ui.Icon(c, s.tvMark).Size(float32(14), float32(14)).TextColor(ui.Hex(animePrimary))
						})
						ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
							ui.Text(c, update.SubjectName).FontSize(sizeQuickFine).FontWeight(600).TextColor(panelInk).SingleLine()
							ui.Text(c, animeEpisodeSummary(update.Episodes)).FontSize(sizeQuickTiny).TextColor(panelFaint).SingleLine()
						})
						ui.Icon(c, s.chevronRightMark).Size(float32(14), float32(14)).Shrink(0).TextColor(colorTextMuted)
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
func (s *State) cacheAnimeCovers() {
	if s.animeService == nil {
		return
	}
	// A failed refresh still answers with the releases of the last one that
	// worked, and those covers are worth caching: the view keeps drawing them.
	// The error is logged because the covers of what is on screen will not
	// change until a refresh succeeds, which is otherwise unexplained.
	items, _, _, viewErr := s.animeService.View()
	if viewErr != nil {
		slog.Warn("the animation releases could not be re-read; their covers are unchanged", "event", "anime_covers_unreadable", "error", viewErr)
	}
	for _, item := range items {
		if _, exists := s.animeCovers[item.SubjectID]; exists {
			continue
		}
		data := s.animeService.Cover(item.SubjectID)
		if len(data) == 0 {
			continue
		}
		cover, err := ui.DecodeBitmap(data)
		if err != nil {
			// Deduplicated by subject: the same cover is retried after every
			// refresh, and a host that keeps serving a format this build
			// cannot read would otherwise write the line on every poll.
			if key := "anime-cover-" + strconv.FormatInt(item.SubjectID, 10); logonce.Should(key, err.Error()) {
				slog.Warn("an animation cover could not be decoded", "event", "anime_cover_undecodable", "subject", item.SubjectID, "bytes", len(data), "error", err)
			}
			continue
		}
		s.animeCovers[item.SubjectID] = cover
	}
}

func groupAnimeUpdates(items []domain.EpisodeRelease) []animeSubjectUpdate {
	bySubject := make(map[int64]int)
	updates := make([]animeSubjectUpdate, 0, len(items))
	for _, item := range items {
		index, ok := bySubject[item.SubjectID]
		if !ok {
			index = len(updates)
			bySubject[item.SubjectID] = index
			updates = append(updates, animeSubjectUpdate{SubjectID: item.SubjectID, SubjectName: item.SubjectName})
		}
		updates[index].Episodes = append(updates[index].Episodes, item)
	}
	return updates
}

func animeEpisodeSummary(items []domain.EpisodeRelease) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		episode := fmt.Sprintf("第 %g 集", item.EpisodeSort)
		if name := strings.TrimSpace(item.EpisodeName); name != "" {
			episode += " · " + name
		}
		parts = append(parts, episode)
	}
	return strings.Join(parts, "、")
}
