package ui

import (
	"sort"
	"time"

	"boku/internal/playtime"
	"boku/internal/presentation"

	"github.com/egoist/mygo/ui"
)

// dayChip is the legend square beside a game's name. It is the same 10-point
// rounded mark the month heat legend uses, so a color means the same kind of
// thing in both places: a key, not a decoration.
const dayChip = 10

// ribbonLayers is how many games the 6-point track can still show when they
// run at the same time. A fourth would be under two points tall. It stays in
// the list; only the bar leaves it out of that slice.
const ribbonLayers = 3

// ribbonMark is the thinnest a session is allowed to paint. Under about twenty
// minutes the true width disappears on a 0–24 track.
const ribbonMark = 2

// ribbonBand is one painted piece of the day track. Start and End are
// fractions of the local day. Lane is this game's row in that instant, and
// Lanes is how many rows that instant has.
type ribbonBand struct {
	Start float64
	End   float64
	Game  int
	Lane  int
	Lanes int
}

// gameInk is the color of one game on this day, longest first. The order is
// the palette the panel already has. It is not stored: tomorrow's longest
// game takes the brand green again.
func gameInk(index int) ui.Color {
	switch index {
	case 0:
		return presentation.ColorBrand
	case 1:
		return presentation.ColorSkyInk
	case 2:
		return presentation.ColorAmberInk
	case 3:
		return presentation.ColorVioletInk
	default:
		return presentation.ColorStoneInk
	}
}

func dayRibbon(c *ui.Context, games []playtime.GameDay, day time.Time) {
	bands := dayBands(games, day)
	if len(bands) == 0 {
		return
	}
	colors := make([]ui.Color, len(games))
	for i := range colors {
		colors[i] = gameInk(i)
	}
	font := ui.Font{Size: presentation.SizeQuickTiny}
	metrics := font.Metrics()
	labelH := metrics.Ascent + metrics.Descent
	gap := float32(2)
	ui.Box(c).FillWidth().Height(labelH + gap + presentation.BarHeight).Label("一天中的游戏时段").Draw(func(p *ui.Painter, r ui.Rect) {
		faint := presentation.PanelFaint
		labels := [...]string{"0", "6", "12", "18", "24"}
		for i, label := range labels {
			glyphs := ui.Shape(label, font)
			width := glyphWidth(glyphs)
			x := r.X + r.W*float32(i)/float32(len(labels)-1) - width/2
			if i == 0 {
				x = r.X
			}
			if i == len(labels)-1 {
				x = r.X + r.W - width
			}
			p.Glyphs(glyphs, x, r.Y+metrics.Ascent, faint)
		}
		track := ui.Rect{X: r.X, Y: r.Y + labelH + gap, W: r.W, H: presentation.BarHeight}
		p.Fill(track, presentation.PanelLine, 999)
		for _, band := range bands {
			paintBand(p, track, band, colors)
		}
	})
}

func glyphWidth(glyphs []ui.Glyph) float32 {
	if len(glyphs) == 0 {
		return 0
	}
	last := glyphs[len(glyphs)-1]
	return last.X + last.Advance
}

func paintBand(p *ui.Painter, track ui.Rect, band ribbonBand, colors []ui.Color) {
	if band.Game < 0 || band.Game >= len(colors) || band.Lanes < 1 || band.End <= band.Start {
		return
	}
	x := track.X + track.W*float32(band.Start)
	w := track.W * float32(band.End-band.Start)
	if w < ribbonMark && track.W >= ribbonMark {
		x -= (ribbonMark - w) / 2
		w = ribbonMark
		if x < track.X {
			x = track.X
		}
		if x+w > track.X+track.W {
			x = track.X + track.W - w
		}
	}
	laneH := track.H / float32(band.Lanes)
	y := track.Y + float32(band.Lane)*laneH
	h := laneH
	if band.Lane == band.Lanes-1 {
		h = track.Y + track.H - y
	}
	p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, colors[band.Game], 999)
}

// dayBands lays each game's runs on one local day. Games that do not overlap
// share the single row. Where they do, the slice splits into one row per
// game, longest first, and stops at ribbonLayers.
func dayBands(games []playtime.GameDay, day time.Time) []ribbonBand {
	if day.IsZero() {
		return nil
	}
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	next := midnight.AddDate(0, 0, 1)
	type span struct {
		start time.Time
		end   time.Time
		game  int
	}
	var spans []span
	var points []time.Time
	for game, item := range games {
		for _, run := range item.Runs {
			start, end := run.StartedAt, run.EndedAt
			if !end.After(start) {
				continue
			}
			if !end.After(midnight) || !start.Before(next) {
				continue
			}
			if start.Before(midnight) {
				start = midnight
			}
			if end.After(next) {
				end = next
			}
			spans = append(spans, span{start: start, end: end, game: game})
			points = append(points, start, end)
		}
	}
	if len(points) == 0 {
		return nil
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
	unique := points[:0]
	for _, point := range points {
		if len(unique) == 0 || unique[len(unique)-1].Before(point) {
			unique = append(unique, point)
		}
	}
	fracs := make([]float64, len(unique))
	spanSeconds := next.Sub(midnight).Seconds()
	for i, point := range unique {
		fracs[i] = point.Sub(midnight).Seconds() / spanSeconds
	}
	var bands []ribbonBand
	for i := 0; i < len(unique)-1; i++ {
		if fracs[i] == fracs[i+1] {
			continue
		}
		from, to := unique[i], unique[i+1]
		seen := map[int]bool{}
		var active []int
		for _, item := range spans {
			if !item.start.Before(to) || !item.end.After(from) {
				continue
			}
			if seen[item.game] {
				continue
			}
			seen[item.game] = true
			active = append(active, item.game)
		}
		if len(active) == 0 {
			continue
		}
		sort.Ints(active)
		if len(active) > ribbonLayers {
			active = active[:ribbonLayers]
		}
		for lane, game := range active {
			bands = append(bands, ribbonBand{
				Start: fracs[i],
				End:   fracs[i+1],
				Game:  game,
				Lane:  lane,
				Lanes: len(active),
			})
		}
	}
	return mergeBands(bands)
}

func mergeBands(bands []ribbonBand) []ribbonBand {
	sort.Slice(bands, func(i, j int) bool {
		if bands[i].Game != bands[j].Game {
			return bands[i].Game < bands[j].Game
		}
		if bands[i].Start != bands[j].Start {
			return bands[i].Start < bands[j].Start
		}
		return bands[i].Lane < bands[j].Lane
	})
	merged := make([]ribbonBand, 0, len(bands))
	for _, band := range bands {
		if n := len(merged); n > 0 {
			prev := &merged[n-1]
			if prev.Game == band.Game && prev.Lane == band.Lane && prev.Lanes == band.Lanes && prev.End == band.Start {
				prev.End = band.End
				continue
			}
		}
		merged = append(merged, band)
	}
	return merged
}
