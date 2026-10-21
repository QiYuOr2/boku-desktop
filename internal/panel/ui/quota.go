// Package ui contains presentation-specific helpers for panels.
package ui

import (
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
	hasLeft, hasReset   bool
	plan                string
}

func read(view panel.View) quota {
	var found quota
	for _, metric := range view.Metrics {
		observation, ok := view.Snapshot.Observation(metric.Key)
		if !ok {
			continue
		}
		switch {
		case metric.Unit == domain.UnitPercent && !found.hasLeft:
			found.remaining, found.hasLeft = observation.Value.Number()
		case metric.ValueType == domain.ValueTypeDuration && !found.hasReset:
			found.resetsIn, found.hasReset = observation.Value.DurationSeconds()
		case metric.ValueType == domain.ValueTypeText && found.plan == "":
			if text, ok := observation.Value.Text(); ok {
				found.plan = strings.TrimSpace(text)
			}
		}
	}
	return found
}

func Used(view panel.View) (int64, bool) {
	q := read(view)
	if !q.hasLeft {
		return 0, false
	}
	return 100 - q.remaining, true
}
func Account(view panel.View) string {
	q := read(view)
	parts := make([]string, 0, 2)
	if q.plan != "" {
		parts = append(parts, planLabel(q.plan))
	}
	if q.hasReset {
		if q.resetsIn <= 0 {
			parts = append(parts, "即将重置")
		} else {
			parts = append(parts, "下次重置 "+resetClock(view.Snapshot.ObservedAt, q.resetsIn))
		}
	}
	return strings.Join(parts, " · ")
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
