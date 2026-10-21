package preferences

import "testing"

func TestAppearanceNormalizesUnknownMode(t *testing.T) {
	appearance := NewAppearanceSection()
	appearance.Set(AppearanceMode("unknown"))
	if got := appearance.Mode(); got != AppearanceSystem {
		t.Fatalf("mode = %q, want system", got)
	}
}
