package domain

import (
	"errors"
	"strings"
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

func TestCredentialsNeverLeakAValue(t *testing.T) {
	creds := Credentials{ProviderID: "deepseek", Secrets: map[string]string{}}
	if _, err := creds.Secret("apiKey"); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	} else if !strings.Contains(err.Error(), "deepseek.apiKey") {
		t.Errorf("the error does not name the missing credential: %v", err)
	}

	creds.Secrets["apiKey"] = "  sk-secret-value  "
	value, err := creds.Secret("apiKey")
	if err != nil {
		t.Fatalf("Secret(apiKey) failed: %v", err)
	}
	if value != "sk-secret-value" {
		t.Errorf("Secret(apiKey) = %q, want the trimmed value", value)
	}
	if _, err := creds.Secret("adminKey"); err == nil || strings.Contains(err.Error(), "sk-secret-value") {
		t.Errorf("the error for a missing credential leaked a value: %v", err)
	}
}

func TestProviderDescriptorValidate(t *testing.T) {
	ok := ProviderDescriptor{
		ID: "deepseek", DisplayName: "DeepSeek",
		CredentialKeys: []string{"apiKey"},
		DocsURL:        "https://platform.deepseek.com/api_keys",
		Endpoint:       "https://api.deepseek.com/user/balance",
	}
	if err := ok.Validate(); err != nil {
		t.Errorf("a valid descriptor was rejected: %v", err)
	}
	if got := ok.MinRefreshInterval(); got != time.Duration(DefaultMinRefreshSeconds)*time.Second {
		t.Errorf("MinRefreshInterval() = %s, want the default", got)
	}
	ok.MinRefreshSeconds = 60
	if got := ok.MinRefreshInterval(); got != time.Minute {
		t.Errorf("MinRefreshInterval() = %s, want 1m", got)
	}

	bad := []ProviderDescriptor{
		{DisplayName: "x", CredentialKeys: []string{"k"}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", CredentialKeys: []string{"k"}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{""}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k", "k"}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k"}, Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k"}, DocsURL: "u"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k"}, DocsURL: "u", Endpoint: "e", MinRefreshSeconds: -1},
	}
	for _, d := range bad {
		if err := d.Validate(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("descriptor %+v: error = %v, want ErrInvalidArgument", d, err)
		}
	}
}

func TestBadResponseErrorCarriesNoSecretAndIsClassifiable(t *testing.T) {
	err := &BadResponseError{
		ProviderID: "deepseek",
		Endpoint:   "https://api.deepseek.com/user/balance",
		HTTPStatus: 200,
		Reason:     "balance_infos is empty",
		Snippet:    `{"is_available":true}`,
	}
	if !errors.Is(err, ErrProviderBadResponse) {
		t.Error("errors.Is(err, ErrProviderBadResponse) is false")
	}
	message := err.Error()
	for _, want := range []string{"deepseek", "user/balance", "200", "balance_infos", `{"is_available":true}`} {
		if !strings.Contains(message, want) {
			t.Errorf("the error does not mention %q: %s", want, message)
		}
	}
}

func TestSnippetIsClipped(t *testing.T) {
	long := make([]byte, 4000)
	for i := range long {
		long[i] = 'a'
	}
	if got := len(Snippet(long)); got > MaxSnippetBytes {
		t.Errorf("Snippet kept %d bytes, want at most %d", got, MaxSnippetBytes)
	}
	if got := Snippet([]byte("  {\"a\":1}  ")); got != `{"a":1}` {
		t.Errorf("Snippet = %q, want the trimmed response", got)
	}
}
