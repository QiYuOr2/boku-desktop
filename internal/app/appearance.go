package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo"
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

// AppearanceMode is what the user chose in Settings → 通用 → 主题.
type AppearanceMode string

// The three choices, spelled as they are written in settings.json.
const (
	// AppearanceSystem follows the desktop, including a change while the
	// application is running.
	AppearanceSystem AppearanceMode = "system"
	// AppearanceLight and AppearanceDark hold that appearance whatever the
	// desktop is doing.
	AppearanceLight AppearanceMode = "light"
	AppearanceDark  AppearanceMode = "dark"
)

// The settings key the choice lives under. It is the file's spelling, not a
// Go field name: the settings file is read by people.
const appearanceThemeKey = "appearanceTheme"

// appearanceSectionName is what errors call this section.
const appearanceSectionName = "appearance"

// Appearance is the stored choice. It holds the mode rather than being it,
// because the settings store hands a section to the file and takes it back.
type Appearance struct {
	mode AppearanceMode
}

// NewAppearanceSection returns the section at its default: follow the
// desktop. A user who has never opened the settings window is in this state.
func NewAppearanceSection() *Appearance { return &Appearance{mode: AppearanceSystem} }

// Name is what errors call this section.
func (a *Appearance) Name() string { return appearanceSectionName }

// Mode is the appearance the user chose, normalized.
func (a *Appearance) Mode() AppearanceMode { return a.mode.Normalize() }

// Set replaces the choice. An unknown value falls back to following the
// desktop rather than leaving the window without a palette.
func (a *Appearance) Set(mode AppearanceMode) { a.mode = mode.Normalize() }

// Dark reports whether the application is dark given what the desktop is
// doing. A fixed choice wins over the desktop; the system mode is the
// desktop's own answer.
func (a *Appearance) Dark(systemDark bool) bool {
	switch a.Mode() {
	case AppearanceDark:
		return true
	case AppearanceLight:
		return false
	default:
		return systemDark
	}
}

// Decode reads the theme out of the file's flat settings object.
//
// A file without the key leaves the choice as it is — the default for a new
// user, the previous value during a read-modify-write. A value this build
// does not know follows the desktop: a settings file written by a newer build
// must not stop this one from starting.
func (a *Appearance) Decode(values map[string]json.RawMessage) error {
	raw, present := values[appearanceThemeKey]
	if !present {
		return nil
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil {
		return fmt.Errorf("%s: %w", appearanceThemeKey, err)
	}
	a.mode = AppearanceMode(mode).Normalize()
	return nil
}

// Encode hands the choice back to the store.
func (a *Appearance) Encode() (map[string]any, error) {
	return map[string]any{appearanceThemeKey: string(a.Mode())}, nil
}

// Normalize maps the file's spelling to one of the three choices. Empty —
// which is what a zero value holds — is the system, the same as absent.
func (m AppearanceMode) Normalize() AppearanceMode {
	switch AppearanceMode(strings.TrimSpace(string(m))) {
	case AppearanceLight:
		return AppearanceLight
	case AppearanceDark:
		return AppearanceDark
	default:
		return AppearanceSystem
	}
}

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

// appearanceLabels and appearanceModes are the segmented control's own list:
// the order the settings window shows, and the mode each segment stands for.
// The user asked for light, dark, automatic, in that order.
var (
	appearanceLabels = []string{"浅色", "深色", "自动"}
	appearanceModes  = []AppearanceMode{AppearanceLight, AppearanceDark, AppearanceSystem}
)

// appearanceIndex is the segment that is chosen for a mode.
func appearanceIndex(mode AppearanceMode) int {
	mode = mode.Normalize()
	for index, choice := range appearanceModes {
		if choice == mode {
			return index
		}
	}
	// Normalize falls back to the automatic mode, which is the last segment.
	return len(appearanceModes) - 1
}

// AppearanceStore is the part of the settings store the choice is written
// through. It is the same shape the features declare for themselves: this
// package does not depend on the storage implementation.
type AppearanceStore interface {
	Update(ctx context.Context, section string, change func() error) error
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
	s.appearanceStatus = ""
	// The choice is in memory before it is in the file: a write that fails
	// still leaves this run on the appearance the user just picked, which is
	// what the status line under the control then explains.
	s.appearance.Set(mode)
	if s.appearanceStore != nil {
		err := s.appearanceStore.Update(backgroundContext(), s.appearance.Name(), func() error {
			// The store reads the file before it runs this, and that read
			// decoded the appearance that was on disk. The closure is where
			// the new choice is made, so that what is written back is the
			// user's click rather than the value the file already had.
			s.appearance.Set(mode)
			return nil
		})
		if err != nil {
			s.appearanceStatus = "主题没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
		}
		// A failed read never ran the closure, and a failed write ran it. This
		// leaves the run on the user's choice either way.
		s.appearance.Set(mode)
	}
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
