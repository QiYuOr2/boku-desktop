// Package codex 描述 Codex CLI 所登录的 ChatGPT 账户：额度面板、读取请求、
// 服务响应到快照的纯映射，以及 Boku 借用 CLI 登录凭据（而非让用户填 key）。
//
// 按代码性质分文件：spec.go 为配置，client.go 出网，adapter.go 为纯函数映射，
// login.go 读取其它工具的凭据文件。adapter.go 不得读时钟、开文件或发请求，
// 以保证可用 fixture 离线测试。
package codex

import "boku/internal/domain"

// ProviderID 为该面板读取的厂商标识，须与跨端契约一致。
const ProviderID = "codex"

const (
	// PanelID 跨启动稳定：快照缓存与数据库以它为键。
	PanelID = "0198a1b2-0000-7000-8000-000000000002"

	// RefreshEverySeconds 为额度数据的保鲜时长；受厂商自身的下限约束。
	RefreshEverySeconds = 300

	// AccessTokenCredential 是 Codex CLI 已登录的 token，Boku 直接读取其所在处，
	// 并非来自 secret store；描述符、刷新调用与错误信息统一使用此名。
	AccessTokenCredential = "accessToken"

	// AccountIDSetting 是登录的非敏感部分：token 所属的 ChatGPT 账户。
	// 作为 setting 而非 secret 传递；登录指明了账户时作为 ChatGPT-Account-Id 头发送。
	AccountIDSetting = "accountId"
)

// 面板的指标 key。
const (
	// MetricSessionRemainingPercent 是 5 小时窗口剩余额度，取值 0..100。
	MetricSessionRemainingPercent = "sessionRemainingPercent"
	// MetricSessionResetsInSeconds 是窗口重置剩余秒数，自取数时刻起算。
	MetricSessionResetsInSeconds = "sessionResetsInSeconds"
	// MetricPlanName 是账号等级，沿用服务端的写法。
	MetricPlanName = "planName"
	// 窗口长度取自响应，与重置倒计时无关。
	MetricSessionWindowSeconds = "sessionWindowSeconds"
	MetricLongWindowSeconds    = "longWindowSeconds"
	// 长窗口观测可选：免费账号或无次级窗口的响应只带会话指标。
	MetricLongWindowRemainingPercent = "longWindowRemainingPercent"
	MetricLongWindowResetsInSeconds  = "longWindowResetsInSeconds"
)

// UsageURL 是 ChatGPT 账户的额度页，面板的进度条点击后打开它。
const UsageURL = "https://chatgpt.com/codex/settings/usage"

// OptionalMetricKeys 列出端点可能省略的观测项。
func OptionalMetricKeys() []string {
	return []string{MetricSessionWindowSeconds, MetricLongWindowRemainingPercent, MetricLongWindowResetsInSeconds, MetricLongWindowSeconds}
}

// Panel 返回额度面板。
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

// Metrics 按显示顺序返回指标：剩余额度在最前，因为进度条是主要信息。
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
		{
			Key:        MetricSessionWindowSeconds,
			Label:      "主窗口周期",
			Unit:       domain.UnitSeconds,
			ValueType:  domain.ValueTypeDuration,
			Comparison: domain.ComparisonNone,
		},
		{
			Key:        MetricLongWindowRemainingPercent,
			Label:      "长周期剩余额度",
			Unit:       domain.UnitPercent,
			ValueType:  domain.ValueTypeNumber,
			Comparison: domain.ComparisonHigherIsBetter,
		},
		{
			Key:        MetricLongWindowResetsInSeconds,
			Label:      "长周期重置",
			Unit:       domain.UnitSeconds,
			ValueType:  domain.ValueTypeDuration,
			Comparison: domain.ComparisonNone,
		},
		{
			Key:        MetricLongWindowSeconds,
			Label:      "长窗口周期",
			Unit:       domain.UnitSeconds,
			ValueType:  domain.ValueTypeDuration,
			Comparison: domain.ComparisonNone,
		},
	}
}

// Metric 按 key 返回指标，并说明它是否属于本面板。
func Metric(key string) (domain.Metric, bool) {
	for _, m := range Metrics() {
		if m.Key == key {
			return m, true
		}
	}
	return domain.Metric{}, false
}
