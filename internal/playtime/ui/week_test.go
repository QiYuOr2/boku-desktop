package ui

import (
	"testing"
	"time"
)

func TestShiftWeekStopsAtThisWeek(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	p := &Panel{}
	if got := p.ShownWeek(now); got.Day() != 25 || got.Weekday() != time.Sunday {
		t.Fatalf("shown = %v", got)
	}
	p.ShiftWeek(now, 1)
	if p.ShownWeek(now).Day() != 25 {
		t.Fatal("moved into the future")
	}
	p.ShiftWeek(now, -1)
	got := p.ShownWeek(now)
	if got.Month() != time.October || got.Day() != 18 || got.Weekday() != time.Sunday {
		t.Fatalf("previous = %v", got)
	}
	p.ShiftWeek(now, 1)
	if p.ShownWeek(now).Day() != 25 {
		t.Fatal("did not return to this week")
	}
	var none *Panel
	none.ShiftWeek(now, -1)
}
