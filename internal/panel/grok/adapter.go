// This file maps Grok's billing response onto the application's snapshot.
// It is a pure function of the response: no network, no clock, and a fixture
// for every case. A missing percentage is a bad response, never a zero.
package grok

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"boku/internal/domain"
)

const usagePeriodPrefix = "USAGE_PERIOD_TYPE_"

var windowSeconds = map[string]int64{
	"DAILY":   86400,
	"WEEKLY":  604800,
	"MONTHLY": 2592000,
}

type billingResponse struct {
	Config *billingConfig `json:"config"`
}

type billingConfig struct {
	CreditUsagePercent *float64       `json:"creditUsagePercent"`
	CurrentPeriod      *billingPeriod `json:"currentPeriod"`
	BillingPeriodEnd   string         `json:"billingPeriodEnd"`
	OnDemandCap        *amount        `json:"onDemandCap"`
	OnDemandUsed       *amount        `json:"onDemandUsed"`
}

type billingPeriod struct {
	Type string `json:"type"`
	End  string `json:"end"`
}

type amount struct {
	Val *float64 `json:"val"`
}

// Adapter maps the response.
type Adapter struct{}

// NewAdapter returns the adapter.
func NewAdapter() *Adapter { return &Adapter{} }

// ProviderID returns the provider this adapter maps.
func (a *Adapter) ProviderID() string { return ProviderID }

// Normalize turns one billing response into a snapshot.
func (a *Adapter) Normalize(result domain.FetchResult, panel domain.Panel) (domain.Snapshot, error) {
	var payload billingResponse
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return domain.Snapshot{}, badResponse(result, "the response is not the billing shape: %v", err)
	}
	if payload.Config == nil || payload.Config.CreditUsagePercent == nil || math.IsNaN(*payload.Config.CreditUsagePercent) || math.IsInf(*payload.Config.CreditUsagePercent, 0) {
		return domain.Snapshot{}, badResponse(result, "the response has no used percentage")
	}
	values := map[string]domain.Value{
		MetricCreditRemainingPercent: domain.NumberValue(remaining(*payload.Config.CreditUsagePercent)),
	}
	periodEnd := payload.Config.BillingPeriodEnd
	if payload.Config.CurrentPeriod != nil {
		periodEnd = payload.Config.CurrentPeriod.End
		if seconds, ok := periodLength(payload.Config.CurrentPeriod.Type); ok {
			values[MetricCreditWindowSeconds] = domain.DurationValue(seconds)
		}
	}
	if resets, ok := resetSeconds(periodEnd, result.FetchedAt); ok {
		values[MetricCreditResetsInSeconds] = domain.DurationValue(resets)
	}
	if err := onDemand(payload.Config, values); err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
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

func onDemand(config *billingConfig, values map[string]domain.Value) error {
	if config.OnDemandCap == nil || config.OnDemandCap.Val == nil {
		return nil
	}
	cap := *config.OnDemandCap.Val
	if math.IsNaN(cap) || math.IsInf(cap, 0) || cap < 0 {
		return fmt.Errorf("the on-demand cap is not a usable amount")
	}
	if cap == 0 {
		return nil
	}
	if config.OnDemandUsed == nil || config.OnDemandUsed.Val == nil || math.IsNaN(*config.OnDemandUsed.Val) || math.IsInf(*config.OnDemandUsed.Val, 0) {
		return fmt.Errorf("the on-demand amount used is missing")
	}
	values[MetricOnDemandRemainingPercent] = domain.NumberValue(remaining(100 * *config.OnDemandUsed.Val / cap))
	return nil
}

func remaining(used float64) int64 {
	rounded := int64(math.Round(used))
	if rounded < 0 {
		rounded = 0
	}
	if rounded > 100 {
		rounded = 100
	}
	return 100 - rounded
}

func periodLength(raw string) (int64, bool) {
	name := strings.TrimPrefix(strings.TrimSpace(raw), usagePeriodPrefix)
	seconds, ok := windowSeconds[name]
	return seconds, ok
}

func resetSeconds(raw string, fetchedAt time.Time) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	end, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		end, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return 0, false
		}
	}
	seconds := int64(end.Sub(fetchedAt).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return seconds, true
}

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
		built = append(built, domain.Observation{MetricKey: key, Value: value, ObservedAt: observedAt})
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
