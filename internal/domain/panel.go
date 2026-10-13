package domain

import (
	"fmt"
	"strings"
)

// PanelKind tells where a panel's numbers come from: recorded by hand, or
// pulled from a provider.
type PanelKind string

const (
	// PanelKindManual is a panel whose entries the user records.
	PanelKindManual PanelKind = "manual"
	// PanelKindProvider is a panel a provider fetches.
	PanelKindProvider PanelKind = "provider"
)

// MetricUnit is what a metric's number counts. It decides how the value is
// stored and how it is shown.
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

// ValueType is the shape a metric's value takes.
type ValueType string

const (
	ValueTypeNumber   ValueType = "number"
	ValueTypeDuration ValueType = "duration"
	ValueTypeText     ValueType = "text"
)

// Comparison says which direction of change is good, so the interface can
// tell a shrinking balance from a growing bill. It never changes a value.
type Comparison string

const (
	ComparisonNone           Comparison = "none"
	ComparisonLowerIsBetter  Comparison = "lower_is_better"
	ComparisonHigherIsBetter Comparison = "higher_is_better"
)

// Panel is one card: what it is called, where its numbers come from, and
// how often they are worth fetching again.
type Panel struct {
	ID           string
	Kind         PanelKind
	Title        string
	ProviderID   string
	MetricKeys   []string
	RefreshEvery int // seconds; 0 means only when asked
	SortOrder    int
	Enabled      bool
}

// Validate reports what is wrong with the panel.
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

// Metric describes what one number of a panel is.
type Metric struct {
	Key       string
	Label     string
	Unit      MetricUnit
	ValueType ValueType
	Precision int
	// Currency is the currency the values are in, when the panel knows it.
	Currency string
	// CurrencyFromProvider is set when only the response knows the currency:
	// the adapter then fills Snapshot.Currency. It excludes Currency.
	CurrencyFromProvider bool
	Comparison           Comparison
}

// Validate reports what is wrong with the metric.
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
