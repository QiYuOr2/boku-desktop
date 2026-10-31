package grok

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/domain"
)

const fixturesDir = "../../../testdata/providers/grok"

func fixture(t *testing.T, name string, fetchedAt time.Time) domain.FetchResult {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read the fixture %s: %v", name, err)
	}
	return domain.FetchResult{ProviderID: ProviderID, Endpoint: billingURL, FetchedAt: fetchedAt, Payload: payload, HTTPStatus: 200}
}

func observation(t *testing.T, snapshot domain.Snapshot, key string) domain.Observation {
	t.Helper()
	found, ok := snapshot.Observation(key)
	if !ok {
		t.Fatalf("the snapshot has no %s; it has %+v", key, snapshot.Observations)
	}
	return found
}

func TestNormalizeMapsTheWeeklyWindow(t *testing.T) {
	end := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	fetchedAt := end.Add(-time.Hour)
	snapshot, err := NewAdapter().Normalize(fixture(t, "billing_weekly.json", fetchedAt), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if snapshot.PanelID != PanelID || snapshot.ProviderID != ProviderID {
		t.Fatalf("snapshot belongs to %s/%s", snapshot.ProviderID, snapshot.PanelID)
	}
	if got, ok := observation(t, snapshot, MetricCreditRemainingPercent).Value.Number(); !ok || got != 57 {
		t.Fatalf("remaining = %d (%v), want 57", got, ok)
	}
	if got, ok := observation(t, snapshot, MetricCreditResetsInSeconds).Value.DurationSeconds(); !ok || got != 3600 {
		t.Fatalf("reset = %d, want 3600", got)
	}
	if got, ok := observation(t, snapshot, MetricCreditWindowSeconds).Value.DurationSeconds(); !ok || got != 604800 {
		t.Fatalf("window = %d, want a week", got)
	}
	if _, ok := snapshot.Observation(MetricOnDemandRemainingPercent); ok {
		t.Fatal("a zero on-demand cap became a bar")
	}
	if _, ok := snapshot.Observation("planName"); ok {
		t.Fatal("the account tier was mapped")
	}
}

func TestExplicitZeroIsAFullDailyWindow(t *testing.T) {
	end := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	fetchedAt := end.Add(-2 * time.Minute)
	snapshot, err := NewAdapter().Normalize(fixture(t, "billing_zero.json", fetchedAt), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if got, ok := observation(t, snapshot, MetricCreditRemainingPercent).Value.Number(); !ok || got != 100 {
		t.Fatalf("remaining = %d, want 100", got)
	}
	if got, _ := observation(t, snapshot, MetricCreditWindowSeconds).Value.DurationSeconds(); got != 86400 {
		t.Fatalf("window = %d, want a day", got)
	}
	if got, _ := observation(t, snapshot, MetricCreditResetsInSeconds).Value.DurationSeconds(); got != 120 {
		t.Fatalf("reset = %d, want 120", got)
	}
}

func TestOnDemandAndMonthlyWindow(t *testing.T) {
	snapshot, err := NewAdapter().Normalize(fixture(t, "billing_on_demand.json", time.Unix(0, 0).UTC()), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if got, _ := observation(t, snapshot, MetricCreditRemainingPercent).Value.Number(); got != 90 {
		t.Fatalf("remaining = %d, want 90", got)
	}
	if got, _ := observation(t, snapshot, MetricCreditWindowSeconds).Value.DurationSeconds(); got != 2592000 {
		t.Fatalf("window = %d, want 30 days", got)
	}
	if got, _ := observation(t, snapshot, MetricOnDemandRemainingPercent).Value.Number(); got != 75 {
		t.Fatalf("on demand = %d, want 75", got)
	}
}

func TestBillingPeriodEndIsUsedWhenTheCurrentPeriodIsAbsent(t *testing.T) {
	end, err := time.Parse(time.RFC3339Nano, "2026-10-31T00:00:00.5Z")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewAdapter().Normalize(fixture(t, "billing_period_end_only.json", end.Add(-90*time.Second)), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if got, _ := observation(t, snapshot, MetricCreditRemainingPercent).Value.Number(); got != 94 {
		t.Fatalf("remaining = %d, want 94", got)
	}
	if _, ok := snapshot.Observation(MetricCreditWindowSeconds); ok {
		t.Fatal("a response without a period type invented a window")
	}
	if got, _ := observation(t, snapshot, MetricCreditResetsInSeconds).Value.DurationSeconds(); got != 90 {
		t.Fatalf("reset = %d, want 90", got)
	}
}

func TestMalformedBillingNeverProducesQuota(t *testing.T) {
	for _, name := range []string{"billing_malformed_percent.json", "billing_malformed_on_demand.json", "billing_malformed_type.json"} {
		t.Run(name, func(t *testing.T) {
			_, err := NewAdapter().Normalize(fixture(t, name, time.Now().UTC()), Panel())
			if !errors.Is(err, domain.ErrProviderBadResponse) {
				t.Fatalf("error = %v, want ErrProviderBadResponse", err)
			}
		})
	}
}
