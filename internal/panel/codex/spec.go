// Package codex is everything this application knows about the ChatGPT
// account the Codex CLI is logged in with: the quota panel its usage is shown
// in, the request that reads it, the pure mapping from the service's answer to
// the application's snapshot, and the login Boku borrows from the CLI instead
// of asking the user for a key.
//
// It is one package because it is one vendor: the panel's definition, the
// credential it needs and the shape of its response change together. The
// division inside the package is by kind of code, and it is kept by review:
// spec.go is configuration, client.go reaches the network, adapter.go is a
// pure function of the response, login.go reads another tool's credential
// file. Nothing in adapter.go may read the clock, open a file or make a
// request — it is what makes the mapping testable from a fixture.
package codex

import "boku/internal/domain"

// ProviderID is the provider this panel reads, as registered and as named in
// the cross-end contract (§7 of the provider contract).
const ProviderID = "codex"

const (
	// PanelID is stable across launches: the snapshot cache and the
	// database key on it.
	PanelID = "0198a1b2-0000-7000-8000-000000000002"

	// RefreshEverySeconds is how long a quota reading stays fresh. The
	// window is five hours long, so a few minutes is far finer than the
	// number can move; the provider's own floor is what constrains it.
	RefreshEverySeconds = 300

	// AccessTokenCredential is the credential the provider asks for. It does
	// not come from the secret store: it is the token the Codex CLI already
	// logged in with, and Boku reads it where it lies (§9 of the security design).
	// It is named here so that the descriptor, the refresh call and the
	// error messages agree on one name.
	AccessTokenCredential = "accessToken"

	// AccountIDSetting is the non-sensitive half of that login: which
	// ChatGPT account the token belongs to. It travels as a setting rather
	// than a secret, the way Steam's id does, and it is sent as the
	// ChatGPT-Account-Id header when the login names one.
	AccountIDSetting = "accountId"
)

// The panel's metric keys.
const (
	// MetricSessionRemainingPercent is what is left of the 5-hour window,
	// as 0..100.
	MetricSessionRemainingPercent = "sessionRemainingPercent"
	// MetricSessionResetsInSeconds is how long until that window resets,
	// counted from the moment the reading was taken.
	MetricSessionResetsInSeconds = "sessionResetsInSeconds"
	// MetricPlanName is the account's tier, as the service spells it.
	MetricPlanName = "planName"
)

// UsageURL is ChatGPT's own page for the account: what a quota panel is a
// summary of. The panel's bar opens it, so it belongs next to the panel's
// keys and titles.
const UsageURL = "https://chatgpt.com/codex/settings/usage"

// Panel returns the quota panel.
func Panel() domain.Panel {
	return domain.Panel{
		ID:           PanelID,
		Kind:         domain.PanelKindProvider,
		Title:        "Codex 额度",
		ProviderID:   ProviderID,
		MetricKeys:   []string{MetricSessionRemainingPercent, MetricSessionResetsInSeconds, MetricPlanName},
		RefreshEvery: RefreshEverySeconds,
		SortOrder:    20,
		Enabled:      true,
	}
}

// Metrics returns the panel's metrics, in the order they are shown: what is
// left first, because the bar is the headline.
func Metrics() []domain.Metric {
	return []domain.Metric{
		{
			Key:        MetricSessionRemainingPercent,
			Label:      "剩余额度",
			Unit:       domain.UnitPercent,
			ValueType:  domain.ValueTypeNumber,
			Precision:  0,
			Comparison: domain.ComparisonHigherIsBetter,
		},
		{
			Key:        MetricSessionResetsInSeconds,
			Label:      "下次重置",
			Unit:       domain.UnitSeconds,
			ValueType:  domain.ValueTypeDuration,
			Comparison: domain.ComparisonNone,
		},
		{
			Key:        MetricPlanName,
			Label:      "账号等级",
			Unit:       domain.UnitText,
			ValueType:  domain.ValueTypeText,
			Comparison: domain.ComparisonNone,
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
