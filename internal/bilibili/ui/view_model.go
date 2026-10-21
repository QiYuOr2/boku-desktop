// Package ui owns Bilibili-monitor presentation state and formatting.
package ui

import (
	"boku/internal/domain"
	"boku/internal/presentation"
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

type Service interface {
	Enabled() bool
	UIDs() []int64
	RefreshMinutes() int
	DynamicCount() int
	Notifications() (bool, bool)
	Configure(context.Context, bool, []int64, int, int, bool, bool) error
	View() ([]domain.FollowedUpdate, time.Time, bool, error)
	Refresh(context.Context) error
	RefreshIfNeeded(context.Context) error
	Avatar(int64) []byte
}

type ViewModel struct {
	service                                                      Service
	avatars                                                      map[int64]*ui.Bitmap
	design                                                       presentation.ReadingDesign
	host                                                         presentation.Host
	bilibiliUIDInput, bilibiliRefreshInput, bilibiliDynamicInput string
	bilibiliLiveNotice, bilibiliDynamicNotice                    bool
}

func New(service Service) *ViewModel {
	v := &ViewModel{service: service, avatars: make(map[int64]*ui.Bitmap)}
	if service != nil {
		v.bilibiliRefreshInput = strconv.Itoa(service.RefreshMinutes())
		v.bilibiliDynamicInput = strconv.Itoa(service.DynamicCount())
		v.bilibiliLiveNotice, v.bilibiliDynamicNotice = service.Notifications()
	}
	return v
}

func (v *ViewModel) AttachHost(host presentation.Host) { v.host = host }

func (v *ViewModel) Avatar(id int64) *ui.Bitmap { return v.avatars[id] }

func (v *ViewModel) CacheAvatar(id int64) {
	if v == nil || v.service == nil || v.avatars[id] != nil {
		return
	}
	data := v.service.Avatar(id)
	if len(data) == 0 {
		return
	}
	if bitmap, err := ui.DecodeBitmap(data); err == nil {
		v.avatars[id] = bitmap
	}
}

func RelativeTime(at, now time.Time) string {
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
