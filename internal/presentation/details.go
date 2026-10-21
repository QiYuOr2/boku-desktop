package presentation

import (
	"boku/internal/domain"
	"github.com/egoist/mygo/ui"
	"math"
)

func DisclosureArrow(c *ui.Context, deg float32, ink ui.Color) {
	ui.Box(c).Size(float32(12), float32(12)).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy, d := r.X+r.W/2, r.Y+r.H/2, r.W/8
		s, co := math.Sincos(float64(deg) * math.Pi / 180)
		sin, cos := float32(s), float32(co)
		at := func(x, y float32) (float32, float32) {
			return cx + x*cos - y*sin, cy + x*sin + y*cos
		}
		x1, y1 := at(-d, -2*d)
		x2, y2 := at(d, 0)
		x3, y3 := at(-d, 2*d)
		var path ui.Path
		path.MoveTo(x1, y1)
		path.LineTo(x2, y2)
		path.LineTo(x3, y3)
		p.StrokePath(&path, 1.5, ink)
	})
}

func AnnotationNote(segment domain.Segment, showReading, showGloss bool) (string, bool) {
	switch {
	case showGloss && segment.Gloss != "":
		return segment.Gloss, true
	case showReading && segment.Reading != "":
		return segment.Reading, false
	}
	return "", false
}
