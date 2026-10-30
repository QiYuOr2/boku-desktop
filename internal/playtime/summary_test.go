package playtime

import (
	"testing"
	"time"
)

func TestGamesOnSplitsASessionAtMidnight(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	session := Session{
		AppID: "1", Name: "Hades II",
		StartedAt: time.Date(2026, 10, 28, 23, 30, 0, 0, loc),
		EndedAt:   time.Date(2026, 10, 29, 1, 15, 0, 0, loc),
	}
	now := session.EndedAt
	yesterday := GamesOn([]Session{session}, time.Date(2026, 10, 28, 15, 0, 0, 0, loc), now)
	if len(yesterday) != 1 || yesterday[0].Seconds != 30*60 || !yesterday[0].Runs[0].UntilMidnight {
		t.Fatalf("yesterday = %+v", yesterday)
	}
	today := GamesOn([]Session{session}, time.Date(2026, 10, 29, 8, 0, 0, 0, loc), now)
	if len(today) != 1 || today[0].Seconds != 75*60 || today[0].Runs[0].Open || today[0].Runs[0].UntilMidnight {
		t.Fatalf("today = %+v", today)
	}
	if got := today[0].Runs[0].StartedAt.Format("15:04"); got != "00:00" {
		t.Fatalf("clipped start = %s", got)
	}
}

func TestLatestPlayedDayFallsBackBehindAnEmptyToday(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, loc)
	yesterday := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	older := time.Date(2025, 12, 31, 22, 0, 0, 0, loc)
	sessions := []Session{
		{AppID: "1", StartedAt: older, EndedAt: older.Add(time.Hour)},
		{AppID: "1", StartedAt: yesterday, EndedAt: yesterday.Add(2 * time.Hour)},
	}
	got, ok := LatestPlayedDay(sessions, now)
	if !ok || !got.Equal(time.Date(2026, 10, 29, 0, 0, 0, 0, loc)) {
		t.Fatalf("latest = %v %v", got, ok)
	}
	if DayHeading(got, now) != "昨日" {
		t.Fatalf("heading = %s", DayHeading(got, now))
	}
	if DayHeading(older, now) != "2025年12月31日" {
		t.Fatalf("old heading = %s", DayHeading(older, now))
	}
	if DayHeading(time.Date(2026, 10, 26, 12, 0, 0, 0, loc), now) != "10月26日" {
		t.Fatal("same-year heading")
	}
	today := time.Date(2026, 10, 30, 1, 0, 0, 0, loc)
	withToday := append(sessions, Session{AppID: "2", StartedAt: today, EndedAt: today.Add(10 * time.Minute)})
	got, ok = LatestPlayedDay(withToday, now)
	if !ok || !got.Equal(time.Date(2026, 10, 30, 0, 0, 0, 0, loc)) || DayHeading(got, now) != "今天" {
		t.Fatalf("today = %v %v %s", got, ok, DayHeading(got, now))
	}
	midnight := Session{AppID: "3", StartedAt: yesterday, EndedAt: time.Date(2026, 10, 30, 0, 0, 0, 0, loc)}
	got, ok = LatestPlayedDay([]Session{midnight}, now)
	if !ok || !got.Equal(time.Date(2026, 10, 29, 0, 0, 0, 0, loc)) {
		t.Fatalf("midnight stop = %v %v", got, ok)
	}
	if _, ok := LatestPlayedDay(nil, now); ok {
		t.Fatal("empty history reported a day")
	}
}

func TestGamesOnKeepsAnOpenSessionUntilNow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	start := time.Date(2026, 10, 29, 18, 16, 0, 0, loc)
	now := start.Add(2*time.Minute + 5*time.Second)
	games := GamesOn([]Session{{AppID: "4115450", Name: "Phantom Blade Zero", StartedAt: start}}, start, now)
	if len(games) != 1 || games[0].Seconds != 125 || !games[0].Runs[0].Open {
		t.Fatalf("open = %+v", games)
	}
}

func TestGamesOnDropsASessionThatDoesNotOverlap(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	day := time.Date(2026, 10, 29, 0, 0, 0, 0, loc)
	sessions := []Session{
		{Name: "no id", StartedAt: day, EndedAt: day.Add(time.Hour)},
		{AppID: "bad", StartedAt: day.Add(time.Hour), EndedAt: day},
		{AppID: "other", StartedAt: day.AddDate(0, 0, -2), EndedAt: day.AddDate(0, 0, -1)},
	}
	if got := GamesOn(sessions, day, day.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("outside = %+v", got)
	}
}

