package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain/deepseek"
	"boku/internal/usecase"
)

// The quick window's own behaviour: what it needs to be built, what its
// actions do, and the refresh it starts behind the frame. What the card draws
// is in card_test.go, the layout in layout_test.go, the settings window in
// settings_test.go, and the clipboard in clipboard_test.go.
func TestNewRequiresItsDependencies(t *testing.T) {
	secrets, err := usecase.NewSecrets(newTestStore())
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := usecase.NewPanels(
		testRegistry{provider: newTestProvider(), adapter: testAdapter{provider: newTestProvider()}},
		newTestStore(),
		nil,
		[]usecase.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
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

// The tray application has no main window, so the quit entry has to work.
func TestQuitActionQuits(t *testing.T) {
	quit := 0
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.quit = func() { quit++ }
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	if err := tt.Click("退出"); err != nil {
		t.Fatalf("click the quit action: %v", err)
	}
	if quit != 1 {
		t.Errorf("quit called %d times after one click, want 1", quit)
	}
}

// MyGo rebuilds the view for every input and repaint, so a view that
// changed state would fight itself.
func TestViewIsIdempotent(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	first := tt.Texts()
	tt.Frame()
	second := tt.Texts()
	if len(first) != len(second) {
		t.Fatalf("redrawing changed the number of texts: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("text %d changed on redraw: %q then %q", i, first[i], second[i])
		}
	}
	// Drawing must not fetch anything: the view is built several times per
	// frame.
	if view := state.panelService.View(deepseek.PanelID); view.Loading {
		t.Error("drawing the view started a refresh")
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
