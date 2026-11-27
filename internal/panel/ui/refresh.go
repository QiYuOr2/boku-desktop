package ui

import (
	"boku/internal/domain"
	"context"
	"log/slog"
)

// RefreshPanelAsync 在主线程之外拉取单个面板。
func (s *State) RefreshPanelAsync(panelID string, force bool) {
	panels := s.panelService
	host := s.host
	if !force && !panels.NeedsRefresh(panelID) {
		return
	}
	go func() {
		ctx := context.Background()
		// 卡片对错误原因只能简短提示，且窗口关闭后即丢失，因此把结果与失败写进日志。
		var (
			snapshot domain.Snapshot
			asked    = true
			err      error
		)
		if force {
			snapshot, err = panels.Refresh(ctx, panelID)
		} else {
			snapshot, asked, err = panels.RefreshIfNeeded(ctx, panelID)
		}
		switch {
		case err != nil:
			slog.Warn("the panel could not be refreshed", "panel", panelID, "error", err)
		case asked:
			slog.Info("the panel was refreshed", "panel", panelID, "status", string(snapshot.Status))
		}
		host.Refresh()
	}()
	// 请求进行中就必须持续申请刷新帧：标题旁的标记要在整个拉取期间转动，
	// 否则没有其它东西会驱动它。
	host.Refresh()
}
