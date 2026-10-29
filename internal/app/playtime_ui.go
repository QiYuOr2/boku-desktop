package app

// CachePlaytimeCovers decodes cover files that are already on disk. The
// download itself happens on the recorder's side; this only runs when the
// flyout opens or a new file has been saved, and it runs on the main thread.
func (s *State) CachePlaytimeCovers() {
	if s.playtimePanel != nil {
		s.playtimePanel.Prepare(s.playtime)
	}
}
