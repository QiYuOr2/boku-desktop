// Package ui 提供面板相关的展示辅助。
package ui

import (
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/panel"
)

func IsQuota(view panel.View) bool {
	return len(view.Metrics) > 0 && view.Metrics[0].Unit == domain.UnitPercent && view.Metrics[0].ValueType == domain.ValueTypeNumber
}

type quota struct {
	remaining, resetsIn int64
	windowSeconds       int64
	hasLeft, hasReset   bool
	sawReset, sawWindow bool
	plan                string
	label               string
	extra               bool
}

func read(view panel.View) quota {
	windows := readWindows(view)
	if len(windows) == 0 {
		return quota{}
	}
	return windows[0]
}

// 每个百分比指标开启一个窗口，其后紧跟的时长依次提供重置倒计时和周期；
// 缺失的可选观测仍占据自己的槽位。
func readWindows(view panel.View) []quota {
	var windows []quota
	var plan string
	for _, metric := range view.Metrics {
		if metric.Unit == domain.UnitPercent && metric.ValueType == domain.ValueTypeNumber {
			windows = append(windows, quota{label: metric.Label, extra: len(windows) > 0})
		}
		observation, ok := view.Snapshot.Observation(metric.Key)
		// 缺失的时长仍需占位，否则缺少重置时间时，后面的周期会被误当成倒计时。
		if metric.ValueType == domain.ValueTypeDuration && len(windows) > 0 {
			found := &windows[len(windows)-1]
			switch {
			case !found.sawReset:
				found.sawReset = true
				if ok {
					found.resetsIn, found.hasReset = observation.Value.DurationSeconds()
				}
			case !found.sawWindow:
				found.sawWindow = true
				if ok {
					found.windowSeconds, _ = observation.Value.DurationSeconds()
				}
			}
			continue
		}
		if !ok {
			continue
		}
		if metric.ValueType == domain.ValueTypeText && plan == "" {
			if text, ok := observation.Value.Text(); ok {
				plan = strings.TrimSpace(text)
			}
			continue
		}
		if len(windows) == 0 {
			continue
		}
		found := &windows[len(windows)-1]
		if metric.Unit == domain.UnitPercent {
			found.remaining, found.hasLeft = observation.Value.Number()
		}
	}
	for i := range windows {
		windows[i].plan = plan
	}
	// 后面没有独立倒计时的进度条（如随订阅窗口重置的按需用量）沿用前面窗口的倒计时。
	if len(windows) > 0 && windows[0].hasReset {
		for i := 1; i < len(windows); i++ {
			if windows[i].hasLeft && !windows[i].hasReset {
				windows[i].resetsIn = windows[0].resetsIn
				windows[i].hasReset = true
			}
		}
	}
	return windows
}

func Used(view panel.View) (int64, bool) {
	q := read(view)
	if !q.hasLeft {
		return 0, false
	}
	return 100 - q.remaining, true
}
func Account(view panel.View) string {
	return planLabel(read(view).plan)
}

func quotaTitle(q quota, observedAt time.Time) string {
	parts := make([]string, 0, 3)
	if q.extra && q.windowSeconds <= 0 && q.label != "" {
		parts = append(parts, q.label)
	}
	if label := windowLabel(q.windowSeconds); label != "" {
		parts = append(parts, label)
	}
	if q.hasReset {
		if q.resetsIn <= 0 {
			parts = append(parts, "即将重置")
		} else {
			parts = append(parts, "下次重置 "+resetClock(observedAt, q.resetsIn))
		}
	}
	return strings.Join(parts, " · ")
}

func windowLabel(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	for _, unit := range []struct {
		seconds int64
		label   string
	}{{86400, "d"}, {3600, "h"}, {60, "m"}, {1, "s"}} {
		if seconds%unit.seconds == 0 {
			return strconv.FormatInt(seconds/unit.seconds, 10) + unit.label
		}
	}
	return ""
}
func resetClock(observedAt time.Time, seconds int64) string {
	if observedAt.IsZero() || seconds <= 0 {
		return "—"
	}
	return observedAt.Add(time.Duration(seconds) * time.Second).Local().Format("01-02 15:04")
}
func planLabel(plan string) string {
	trimmed := strings.TrimSpace(plan)
	switch strings.ToLower(trimmed) {
	case "free", "plus", "pro", "business", "enterprise", "team", "edu":
		return strings.ToUpper(trimmed[:1]) + strings.ToLower(trimmed[1:])
	default:
		return trimmed
	}
}
