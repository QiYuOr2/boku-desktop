package ui

import (
	"boku/internal/domain"
	"context"
	"log/slog"
)

// RefreshPanelAsync fetches one panel away from the main thread: the panel
// whose own refresh action was pressed, and the panel whose block is showing
// a failure.
func (s *State) RefreshPanelAsync(panelID string, force bool) {
	panels := s.panelService
	host := s.host
	if !force && !panels.NeedsRefresh(panelID) {
		return
	}
	go func() {
		ctx := context.Background()
		// What a refresh found, and what it failed with, is written down
		// here: a card can only say a little about a wrong key, a timeout or
		// a service that answered something else, and it is gone when the
		// window closes. The log keeps it.
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
	// The frame the request needs while it is in flight, rather than only the
	// one its answer lands in: the mark by the title turns for as long as a
	// fetch runs, and nothing else would ask for the frames that turn it.
	host.Refresh()
}
