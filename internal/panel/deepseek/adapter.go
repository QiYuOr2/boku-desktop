// 本文件把 DeepSeek 的余额响应映射为应用的快照。它是响应的纯函数：不碰网络、
// 不读时钟，并且每种情况都有 fixture，使服务改字段名时由测试发现，
// 而不是由卡片显示 0 来发现。
package deepseek

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/kit"
)

// balanceResponse 是 DeepSeek 自己的结构，也是整个应用中唯一出现这些字段名的地方。
type balanceResponse struct {
	// IsAvailable 用指针，使缺少该字段的响应被判为坏响应，而不是"账户不可用"。
	IsAvailable  *bool         `json:"is_available"`
	BalanceInfos []balanceInfo `json:"balance_infos"`
}

type balanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// Adapter 把响应映射为快照。
type Adapter struct{}

// NewAdapter 返回 adapter。
func NewAdapter() *Adapter { return &Adapter{} }

// ProviderID 返回该 adapter 所映射的 provider。
func (a *Adapter) ProviderID() string { return contract.ProviderID }

// Normalize 把一条余额响应转成快照。
//
// 当响应不是已知结构、没有余额可显示、或某金额不是数字时，它选择拒绝而不是猜测：
// 卡片绝不能以 0 代替失败。账户报告自身不可用不算失败——那是余额确实为零的账户——
// 这种情况会变成带说明的部分快照。
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
		MetricBalanceMinor:  total,
		MetricGrantedMinor:  granted,
		MetricToppedUpMinor: toppedUp,
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

// pickCurrency 选出要显示的余额并返回被忽略的币种：静默丢弃会掩盖账户的钱。
func pickCurrency(infos []balanceInfo) (balanceInfo, []string) {
	preferred := slices.IndexFunc(infos, func(info balanceInfo) bool {
		return strings.EqualFold(strings.TrimSpace(info.Currency), PreferCurrency)
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

// amount 把一个十进制金额读成最小货币单位。
func amount(info balanceInfo, field, value string) (int64, error) {
	minor, err := kit.ParseMoney(value)
	if err != nil {
		return 0, fmt.Errorf("cannot read %s of the %s balance: %v", field, strings.ToUpper(info.Currency), err)
	}
	return minor, nil
}

// observations 按面板定义的指标顺序构建观测项，主指标排第一。响应中没有值的
// 指标判为坏响应，而不是零。
func observations(panel domain.Panel, values map[string]int64, observedAt time.Time) ([]domain.Observation, error) {
	all := Metrics()
	if len(panel.MetricKeys) == 0 {
		panel.MetricKeys = make([]string, 0, len(all))
		for _, m := range all {
			panel.MetricKeys = append(panel.MetricKeys, m.Key)
		}
	}
	built := make([]domain.Observation, 0, len(panel.MetricKeys))
	for _, key := range panel.MetricKeys {
		metric, ok := Metric(key)
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
	return contract.ProviderID
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
