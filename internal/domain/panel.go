package domain

import (
	"fmt"
	"strings"
)

// PanelKind 说明面板的数字从哪来：用户手动记录，还是从 provider 拉取。
type PanelKind string

const (
	// PanelKindManual 是用户自行录入数据的面板。
	PanelKindManual PanelKind = "manual"
	// PanelKindProvider 是由 provider 拉取数据的面板。
	PanelKindProvider PanelKind = "provider"
)

// MetricUnit 是指标数字计量的对象，决定该值如何存储与显示。
type MetricUnit string

const (
	UnitCount    MetricUnit = "count"
	UnitPercent  MetricUnit = "percent"
	UnitSeconds  MetricUnit = "seconds"
	UnitCurrency MetricUnit = "currency"
	UnitBytes    MetricUnit = "bytes"
	UnitTokens   MetricUnit = "tokens"
	UnitText     MetricUnit = "text"
)

// ValueType 是指标值所取的形状。
type ValueType string

const (
	ValueTypeNumber   ValueType = "number"
	ValueTypeDuration ValueType = "duration"
	ValueTypeText     ValueType = "text"
)

// Comparison 说明哪个方向的变动是好的，好让界面区分余额缩水与账单增长。
// 它从不改变值本身。
type Comparison string

const (
	ComparisonNone           Comparison = "none"
	ComparisonLowerIsBetter  Comparison = "lower_is_better"
	ComparisonHigherIsBetter Comparison = "higher_is_better"
)

// Panel 是一张卡片：叫什么、数字从哪来、隔多久值得再取一次。
type Panel struct {
	ID           string
	Kind         PanelKind
	Title        string
	ProviderID   string
	MetricKeys   []string
	RefreshEvery int // 秒；0 表示仅在请求时刷新
	SortOrder    int
	Enabled      bool
}

// Validate 报告面板有什么问题。
func (p Panel) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("%w: panel id is empty", ErrInvalidArgument)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: panel %s has no title", ErrInvalidArgument, p.ID)
	}
	switch p.Kind {
	case PanelKindManual:
		if p.ProviderID != "" {
			return fmt.Errorf("%w: manual panel %s has a provider id %q", ErrInvalidArgument, p.ID, p.ProviderID)
		}
	case PanelKindProvider:
		if strings.TrimSpace(p.ProviderID) == "" {
			return fmt.Errorf("%w: provider panel %s has no provider id", ErrInvalidArgument, p.ID)
		}
	default:
		return fmt.Errorf("%w: panel %s has kind %q", ErrInvalidArgument, p.ID, p.Kind)
	}
	if p.RefreshEvery < 0 {
		return fmt.Errorf("%w: panel %s refreshes every %d seconds", ErrInvalidArgument, p.ID, p.RefreshEvery)
	}
	seen := make(map[string]bool, len(p.MetricKeys))
	for _, key := range p.MetricKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: panel %s has an empty metric key", ErrInvalidArgument, p.ID)
		}
		if seen[key] {
			return fmt.Errorf("%w: panel %s lists metric %q twice", ErrInvalidArgument, p.ID, key)
		}
		seen[key] = true
	}
	return nil
}

// Metric 描述面板里的一个数字是什么。
type Metric struct {
	Key       string
	Label     string
	Unit      MetricUnit
	ValueType ValueType
	Precision int
	// Currency 是指标值所用的货币，面板已知时填写。
	Currency string
	// CurrencyFromProvider 表示只有响应才知道货币：此时由 adapter 填充
	// Snapshot.Currency。它与 Currency 互斥。
	CurrencyFromProvider bool
	Comparison           Comparison
}

// Validate 报告指标有什么问题。
func (m Metric) Validate() error {
	if strings.TrimSpace(m.Key) == "" {
		return fmt.Errorf("%w: a metric has no key", ErrInvalidArgument)
	}
	if strings.TrimSpace(m.Label) == "" {
		return fmt.Errorf("%w: metric %s has no label", ErrInvalidArgument, m.Key)
	}
	if m.Precision < 0 {
		return fmt.Errorf("%w: metric %s has precision %d", ErrInvalidArgument, m.Key, m.Precision)
	}
	switch m.ValueType {
	case ValueTypeNumber:
		switch m.Unit {
		case UnitCount, UnitPercent, UnitCurrency, UnitBytes, UnitTokens:
		default:
			return fmt.Errorf("%w: metric %s is a number in unit %q", ErrInvalidArgument, m.Key, m.Unit)
		}
	case ValueTypeDuration:
		if m.Unit != UnitSeconds {
			return fmt.Errorf("%w: metric %s is a duration in unit %q", ErrInvalidArgument, m.Key, m.Unit)
		}
	case ValueTypeText:
		if m.Unit != UnitText {
			return fmt.Errorf("%w: metric %s is text in unit %q", ErrInvalidArgument, m.Key, m.Unit)
		}
	default:
		return fmt.Errorf("%w: metric %s has value type %q", ErrInvalidArgument, m.Key, m.ValueType)
	}
	if m.Unit == UnitCurrency {
		switch {
		case m.Currency == "" && !m.CurrencyFromProvider:
			return fmt.Errorf("%w: currency metric %s names no currency", ErrInvalidArgument, m.Key)
		case m.Currency != "" && m.CurrencyFromProvider:
			return fmt.Errorf("%w: currency metric %s names a currency and takes it from the provider", ErrInvalidArgument, m.Key)
		}
	} else if m.Currency != "" || m.CurrencyFromProvider {
		return fmt.Errorf("%w: metric %s has a currency but its unit is %q", ErrInvalidArgument, m.Key, m.Unit)
	}
	switch m.Comparison {
	case ComparisonNone, ComparisonLowerIsBetter, ComparisonHigherIsBetter:
	default:
		return fmt.Errorf("%w: metric %s has comparison %q", ErrInvalidArgument, m.Key, m.Comparison)
	}
	return nil
}
