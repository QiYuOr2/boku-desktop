package app

import (
	"boku/internal/panel/deepseek"
	"boku/internal/preferences"
	"testing"
)

func TestQuickLayoutResolvesProviderAndSettingsEntry(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	id, found := state.quickPanelID("deepseek")
	if !found || id != deepseek.PanelID {
		t.Fatalf("panel = %q, %v", id, found)
	}
	for _, page := range state.settingsPages() {
		if page.kind == sectionQuickLayout && page.name == "面板布局" && page.build != nil {
			return
		}
	}
	t.Fatal("layout settings entry is missing")
}

func TestHiddenReadingBlockDoesNotEnlargeWindow(t *testing.T) {
	layout := preferences.NewQuickWindowLayout()
	state := &State{bilibiliService: testBilibili{}, quickWindowLayout: layout}
	if !state.readingBlocksOn() {
		t.Fatal("visible block should enlarge window")
	}
	layout.SetVisible("bilibili", false)
	if state.readingBlocksOn() {
		t.Fatal("hidden block should not enlarge window")
	}
}
