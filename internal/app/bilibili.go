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

// NoteBilibiliChanges 在功能成功刷新后调用。首次结果只用来建立基线；
// 通知描述的是用户开始监控以来的变化，绝不是屏幕上已有的每一项。
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
