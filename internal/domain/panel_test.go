package domain

import (
	"errors"
	"testing"
)

func TestPanelValidate(t *testing.T) {
	ok := Panel{
		ID:         "p1",
		Kind:       PanelKindProvider,
		Title:      "API 余额",
		ProviderID: "deepseek",
		MetricKeys: []string{"balanceMinor"},
		Enabled:    true,
	}
	if err := ok.Validate(); err != nil {
		t.Errorf("a valid panel was rejected: %v", err)
	}

	cases := []struct {
		name  string
		panel Panel
	}{
		{"no id", Panel{Kind: PanelKindManual, Title: "x"}},
		{"no title", Panel{ID: "p", Kind: PanelKindManual}},
		{"provider panel without a provider", Panel{ID: "p", Kind: PanelKindProvider, Title: "x"}},
		{"manual panel with a provider", Panel{ID: "p", Kind: PanelKindManual, Title: "x", ProviderID: "deepseek"}},
		{"unknown kind", Panel{ID: "p", Kind: "other", Title: "x"}},
		{"negative refresh", Panel{ID: "p", Kind: PanelKindManual, Title: "x", RefreshEvery: -1}},
		{"repeated metric", Panel{ID: "p", Kind: PanelKindManual, Title: "x", MetricKeys: []string{"a", "a"}}},
		{"empty metric key", Panel{ID: "p", Kind: PanelKindManual, Title: "x", MetricKeys: []string{""}}},
	}
	for _, c := range cases {
		if err := c.panel.Validate(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: error = %v, want ErrInvalidArgument", c.name, err)
		}
	}
}

func TestMetricValidate(t *testing.T) {
	valid := Metric{
		Key: "balanceMinor", Label: "总余额",
		Unit: UnitCurrency, ValueType: ValueTypeNumber, Precision: 2,
		CurrencyFromProvider: true, Comparison: ComparisonLowerIsBetter,
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("a valid metric was rejected: %v", err)
	}
	fixed := valid
	fixed.CurrencyFromProvider = false
	fixed.Currency = "CNY"
	if err := fixed.Validate(); err != nil {
		t.Errorf("a metric with a fixed currency was rejected: %v", err)
	}

	broken := func(f func(m *Metric)) Metric {
		m := valid
		f(&m)
		return m
	}
	cases := []struct {
		name   string
		metric Metric
	}{
		{"no key", broken(func(m *Metric) { m.Key = "" })},
		{"no label", broken(func(m *Metric) { m.Label = "" })},
		{"negative precision", broken(func(m *Metric) { m.Precision = -1 })},
		{"number in seconds", broken(func(m *Metric) { m.Unit = UnitSeconds })},
		{"duration in currency", broken(func(m *Metric) { m.ValueType = ValueTypeDuration })},
		{"text in count", broken(func(m *Metric) { m.ValueType = ValueTypeText })},
		{"unknown value type", broken(func(m *Metric) { m.ValueType = "money" })},
		{"currency without a currency", broken(func(m *Metric) { m.CurrencyFromProvider = false })},
		{"currency from the provider and named", broken(func(m *Metric) { m.Currency = "CNY" })},
		{"a currency on a count", broken(func(m *Metric) {
			m.Unit = UnitCount
			m.CurrencyFromProvider = false
			m.Currency = "CNY"
		})},
		{"unknown comparison", broken(func(m *Metric) { m.Comparison = "up" })},
	}
	for _, c := range cases {
		if err := c.metric.Validate(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: error = %v, want ErrInvalidArgument", c.name, err)
		}
	}
}
