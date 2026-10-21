package assets

import "testing"

func TestMarksAreSVGDocuments(t *testing.T) {
	marks := map[string][]byte{
		"TV":         TV(),
		"TVBilibili": TVBilibili(),
	}
	for name, data := range marks {
		if len(data) == 0 {
			t.Fatalf("%s is empty", name)
		}
		if string(data[:4]) != "<svg" {
			t.Errorf("%s does not start with <svg: %q", name, string(data[:20]))
		}
	}
	if string(TV()) == string(TVBilibili()) {
		t.Error("the animation block and the followed-UP block share one drawing")
	}
}
