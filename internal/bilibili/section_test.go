package bilibili

import (
	"encoding/json"
	"testing"
)

func TestSectionKeepsDefaultsForMissingKeys(t *testing.T) {
	s := NewSection()
	if err := s.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	v := s.Settings()
	if !v.Enabled || v.RefreshMinutes != 5 || v.DynamicCount != 1 {
		t.Fatalf("settings = %#v", v)
	}
}

func TestSectionNormalizesUIDsAndLimits(t *testing.T) {
	s := NewSection()
	s.Set(Settings{UIDs: []int64{2, 2, 0, 1, 3, 4, 5, 6, 7, 8, 9}, RefreshMinutes: 99, DynamicCount: 9})
	v := s.Settings()
	if len(v.UIDs) != 8 || v.UIDs[0] != 1 || v.UIDs[7] != 8 {
		t.Fatalf("UIDs = %v", v.UIDs)
	}
	if v.RefreshMinutes != 5 || v.DynamicCount != 1 {
		t.Fatalf("limits = %#v", v)
	}
}