func TestWeekOfStartsOnSunday(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	thursday := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	start := time.Date(2026, 10, 26, 10, 0, 0, 0, loc)
	days := WeekOf([]Session{{
		AppID: "1", StartedAt: start, EndedAt: start.Add(90 * time.Minute),
	}}, thursday)
	if len(days) != 7 || days[0].Day.Weekday() != time.Sunday || days[0].Day.Day() != 25 || days[6].Day.Day() != 31 {
		t.Fatalf("week = %+v", days)
	}
	if days[1].Seconds != 90*60 || days[4].Seconds != 0 {
		t.Fatalf("seconds = %d %d", days[1].Seconds, days[4].Seconds)
	}
}

func TestWeekContainingUsesTheAskedWeek(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	played := time.Date(2026, 10, 26, 10, 0, 0, 0, loc)
	sessions := []Session{{AppID: "1", StartedAt: played, EndedAt: played.Add(90 * time.Minute)}}
	earlier := time.Date(2026, 10, 18, 12, 0, 0, 0, loc)
	days := WeekContaining(sessions, earlier, now)
	if len(days) != 7 || days[0].Day.Day() != 18 || days[6].Day.Day() != 24 {
		t.Fatalf("week = %+v", days)
	}
	for _, day := range days {
		if day.Seconds != 0 {
			t.Fatalf("earlier week counted %d", day.Seconds)
		}
	}
	if WeekContaining(sessions, time.Time{}, now) != nil || WeekContaining(sessions, earlier, time.Time{}) != nil {
		t.Fatal("a zero bound invented a week")
	}
}

func TestMonthOfPadsUntilSunday(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, loc)
	cells := MonthOf(nil, now, now)
	if len(cells) != 4+31 || !cells[0].Blank || cells[4].Day.Day() != 1 || cells[len(cells)-1].Day.Day() != 31 {
		t.Fatalf("cells = %d first blank %v day1 %v last %v", len(cells), cells[0].Blank, cells[4].Day, cells[len(cells)-1].Day)
	}
	if MonthOf(nil, time.Time{}, now) != nil {
		t.Fatal("a zero month should not invent a calendar")
	}
}

func TestGamesDuringSumsOnlyTheAskedRange(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	monday := time.Date(2026, 10, 26, 10, 0, 0, 0, loc)
	thursday := time.Date(2026, 10, 29, 18, 0, 0, 0, loc)
	outside := time.Date(2026, 10, 18, 12, 0, 0, 0, loc)
	week := time.Date(2026, 10, 25, 0, 0, 0, 0, loc)
	games := GamesDuring([]Session{
		{AppID: "1", Name: "Hades II", StartedAt: monday, EndedAt: monday.Add(30 * time.Minute)},
		{AppID: "1", Name: "Hades II", StartedAt: thursday, EndedAt: thursday.Add(90 * time.Minute)},
		{AppID: "2", Name: "Other", StartedAt: outside, EndedAt: outside.Add(5 * time.Hour)},
	}, week, week.AddDate(0, 0, 7), thursday.Add(2*time.Hour))
	if len(games) != 1 || games[0].AppID != "1" || games[0].Seconds != 120*60 {
		t.Fatalf("week games = %+v", games)
	}
	if GamesDuring(nil, week, week, thursday) != nil {
		t.Fatal("an empty range should not list games")
	}
}

func TestHeatAndFormat(t *testing.T) {
	const baseline int64 = 100
	if Heat(0, baseline) != 0 || Heat(49, baseline) != 1 || Heat(50, baseline) != 2 || Heat(100, baseline) != 3 || Heat(150, baseline) != 4 {
		t.Fatalf("heat = %d %d %d %d %d", Heat(0, baseline), Heat(49, baseline), Heat(50, baseline), Heat(100, baseline), Heat(150, baseline))
	}
	if Heat(10, 0) != 4 {
		t.Fatal("play without a baseline should be the darkest")
	}
	cells := []CalendarDay{{Blank: true, Seconds: 999}, {Seconds: 0}, {Seconds: 60}, {Seconds: 120}}
	if PlayedBaseline(cells) != 90 || PlayedBaseline(nil) != 0 {
		t.Fatalf("baseline = %d", PlayedBaseline(cells))
	}
	if FormatSeconds(4*3600+23*60) != "4小时23分" || FormatSeconds(125) != "2分5秒" || FormatSeconds(0) != "0秒" {
		t.Fatalf("format = %q %q %q", FormatSeconds(4*3600+23*60), FormatSeconds(125), FormatSeconds(0))
	}
}
