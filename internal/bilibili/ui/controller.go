package ui

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
)

func (s *ViewModel) status(text string) { s.host.Status(text); s.host.Refresh() }

func (s *ViewModel) addBilibiliUID(enabled bool) {
	raw := strings.TrimSpace(s.bilibiliUIDInput)
	for _, prefix := range []string{"https://", "http://"} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	raw = strings.TrimPrefix(raw, "space.bilibili.com/")
	raw = strings.SplitN(strings.SplitN(raw, "?", 2)[0], "#", 2)[0]
	raw = strings.TrimSuffix(raw, "/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		s.status("B站设置未保存：请输入有效 UID 或空间链接。")
		return
	}
	ids := s.service.UIDs()
	for _, old := range ids {
		if old == id {
			return
		}
	}
	s.bilibiliUIDInput = ""
	s.saveBilibili(enabled, append(ids, id))
}

func (s *ViewModel) removeBilibiliUID(enabled bool, id int64) {
	ids := make([]int64, 0)
	for _, old := range s.service.UIDs() {
		if old != id {
			ids = append(ids, old)
		}
	}
	s.saveBilibili(enabled, ids)
}

func (s *ViewModel) saveBilibili(enabled bool, ids []int64) {
	minutes, err := strconv.Atoi(strings.TrimSpace(s.bilibiliRefreshInput))
	if err != nil || minutes < 1 || minutes > 60 {
		s.status("B站设置未保存：刷新间隔必须是 1–60 的整数。")
		return
	}
	dynamics, err := strconv.Atoi(strings.TrimSpace(s.bilibiliDynamicInput))
	if err != nil || dynamics < 1 || dynamics > 5 {
		s.status("B站设置未保存：动态条数必须是 1–5 的整数。")
		return
	}
	live, dynamic := s.bilibiliLiveNotice, s.bilibiliDynamicNotice
	service, host := s.service, s.host
	ids = append([]int64(nil), ids...)
	go func() {
		err := service.Configure(context.Background(), enabled, ids, minutes, dynamics, live, dynamic)
		if err == nil && enabled && len(ids) > 0 {
			err = service.RefreshIfNeeded(context.Background())
		}
		host.OnMain(func() {
			if err != nil {
				s.status("B站设置未保存：" + s.design.FriendlyError(err))
			}
			s.bilibiliRefreshInput = strconv.Itoa(service.RefreshMinutes())
			s.bilibiliDynamicInput = strconv.Itoa(service.DynamicCount())
			s.CacheAvatars()
			host.FitQuick()
			host.Refresh()
		})
	}()
}

func (s *ViewModel) RefreshAsync(force bool) {
	if s == nil || s.service == nil {
		return
	}
	service, host := s.service, s.host
	go func() {
		var err error
		if force {
			err = service.Refresh(context.Background())
		} else {
			err = service.RefreshIfNeeded(context.Background())
		}
		if err != nil {
			slog.Warn("Bilibili UPs could not be refreshed", "error", err)
		}
		host.OnMain(func() { s.CacheAvatars(); host.FitQuick() })
	}()
	host.Refresh()
}
