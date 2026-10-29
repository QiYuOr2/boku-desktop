package playtime

import (
	"fmt"
	"sort"
	"time"
)

// Run is one session's share of a local day. The times are clipped to that
// day, and Seconds is the whole seconds of the overlap. UntilMidnight means
// the session continued into the next day, so the day's end is not a stop.
type Run struct {
	StartedAt     time.Time
	EndedAt       time.Time
	Open          bool
	UntilMidnight bool
	Seconds       int64
}

// GameDay is one game's play on one local day, longest first when listed.
type GameDay struct {
	AppID   string
	Name    string
	Seconds int64
	Runs    []Run
}

// DayTotal is the play on one local midnight.
type DayTotal struct {
	Day     time.Time
	Seconds int64
}

// CalendarDay is one cell of a month. Blank is the empty cell before the
// first of the month, so the columns stay Sunday through Saturday.
type CalendarDay struct {
	Day     time.Time
	Seconds int64
	Blank   bool
}

// GamesOn lists each game that overlaps the local day of day. now ends a
// session that is still running. A session that crosses midnight contributes
// only its share of this day.
// LatestPlayedDay is the newest local day, up to now, that has any play.
// A session that stops exactly at midnight belongs to the day before.
func LatestPlayedDay(sessions []Session, now time.Time) (time.Time, bool) {
	if now.IsZero() {
		return time.Time{}, false
	}
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var latest time.Time
	found := false
	for _, session := range sessions {
		end, ok := playedUntil(session, now)
		if !ok {
			continue
		}
		start := session.StartedAt.In(loc)
		stop := end.In(loc)
		last := time.Date(stop.Year(), stop.Month(), stop.Day(), 0, 0, 0, 0, loc)
		if stop.Equal(last) {
			last = last.AddDate(0, 0, -1)
		}
		if last.After(today) {
			last = today
		}
		first := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
		if last.Before(first) {
			continue
		}
		if !found || last.After(latest) {
			latest = last
			found = true
		}
	}
	return latest, found
}

// DayHeading names a local day relative to now: 今天, 昨日, or a date.
func DayHeading(day, now time.Time) string {
	loc := now.Location()
	shown := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if shown.Equal(today) {
		return "今天"
	}
	if shown.Equal(today.AddDate(0, 0, -1)) {
		return "昨日"
	}
	if shown.Year() == today.Year() {
		return shown.Format("1月2日")
	}
	return shown.Format("2006年1月2日")
}

func playedUntil(session Session, now time.Time) (time.Time, bool) {
	if session.AppID == "" || session.StartedAt.IsZero() || !now.After(session.StartedAt) {
		return time.Time{}, false
	}
	end := session.EndedAt
	if end.IsZero() || end.After(now) {
		end = now
	}
	if !end.After(session.StartedAt) {
		return time.Time{}, false
	}
	return end, true
}

