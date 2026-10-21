package app

import (
	"testing"

	"boku/internal/app/assets"
	"github.com/egoist/mygo/ui"
)

// The framework must be able to parse both blocks' marks: a malformed SVG
// fails at startup rather than at the moment a block first draws.
func TestBlockMarksParse(t *testing.T) {
	if _, err := ui.ParseSVG(assets.TV()); err != nil {
		t.Fatalf("the animation mark does not parse: %v", err)
	}
	if _, err := ui.ParseSVG(assets.TVBilibili()); err != nil {
		t.Fatalf("the followed-UP mark does not parse: %v", err)
	}
}
