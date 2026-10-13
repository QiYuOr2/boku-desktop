// Package deepseek is the contract of one provider's panel: which numbers
// it shows, what they are called and how often they are worth fetching.
//
// It sits next to the domain rather than inside it because the domain must
// know no vendor, and outside the provider because a provider must know no
// panel: the use case that reads a panel and the adapter that fills it both
// read these definitions, so they cannot drift apart. Nothing here is an
// entity: it is configuration.
package deepseek

import "boku/internal/domain"

// ProviderID is the provider this panel reads, as registered and as stored
// in the secret store ("deepseek.apiKey").
const ProviderID = "deepseek"

const (
	// PanelID is stable across launches: the snapshot cache and the
	// database key on it.
	PanelID = "0198a1b2-0000-7000-8000-000000000001"

	// RefreshEverySeconds is how long a balance stays fresh. The balance
	// endpoint is cheap, so it is far below the global floor; the
	// provider's own floor is what actually constrains it.
	RefreshEverySeconds = 600

	// APIKeyCredential is the credential the provider needs, without its
	// prefix: the store keeps it as CredentialKey(ProviderID, APIKeyCredential).
	APIKeyCredential = "apiKey"
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
		ProviderID:   ProviderID,
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
