package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValueShapesAreExclusive(t *testing.T) {
	number := NumberValue(4210)
	if n, ok := number.Number(); !ok || n != 4210 {
		t.Errorf("NumberValue(4210).Number() = %d, %v", n, ok)
	}
	if _, ok := number.Text(); ok {
		t.Error("a number value also answered as text")
	}
	if _, ok := number.DurationSeconds(); ok {
		t.Error("a number value also answered as a duration")
	}

	duration := DurationValue(90)
	if d, ok := duration.DurationSeconds(); !ok || d != 90 {
		t.Errorf("DurationValue(90).DurationSeconds() = %d, %v", d, ok)
	}

	text := TextValue("active")
	if s, ok := text.Text(); !ok || s != "active" {
		t.Errorf("TextValue(%q).Text() = %q, %v", "active", s, ok)
	}

	var zero Value
	if zero.Valid() {
		t.Error("the zero value is valid")
	}
	if !number.Valid() || !duration.Valid() || !text.Valid() {
		t.Error("a constructed value is not valid")
	}
}

func TestValueMatchesValueType(t *testing.T) {
	if !NumberValue(1).Matches(ValueTypeNumber) {
		t.Error("a number does not match the number type")
	}
	if NumberValue(1).Matches(ValueTypeDuration) {
		t.Error("a number matches the duration type")
	}
	if !DurationValue(1).Matches(ValueTypeDuration) {
		t.Error("a duration does not match the duration type")
	}
	if !TextValue("x").Matches(ValueTypeText) {
		t.Error("a text does not match the text type")
	}
	if TextValue("x").Matches(ValueTypeNumber) {
		t.Error("a text matches the number type")
	}
}

func TestSnapshotValidateAndCurrency(t *testing.T) {
	metric := Metric{
		Key: "balanceMinor", Label: "总余额",
		Unit: UnitCurrency, ValueType: ValueTypeNumber, Precision: 2,
		CurrencyFromProvider: true,
	}
	snapshot := Snapshot{
		PanelID: "p", ProviderID: "deepseek",
		ObservedAt: time.Unix(0, 0).UTC(),
		Status:     StatusOK,
		Currency:   "CNY",
		Observations: []Observation{
			{MetricKey: "balanceMinor", Value: NumberValue(4210)},
		},
	}
	if err := snapshot.Validate(); err != nil {
		t.Errorf("a valid snapshot was rejected: %v", err)
	}
	if currency, err := snapshot.MetricCurrency(metric); err != nil || currency != "CNY" {
		t.Errorf("MetricCurrency = %q, %v; want CNY", currency, err)
	}

	// A metric that names its own currency does not need the snapshot's.
	fixed := snapshot
	fixed.Currency = ""
	named := metric
	named.CurrencyFromProvider = false
	named.Currency = "USD"
	if currency, err := fixed.MetricCurrency(named); err != nil || currency != "USD" {
		t.Errorf("MetricCurrency = %q, %v; want USD", currency, err)
	}

	// Without either, showing the amount would be a guess.
	if _, err := fixed.MetricCurrency(metric); !errors.Is(err, ErrInvalidMetricValue) {
		t.Errorf("MetricCurrency error = %v, want ErrInvalidMetricValue", err)
	}
	if _, err := fixed.MetricCurrency(Metric{Key: "count", Unit: UnitCount}); !errors.Is(err, ErrInvalidMetricValue) {
		t.Errorf("MetricCurrency on a count error = %v, want ErrInvalidMetricValue", err)
	}

	bad := []Snapshot{
		{Status: "unknown"},
		{Status: StatusOK, Observations: []Observation{{Value: NumberValue(1)}}},
		{Status: StatusOK, Observations: []Observation{{MetricKey: "a"}}},
		{Status: StatusOK, Observations: []Observation{{MetricKey: "a", Value: NumberValue(1)}, {MetricKey: "a", Value: NumberValue(2)}}},
	}
	for _, s := range bad {
		if err := s.Validate(); !errors.Is(err, ErrInvalidMetricValue) {
			t.Errorf("Validate() error = %v, want ErrInvalidMetricValue", err)
		}
	}
}
