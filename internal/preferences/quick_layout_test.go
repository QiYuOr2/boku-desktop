package preferences

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

type layoutStoreFunc func(context.Context, string, func() error) error

func (f layoutStoreFunc) Update(ctx context.Context, section string, change func() error) error {
	return f(ctx, section, change)
}

func TestLayoutSavePreservesInteractionAfterReloadAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		layout := NewQuickWindowLayout()
		layout.MoveToEnd("deepseek")
		layout.SetVisible("anime", false)
		want := layout.Order()
		diskErr := errors.New("write failed")
		err := layout.Save(context.Background(), layoutStoreFunc(func(_ context.Context, section string, change func() error) error {
			if section != layout.Name() {
				t.Fatalf("section=%q", section)
			}
			if err := layout.Decode(map[string]json.RawMessage{quickLayoutKey: json.RawMessage(`{"order":["codex"],"hidden":[]}`)}); err != nil {
				return err
			}
			if err := change(); err != nil {
				return err
			}
			if fail {
				return diskErr
			}
			return nil
		}))
		if (fail && !errors.Is(err, diskErr)) || (!fail && err != nil) {
			t.Fatalf("save error=%v", err)
		}
		if !slices.Equal(layout.Order(), want) || layout.Visible("anime") {
			t.Fatalf("interaction lost: %v", layout.Order())
		}
	}
}

func TestQuickLayoutIgnoresInvalidMoves(t *testing.T) {
	layout := NewQuickWindowLayout()
	layout.MoveBefore("unknown", "codex")
	layout.MoveBefore("codex", "unknown")
	layout.MoveBefore("codex", "codex")
	layout.MoveToEnd("unknown")
	layout.SetVisible("unknown", false)
	if !slices.Equal(layout.Order(), defaultQuickBlockOrder) {
		t.Fatalf("invalid moves changed order: %v", layout.Order())
	}
	if err := layout.Decode(map[string]json.RawMessage{quickLayoutKey: json.RawMessage(`{"order":42}`)}); err == nil {
		t.Fatal("invalid format accepted")
	}
}

func TestQuickWindowLayoutDefaultsAndMoves(t *testing.T) {
	layout := NewQuickWindowLayout()
	if got, want := layout.Order(), defaultQuickBlockOrder; !slices.Equal(got, want) {
		t.Fatalf("default order = %v, want %v", got, want)
	}
	layout.MoveBefore(quickBlockBilibili, quickBlockCodex)
	want := []string{quickBlockPlaytime, quickBlockDeepSeek, quickBlockBilibili, quickBlockCodex, quickBlockGrok, quickBlockAnime}
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("moved order = %v, want %v", got, want)
	}
}

func TestQuickWindowLayoutMoveToEnd(t *testing.T) {
	layout := NewQuickWindowLayout()
	layout.MoveToEnd(quickBlockCodex)
	want := []string{quickBlockPlaytime, quickBlockDeepSeek, quickBlockGrok, quickBlockAnime, quickBlockBilibili, quickBlockCodex}
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("moved order = %v, want %v", got, want)
	}

	layout.MoveToEnd(quickBlockCodex)
	if got := layout.Order(); !slices.Equal(got, want) {
		t.Errorf("moving the last block changed order to %v, want %v", got, want)
	}
}

func TestQuickWindowLayoutNormalizesStoredValues(t *testing.T) {
	layout := NewQuickWindowLayout()
	raw := json.RawMessage(`{"order":["codex","codex","unknown"],"hidden":["anime","unknown"]}`)
	if err := layout.Decode(map[string]json.RawMessage{quickLayoutKey: raw}); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	want := []string{quickBlockPlaytime, quickBlockCodex, quickBlockDeepSeek, quickBlockGrok, quickBlockAnime, quickBlockBilibili}
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
	layout.MoveBefore(quickBlockAnime, quickBlockPlaytime)
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

func TestOlderLayoutKeepsPlaytimeAtTheFrontUntilItIsPlaced(t *testing.T) {
	layout := NewQuickWindowLayout()
	raw := json.RawMessage(`{"order":["bilibili","deepseek","codex","anime"],"hidden":[]}`)
	if err := layout.Decode(map[string]json.RawMessage{quickLayoutKey: raw}); err != nil {
		t.Fatal(err)
	}
	if got := layout.Order()[0]; got != quickBlockPlaytime {
		t.Fatalf("first = %q", got)
	}
	layout.MoveToEnd(quickBlockPlaytime)
	values, err := layout.Encode()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(values[quickLayoutKey])
	if err != nil {
		t.Fatal(err)
	}
	reloaded := NewQuickWindowLayout()
	if err := reloaded.Decode(map[string]json.RawMessage{quickLayoutKey: encoded}); err != nil {
		t.Fatal(err)
	}
	order := reloaded.Order()
	if order[len(order)-1] != quickBlockPlaytime {
		t.Fatalf("placed order = %v", order)
	}
}
