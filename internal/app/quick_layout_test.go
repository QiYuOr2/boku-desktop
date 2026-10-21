package app

import (
	"encoding/json"
	"slices"
	"testing"

	"boku/internal/panel/deepseek"
)

func TestQuickWindowLayoutDefaultsAndMoves(t *testing.T) {
	layout := NewQuickWindowLayout()
	if got, want := layout.Order(), defaultQuickBlockOrder; !slices.Equal(got, want) {
		t.Fatalf("default order = %v, want %v", got, want)
	}
	layout.MoveBefore(quickBlockBilibili, quickBlockCodex)
	want := []string{quickBlockDeepSeek, quickBlockBilibili, quickBlockCodex, quickBlockAnime}
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("moved order = %v, want %v", got, want)
	}
}

func TestQuickWindowLayoutMoveToEnd(t *testing.T) {
	layout := NewQuickWindowLayout()
	layout.MoveToEnd(quickBlockCodex)
	want := []string{quickBlockDeepSeek, quickBlockAnime, quickBlockBilibili, quickBlockCodex}
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("moved order = %v, want %v", got, want)
	}

	layout.MoveToEnd(quickBlockCodex)
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("moving the last block changed order to %v, want %v", got, want)
	}
}

func TestQuickPanelIDUsesProviderIdentity(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	got, found := state.quickPanelID(quickBlockDeepSeek)
	if !found || got != deepseek.PanelID {
		t.Errorf("quickPanelID(deepseek) = %q, %v; want %q, true", got, found, deepseek.PanelID)
	}
}

func TestQuickWindowLayoutNormalizesStoredValues(t *testing.T) {
	layout := NewQuickWindowLayout()
	raw := json.RawMessage(`{"order":["codex","codex","unknown"],"hidden":["anime","unknown"]}`)
	if err := layout.Decode(map[string]json.RawMessage{quickLayoutKey: raw}); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	want := []string{quickBlockCodex, quickBlockDeepSeek, quickBlockAnime, quickBlockBilibili}
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if layout.Visible(quickBlockAnime) {
		t.Error("anime block remained visible")
	}
	if !layout.Visible(quickBlockDeepSeek) {
		t.Error("DeepSeek block became hidden")
	}
}

func TestQuickWindowLayoutEncodingRoundTripsVisibility(t *testing.T) {
	layout := NewQuickWindowLayout()
	layout.SetVisible(quickBlockBilibili, false)
	layout.MoveBefore(quickBlockAnime, quickBlockDeepSeek)
	values, err := layout.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	encoded, err := json.Marshal(values[quickLayoutKey])
	if err != nil {
		t.Fatalf("marshal encoded layout: %v", err)
	}
	reloaded := NewQuickWindowLayout()
	if err := reloaded.Decode(map[string]json.RawMessage{quickLayoutKey: encoded}); err != nil {
		t.Fatalf("round-trip Decode failed: %v", err)
	}
	if reloaded.Visible(quickBlockBilibili) {
		t.Error("hidden Bilibili block became visible after round trip")
	}
	if got, want := reloaded.Order()[0], quickBlockAnime; got != want {
		t.Errorf("first reordered block = %q, want %q", got, want)
	}
}
