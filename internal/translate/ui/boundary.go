package ui

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (s *State) SettingsView(c *ui.Context, head *ui.Handle) { s.translateSection(c, head) }
func (s *State) QuickView(c *ui.Context)                     { s.imageTranslationTasks(c) }
func (s *State) StartScreenshotTranslation()                 { s.startScreenshotTranslation() }
func (s *State) StartQRRecognition()                         { s.startQRRecognition() }
func (s *State) HasScreenshot() bool                         { return s.screenshot != nil }
func (s *State) ImageTaskCount() int                         { return len(s.imageTasks) }
func (s *State) ScreenNear(at mygo.Point) mygo.Rectangle     { return s.screenNear(at) }
func (s *State) CursorPoint() mygo.Point                     { return s.cursorPoint() }
func (s *State) Repaint(after func()) bool {
	if s.translateWindow == nil {
		return false
	}
	s.translateWindow.Update(after)
	return true
}
func RequestScreenRecording() { requestScreenRecording() }
func PickerTranslateCards(cards []presentation.Connection) []presentation.Connection {
	return pickerTranslateCards(cards)
}
func SplitTranslateCards(cards []presentation.Connection) ([]presentation.Connection, []presentation.Connection) {
	return splitTranslateCards(cards)
}
