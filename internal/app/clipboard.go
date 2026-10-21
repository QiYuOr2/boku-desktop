package app

import (
	"context"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	historyWidth  = 420
	historyHeight = 640
)

func (s *State) clipboardRegion(c *ui.Context) { s.clipboardUI.QuickView(c, s.clipboardDesign()) }
func (s *State) historyView(c *ui.Context) {
	s.useDesign(c)
	s.clipboardUI.HistoryView(c, s.clipboardDesign())
}
func (s *State) openHistory() {
	if s.clipboard == nil {
		return
	}
	s.syncActivationPolicy()
	if s.historyWindow == nil {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:     "剪贴板历史",
			Width:     historyWidth,
			Height:    historyHeight,
			MinWidth:  340,
			MinHeight: 320,
			Content:   ui.View(s.historyView),
		})
		s.historyWindow = window
		s.syncActivationPolicy()
		window.OnClosed(func() {
			if s.historyWindow == window {
				s.historyWindow = nil
				s.syncActivationPolicy()
			}
		})
	}
	window := s.historyWindow
	if !s.clipboard.PolledRecently() {
		s.clipboard.Open(context.Background())
	}
	window.Show()
	window.Focus()
	// One frame for the list as it stands: the history that was already in
	// memory is worth showing before any clipboard read answers.
	window.Update(func() {})
}
