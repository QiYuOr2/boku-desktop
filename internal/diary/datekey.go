package diary

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DateKey 是日记归属日期的稳定公历日（yyyy-MM-dd），不随时区或展示格式漂移。
type DateKey struct {
	Year  int
	Month int
	Day   int
}

func NewDateKey(year, month, day int) (DateKey, error) {
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return DateKey{}, fmt.Errorf("invalid diary date %04d-%02d-%02d", year, month, day)
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return DateKey{}, fmt.Errorf("invalid diary date %04d-%02d-%02d", year, month, day)
	}
	return DateKey{Year: year, Month: month, Day: day}, nil
}

func ParseDateKey(raw string) (DateKey, error) {
	if len(raw) != 10 || raw[4] != '-' || raw[7] != '-' {
		return DateKey{}, fmt.Errorf("invalid diary date key %q", raw)
	}
	year, err1 := strconv.Atoi(raw[0:4])
	month, err2 := strconv.Atoi(raw[5:7])
	day, err3 := strconv.Atoi(raw[8:10])
	if err1 != nil || err2 != nil || err3 != nil {
		return DateKey{}, fmt.Errorf("invalid diary date key %q", raw)
	}
	return NewDateKey(year, month, day)
}

// DateKeyFromTime 按 t 所在时区的本地自然日提取日期键。
func DateKeyFromTime(t time.Time) DateKey {
	return DateKey{Year: t.Year(), Month: int(t.Month()), Day: t.Day()}
}

func Today(now time.Time) DateKey {
	return DateKeyFromTime(now.Local())
}

func (d DateKey) IsZero() bool {
	return d.Year == 0 && d.Month == 0 && d.Day == 0
}

func (d DateKey) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func (d DateKey) Compare(other DateKey) int {
	if d.Year != other.Year {
		if d.Year < other.Year {
			return -1
		}
		return 1
	}
	if d.Month != other.Month {
		if d.Month < other.Month {
			return -1
		}
		return 1
	}
	if d.Day != other.Day {
		if d.Day < other.Day {
			return -1
		}
		return 1
	}
	return 0
}

func (d DateKey) Before(other DateKey) bool { return d.Compare(other) < 0 }
func (d DateKey) After(other DateKey) bool  { return d.Compare(other) > 0 }

// IsFuture 按 now 所在时区的本地自然日判断日期键是否在未来。
func (d DateKey) IsFuture(now time.Time) bool {
	return d.After(DateKeyFromTime(now))
}

func (d DateKey) AddDays(days int) DateKey {
	t := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	return DateKey{Year: t.Year(), Month: int(t.Month()), Day: t.Day()}
}

func (d DateKey) MonthValue() Month {
	return Month{Year: d.Year, Month: d.Month}
}

func (d DateKey) Weekday() time.Weekday {
	return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

func (d DateKey) ShortWeekdayZH() string {
	switch d.Weekday() {
	case time.Sunday:
		return "周日"
	case time.Monday:
		return "周一"
	case time.Tuesday:
		return "周二"
	case time.Wednesday:
		return "周三"
	case time.Thursday:
		return "周四"
	case time.Friday:
		return "周五"
	default:
		return "周六"
	}
}

func (d DateKey) LongWeekdayZH() string {
	switch d.Weekday() {
	case time.Sunday:
		return "星期日"
	case time.Monday:
		return "星期一"
	case time.Tuesday:
		return "星期二"
	case time.Wednesday:
		return "星期三"
	case time.Thursday:
		return "星期四"
	case time.Friday:
		return "星期五"
	default:
		return "星期六"
	}
}

func (d DateKey) DisplayMonthDay() string {
	return fmt.Sprintf("%d月%d日 %s", d.Month, d.Day, d.LongWeekdayZH())
}

func (d DateKey) DisplayHeading(today DateKey) string {
	if d == today {
		return "今天"
	}
	if d == today.AddDays(-1) {
		return "昨天"
	}
	if d.Year == today.Year {
		return fmt.Sprintf("%d月%d日", d.Month, d.Day)
	}
	return fmt.Sprintf("%d年%d月%d日", d.Year, d.Month, d.Day)
}

func (d DateKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *DateKey) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseDateKey(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Month 表示一个公历自然月。
type Month struct {
	Year  int
	Month int
}

func NewMonth(year, month int) (Month, error) {
	if year < 1 || year > 9999 || month < 1 || month > 12 {
		return Month{}, fmt.Errorf("invalid month %04d-%02d", year, month)
	}
	return Month{Year: year, Month: month}, nil
}

func MonthOf(t time.Time) Month {
	return Month{Year: t.Year(), Month: int(t.Month())}
}

func (m Month) FirstDay() DateKey {
	return DateKey{Year: m.Year, Month: m.Month, Day: 1}
}

func (m Month) NumberOfDays() int {
	first := time.Date(m.Year, time.Month(m.Month), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	return int(next.Sub(first).Hours() / 24)
}

func (m Month) AddMonths(delta int) Month {
	t := time.Date(m.Year, time.Month(m.Month), 1, 0, 0, 0, 0, time.UTC).AddDate(0, delta, 0)
	return Month{Year: t.Year(), Month: int(t.Month())}
}

func (m Month) Compare(other Month) int {
	if m.Year != other.Year {
		if m.Year < other.Year {
			return -1
		}
		return 1
	}
	if m.Month != other.Month {
		if m.Month < other.Month {
			return -1
		}
		return 1
	}
	return 0
}

func (m Month) DisplayZH() string {
	return fmt.Sprintf("%d年%d月", m.Year, m.Month)
}

// CalendarGrid 返回按周日为首日的自然月网格，前后补齐跨月占位日。
func CalendarGrid(m Month) [][]DateKey {
	first := m.FirstDay()
	leading := int(first.Weekday())
	total := leading + m.NumberOfDays()
	cells := ((total + 6) / 7) * 7
	weeks := make([][]DateKey, 0, cells/7)
	for offset := 0; offset < cells; offset += 7 {
		week := make([]DateKey, 7)
		for col := 0; col < 7; col++ {
			week[col] = first.AddDays(offset + col - leading)
		}
		weeks = append(weeks, week)
	}
	return weeks
}

// Streak 计算连续有日记的天数，以今天结束；今天未写时从昨天起算。
func Streak(dayCounts map[DateKey]int, today DateKey) int {
	day := today
	if dayCounts[day] <= 0 {
		day = day.AddDays(-1)
	}
	length := 0
	for dayCounts[day] > 0 {
		length++
		day = day.AddDays(-1)
	}
	return length
}

// MonthCount 统计指定自然月的日记篇数。
func MonthCount(dayCounts map[DateKey]int, month Month) int {
	total := 0
	for key, count := range dayCounts {
		if key.MonthValue() == month {
			total += count
		}
	}
	return total
}
