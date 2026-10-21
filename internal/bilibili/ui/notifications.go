package ui

import (
	"boku/internal/domain"
	"fmt"
)

func (s *ViewModel) NoteChanges(previous, current []domain.FollowedUpdate, notify func(string, string, string)) {
	if len(previous) == 0 || s.service == nil {
		return
	}
	s.host.OnMain(func() {
		liveNotice, dynamicNotice := s.service.Notifications()
		if notify == nil {
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
				notify(fmt.Sprintf("boku-bilibili-live-%d-%d", item.CreatorID, item.LiveRoomID), item.CreatorName+" 开播了", item.LiveTitle)
				continue
			}
			if dynamicNotice && !item.Live && item.URL != "" && item.URL != before.URL {
				notify("boku-bilibili-dynamic-"+fmt.Sprint(item.CreatorID), item.CreatorName+" 发布了新动态", item.Summary)
			}
		}
	})
}
