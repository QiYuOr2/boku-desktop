package ui

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"boku/internal/contract/bangumi"
)

func (s *ViewModel) saveAnimeToken() {
	if s.secrets == nil || strings.TrimSpace(s.animeTokenInput) == "" {
		return
	}
	token, secrets, host := strings.TrimSpace(s.animeTokenInput), s.secrets, s.host
	go func() {
		err := secrets.Set(context.Background(), bangumi.ProviderID, "accessToken", token)
		host.OnMain(func() {
			if err != nil {
				host.Status("Bangumi Token 未保存：" + s.design.FriendlyError(err))
			} else {
				s.animeTokenInput = ""
				host.Status("Bangumi Access Token 已保存。")
			}
			host.Refresh()
		})
	}()
}

func (s *ViewModel) saveAnime(enabled bool) {
	if s.service == nil {
		return
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(s.animeRefreshInput))
	if err != nil {
		s.host.Status("动画设置未保存：刷新间隔必须是数字。")
		s.host.Refresh()
		return
	}
	service, host, username := s.service, s.host, s.animeUsername
	go func() {
		err := service.Configure(context.Background(), enabled, username, minutes)
		if err == nil && enabled && strings.TrimSpace(username) != "" {
			err = service.RefreshIfNeeded(context.Background())
		}
		host.OnMain(func() {
			if err != nil {
				host.Status("动画设置未保存：" + s.design.FriendlyError(err))
			}
			s.animeUsername = service.Username()
			s.animeRefreshInput = strconv.Itoa(service.RefreshMinutes())
			s.PrepareCovers()
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
			slog.Warn("anime releases could not be refreshed", "error", err)
		}
		host.OnMain(func() { s.PrepareCovers(); host.FitQuick() })
	}()
	host.Refresh()
}
