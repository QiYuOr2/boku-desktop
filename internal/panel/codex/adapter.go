// This file maps ChatGPT's usage response onto the application's snapshot. It
// is a pure function of the response and of the moment it was fetched at: no
// network, no clock of its own, and a fixture for every case, so a field the
// service renames is caught by a test rather than by a bar showing 0.
package codex

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"boku/internal/domain"
)

// usageResponse is the endpoint's own shape, and the only place in the
// application where these field names appear. Everything that must be there
// is a pointer, so that a response without it is a bad response rather than
// a quota of zero.
type usageResponse struct {
	PlanType  string     `json:"plan_type"`
	RateLimit *rateLimit `json:"rate_limit"`
}

type rateLimit struct {
	PrimaryWindow *window `json:"primary_window"`
	// SecondaryWindow is optional and is shown for non-free accounts.
	SecondaryWindow *window `json:"secondary_window"`
}

type window struct {
	UsedPercent *int `json:"used_percent"`
	// ResetAt is Unix seconds.
	ResetAt *int64 `json:"reset_at"`
	// LimitWindowSeconds supplies the period label, independently of when
	// the window next resets. A missing length must not imply five hours.
	LimitWindowSeconds *int64 `json:"limit_window_seconds"`
}

// Adapter maps the response.
type Adapter struct{}

// NewAdapter returns the adapter.
func NewAdapter() *Adapter { return &Adapter{} }

// ProviderID returns the provider this adapter maps.
func (a *Adapter) ProviderID() string { return ProviderID }

// Normalize turns one usage response into a snapshot.
//
// It refuses, rather than guessing, when the response is not the shape it
// knows or when the window has no percentage or no reset time: a bar that
// says "0% left" because a field was renamed is worse than a card that says
// it could not read the answer.
func (a *Adapter) Normalize(result domain.FetchResult, panel domain.Panel) (domain.Snapshot, error) {
	var payload usageResponse
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return domain.Snapshot{}, badResponse(result, "the response is not the usage shape: %v", err)
	}
	if payload.RateLimit == nil || payload.RateLimit.PrimaryWindow == nil {
		return domain.Snapshot{}, badResponse(result, "the response carries no rate-limit window")
	}
	session := payload.RateLimit.PrimaryWindow
	remaining, resetsIn, err := windowValues(session, result.FetchedAt)
	if err != nil {
		return domain.Snapshot{}, badResponse(result, "primary window: %v", err)
	}

	values := map[string]domain.Value{
		MetricSessionRemainingPercent: domain.NumberValue(remaining),
		MetricSessionResetsInSeconds:  domain.DurationValue(resetsIn),
		MetricPlanName:                domain.TextValue(strings.TrimSpace(payload.PlanType)),
	}
	if seconds := session.LimitWindowSeconds; seconds != nil && *seconds > 0 {
		values[MetricSessionWindowSeconds] = domain.DurationValue(*seconds)
	}
	plan := strings.TrimSpace(payload.PlanType)
	if secondary := payload.RateLimit.SecondaryWindow; secondary != nil && plan != "" && !strings.EqualFold(plan, "free") {
		remaining, resetsIn, err := windowValues(secondary, result.FetchedAt)
		if err != nil {
			return domain.Snapshot{}, badResponse(result, "secondary window: %v", err)
		}
		values[MetricLongWindowRemainingPercent] = domain.NumberValue(remaining)
		values[MetricLongWindowResetsInSeconds] = domain.DurationValue(resetsIn)
		if seconds := secondary.LimitWindowSeconds; seconds != nil && *seconds > 0 {
			values[MetricLongWindowSeconds] = domain.DurationValue(*seconds)
		}
	}
	observations, err := observations(panel, values, result.FetchedAt)
	if err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}

	snapshot := domain.Snapshot{
		PanelID:      panel.ID,
		ProviderID:   providerIDOf(result),
		ObservedAt:   result.FetchedAt,
		Status:       domain.StatusOK,
		Observations: observations,
	}
	if err := snapshot.Validate(); err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}
	return snapshot, nil
}

func windowValues(w *window, fetchedAt time.Time) (remaining, resetsIn int64, err error) {
	if w.UsedPercent == nil || w.ResetAt == nil {
		return 0, 0, fmt.Errorf("the rate-limit window must say how much is used and when it resets")
	}
	used := max(int64(0), min(int64(100), int64(*w.UsedPercent)))
	return 100 - used, max(int64(0), *w.ResetAt-fetchedAt.Unix()), nil
}

// observations builds the panel's observations in the order its metrics are
// defined, so the bar's own number comes first. A metric the response has no
// value for is a bad response, not a zero.
func observations(panel domain.Panel, values map[string]domain.Value, observedAt time.Time) ([]domain.Observation, error) {
	keys := append([]string(nil), panel.MetricKeys...)
	if len(keys) == 0 {
		for _, metric := range Metrics() {
			if _, ok := values[metric.Key]; ok {
				keys = append(keys, metric.Key)
			}
		}
	} else {
		for _, key := range OptionalMetricKeys() {
			if _, ok := values[key]; ok && !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	built := make([]domain.Observation, 0, len(keys))
	for _, key := range keys {
		metric, ok := Metric(key)
		if !ok {
			return nil, fmt.Errorf("the panel asks for metric %q, which this provider does not report", key)
		}
		value, ok := values[key]
		if !ok {
			return nil, fmt.Errorf("the response has no value for metric %q", key)
		}
		if !value.Matches(metric.ValueType) {
			return nil, fmt.Errorf("metric %q is not the %s this response carries", key, metric.ValueType)
		}
		built = append(built, domain.Observation{
			MetricKey:  key,
			Value:      value,
			ObservedAt: observedAt,
		})
	}
	return built, nil
}

func providerIDOf(result domain.FetchResult) string {
	if result.ProviderID != "" {
		return result.ProviderID
	}
	return ProviderID
}

func badResponse(result domain.FetchResult, format string, args ...any) error {
	return &domain.BadResponseError{
		ProviderID: providerIDOf(result),
		Endpoint:   result.Endpoint,
		HTTPStatus: result.HTTPStatus,
		Reason:     fmt.Sprintf(format, args...),
		Snippet:    domain.Snippet(result.Payload),
	}
}

var _ domain.Adapter = (*Adapter)(nil)
