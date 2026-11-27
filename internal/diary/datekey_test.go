package diary

import (
	"testing"
	"time"
)

func TestDateKeyParsingAndLeapYears(t *testing.T) {
	if _, err := ParseDateKey("2024-02-29"); err != nil {
		t.Fatalf("leap year Feb 29 should be valid: %v", err)
	}
	for _, invalid := range []string{"2025-02-29", "2026-13-01", "2026-00-10", "2026-4-20", "abcd-ef-gh", ""} {
		if _, err := ParseDateKey(invalid); err == nil {
			t.Errorf("expected invalid date key %q to fail", invalid)
		}
	}
}

func TestDateKeyFutureUsesTimeLocation(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 4, 20, 0, 30, 0, 0, shanghai)
	today, _ := ParseDateKey("2026-04-20")
	tomorrow, _ := ParseDateKey("2026-04-21")
	yesterday, _ := ParseDateKey("2026-04-19")

	if today.IsFuture(now) {
		t.Fatalf("today should not be future")
	}
	if yesterday.IsFuture(now) {
		t.Fatalf("yesterday should not be future")
	}
	if !tomorrow.IsFuture(now) {
		t.Fatalf("tomorrow should be future")
	}
}

func TestCalendarGridAndStats(t *testing.T) {
	m := Month{Year: 2026, Month: 4}
	grid := CalendarGrid(m)
	if len(grid) < 4 || len(grid) > 6 {
		t.Fatalf("unexpected weeks count %d", len(grid))
	}
	for _, w := range grid {
		if len(w) != 7 {
			t.Fatalf("week must have 7 days, got %d", len(w))
		}
	}
	if grid[0][0].Weekday() != time.Sunday {
		t.Fatalf("grid must start on Sunday, got %v", grid[0][0].Weekday())
	}

	today := DateKey{Year: 2026, Month: 4, Day: 20}
	counts := map[DateKey]int{
		{Year: 2026, Month: 4, Day: 20}: 2,
		{Year: 2026, Month: 4, Day: 19}: 1,
		{Year: 2026, Month: 4, Day: 18}: 1,
		{Year: 2026, Month: 3, Day: 31}: 3,
	}
	if got := Streak(counts, today); got != 3 {
		t.Fatalf("expected streak 3, got %d", got)
	}
	if got := MonthCount(counts, m); got != 4 {
		t.Fatalf("expected month count 4, got %d", got)
	}

	// 今天未写时从昨天开始算连续天数。
	delete(counts, today)
	if got := Streak(counts, today); got != 2 {
		t.Fatalf("expected streak 2 when today is unwritten, got %d", got)
	}
	delete(counts, today.AddDays(-1))
	if got := Streak(counts, today); got != 0 {
		t.Fatalf("expected broken streak 0, got %d", got)
	}
}
