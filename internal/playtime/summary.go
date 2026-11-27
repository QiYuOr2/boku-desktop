package playtime

import (
	"fmt"
	"sort"
	"time"
)

// Run 是一局在某个本地日内的份额。时间被裁剪到该日，Seconds 为重叠的整秒数。
// UntilMidnight 表示该局延续到次日，因此该日末尾并非结束。
type Run struct {
	StartedAt     time.Time
	EndedAt       time.Time
	Open          bool
	UntilMidnight bool
	Seconds       int64
}

// GameDay 是一个游戏在一个本地日的游玩，列出时按秒数从长到短。
type GameDay struct {
	AppID   string
	Name    string
	Seconds int64
	Runs    []Run
}

type DayTotal struct {
	Day     time.Time
	Seconds int64
}

// CalendarDay 是月历的一格。Blank 是当月第一天之前的空单元格，
// 以保持周日到周六的列序。
type CalendarDay struct {
	Day     time.Time
	Seconds int64
	Blank   bool
}

// GamesOn 列出与 day 所在本地日重叠的每个游戏。now 用于结束仍在进行的一局；
// 跨午夜的一局只计入本日份额。
// LatestPlayedDay 返回截至 now 最近一个有任何游玩的本地日。
// 恰好停在午夜的一局归入前一天。
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

// DayHeading 以相对 now 的方式命名本地日：今天、昨日，或日期。
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

// GamesDuring 列出与 [from, to) 重叠的每个游戏，从长到短。
// now 用于结束仍在进行的一局；一局只计入区间内的秒数。
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

func WeekOf(sessions []Session, now time.Time) []DayTotal {
	return WeekContaining(sessions, now, now)
}

// WeekContaining 是包含 day 的周日至周六的一周。
// now 用于结束仍在进行的一局，即使 day 属于更早的一周。
func WeekContaining(sessions []Session, day, now time.Time) []DayTotal {
	if day.IsZero() || now.IsZero() {
		return nil
	}
	loc := day.Location()
	anchor := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	sunday := anchor.AddDate(0, 0, -int(anchor.Weekday()))
	days := make([]DayTotal, 7)
	for i := range days {
		date := sunday.AddDate(0, 0, i)
		days[i] = DayTotal{Day: date, Seconds: secondsOn(sessions, date, now)}
	}
	return days
}

// MonthOf 是 month 的月历，周日排在第一列，第一天之前每个工作日前留一个空格。
// now 用于结束仍在进行的一局。
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

// PlayedBaseline 是本月总游玩除以有游玩的天数。没有游玩的月份没有基准。
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

// Heat 按 baseline 给某天排名。0 为无游玩，即空白色。
// Baseline 是有游玩日的平均值：其一半为 1，略低于它为 2，等于它为 3，一倍半为 4。
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

// FormatSeconds 写出一段可展示的时长。超过一小时则略去剩余秒数；
// 不足一分钟的时长则保留秒数。
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
