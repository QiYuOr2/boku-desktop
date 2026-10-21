package app

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) card(c *ui.Context) { s.panelUI.QuickView(c, s.dark) }
func (s *State) refreshButton(c *ui.Context, loading, retry bool) ui.Element {
	return presentation.RefreshButton(c, s.refreshMark, loading, retry)
}
func friendlyError(err error) string { return presentation.FriendlyError(err) }
