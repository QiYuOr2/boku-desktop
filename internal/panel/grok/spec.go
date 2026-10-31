// Package grok is the Grok subscription quota: the panel it is shown in, the
// request that reads it, the pure mapping of the answer, and the device-code
// login Boku keeps for itself.
//
// The division is the same as the other vendors. spec.go is configuration,
// client.go reaches the network, adapter.go is a pure function of the
// response, and login.go keeps the tokens. Nothing in adapter.go may read
// the clock, open a file or make a request.
package grok

import "boku/internal/domain"

const ProviderID = "grok"

const (
	PanelID = "0198a1b2-0000-7000-8000-000000000003"
	// RefreshEverySeconds is how long a reading stays fresh. The panel asks
	// again after five minutes; the provider's own floor is one minute.
	RefreshEverySeconds = 300
	// AccessTokenCredential is the access token the billing request carries.
	// The refresh token is kept beside it, but the provider itself only asks
	// for the access token.
	AccessTokenCredential  = "accessToken"
	RefreshTokenCredential = "refreshToken"
)

const (
	MetricCreditRemainingPercent   = "creditRemainingPercent"
	MetricCreditResetsInSeconds    = "creditResetsInSeconds"
	MetricCreditWindowSeconds      = "creditWindowSeconds"
	MetricOnDemandRemainingPercent = "onDemandRemainingPercent"
)

// UsageURL is the account page a quota panel is a summary of.
const UsageURL = "https://grok.com"

// OptionalMetricKeys names observations the endpoint may omit. The order is
// the order the quota card reads: a reset, then the period it belongs to,
// then the on-demand bar.
func OptionalMetricKeys() []string {
	return []string{MetricCreditResetsInSeconds, MetricCreditWindowSeconds, MetricOnDemandRemainingPercent}
}

// Panel returns the quota panel.
func Panel() domain.Panel {
	return domain.Panel{
		ID:           PanelID,
		Kind:         domain.PanelKindProvider,
		Title:        "Grok 额度",
		ProviderID:   ProviderID,
		MetricKeys:   []string{MetricCreditRemainingPercent},
		RefreshEvery: RefreshEverySeconds,
		SortOrder:    30,
		Enabled:      true,
	}
}

// Metrics returns the panel's metrics in the order the card reads them.
func Metrics() []domain.Metric {
	return []domain.Metric{
		{
			Key:        MetricCreditRemainingPercent,
			Label:      "剩余额度",
			Unit:       domain.UnitPercent,
			ValueType:  domain.ValueTypeNumber,
			Comparison: domain.ComparisonHigherIsBetter,
		},
		{
			Key:        MetricCreditResetsInSeconds,
			Label:      "下次重置",
			Unit:       domain.UnitSeconds,
			ValueType:  domain.ValueTypeDuration,
			Comparison: domain.ComparisonNone,
		},
		{
			Key:        MetricCreditWindowSeconds,
			Label:      "周期",
			Unit:       domain.UnitSeconds,
			ValueType:  domain.ValueTypeDuration,
			Comparison: domain.ComparisonNone,
		},
		{
			Key:        MetricOnDemandRemainingPercent,
			Label:      "按量",
			Unit:       domain.UnitPercent,
			ValueType:  domain.ValueTypeNumber,
			Comparison: domain.ComparisonHigherIsBetter,
		},
	}
}

// Metric returns one metric by key, and whether it is the panel's.
func Metric(key string) (domain.Metric, bool) {
	for _, metric := range Metrics() {
		if metric.Key == key {
			return metric, true
		}
	}
	return domain.Metric{}, false
}
