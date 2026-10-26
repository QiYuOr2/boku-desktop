package app

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/credential"
	"boku/internal/panel"
	"boku/internal/panel/deepseek"
)

func TestNewRequiresItsDependencies(t *testing.T) {
	secrets, err := credential.NewSecrets(newTestStore(t))
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := panel.NewPanels(
		testPanelRegistry(t, newTestProvider(t)),
		newTestStore(t),
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
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "0.1.0")
	state.Activate()
	if state.panel != nil {
		t.Error("Activate opened a quick window with no tray to anchor it to")
	}
}

// Without a version the footer still has to say something.
func TestVersionLineFallsBack(t *testing.T) {
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "")
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
	provider := newTestProvider(t)
	gate := provider.hold()
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	state := testState(t, provider, withCredential(newTestStore(t)), "0.1.0")

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		state.refreshAsync(false)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("refreshAsync waited for the blocked HTTP request")
	}

	deadline := time.Now().Add(2 * time.Second)
	for provider.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if provider.count() != 1 {
		t.Fatalf("the provider was called %d times, want 1", provider.count())
	}
	release()

	// Wait for the fetch to settle, then a second call must not ask again.
	deadline = time.Now().Add(2 * time.Second)
	for state.panelService.View(deepseek.PanelID).Loading && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if view := state.panelService.View(deepseek.PanelID); !view.HasSnapshot || view.Loading || view.Err != nil {
		t.Fatalf("the refresh did not produce a snapshot: %+v", view)
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
