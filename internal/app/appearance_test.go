package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"boku/internal/settings"
)

// The appearance is a setting like any feature's: it has a default, it reads
// only its own key, and it writes the choice back under the same name. These
// are the rules of settings.json, checked where the section is written.
func TestAppearanceDefaultsToFollowingTheDesktop(t *testing.T) {
	appearance := NewAppearanceSection()
	if got := appearance.Name(); got != "appearance" {
		t.Errorf("Name = %q, want appearance", got)
	}
	if got := appearance.Mode(); got != AppearanceSystem {
		t.Errorf("Mode = %q, want %q", got, AppearanceSystem)
	}
	values, err := appearance.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := values[appearanceThemeKey]; got != "system" {
		t.Errorf("%s = %v, want system", appearanceThemeKey, got)
	}
}

func TestAppearanceReadsEachMode(t *testing.T) {
	for _, mode := range []AppearanceMode{AppearanceLight, AppearanceDark, AppearanceSystem} {
		t.Run(string(mode), func(t *testing.T) {
			appearance := NewAppearanceSection()
			values := map[string]json.RawMessage{
				appearanceThemeKey: json.RawMessage(`"` + string(mode) + `"`),
			}
			if err := appearance.Decode(values); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := appearance.Mode(); got != mode {
				t.Errorf("Mode = %q, want %q", got, mode)
			}
		})
	}
}

// A file without the key is a user who never chose: the default stays, as it
// does for every feature's settings.
func TestAppearanceAbsentKeyKeepsTheChoice(t *testing.T) {
	appearance := NewAppearanceSection()
	appearance.Set(AppearanceDark)
	if err := appearance.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := appearance.Mode(); got != AppearanceDark {
		t.Errorf("Mode = %q, want the previous dark", got)
	}
}

// A value this build does not know follows the desktop rather than refusing
// to start: a settings file written by a newer build is still readable.
func TestAppearanceUnknownModeFollowsTheDesktop(t *testing.T) {
	appearance := NewAppearanceSection()
	appearance.Set(AppearanceDark)
	values := map[string]json.RawMessage{appearanceThemeKey: json.RawMessage(`"sepia"`)}
	if err := appearance.Decode(values); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := appearance.Mode(); got != AppearanceSystem {
		t.Errorf("Mode = %q, want %q", got, AppearanceSystem)
	}
}

// A value of the wrong JSON type is a broken file, and the store reports it
// instead of silently changing the user's choice.
func TestAppearanceRejectsAValueThatIsNotAString(t *testing.T) {
	appearance := NewAppearanceSection()
	values := map[string]json.RawMessage{appearanceThemeKey: json.RawMessage(`1`)}
	if err := appearance.Decode(values); err == nil {
		t.Fatal("Decode accepted a number as a theme")
	}
}

func TestAppearanceDarkFollowsTheChoice(t *testing.T) {
	tests := []struct {
		mode       AppearanceMode
		systemDark bool
		want       bool
	}{
		{AppearanceLight, false, false},
		{AppearanceLight, true, false},
		{AppearanceDark, false, true},
		{AppearanceDark, true, true},
		{AppearanceSystem, false, false},
		{AppearanceSystem, true, true},
		{"", true, true},
	}
	for _, test := range tests {
		appearance := NewAppearanceSection()
		appearance.Set(test.mode)
		if got := appearance.Dark(test.systemDark); got != test.want {
			t.Errorf("Dark(%q, system=%v) = %v, want %v", test.mode, test.systemDark, got, test.want)
		}
	}
}

func TestThemeSourceMatchesTheMode(t *testing.T) {
	tests := []struct {
		mode AppearanceMode
		want mygo.ThemeSource
	}{
		{AppearanceLight, mygo.ThemeLight},
		{AppearanceDark, mygo.ThemeDark},
		{AppearanceSystem, mygo.ThemeSystem},
		{"unknown", mygo.ThemeSystem},
	}
	for _, test := range tests {
		if got := ThemeSource(test.mode); got != test.want {
			t.Errorf("ThemeSource(%q) = %q, want %q", test.mode, got, test.want)
		}
	}
}

// appearanceStore is a settings store that records what it was asked to do.
type appearanceStore struct {
	calls   int
	section string
	err     error
	// changeRan says whether the store reached the change: a read that failed
	// does not, which is the case the window still has to answer.
	changeRan bool
}

func (s *appearanceStore) Update(_ context.Context, section string, change func() error) error {
	s.calls++
	s.section = section
	if s.err != nil {
		return s.err
	}
	s.changeRan = true
	return change()
}

// A theme click is written through the store, applied at once, and repaints:
// the file and the frame are the same answer.
func TestSetAppearanceWritesAppliesAndRepaints(t *testing.T) {
	store := &appearanceStore{}
	var applied []AppearanceMode
	state := &State{
		appearance:      NewAppearanceSection(),
		appearanceStore: store,
		applyAppearance: func(mode AppearanceMode) { applied = append(applied, mode) },
	}

	state.setAppearance(AppearanceDark)

	if store.calls != 1 || store.section != "appearance" {
		t.Errorf("the change went through %d store calls for %q, want one for appearance", store.calls, store.section)
	}
	if !store.changeRan {
		t.Error("the store did not run the change")
	}
	if got := state.appearance.Mode(); got != AppearanceDark {
		t.Errorf("stored mode = %q, want dark", got)
	}
	if !state.dark {
		t.Error("the state is still light after choosing dark")
	}
	if len(applied) != 1 || applied[0] != AppearanceDark {
		t.Errorf("applyAppearance got %v, want [dark]", applied)
	}
	if state.appearanceStatusLine() != "" {
		t.Errorf("a successful save said %q", state.appearanceStatusLine())
	}
}

