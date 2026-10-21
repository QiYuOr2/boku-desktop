package app

import (
	"github.com/egoist/mygo"

	"boku/internal/preferences"
	preferencesui "boku/internal/preferences/ui"
)

// This file is the appearance the user chose: light, dark, or following the
// desktop.
//
// It is the one setting the presentation layer owns. The choice is written in
// settings.json beside the features' own keys, and the look itself lives in
// design.go: this file decides which palette design.go installs, and tells
// the desktop so that the windows it draws for Boku — title bars, popover
// material — are in the same appearance as the page inside them.
//
// The setting is a Section like a feature's own; the store knows nothing about
// it. "system" is the default, and what an absent or unreadable value means:
// a user who never chose an appearance gets the one their computer is in.

type AppearanceMode = preferences.AppearanceMode
type Appearance = preferences.Appearance
type AppearanceStore = preferences.Store

const (
	AppearanceSystem = preferences.AppearanceSystem
	AppearanceLight  = preferences.AppearanceLight
	AppearanceDark   = preferences.AppearanceDark
)

const appearanceThemeKey = preferences.ThemeKey

// NewAppearanceSection remains a compatibility constructor while callers
// migrate to preferences.NewAppearanceSection.
func NewAppearanceSection() *Appearance { return preferences.NewAppearanceSection() }

// ThemeSource is the appearance to give the desktop. It is what makes the
// title bar and the menus match the page: Boku draws its own colors, but the
// window around them is the system's. (The light quick window's popover
// material follows this too; the dark one does not use a popover at all —
// see State.Open.)
func ThemeSource(mode AppearanceMode) mygo.ThemeSource {
	switch mode.Normalize() {
	case AppearanceLight:
		return mygo.ThemeLight
	case AppearanceDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

// PopoverAppearance gives an already-created native popover the dark
// appearance a forced dark mode draws. It is the one place the presentation
// knows a platform bridge exists; main supplies the implementation, and nil
// means the platform has none, in which case forced dark uses the
// application's card.
//
// It is deliberately a single method: when MyGo exposes the popover's own
// appearance, main replaces (or removes) the implementation and nothing in
// the views or the state machine changes.
type PopoverAppearance interface {
	// SetDark gives the native window handle the dark appearance and reports
	// whether it was set.
	SetDark(window uintptr) bool
}

// SyncAppearance records what the desktop is doing and repaints every open
// window. The main function calls it once the application is ready, and again
// from mygo.Theme.OnUpdated: in the automatic mode, a desktop that turns dark
// at sunset turns Boku dark with it.
func (s *State) SyncAppearance(systemDark bool) {
	s.systemDark = systemDark
	s.dark = s.appearance.Dark(systemDark)
	s.syncPopoverAppearance()
	s.Refresh()
}

// syncPopoverAppearance brings the open quick window's carrier in line with
// the appearance now in force.
//
// The application's card always repaints with the palette, so it needs
// nothing. A native popover is the fragile one: 自动 follows the desktop, a
// forced dark appearance needs the injected dark material, and a forced light
// appearance cannot use a popover at all (the material would follow a dark
// desktop) and closes it for the card the next open draws. A popover that
// was given the injected dark material cannot be returned to the desktop's
// appearance in place, so switching back to 自动 closes it too.
func (s *State) syncPopoverAppearance() {
	if s.panel == nil || !s.panelPopover {
		return
	}
	if !s.quickWindowUsesPopover() {
		s.panel.Close()
		return
	}
	switch s.appearance.Mode() {
	case AppearanceSystem:
		if s.panelInjectedDark {
			s.panel.Close()
		}
	case AppearanceDark:
		if s.popoverAppearance == nil {
			return
		}
		if s.popoverAppearance.SetDark(s.panel.NativeHandle()) {
			s.panelInjectedDark = true
			return
		}
		// The popover's native window may be adopted a moment after the
		// flyout is created. Ask for one more frame and try once there; a
		// closed or replaced window is left alone by the identity check.
		panel := s.panel
		panel.Update(func() {
			if s.panel != panel || !s.panelPopover {
				return
			}
			s.panelInjectedDark = s.popoverAppearance.SetDark(panel.NativeHandle())
		})
	}
}

// setAppearance saves the user's choice and puts it on screen at once.
//
// It goes through the store's Update rather than changing the file a step at
// a time, so that a theme change cannot put back a setting another feature
// changed in between. A write that fails still changes the appearance for
// this run and says so under the control: the user asked for it, and a window
// that ignored the click would be the worse answer.
func (s *State) setAppearance(mode AppearanceMode) {
	mode = mode.Normalize()
	if s.preferencesUI == nil {
		s.preferencesUI = preferencesui.New(nil, s.featureHost())
	}
	s.preferencesUI.SaveAppearance(s.appearance, s.appearanceStore, mode)
	// The fixed modes are known here; the automatic one is the desktop's
	// answer, which the assembly re-reads after it has been told the new
	// source (SyncAppearance). This is the answer for a state with nothing to
	// tell, such as a test.
	s.dark = s.appearance.Dark(s.systemDark)
	s.syncPopoverAppearance()
	if s.applyAppearance != nil {
		s.applyAppearance(mode)
	}
	s.Refresh()
}

func (s *State) appearanceStatusLine() string {
	if s.preferencesUI == nil {
		return ""
	}
	return s.preferencesUI.AppearanceStatus()
}
