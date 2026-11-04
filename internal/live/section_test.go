package live

import (
	"encoding/json"
	"testing"
)

func TestMissingKeysKeepTheDefaults(t *testing.T) {
	section := NewSection()
	section.current.Enabled = true
	if err := section.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	got := section.Current()
	if !got.Enabled || got.Input != InputSystem || got.ASRModel != defaultASRModel || got.MTModel != defaultMTModel || got.Opacity != defaultOpacity {
		t.Fatalf("%+v", got)
	}
}

func TestOpacityStaysInRange(t *testing.T) {
	section := NewSection()
	low, _ := json.Marshal(1)
	if err := section.Decode(map[string]json.RawMessage{keyOpacity: low}); err != nil {
		t.Fatal(err)
	}
	if section.Current().Opacity != minOpacity {
		t.Fatalf("%+v", section.Current())
	}
	high, _ := json.Marshal(180)
	if err := section.Decode(map[string]json.RawMessage{keyOpacity: high}); err != nil {
		t.Fatal(err)
	}
	if section.Current().Opacity != maxOpacity {
		t.Fatalf("%+v", section.Current())
	}
	encoded, err := section.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded[keyOpacity] != maxOpacity {
		t.Fatalf("%+v", encoded)
	}
}

func TestPresentKeysReplaceOnlyThemselves(t *testing.T) {
	section := NewSection()
	raw, _ := json.Marshal(InputMicrophone)
	if err := section.Decode(map[string]json.RawMessage{keyInput: raw}); err != nil {
		t.Fatal(err)
	}
	if section.Current().Input != InputMicrophone || section.Current().Enabled {
		t.Fatalf("%+v", section.Current())
	}
	encoded, err := section.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded[keyInput] != InputMicrophone || encoded[keyEnabled] != false {
		t.Fatalf("%+v", encoded)
	}
	if _, ok := encoded["liveAccelerator"]; ok {
		t.Fatal("live translation has no shortcut key")
	}
}