// A file that cannot be written still changes this run's appearance: the
// click was the user's. The window says the choice was not stored, because
// the next launch will not remember it.
func TestSetAppearanceSurvivesAFailedWrite(t *testing.T) {
	store := &appearanceStore{err: errors.New("disk is full")}
	var applied []AppearanceMode
	state := &State{
		appearance:      NewAppearanceSection(),
		appearanceStore: store,
		applyAppearance: func(mode AppearanceMode) { applied = append(applied, mode) },
	}

	state.setAppearance(AppearanceLight)

	if got := state.appearance.Mode(); got != AppearanceLight {
		t.Errorf("stored mode = %q, want light in this run", got)
	}
	if state.dark {
		t.Error("the state is dark after choosing light")
	}
	if len(applied) != 1 || applied[0] != AppearanceLight {
		t.Errorf("applyAppearance got %v, want [light]", applied)
	}
	if !strings.Contains(state.appearanceStatusLine(), "本次运行内已生效") {
		t.Errorf("the failure said %q, want it to say this run still uses the choice", state.appearanceStatusLine())
	}
}

// The real store is the one place this can go wrong in a way a fake cannot
// show: Update reads the file before it runs the change, so a section that
// set its value only before the call would have it overwritten by the value
// already on disk and write that back. The choice has to survive the read.
func TestSetAppearanceSurvivesTheStoresRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	appearance := NewAppearanceSection()
	store, err := settings.New(path, appearance)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	// Give the file a value to be overwritten by: the default is already in
	// memory, so save it once.
	if err := store.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	state := &State{appearance: appearance, appearanceStore: store}
	state.setAppearance(AppearanceDark)
	if state.appearanceStatusLine() != "" {
		t.Fatalf("the save failed: %s", state.appearanceStatusLine())
	}

	reloaded := NewAppearanceSection()
	reopened, err := settings.New(path, reloaded)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := reopened.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reloaded.Mode(); got != AppearanceDark {
		t.Errorf("the file has %q after choosing dark", got)
	}
}

// A fixed appearance cannot keep the system popover: its material follows the
// desktop, not the appearance Boku forces, so the page and its background
// would disagree. Only the automatic mode, whose page and popover both follow
// the desktop, uses the native popover.
func TestTheQuickWindowIsAPopoverOnlyInTheAutomaticAppearance(t *testing.T) {
	state := &State{appearance: NewAppearanceSection()}
	if !state.quickWindowUsesPopover() {
		t.Error("the automatic appearance does not use the system popover")
	}
	state.appearance.Set(AppearanceLight)
	if state.quickWindowUsesPopover() {
		t.Error("a fixed light appearance still asks for the system popover")
	}
	state.appearance.Set(AppearanceDark)
	if state.quickWindowUsesPopover() {
		t.Error("a fixed dark appearance still asks for the system popover")
	}
}

// The card a fixed appearance draws centres itself on the tray icon, where a
// popover's arrow would be. Other platforms keep their end alignment.
func TestTheFixedAppearanceCardCentresOnTheTrayIcon(t *testing.T) {
	tests := []struct {
		goos    string
		popover bool
		want    mygo.Placement
	}{
		{"darwin", true, mygo.PlacementTopEnd},
		{"darwin", false, mygo.PlacementTop},
		{"windows", true, mygo.PlacementTopEnd},
		{"windows", false, mygo.PlacementTopEnd},
		{"linux", false, mygo.PlacementTopEnd},
	}
	for _, test := range tests {
		if got := quickWindowPlacement(test.goos, test.popover); got != test.want {
			t.Errorf("quickWindowPlacement(%q, popover=%v) = %q, want %q",
				test.goos, test.popover, got, test.want)
		}
	}
}

// popoverAppearance records the call the state would make to the platform.
type popoverAppearance struct {
	dark  bool
	calls int
}

func (p *popoverAppearance) SetDark(uintptr) bool {
	p.calls++
	return p.dark
}

// Forced dark uses the native popover only when a platform bridge is
// injected; the automatic appearance always does, and forced light never
// does (its material could follow a dark desktop).
func TestThePopoverBridgeIsOnlyNeededForForcedDark(t *testing.T) {
	bridge := &popoverAppearance{dark: true}
	state := &State{appearance: NewAppearanceSection(), popoverAppearance: bridge}

	state.appearance.Set(AppearanceSystem)
	if !state.quickWindowUsesPopover() {
		t.Error("the automatic appearance stopped using the system popover")
	}
	state.appearance.Set(AppearanceLight)
	if state.quickWindowUsesPopover() {
		t.Error("a forced light appearance used the system popover")
	}
	state.appearance.Set(AppearanceDark)
	if !state.quickWindowUsesPopover() {
		t.Error("a forced dark appearance with a bridge did not use the system popover")
	}

	// Without the bridge, forced dark is the application's card.
	state.popoverAppearance = nil
	if state.quickWindowUsesPopover() {
		t.Error("forced dark used a popover without a bridge to make it dark")
	}
}
