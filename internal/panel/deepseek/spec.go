// Package deepseek 包含本应用关于 DeepSeek 的全部知识：展示余额的面板、
// 读取余额的请求，以及把服务响应纯映射为应用快照的代码。
//
// 之所以是同一个包，因为它属于同一个厂商：面板定义、所需凭据与响应结构
// 一起变化，改任何一个都是改 DeepSeek。这里不包含的是服务身份——provider id
// 与凭据 key——它们放在 internal/contract/deepseek，因为翻译器以同样的名字
// 访问同一个服务。
//
// 包内按代码性质分文件，并由 review 维持：spec.go 是配置，client.go 出网并做
// 错误翻译，adapter.go 是响应的纯函数。adapter.go 中不得读时钟、开文件或
// 发请求——这正是映射可以用 fixture 测试的原因。
package deepseek

import (
	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
)

const (
	// PanelID 跨启动稳定：快照缓存与数据库都以它为键。
	PanelID = "0198a1b2-0000-7000-8000-000000000001"

	// RefreshEverySeconds 是余额保持新鲜的时长。余额端点开销小，因此远低于
	// 全局下限；真正约束它的是 provider 自己的下限。
	RefreshEverySeconds = 600
)

// 面板的指标 key。
const (
	MetricBalanceMinor  = "balanceMinor"
	MetricGrantedMinor  = "grantedMinor"
	MetricToppedUpMinor = "toppedUpMinor"
)

// PreferCurrency 是账户报告多种币种时选用的币种。它是偏好而非强制：
// 只报告 USD 的账户就按 USD 显示。
const PreferCurrency = "CNY"

// UsageURL 是 DeepSeek 自己的账户页：面板的余额就是它的摘要。面板主指标会
// 打开它，所以它和这个 provider 面板的其他定义（key、标题）放在一起。
const UsageURL = "https://platform.deepseek.com/usage"

// Panel 返回余额面板。
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

// Metrics 返回面板的指标，按显示顺序：余额在前，因为它是主指标。
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

// Metric 按 key 返回指标，以及它是否属于该面板。
func Metric(key string) (domain.Metric, bool) {
	for _, m := range Metrics() {
		if m.Key == key {
			return m, true
		}
	}
	return domain.Metric{}, false
}
