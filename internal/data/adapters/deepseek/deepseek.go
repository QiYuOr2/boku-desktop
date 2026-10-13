// Package deepseek maps DeepSeek's balance response onto the application's
// snapshot. It is a pure function of the response: no network, no clock, and
// a fixture for every case, so a field the service renames is caught by a
// test rather than by a card showing 0.
package deepseek

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"boku/internal/domain"
	deepseekdomain "boku/internal/domain/deepseek"
	"boku/internal/tools"
)

// balanceResponse is DeepSeek's own shape, and the only place in the
// application where these field names appear.
type balanceResponse struct {
	// IsAvailable is a pointer so that a response without it is a bad
	// response rather than an account that is unavailable.
	IsAvailable  *bool         `json:"is_available"`
	BalanceInfos []balanceInfo `json:"balance_infos"`
}

type balanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// Adapter maps the response.
type Adapter struct{}

// NewAdapter returns the adapter.
func NewAdapter() *Adapter { return &Adapter{} }

// ProviderID returns the provider this adapter maps.
func (a *Adapter) ProviderID() string { return deepseekdomain.ProviderID }

// Normalize turns one balance response into a snapshot.
//
// It refuses, rather than guessing, when the response is not the shape it
// knows, when there is no balance to show, or when an amount is not a
// number: a card must never show 0 in place of a failure. An account that
// reports itself unavailable is not a failure — it is an account with a
// real, zero balance — so that case becomes a partial snapshot that says so.
func (a *Adapter) Normalize(result domain.FetchResult, panel domain.Panel) (domain.Snapshot, error) {
	var payload balanceResponse
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return domain.Snapshot{}, badResponse(result, "the response is not the balance shape: %v", err)
	}
	if payload.IsAvailable == nil {
		return domain.Snapshot{}, badResponse(result, "the response does not say whether the account is available")
	}
	if len(payload.BalanceInfos) == 0 {
		return domain.Snapshot{}, badResponse(result, "the response carries no balance")
	}

	info, ignored := pickCurrency(payload.BalanceInfos)
	if strings.TrimSpace(info.Currency) == "" {
		return domain.Snapshot{}, badResponse(result, "a balance does not name its currency")
	}
	total, err := amount(info, "total_balance", info.TotalBalance)
	if err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}
	granted, err := amount(info, "granted_balance", info.GrantedBalance)
	if err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}
	toppedUp, err := amount(info, "topped_up_balance", info.ToppedUpBalance)
	if err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}

	values := map[string]int64{
		deepseekdomain.MetricBalanceMinor:  total,
		deepseekdomain.MetricGrantedMinor:  granted,
		deepseekdomain.MetricToppedUpMinor: toppedUp,
	}
	observations, err := observations(panel, values, result.FetchedAt)
	if err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}

	status, message := domain.StatusOK, ""
	if !*payload.IsAvailable {
		status = domain.StatusPartial
		message = "账户当前不可用，余额仅供参考。"
	}
	if len(ignored) > 0 {
		note := "已忽略其他币种：" + strings.Join(ignored, "、") + "。"
		if message == "" {
			message = note
		} else {
			message += note
		}
	}

	snapshot := domain.Snapshot{
		PanelID:      panel.ID,
		ProviderID:   providerIDOf(result),
		ObservedAt:   result.FetchedAt,
		Status:       status,
		Observations: observations,
		Message:      message,
		Currency:     strings.ToUpper(strings.TrimSpace(info.Currency)),
	}
	if err := snapshot.Validate(); err != nil {
		return domain.Snapshot{}, badResponse(result, "%v", err)
	}
	return snapshot, nil
}

// pickCurrency chooses the balance to show and reports the currencies it
// ignores: dropping one silently would hide an account's money.
func pickCurrency(infos []balanceInfo) (balanceInfo, []string) {
	preferred := slices.IndexFunc(infos, func(info balanceInfo) bool {
		return strings.EqualFold(strings.TrimSpace(info.Currency), deepseekdomain.PreferCurrency)
	})
	chosen := 0
	if preferred >= 0 {
		chosen = preferred
	}
	var ignored []string
	seen := map[string]bool{}
	for i, info := range infos {
		if i == chosen {
			continue
		}
		currency := strings.ToUpper(strings.TrimSpace(info.Currency))
		if currency == "" || seen[currency] {
			continue
		}
		seen[currency] = true
		ignored = append(ignored, currency)
	}
	return infos[chosen], ignored
}

// amount reads one decimal amount as minor units.
func amount(info balanceInfo, field, value string) (int64, error) {
	minor, err := tools.ParseMoney(value)
	if err != nil {
		return 0, fmt.Errorf("cannot read %s of the %s balance: %v", field, strings.ToUpper(info.Currency), err)
	}
	return minor, nil
}

// observations builds the panel's observations in the order its metrics are
// defined, so the headline is the first one. A metric the response has no
// value for is a bad response, not a zero.
func observations(panel domain.Panel, values map[string]int64, observedAt time.Time) ([]domain.Observation, error) {
	all := deepseekdomain.Metrics()
	if len(panel.MetricKeys) == 0 {
		panel.MetricKeys = make([]string, 0, len(all))
		for _, m := range all {
			panel.MetricKeys = append(panel.MetricKeys, m.Key)
		}
	}
	built := make([]domain.Observation, 0, len(panel.MetricKeys))
	for _, key := range panel.MetricKeys {
		metric, ok := deepseekdomain.Metric(key)
		if !ok {
			return nil, fmt.Errorf("the panel asks for metric %q, which this provider does not report", key)
		}
		value, ok := values[key]
		if !ok {
			return nil, fmt.Errorf("the response has no value for metric %q", key)
		}
		if metric.ValueType != domain.ValueTypeNumber {
			return nil, fmt.Errorf("metric %q is not a number", key)
		}
		built = append(built, domain.Observation{
			MetricKey:  key,
			Value:      domain.NumberValue(value),
			ObservedAt: observedAt,
		})
	}
	return built, nil
}

func providerIDOf(result domain.FetchResult) string {
	if result.ProviderID != "" {
		return result.ProviderID
	}
	return deepseekdomain.ProviderID
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
