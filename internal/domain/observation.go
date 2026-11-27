package domain

import (
	"fmt"
	"time"
)

type valueKind uint8

const (
	kindNone valueKind = iota
	kindNumber
	kindDuration
	kindText
)

// Value 是指标被观测到的值：数字、以秒为单位的时长、或文本。三者互斥，由
// 构造函数保证；Value 创建后形状不再改变。
//
// 金额以整数最小货币单位传递，时长以整数秒传递，因此 Value 从不保存浮点数。
type Value struct {
	kind   valueKind
	number int64
	text   string
}

// NumberValue 返回数值，用于 count、percent、currency、bytes、tokens 等单位。
func NumberValue(n int64) Value { return Value{kind: kindNumber, number: n} }

// DurationValue 返回以整秒表示的时长。
func DurationValue(seconds int64) Value {
	return Value{kind: kindDuration, number: seconds}
}

// TextValue 返回文本值。
func TextValue(s string) Value { return Value{kind: kindText, text: s} }

// Number 返回数字以及该值是否为数字。
func (v Value) Number() (int64, bool) {
	return v.number, v.kind == kindNumber
}

// DurationSeconds 返回以秒计的时长以及该值是否为时长。
func (v Value) DurationSeconds() (int64, bool) {
	return v.number, v.kind == kindDuration
}

// Text 返回文本以及该值是否为文本。
func (v Value) Text() (string, bool) {
	return v.text, v.kind == kindText
}

// Valid 报告该值是否具有三种形状之一。
func (v Value) Valid() bool { return v.kind != kindNone }

// Matches 报告该值的形状是否符合其指标值类型的要求。
func (v Value) Matches(t ValueType) bool {
	switch t {
	case ValueTypeNumber:
		return v.kind == kindNumber
	case ValueTypeDuration:
		return v.kind == kindDuration
	case ValueTypeText:
		return v.kind == kindText
	}
	return false
}

// Observation 是某一时刻的一个指标。
type Observation struct {
	MetricKey  string
	Value      Value
	ObservedAt time.Time
}

// SnapshotStatus 表示一个快照带有多完整的面板数据。
type SnapshotStatus string

const (
	// StatusOK 表示所有指标都已获取。
	StatusOK SnapshotStatus = "ok"
	// StatusPartial 表示部分指标存在、部分缺失：界面显示已有的，并说明缺
	// 了什么。
	StatusPartial SnapshotStatus = "partial"
	// StatusError 表示什么都没获取到。界面说明原因，绝不以 0 代替。
	StatusError SnapshotStatus = "error"
)

// Snapshot 是某一时刻一个面板的数据：provider 的应答被转换成唯一的这个形状。
// 界面只读快照，不读别的。
type Snapshot struct {
	PanelID      string
	ProviderID   string
	ObservedAt   time.Time
	Status       SnapshotStatus
	Observations []Observation
	// Message 向用户解释部分或失败的快照。
	Message string
	// Currency 是这些值所用的货币，供从 provider 获取货币的指标使用。
	Currency string
}

// Observation 返回某个指标 key 的观测值。
func (s Snapshot) Observation(metricKey string) (Observation, bool) {
	for _, o := range s.Observations {
		if o.MetricKey == metricKey {
			return o, true
		}
	}
	return Observation{}, false
}

// MetricCurrency 返回指标值所用的货币：指标自身指定的，或响应携带的。货币
// 指标两者都没有时无法显示，这是错误而非猜测。
func (s Snapshot) MetricCurrency(m Metric) (string, error) {
	switch {
	case m.Currency != "":
		return m.Currency, nil
	case m.CurrencyFromProvider:
		if s.Currency == "" {
			return "", fmt.Errorf("%w: metric %s takes its currency from the provider, and the snapshot has none", ErrInvalidMetricValue, m.Key)
		}
		return s.Currency, nil
	default:
		return "", fmt.Errorf("%w: metric %s is not an amount", ErrInvalidMetricValue, m.Key)
	}
}

// Validate 报告快照有什么问题。
func (s Snapshot) Validate() error {
	switch s.Status {
	case StatusOK, StatusPartial, StatusError:
	default:
		return fmt.Errorf("%w: snapshot has status %q", ErrInvalidMetricValue, s.Status)
	}
	seen := make(map[string]bool, len(s.Observations))
	for _, o := range s.Observations {
		if o.MetricKey == "" {
			return fmt.Errorf("%w: snapshot has an observation without a metric key", ErrInvalidMetricValue)
		}
		if !o.Value.Valid() {
			return fmt.Errorf("%w: observation %s has no value", ErrInvalidMetricValue, o.MetricKey)
		}
		if seen[o.MetricKey] {
			return fmt.Errorf("%w: snapshot has two observations of %s", ErrInvalidMetricValue, o.MetricKey)
		}
		seen[o.MetricKey] = true
	}
	return nil
}
