package app

import (
	"boku/internal/presentation"
	translateui "boku/internal/translate/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type ShortcutRegistrar = translateui.ShortcutRegistrar
type cardWindow = translateui.CardWindow

func clampInt(value, low, high int) int { return max(low, min(value, high)) }

func MyGoShortcuts() ShortcutRegistrar        { return translateui.MyGoShortcuts() }
func (s *State) RegisterShortcuts()           { s.translationUI.RegisterShortcuts() }
func (s *State) UnregisterShortcuts()         { s.translationUI.UnregisterShortcuts() }
func (s *State) RefreshSelectionAccess()      { s.translationUI.RefreshSelectionAccess() }
func (s *State) RequestSelectionAccess() bool { return s.translationUI.RequestSelectionAccess() }
func (s *State) translateSection(c *ui.Context, head *ui.Handle) {
	s.translationUI.SettingsView(c, head)
}
func (s *State) screenNear(at mygo.Point) mygo.Rectangle { return s.translationUI.ScreenNear(at) }
func (s *State) cursorPoint() mygo.Point                 { return s.translationUI.CursorPoint() }
func requestScreenRecording()                            { translateui.RequestScreenRecording() }
func pickerTranslateCards(cards []presentation.Connection) []presentation.Connection {
	return translateui.PickerTranslateCards(cards)
}
func splitTranslateCards(cards []presentation.Connection) ([]presentation.Connection, []presentation.Connection) {
	return translateui.SplitTranslateCards(cards)
}
