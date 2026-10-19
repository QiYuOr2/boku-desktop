package app

import (
	"errors"
	"testing"

	"boku/internal/platform/autostart"
)

// The login-start switch is the settings page's only switch about the
// machine rather than about Boku: no key in settings.json, and every frame
// asks the platform. These tests drive it with a manager the test decides, so
// they say what the switch does — not what the section draws, which is a
// runtime acceptance.

// testAutostart is a login start the test decides: whether it is on, and what
// it fails with.
type testAutostart struct {
	on bool
	// onErr and setErr are what each answer fails with; nil means it works.
	onErr  error
	setErr error
	// writes records every Start and Stop, in the order they happened.
	writes []bool
	// keys records where the start was said to live.
	keys []string
}

func (a *testAutostart) Read(autostart.Launcher) (bool, error) {
	if a.onErr != nil {
		return false, a.onErr
	}
	return a.on, nil
}

func (a *testAutostart) Location(l autostart.Launcher) (string, error) {
	return "test-start/" + l.Label, nil
}

func (a *testAutostart) Start(autostart.Launcher) error {
	a.writes = append(a.writes, true)
	if a.setErr != nil {
		return a.setErr
	}
	a.on = true
	return nil
}

func (a *testAutostart) Stop(autostart.Launcher) error {
	a.writes = append(a.writes, false)
	if a.setErr != nil {
		return a.setErr
	}
	a.on = false
	return nil
}

// autostartState with an injected manager: the fake state is the real one,
// with the platform's login start replaced.
func autostartState(t *testing.T, manager autostart.Manager) *State {
	t.Helper()
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.autostart = manager
	return state
}

// The switch is drawn from the platform: on when it says on, off when it says
// off, and drawn at all only where the platform has a login start.
func TestAutostartStateReadsThePlatform(t *testing.T) {
	manager := &testAutostart{}
	state := autostartState(t, manager)
	on, enabled, status := state.autostartState()
	if !enabled || on || status.text != "" {
		t.Fatalf("a start that is off = on %v, enabled %v, status %q; want off and enabled", on, enabled, status.text)
	}
	manager.on = true
	if on, enabled, _ := state.autostartState(); !enabled || !on {
		t.Errorf("a start that is on = on %v, enabled %v; want both true", on, enabled)
	}
}

// A State with no manager is one nothing was injected into — a test, or a
// build without an assembly. It draws no switch, and asking it is safe.
func TestAutostartWithoutAManager(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	on, enabled, status := state.autostartState()
	if on || enabled {
		t.Errorf("without a manager = on %v, enabled %v; want neither", on, enabled)
	}
	if status.text != "" {
		t.Errorf("without a manager the section said %q, want nothing", status.text)
	}
	// Pressing it anyway must not panic or write anywhere.
	state.setAutostart(true)
	if status := state.autostartStatus; status.text != "" {
		t.Errorf("setAutostart without a manager said %q, want nothing", status.text)
	}
}

// The switch turning it on writes the start; turning it off takes it away.
// The section then reads the platform again, so what it shows is the system's
// record rather than what was asked for.
func TestAutostartSwitchWritesTheStart(t *testing.T) {
	manager := &testAutostart{}
	state := autostartState(t, manager)
	state.setAutostart(true)
	if len(manager.writes) != 1 || !manager.writes[0] {
		t.Fatalf("the writes were %v, want one Start", manager.writes)
	}
	if on, _, status := state.autostartState(); !on || status.text != "" {
		t.Errorf("after turning it on = on %v, status %q; want on and no complaint", on, status.text)
	}
	state.setAutostart(false)
	if len(manager.writes) != 2 || manager.writes[1] {
		t.Fatalf("the writes were %v, want a Stop after the Start", manager.writes)
	}
	if on, _, status := state.autostartState(); on || status.text != "" {
		t.Errorf("after turning it off = on %v, status %q; want off and no complaint", on, status.text)
	}
}

