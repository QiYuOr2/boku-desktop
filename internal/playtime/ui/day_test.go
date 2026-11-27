package ui

import (
	"testing"
	"time"
)

func TestShowDayPicksDayAndSwitchesToDayView(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	p := &Panel{span: 2}
	p.ShowDay(time.Date(2026, 10, 12, 9, 30, 0, 0, loc), now)
	if p.span != 0 {
		t.Fatalf("span = %d, want day view", p.span)
	}
	got := p.SelectedDay()
	want := time.Date(2026, 10, 12, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
}

func TestShowDayClampsFutureToToday(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	p := &Panel{}
	p.ShowDay(time.Date(2026, 10, 31, 10, 0, 0, 0, loc), now)
	want := time.Date(2026, 10, 29, 0, 0, 0, 0, loc)
	if !p.SelectedDay().Equal(want) {
		t.Fatalf("selected = %v, want today %v", p.SelectedDay(), want)
	}
}

func TestShowTodayDropsPickedDay(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	p := &Panel{}
	p.ShowDay(time.Date(2026, 10, 12, 9, 0, 0, 0, loc), now)
	p.ShowToday()
	if !p.SelectedDay().IsZero() {
		t.Fatalf("selected = %v, want zero", p.SelectedDay())
	}
	if p.span != 0 {
		t.Fatalf("span = %d, want day view", p.span)
	}
}

func TestNilPanelShowDayAndToday(t *testing.T) {
	var p *Panel
	p.ShowDay(time.Now(), time.Now())
	p.ShowToday()
	if !p.SelectedDay().IsZero() {
		t.Fatal("nil panel should have no selected day")
	}
}
