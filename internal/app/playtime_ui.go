package app

import "boku/internal/platform/gamepad"

// SetPlaytimeControllers is the controller list the playtime title draws.
// The watcher calls it on the main thread with a snapshot it already took.
func (s *State) SetPlaytimeControllers(pads []gamepad.Pad) {
	if s == nil || s.playtimePanel == nil {
		return
	}
	s.playtimePanel.SetControllers(pads)
}

// CachePlaytimeCovers decodes cover files that are already on disk. The
// download itself happens on the recorder's side; this only runs when the
// flyout opens or a new file has been saved, and it runs on the main thread.
func (s *State) CachePlaytimeCovers() {
	if s.playtimePanel != nil {
		s.playtimePanel.Prepare(s.playtime)
	}
}