// A write the system refused leaves the switch where it was and says so: the
// page must not draw a start that did not happen.
func TestAutostartSwitchReportsARefusedWrite(t *testing.T) {
	manager := &testAutostart{setErr: errors.New("the registry refused the write")}
	state := autostartState(t, manager)
	state.setAutostart(true)
	on, _, status := state.autostartState()
	if on {
		t.Error("the switch is on after a write that failed")
	}
	if status.text == "" || status.kind != noteDanger {
		t.Errorf("after a refused write the status = %q, %v; want a danger line", status.text, status.kind)
	}
}

// A platform that cannot answer does not get a switch to draw: the section
// says the mechanism is missing rather than showing a switch that would fail.
func TestAutostartUnsupportedPlatform(t *testing.T) {
	manager := &testAutostart{onErr: autostart.ErrUnsupported}
	state := autostartState(t, manager)
	_, enabled, status := state.autostartState()
	if enabled {
		t.Error("the switch is drawn on a platform with no login start")
	}
	if status.text != autostartUnsupportedNote || status.kind != notePlain {
		t.Errorf("the note = %q, %v; want the unsupported line", status.text, status.kind)
	}
}

// A login item the user turned off in System Settings is not the same as one
// that was never registered: the switch is off, but the note says where to
// allow it instead of pretending the feature is missing or broken.
func TestAutostartNeedsApproval(t *testing.T) {
	manager := &testAutostart{onErr: autostart.ErrNeedsApproval}
	state := autostartState(t, manager)
	on, enabled, status := state.autostartState()
	if on || !enabled {
		t.Errorf("a login item needing approval = on %v, enabled %v; want off and enabled", on, enabled)
	}
	if status.text != autostartApprovalNote || status.kind != notePlain {
		t.Errorf("the status = %q, %v; want the approval note", status.text, status.kind)
	}
}

// A start the system recorded but still needs approval for says so after the
// press too, rather than showing a danger line for a write that happened.
func TestAutostartStartNeedsApproval(t *testing.T) {
	manager := &testAutostart{setErr: autostart.ErrNeedsApproval}
	state := autostartState(t, manager)
	state.setAutostart(true)
	if status := state.autostartStatus; status.text != autostartApprovalNote || status.kind != notePlain {
		t.Errorf("after a start needing approval the status = %q, %v; want the approval note", status.text, status.kind)
	}
}

// A build outside an Applications folder cannot be registered: the page says
// where to put the application instead of showing the system's raw error.
func TestAutostartStartNotInstalled(t *testing.T) {
	manager := &testAutostart{setErr: autostart.ErrNotInstalled}
	state := autostartState(t, manager)
	state.setAutostart(true)
	if status := state.autostartStatus; status.text != autostartNotInstalledNote || status.kind != noteDanger {
		t.Errorf("after a start outside Applications the status = %q, %v; want the install note", status.text, status.kind)
	}
}

// A read that fails is not the same as "off": the page says it could not tell,
// rather than telling the user the start is off when nobody knows.
func TestAutostartReadFailureIsNotOff(t *testing.T) {
	manager := &testAutostart{onErr: errors.New("the launch agent could not be read")}
	state := autostartState(t, manager)
	_, enabled, status := state.autostartState()
	if enabled {
		t.Error("the switch is drawn as working when the platform could not be read")
	}
	if status.kind != noteDanger || status.text == "" {
		t.Errorf("the status = %q, %v; want a danger line", status.text, status.kind)
	}
}

// The line under the switch names where the start lives, which the platform
// decides: a System Settings page, a Windows value name. It is asked, not
// assumed.
func TestAutostartLocationLineComesFromThePlatform(t *testing.T) {
	state := autostartState(t, &testAutostart{})
	location := state.autostartLocationLine()
	if location == "" {
		t.Fatal("the section has no location to show under the switch")
	}
	if got, want := location, "test-start/"+autostartIdentifier; got != want {
		t.Errorf("the location = %q, want %q", got, want)
	}
}
