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

// Value is what a metric was observed to be: a number, a duration in
// seconds, or a text. The three exclude each other, which the constructors
// guarantee; a Value never changes shape afterwards.
//
// Amounts travel as integer minor units and durations as integer seconds,
// so no Value ever holds a float.
type Value struct {
	kind   valueKind
	number int64
	text   string
}

// NumberValue returns a numeric value, for units such as count, percent,
// currency, bytes or tokens.
func NumberValue(n int64) Value { return Value{kind: kindNumber, number: n} }

// DurationValue returns a duration, in whole seconds.
func DurationValue(seconds int64) Value {
	return Value{kind: kindDuration, number: seconds}
}

// TextValue returns a text value.
func TextValue(s string) Value { return Value{kind: kindText, text: s} }

// Number returns the number, and whether the value is one.
func (v Value) Number() (int64, bool) {
	return v.number, v.kind == kindNumber
}

// DurationSeconds returns the duration in seconds, and whether the value is
// one.
func (v Value) DurationSeconds() (int64, bool) {
	return v.number, v.kind == kindDuration
}

// Text returns the text, and whether the value is one.
func (v Value) Text() (string, bool) {
	return v.text, v.kind == kindText
}

// Valid reports whether the value has one of the three shapes.
func (v Value) Valid() bool { return v.kind != kindNone }

// Matches reports whether the value has the shape its metric's value type
// asks for.
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

// Observation is one metric, as of one moment.
type Observation struct {
	MetricKey  string
	Value      Value
	ObservedAt time.Time
}

// SnapshotStatus says how much of a panel's data a snapshot carries.
type SnapshotStatus string

const (
	// StatusOK means every metric was fetched.
	StatusOK SnapshotStatus = "ok"
	// StatusPartial means some metrics are there and something is missing:
	// the interface shows what it has and says what it does not.
	StatusPartial SnapshotStatus = "partial"
	// StatusError means nothing was fetched. The interface says why and
	// never shows 0 in place of it.
	StatusError SnapshotStatus = "error"
)

// Snapshot is one panel's data as of one moment: the only shape a provider's
// answer is turned into. The interface reads snapshots and nothing else.
type Snapshot struct {
	PanelID      string
	ProviderID   string
	ObservedAt   time.Time
	Status       SnapshotStatus
	Observations []Observation
	// Message explains a partial or failed snapshot to the user.
	Message string
	// Currency is the currency the values are in, for metrics that take
	// their currency from the provider.
	Currency string
}

// Observation returns the observation of one metric key.
func (s Snapshot) Observation(metricKey string) (Observation, bool) {
	for _, o := range s.Observations {
		if o.MetricKey == metricKey {
			return o, true
		}
	}
	return Observation{}, false
}

// MetricCurrency returns the currency a metric's values are in: the one the
// metric names, or the one the response carried. A currency metric without
// either cannot be shown, which is an error rather than a guess.
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

// Validate reports what is wrong with the snapshot.
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
