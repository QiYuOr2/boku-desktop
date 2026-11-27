// 本文件把 ChatGPT 的用量响应映射为应用的快照。它是响应与取数时刻的纯函数：
// 不发起网络请求、不读取自己的时钟。
package codex

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"boku/internal/domain"
)

// usageResponse 是接口自身的结构，也是应用里唯一出现这些字段名的地方。必填字段
// 都用指针，使缺失时被判为坏响应，而不是当成 0 配额。
type usageResponse struct {
	PlanType  string     `json:"plan_type"`
	RateLimit *rateLimit `json:"rate_limit"`
}

type rateLimit struct {
	PrimaryWindow *window `json:"primary_window"`
	// SecondaryWindow 可选，只对非免费账户展示。
	SecondaryWindow *window `json:"secondary_window"`
}

type window struct {
	UsedPercent *int `json:"used_percent"`
	// ResetAt 是 Unix 秒。
	ResetAt *int64 `json:"reset_at"`
	// LimitWindowSeconds 决定周期标签，与下次重置时间无关。缺失时不能默认按五小时处理。
	LimitWindowSeconds *int64 `json:"limit_window_seconds"`
}

type Adapter struct{}

func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) ProviderID() string { return ProviderID }

// Normalize 把一份用量响应转成快照。
//
// 当响应不是已知结构，或窗口缺少百分比、重置时间时，它选择报错而不是猜测：字段
// 被改名时若显示「剩余 0%」，比提示读不到结果更糟。
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

// observations 按面板定义的指标顺序构建观测值，使进度条自身的数字排在最前。
// 响应中缺值的指标会被判为坏响应，而不是 0。
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
