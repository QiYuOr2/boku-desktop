// Package grok 提供 Grok 订阅额度：展示它的面板、读取它的请求、回答的纯映射，
// 以及 Boku 自行保留的设备码登录。
//
// 分工与其他厂商一致：spec.go 是配置，client.go 出网，adapter.go 是响应的纯函数，
// login.go 保存 token。adapter.go 不得读时钟、开文件或发请求。
package grok

import "boku/internal/domain"

const ProviderID = "grok"

const (
	PanelID = "0198a1b2-0000-7000-8000-000000000003"
	// RefreshEverySeconds 是一次读数的新鲜时长：面板每五分钟再问一次，
	// 而厂商自己的下限是一分钟。
	RefreshEverySeconds = 300
	// AccessTokenCredential 是账单请求携带的 access token。refresh token 存在它旁边，
	// 但 provider 本身只索要 access token。
	AccessTokenCredential  = "accessToken"
	RefreshTokenCredential = "refreshToken"
)

const (
	MetricCreditRemainingPercent   = "creditRemainingPercent"
	MetricCreditResetsInSeconds    = "creditResetsInSeconds"
	MetricCreditWindowSeconds      = "creditWindowSeconds"
	MetricOnDemandRemainingPercent = "onDemandRemainingPercent"
)

// UsageURL 是额度面板所概括的账号页面。
const UsageURL = "https://grok.com"

// OptionalMetricKeys 列出端点可能省略的观测。顺序即额度卡读取顺序：
// 先重置，再所属周期，最后按量条。
func OptionalMetricKeys() []string {
	return []string{MetricCreditResetsInSeconds, MetricCreditWindowSeconds, MetricOnDemandRemainingPercent}
}

// Panel 返回额度面板。
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

// Metrics 按卡片读取顺序返回面板的指标。
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

// Metric 按 key 返回一个指标，以及它是否属于本面板。
func Metric(key string) (domain.Metric, bool) {
	for _, metric := range Metrics() {
		if metric.Key == key {
			return metric, true
		}
	}
	return domain.Metric{}, false
}
