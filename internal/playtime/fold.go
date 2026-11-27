package playtime

import (
	"fmt"
	"time"
)

// Point 是一个游戏在一个时间桶内的秒数。
//
// Seconds 为整数。游戏未触及的桶也会以零值出现，这样由这些点画出的线
// 不会跳过安静的分钟。
type Point struct {
	Start   time.Time
	AppID   string
	Name    string
	Seconds int64
}

// Fold 把会话切成宽度为 width、范围 [from, to) 的桶。
//
// from 与 to 是时刻。要按本地小时统计的调用方传入那些小时的时刻即可，
// Fold 不认识时区。now 用来收尾仍打开的会话，使「至今今天」的图表能包含
// 正在运行的游戏。
func Fold(sessions []Session, from, to time.Time, width time.Duration, now time.Time) ([]Point, error) {
	if width <= 0 {
		return nil, fmt.Errorf("playtime: a bucket must be wider than zero")
	}
	from = from.UTC()
	to = to.UTC()
	now = now.UTC()
	if !from.Before(to) {
		return nil, nil
	}
	names := map[string]string{}
	var namedAt = map[string]time.Time{}
	type span struct {
		appID string
		start time.Time
		end   time.Time
	}
	var spans []span
	seen := map[string]bool{}
	for _, session := range sessions {
		if session.AppID == "" {
			continue
		}
		end := session.EndedAt
		if end.IsZero() {
			end = now
		}
		end = end.UTC()
		start := session.StartedAt.UTC()
		if !start.Before(end) || !start.Before(to) || !end.After(from) {
			continue
		}
		if session.Name != "" && (namedAt[session.AppID].IsZero() || session.StartedAt.After(namedAt[session.AppID])) {
			names[session.AppID] = session.Name
			namedAt[session.AppID] = session.StartedAt
		}
		spans = append(spans, span{session.AppID, start, end})
		seen[session.AppID] = true
	}
	var apps []string
	for appID := range seen {
		apps = append(apps, appID)
	}
	sortStrings(apps)
	var points []Point
	for bucket := from; bucket.Before(to); bucket = bucket.Add(width) {
		bucketEnd := bucket.Add(width)
		if bucketEnd.After(to) {
			bucketEnd = to
		}
		for _, appID := range apps {
			var seconds int64
			for _, span := range spans {
				if span.appID != appID {
					continue
				}
				seconds += overlapSeconds(span.start, span.end, bucket, bucketEnd)
			}
			points = append(points, Point{Start: bucket, AppID: appID, Name: names[appID], Seconds: seconds})
		}
	}
	return points, nil
}

func overlapSeconds(start, end, bucket, bucketEnd time.Time) int64 {
	if !start.Before(end) || !bucket.Before(bucketEnd) {
		return 0
	}
	if bucket.After(start) {
		start = bucket
	}
	if bucketEnd.Before(end) {
		end = bucketEnd
	}
	if !start.Before(end) {
		return 0
	}
	return int64(end.Sub(start) / time.Second)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
