// Package ui owns animation-update presentation state and view helpers.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/platform/logonce"
	"boku/internal/presentation"
)

// ViewModel owns UI-only decoded cover bitmaps. The feature service retains
// source bytes; converting them for MyGo belongs to presentation, not to the
// network/cache layer.
type Service interface {
	Enabled() bool
	Username() string
	RefreshMinutes() int
	Configure(context.Context, bool, string, int) error
	View() ([]domain.EpisodeRelease, time.Time, bool, error)
	Refresh(context.Context) error
	RefreshIfNeeded(context.Context) error
	Cover(int64) []byte
}

type SecretWriter interface {
	Set(context.Context, string, string, string) error
}

type ViewModel struct {
	service                                           Service
	covers                                            map[int64]*ui.Bitmap
	design                                            presentation.ReadingDesign
	host                                              presentation.Host
	secrets                                           SecretWriter
	animeUsername, animeRefreshInput, animeTokenInput string
}

func New(service Service) *ViewModel {
	v := &ViewModel{service: service, covers: make(map[int64]*ui.Bitmap)}
	if service != nil {
		v.animeUsername = service.Username()
		v.animeRefreshInput = strconv.Itoa(service.RefreshMinutes())
	}
	return v
}

func (v *ViewModel) AttachHost(host presentation.Host, secrets SecretWriter) {
	v.host, v.secrets = host, secrets
}

func (v *ViewModel) Cover(subjectID int64) *ui.Bitmap { return v.covers[subjectID] }

func (v *ViewModel) CacheCovers(items []domain.EpisodeRelease) {
	if v == nil || v.service == nil {
		return
	}
	for _, item := range items {
		if _, exists := v.covers[item.SubjectID]; exists {
			continue
		}
		data := v.service.Cover(item.SubjectID)
		if len(data) == 0 {
			continue
		}
		bitmap, err := ui.DecodeBitmap(data)
		if err != nil {
			if logonce.Should(CoverKey(item.SubjectID), err.Error()) {
				slog.Warn("an animation cover could not be decoded", "event", "anime_cover_undecodable", "subject", item.SubjectID, "bytes", len(data), "error", err)
			}
			continue
		}
		v.covers[item.SubjectID] = bitmap
	}
}

type SubjectUpdate struct {
	SubjectID   int64
	SubjectName string
	Episodes    []domain.EpisodeRelease
}

func Group(items []domain.EpisodeRelease) []SubjectUpdate {
	bySubject := make(map[int64]int)
	updates := make([]SubjectUpdate, 0, len(items))
	for _, item := range items {
		index, ok := bySubject[item.SubjectID]
		if !ok {
			index = len(updates)
			bySubject[item.SubjectID] = index
			updates = append(updates, SubjectUpdate{SubjectID: item.SubjectID, SubjectName: item.SubjectName})
		}
		updates[index].Episodes = append(updates[index].Episodes, item)
	}
	return updates
}

func EpisodeSummary(items []domain.EpisodeRelease) string {
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

func CoverKey(subjectID int64) string { return "anime-cover-" + strconv.FormatInt(subjectID, 10) }
