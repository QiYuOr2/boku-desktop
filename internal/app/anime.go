package app

import "github.com/egoist/mygo/ui"

func (s *State) animeUpdates(c *ui.Context) { s.animeUI.QuickView(c, s.readingDesign(s.tvMark)) }
func (s *State) cacheAnimeCovers() {
	if s.animeUI != nil {
		s.animeUI.PrepareCovers()
	}
}
