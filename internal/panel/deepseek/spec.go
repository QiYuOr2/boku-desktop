// Package deepseek is everything this application knows about DeepSeek: the
// panel its balance is shown in, the request that reads it, and the pure
// mapping from the service's answer to the application's snapshot.
//
// It is one package because it is one vendor: the panel's definition, the
// credential it needs and the shape of its response change together, and a
// change to any of them is a change to DeepSeek. What is *not* here is the
// service's identity — the provider id and the credential key — which lives
// in internal/contract/deepseek, because the translator talks to the same
// service under the same name.
//
// The division inside the package is by kind of code, and it is kept by
// review: spec.go is configuration, client.go reaches the network and does
// error translation, adapter.go is a pure function of the response. Nothing
// in adapter.go may read the clock, open a file or make a request — it is
// what makes the mapping testable from a fixture.
package deepseek

import (
	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
)

const (
	// PanelID is stable across launches: the snapshot cache and the
	// database key on it.
	PanelID = "0198a1b2-0000-7000-8000-000000000001"

	// RefreshEverySeconds is how long a balance stays fresh. The balance
	// endpoint is cheap, so it is far below the global floor; the
	// provider's own floor is what actually constrains it.
	RefreshEverySeconds = 600
)

// The panel's metric keys.
const (
	MetricBalanceMinor  = "balanceMinor"
	MetricGrantedMinor  = "grantedMinor"
	MetricToppedUpMinor = "toppedUpMinor"
)

// PreferCurrency is the currency picked when the account reports several.
// It is a preference, not a requirement: an account reporting only USD is
// shown in USD.
const PreferCurrency = "CNY"

// UsageURL is DeepSeek's own page for the account: what the balance of the
// panel is a summary of. The panel's headline opens it, so it belongs with
// the rest of what this provider's panel is, next to its keys and titles.
const UsageURL = "https://platform.deepseek.com/usage"

// Panel returns the balance panel.
func Panel() domain.Panel {
	return domain.Panel{
		ID:           PanelID,
		Kind:         domain.PanelKindProvider,
		Title:        "DeepSeek 余额",
		ProviderID:   contract.ProviderID,
		MetricKeys:   []string{MetricBalanceMinor, MetricGrantedMinor, MetricToppedUpMinor},
		RefreshEvery: RefreshEverySeconds,
		SortOrder:    10,
		Enabled:      true,
	}
}

// Metrics returns the panel's metrics, in the order they are shown: the
// balance first, because it is the headline.
func Metrics() []domain.Metric {
	return []domain.Metric{
		{
			Key:                  MetricBalanceMinor,
			Label:                "总余额",
			Unit:                 domain.UnitCurrency,
			ValueType:            domain.ValueTypeNumber,
			Precision:            2,
			CurrencyFromProvider: true,
			Comparison:           domain.ComparisonLowerIsBetter,
		},
		{
			Key:                  MetricGrantedMinor,
			Label:                "赠送余额",
			Unit:                 domain.UnitCurrency,
			ValueType:            domain.ValueTypeNumber,
			Precision:            2,
			CurrencyFromProvider: true,
			Comparison:           domain.ComparisonLowerIsBetter,
		},
		{
			Key:                  MetricToppedUpMinor,
			Label:                "充值余额",
			Unit:                 domain.UnitCurrency,
			ValueType:            domain.ValueTypeNumber,
			Precision:            2,
			CurrencyFromProvider: true,
			Comparison:           domain.ComparisonHigherIsBetter,
		},
	}
}

// Metric returns one metric by key, and whether it is the panel's.
func Metric(key string) (domain.Metric, bool) {
	for _, m := range Metrics() {
		if m.Key == key {
			return m, true
		}
	}
	return domain.Metric{}, false
}
