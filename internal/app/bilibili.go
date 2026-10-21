package app

import (
	bilibiliui "boku/internal/bilibili/ui"
	"boku/internal/domain"
	"github.com/egoist/mygo/ui"
)

func (s *State) bilibiliUpdates(c *ui.Context) {
	s.bilibiliUI.QuickView(c, s.readingDesign(s.bilibiliMark))
}
func (s *State) cacheBilibiliAvatars() {
	if s.bilibiliUI != nil {
		s.bilibiliUI.CacheAvatars()
	}
}

// NoteBilibiliChanges is called by the feature after a successful refresh.
// The first result only establishes the baseline; notifications describe a
// change since the user began monitoring, never every item already on screen.
func (s *State) NoteBilibiliChanges(previous, current []domain.FollowedUpdate) {
	if s.bilibiliUI == nil {
		s.bilibiliUI = bilibiliui.New(s.bilibiliService)
		host := s.featureHost()
		s.bilibiliUI.AttachHost(host)
	}
	if s.notifier == nil || !s.notifier.Supported() {
		return
	}
	s.bilibiliUI.NoteChanges(previous, current, func(id, title, body string) {
		s.notifier.Notify(id, title, body, s.Activate)
	})
}
