// This file maps ChatGPT's usage response onto the application's snapshot. It
// is a pure function of the response and of the moment it was fetched at: no
// network, no clock of its own, and a fixture for every case, so a field the
// service renames is caught by a test rather than by a bar showing 0.
package codex

import (
	"encoding/json"
	"fmt"
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
	// SecondaryWindow is the weekly window. It is part of the response and
	// deliberately not part of this panel (Task 027's Out of Scope): the
	// panel's metrics are what the adapter can always produce.
	SecondaryWindow *window `json:"secondary_window"`
}

type window struct {
	UsedPercent *int `json:"used_percent"`
	// ResetAt is Unix seconds.
	ResetAt *int64 `json:"reset_at"`
	// LimitWindowSeconds is the length of the window (18000 for the
	// session, 604800 for the week). The panel's labels do not name a
	// length, so it is read only to keep the response's shape in one place.
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
	if session.UsedPercent == nil {
		return domain.Snapshot{}, badResponse(result, "the rate-limit window does not say how much is used")
	}
	if session.ResetAt == nil {
		return domain.Snapshot{}, badResponse(result, "the rate-limit window does not say when it resets")
	}

	// What is left is the whole of what the bar shows. A window that is
	// over its limit reports 0 rather than a negative amount, and a
	// percentage outside 0..100 is read as its nearest end rather than
	// refused: the service is allowed to report an over-quota account.
	used := int64(*session.UsedPercent)
	switch {
	case used < 0:
		used = 0
	case used > 100:
		used = 100
	}
	remaining := 100 - used

	// The reset time is absolute and the adapter has no clock: the moment
	// the provider fetched the answer is what it is counted from.
	resetsIn := *session.ResetAt - result.FetchedAt.Unix()
	if resetsIn < 0 {
		resetsIn = 0
	}

	values := map[string]domain.Value{
		MetricSessionRemainingPercent: domain.NumberValue(remaining),
		MetricSessionResetsInSeconds:  domain.DurationValue(resetsIn),
		MetricPlanName:                domain.TextValue(strings.TrimSpace(payload.PlanType)),
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

// observations builds the panel's observations in the order its metrics are
// defined, so the bar's own number comes first. A metric the response has no
// value for is a bad response, not a zero.
func observations(panel domain.Panel, values map[string]domain.Value, observedAt time.Time) ([]domain.Observation, error) {
	keys := panel.MetricKeys
	if len(keys) == 0 {
		for _, metric := range Metrics() {
			keys = append(keys, metric.Key)
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
