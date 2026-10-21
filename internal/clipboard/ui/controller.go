package ui

import (
	"boku/internal/domain"
	"log/slog"
	"time"
)

func (s *State) copyEntry(entry domain.ClipboardEntry) {
	if s.clipboard == nil {
		return
	}
	s.copiedEntry, s.copiedAt = entry.ID, time.Now()
	host := s.host
	host.Refresh()
	s.clipboard.CopyAsync(entry, func(err error) {
		if err == nil {
			return
		}
		slog.Warn("the clipboard entry could not be written back", "error", err)
		host.OnMain(func() {
			if s.copiedEntry == entry.ID {
				s.clearCopied()
			}
		})
	})
	deadline := s.copiedAt
	time.AfterFunc(700*time.Millisecond, func() {
		host.OnMain(func() {
			if s.copiedAt.Equal(deadline) {
				s.clearCopied()
			}
		})
	})
}

func (s *State) clearCopied() { s.copiedEntry = ""; s.copiedAt = time.Time{} }