func GamesOn(sessions []Session, day, now time.Time) []GameDay {
	if day.IsZero() {
		return nil
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	return GamesDuring(sessions, start, start.AddDate(0, 0, 1), now)
}

// GamesDuring lists each game that overlaps [from, to), longest first.
// now ends a session that is still running. A session contributes only the
// seconds inside the range.
func GamesDuring(sessions []Session, from, to, now time.Time) []GameDay {
	if !from.Before(to) {
		return nil
	}
	type acc struct {
		name    string
		namedAt time.Time
		seconds int64
		runs    []Run
	}
	byID := map[string]*acc{}
	var order []string
	for _, session := range sessions {
		run, ok := clip(session, from, to, now)
		if !ok {
			continue
		}
		item := byID[session.AppID]
		if item == nil {
			item = &acc{}
			byID[session.AppID] = item
			order = append(order, session.AppID)
		}
		if session.Name != "" && (item.namedAt.IsZero() || !session.StartedAt.Before(item.namedAt)) {
			item.name = session.Name
			item.namedAt = session.StartedAt
		}
		item.seconds += run.Seconds
		item.runs = append(item.runs, run)
	}
	games := make([]GameDay, 0, len(order))
	for _, appID := range order {
		item := byID[appID]
		sort.Slice(item.runs, func(i, j int) bool {
			return item.runs[i].StartedAt.Before(item.runs[j].StartedAt)
		})
		games = append(games, GameDay{AppID: appID, Name: item.name, Seconds: item.seconds, Runs: item.runs})
	}
	sort.Slice(games, func(i, j int) bool {
		if games[i].Seconds != games[j].Seconds {
			return games[i].Seconds > games[j].Seconds
		}
		if games[i].Name != games[j].Name {
			return games[i].Name < games[j].Name
		}
		return games[i].AppID < games[j].AppID
	})
	return games
}

// WeekOf is the Sunday-to-Saturday week that contains now, in now's location.
func WeekOf(sessions []Session, now time.Time) []DayTotal {
	if now.IsZero() {
		return nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sunday := today.AddDate(0, 0, -int(today.Weekday()))
	days := make([]DayTotal, 7)
	for i := range days {
		day := sunday.AddDate(0, 0, i)
		days[i] = DayTotal{Day: day, Seconds: secondsOn(sessions, day, now)}
	}
	return days
}

// MonthOf is the calendar of month, Sunday in the first column, with a blank
// cell for each weekday before the first. now ends a session still running.
func MonthOf(sessions []Session, month, now time.Time) []CalendarDay {
	if month.IsZero() || now.IsZero() {
		return nil
	}
	loc := month.Location()
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, loc)
	blanks := int(first.Weekday())
	days := first.AddDate(0, 1, -1).Day()
	cells := make([]CalendarDay, blanks+days)
	for i := range blanks {
		cells[i].Blank = true
	}
	for day := 1; day <= days; day++ {
		date := time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, loc)
		cells[blanks+day-1] = CalendarDay{Day: date, Seconds: secondsOn(sessions, date, now)}
	}
	return cells
}

// PlayedBaseline is the month's play divided by the days that have any.
// A month with no play has no baseline.
func PlayedBaseline(cells []CalendarDay) int64 {
	var total int64
	var days int
	for _, cell := range cells {
		if cell.Blank || cell.Seconds <= 0 {
			continue
		}
		total += cell.Seconds
		days++
	}
	if days == 0 {
		return 0
	}
	return total / int64(days)
}

// Heat ranks a day against baseline. 0 is no play, the blank color.
// Baseline is the average of days that were played: half of it is 1,
// just under it is 2, the baseline itself is 3, and one and a half
// times it is 4.
func Heat(seconds, baseline int64) int {
	if seconds <= 0 {
		return 0
	}
	if baseline <= 0 {
		return 4
	}
	switch {
	case seconds*2 < baseline:
		return 1
	case seconds < baseline:
		return 2
	case seconds*2 < baseline*3:
		return 3
	default:
		return 4
	}
}

// FormatSeconds writes a duration the panel can show. Hours drop the
// leftover seconds; a span under a minute keeps them.
func FormatSeconds(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remain := seconds % 60
	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d小时%d分", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%d小时", hours)
	case minutes > 0 && remain > 0:
		return fmt.Sprintf("%d分%d秒", minutes, remain)
	case minutes > 0:
		return fmt.Sprintf("%d分", minutes)
	default:
		return fmt.Sprintf("%d秒", seconds)
	}
}

func secondsOn(sessions []Session, day, now time.Time) int64 {
	var total int64
	for _, game := range GamesOn(sessions, day, now) {
		total += game.Seconds
	}
	return total
}

func clip(session Session, from, to, now time.Time) (Run, bool) {
	if session.AppID == "" || session.StartedAt.IsZero() || !from.Before(to) {
		return Run{}, false
	}
	open := session.EndedAt.IsZero()
	stop := session.EndedAt
	if open {
		if now.IsZero() || !session.StartedAt.Before(now) {
			return Run{}, false
		}
		stop = now
	}
	seconds := overlapSeconds(session.StartedAt, stop, from, to)
	if seconds <= 0 {
		return Run{}, false
	}
	started := session.StartedAt
	if started.Before(from) {
		started = from
	}
	ended := stop
	if ended.After(to) {
		ended = to
	}
	inDay := !now.Before(from) && now.Before(to)
	return Run{
		StartedAt:     started.In(from.Location()),
		EndedAt:       ended.In(from.Location()),
		Open:          open && inDay,
		UntilMidnight: !open && !stop.Before(to) || open && !inDay && !stop.Before(to),
		Seconds:       seconds,
	}, true
}
