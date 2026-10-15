package deepseek

import (
	"testing"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
)

func TestPanelIsValid(t *testing.T) {
	panel := Panel()
	if err := panel.Validate(); err != nil {
		t.Fatalf("the panel is invalid: %v", err)
	}
	if panel.Kind != domain.PanelKindProvider || panel.ProviderID != contract.ProviderID {
		t.Errorf("panel = %+v, want a %s panel of %s", panel, domain.PanelKindProvider, contract.ProviderID)
	}
	if panel.RefreshEvery != RefreshEverySeconds {
		t.Errorf("panel refreshes every %d seconds, want %d", panel.RefreshEvery, RefreshEverySeconds)
	}
}

func TestMetricsAreValidAndMatchThePanel(t *testing.T) {
	metrics := Metrics()
	if len(metrics) == 0 {
		t.Fatal("the panel defines no metrics")
	}
	keys := make(map[string]bool, len(metrics))
	for _, m := range metrics {
		if err := m.Validate(); err != nil {
			t.Errorf("metric %s is invalid: %v", m.Key, err)
		}
		if keys[m.Key] {
			t.Errorf("metric %s is defined twice", m.Key)
		}
		keys[m.Key] = true
	}
	panel := Panel()
	if len(panel.MetricKeys) != len(metrics) {
		t.Fatalf("the panel lists %d metrics and defines %d", len(panel.MetricKeys), len(metrics))
	}
	for _, key := range panel.MetricKeys {
		if !keys[key] {
			t.Errorf("the panel lists metric %q, which is not defined", key)
		}
	}
	// The headline metric is the first one: the card shows it large.
	if metrics[0].Key != MetricBalanceMinor {
		t.Errorf("the first metric is %s, want %s", metrics[0].Key, MetricBalanceMinor)
	}
	if m, ok := Metric(MetricToppedUpMinor); !ok || m.Comparison != domain.ComparisonHigherIsBetter {
		t.Errorf("Metric(%s) = %+v, %v", MetricToppedUpMinor, m, ok)
	}
	if _, ok := Metric("nope"); ok {
		t.Error("Metric(\"nope\") reported a metric")
	}
}
