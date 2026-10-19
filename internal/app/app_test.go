package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/credential"
	"boku/internal/panel"
	"boku/internal/panel/deepseek"
)

// The quick window's own behaviour: what it needs to be built, what its
// actions do, and the refresh it starts behind the frame. What the card draws
// is in card_test.go, the layout in layout_test.go, the settings window in
// settings_test.go, and the clipboard in clipboard_test.go.
func TestNewRequiresItsDependencies(t *testing.T) {
	secrets, err := credential.NewSecrets(newTestStore())
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := panel.NewPanels(
		testRegistry{provider: newTestProvider(), adapter: testAdapter{provider: newTestProvider()}},
		newTestStore(),
		panel.NewSection(),
		nil,
		[]panel.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if _, err := New(mygo.App, Deps{Secrets: secrets}); err == nil {
		t.Error("New accepted a state without a panel service")
	}
	if _, err := New(mygo.App, Deps{Panels: panels}); err == nil {
		t.Error("New accepted a state without a secret service")
	}
	if _, err := New(nil, Deps{Panels: panels, Secrets: secrets}); err == nil {
		t.Error("New accepted a state without an application")
	}
}

// A second launch brings the quick window forward, but never against a tray
// that does not exist yet: MyGo's NewFlyout refuses a flyout with neither a
// tray nor a parent, and a launch arriving while the application is still
// starting must not take it down.
func TestActivateWithoutATrayOpensNothing(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.Activate()
	if state.panel != nil {
		t.Error("Activate opened a quick window with no tray to anchor it to")
	}
}

// Without a version the footer still has to say something.
func TestVersionLineFallsBack(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "")
	if got := state.versionLine(); got != "Boku dev" {
		t.Errorf("versionLine = %q, want Boku dev", got)
	}
	state.version = "1.2.3"
	if got := state.versionLine(); got != "Boku 1.2.3" {
		t.Errorf("versionLine = %q, want Boku 1.2.3", got)
	}
}

// refreshAsync is what the interface calls; it must never block the frame,
// and it must ask only when there is something to ask for.
func TestRefreshAsyncDoesNotBlockAndOnlyAsksWhenNeeded(t *testing.T) {
	provider := newTestProvider()
	gate := provider.hold()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")

	start := time.Now()
	state.refreshAsync(false)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("refreshAsync blocked for %s", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for provider.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if provider.count() != 1 {
		t.Fatalf("the provider was called %d times, want 1", provider.count())
	}
	close(gate)

	// Wait for the fetch to settle, then a second call must not ask again.
	deadline = time.Now().Add(2 * time.Second)
	for state.panelService.View(deepseek.PanelID).Loading && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state.refreshAsync(false)
	time.Sleep(20 * time.Millisecond)
	if got := provider.count(); got != 1 {
		t.Errorf("a fresh panel was fetched again: %d calls, want 1", got)
	}
}

// Window.Update invokes its callback unconditionally. A refresh with no
// state change must therefore still hand it a callable no-op; otherwise any
// open settings window panics while navigating its sidebar.
func TestRefreshCallbackIsAlwaysCallable(t *testing.T) {
	refreshCallback(nil)()

	calls := 0
	refreshCallback(func() { calls++ })()
	if calls != 1 {
		t.Errorf("the refresh callback ran %d times, want once", calls)
	}
}
