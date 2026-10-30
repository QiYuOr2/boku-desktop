package assets

import (
	"bytes"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMarksAreSVGDocuments(t *testing.T) {
	marks := map[string][]byte{
		"TV":         TV(),
		"TVBilibili": TVBilibili(),
		"DualSense":  DualSense(),
		"SwitchPro":  SwitchPro(),
		"Xbox":       Xbox(),
		"Lightbulb":  Lightbulb(),
		"Command":    Command(),
		"Return":     Return(),
	}
	for name, data := range marks {
		t.Run(name, func(t *testing.T) {
			if _, err := ui.ParseSVG(data); err != nil {
				t.Fatalf("parse %s SVG: %v", name, err)
			}
		})
	}
	if bytes.Equal(TV(), TVBilibili()) {
		t.Error("the animation block and the followed-UP block share one drawing")
	}
}
