package ui

import (
	"testing"
	"time"

	"boku/internal/playtime"
)

func TestDayBandsSeparateGamesStayOnOneRow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	day := time.Date(2026, 10, 30, 12, 0, 0, 0, loc)
	games := []playtime.GameDay{
		{Runs: []playtime.Run{runBetween(loc, 12, 28, 14, 32)}},
		{Runs: []playtime.Run{runBetween(loc, 15, 10, 15, 58)}},
	}
	bands := dayBands(games, day)
	if len(bands) != 2 {
		t.Fatalf("got %d bands, want 2: %+v", len(bands), bands)
	}
	assertBand(t, bands[0], 0, 0, 1, minutes(12, 28), minutes(14, 32))
	assertBand(t, bands[1], 1, 0, 1, minutes(15, 10), minutes(15, 58))
}

func TestDayBandsSplitOnlyTheOverlap(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	day := time.Date(2026, 10, 30, 12, 0, 0, 0, loc)
	games := []playtime.GameDay{
		{Runs: []playtime.Run{runBetween(loc, 12, 0, 15, 0)}},
		{Runs: []playtime.Run{runBetween(loc, 14, 0, 16, 0)}},
	}
	bands := dayBands(games, day)
	if len(bands) != 4 {
		t.Fatalf("got %d bands, want 4: %+v", len(bands), bands)
	}
	assertBand(t, bands[0], 0, 0, 1, minutes(12, 0), minutes(14, 0))
	assertBand(t, bands[1], 0, 0, 2, minutes(14, 0), minutes(15, 0))
	assertBand(t, bands[2], 1, 1, 2, minutes(14, 0), minutes(15, 0))
	assertBand(t, bands[3], 1, 0, 1, minutes(15, 0), minutes(16, 0))
}

func TestDayBandsKeepThreeLayers(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	day := time.Date(2026, 10, 30, 12, 0, 0, 0, loc)
	games := make([]playtime.GameDay, 4)
	for i := range games {
		games[i].Runs = []playtime.Run{runBetween(loc, 12, 0, 13, 0)}
	}
	bands := dayBands(games, day)
	if len(bands) != ribbonLayers {
		t.Fatalf("got %d bands, want %d: %+v", len(bands), ribbonLayers, bands)
	}
	for i, band := range bands {
		if band.Game != i || band.Lane != i || band.Lanes != ribbonLayers {
			t.Fatalf("band %d = %+v", i, band)
		}
	}
}

func TestDayBandsSkipAZeroDay(t *testing.T) {
	if bands := dayBands(nil, time.Time{}); bands != nil {
		t.Fatalf("got %+v", bands)
	}
}

func runBetween(loc *time.Location, h1, m1, h2, m2 int) playtime.Run {
	return playtime.Run{
		StartedAt: time.Date(2026, 10, 30, h1, m1, 0, 0, loc),
		EndedAt:   time.Date(2026, 10, 30, h2, m2, 0, 0, loc),
	}
}

func minutes(hour, minute int) float64 {
	return float64(hour*60+minute) / (24 * 60)
}

func assertBand(t *testing.T, band ribbonBand, game, lane, lanes int, start, end float64) {
	t.Helper()
	if band.Game != game || band.Lane != lane || band.Lanes != lanes {
		t.Fatalf("band identity %+v, want game %d lane %d of %d", band, game, lane, lanes)
	}
	const eps = 1e-9
	if band.Start < start-eps || band.Start > start+eps || band.End < end-eps || band.End > end+eps {
		t.Fatalf("band range %v–%v, want %v–%v", band.Start, band.End, start, end)
	}
}
