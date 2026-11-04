package app

import "github.com/egoist/mygo/ui"

func (s *State) liveSection(c *ui.Context, head *ui.Handle) {
	if s.liveUI == nil {
		return
	}
	s.useDesign(c)
	s.liveUI.SettingsView(c, head)
}
