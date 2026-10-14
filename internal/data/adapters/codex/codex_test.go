package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/domain"
	codexdomain "boku/internal/domain/codex"
)

// fixtures live at the repository root, shared with the Apple target: both
// ends must read the same response the service sends.
const fixturesDir = "../../../../../../fixtures/providers/codex"

// fetchedAt is the moment the fixtures are taken to be fetched at, and
// sessionResetAt is the reset time usage_ok.json carries: the adapter counts
// one from the other, so both are written down here.
var fetchedAt = time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)

const sessionResetAt = 1791943200 // 2026-10-14T04:00:00Z, two hours later

func fixture(t *testing.T, name string) domain.FetchResult {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read the fixture %s: %v", name, err)
	}
	return domain.FetchResult{
		ProviderID: codexdomain.ProviderID,
		Endpoint:   "https://chatgpt.com/backend-api/wham/usage",
		FetchedAt:  fetchedAt,
		Payload:    payload,
		HTTPStatus: 200,
	}
}

func normalize(t *testing.T, result domain.FetchResult) (domain.Snapshot, error) {
	t.Helper()
	return NewAdapter().Normalize(result, codexdomain.Panel())
}

// observation returns one metric of a snapshot, failing the test when it is
// not there.
func observation(t *testing.T, snapshot domain.Snapshot, key string) domain.Observation {
	t.Helper()
	found, ok := snapshot.Observation(key)
	if !ok {
		t.Fatalf("the snapshot has no %s; it has %+v", key, snapshot.Observations)
	}
	return found
}

// The usage response becomes the three numbers the panel shows: what is left
// of the window, how long until it resets, and which plan the account is on.
func TestNormalizeMapsTheSessionWindow(t *testing.T) {
	snapshot, err := normalize(t, fixture(t, "usage_ok.json"))
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if snapshot.Status != domain.StatusOK {
		t.Errorf("status = %q, want ok", snapshot.Status)
	}
	if snapshot.ProviderID != codexdomain.ProviderID || snapshot.PanelID != codexdomain.PanelID {
		t.Errorf("snapshot belongs to %s/%s", snapshot.ProviderID, snapshot.PanelID)
	}
	if !snapshot.ObservedAt.Equal(fetchedAt) {
		t.Errorf("ObservedAt = %s, want the moment it was fetched", snapshot.ObservedAt)
	}

	// 6% used leaves 94% of the five-hour window.
	remaining := observation(t, snapshot, codexdomain.MetricSessionRemainingPercent)
	if got, ok := remaining.Value.Number(); !ok || got != 94 {
		t.Errorf("sessionRemainingPercent = %v (%v), want 94", got, remaining.Value)
	}
	// The reset time is absolute in the response and a duration in the
	// snapshot: two hours after the moment it was fetched.
	resetsIn := observation(t, snapshot, codexdomain.MetricSessionResetsInSeconds)
	if got, ok := resetsIn.Value.DurationSeconds(); !ok || got != sessionResetAt-fetchedAt.Unix() {
		t.Errorf("sessionResetsInSeconds = %v (%v), want %d", got, resetsIn.Value, sessionResetAt-fetchedAt.Unix())
	}
	if got, ok := observation(t, snapshot, codexdomain.MetricPlanName).Value.Text(); !ok || got != "plus" {
		t.Errorf("planName = %q (%v), want plus", got, observation(t, snapshot, codexdomain.MetricPlanName).Value)
	}
	// The metrics come out in the panel's own order, so the bar's number is
	// first.
	if len(snapshot.Observations) != len(codexdomain.Metrics()) {
		t.Fatalf("observations = %d, want the panel's %d", len(snapshot.Observations), len(codexdomain.Metrics()))
	}
	for index, metric := range codexdomain.Metrics() {
		if got := snapshot.Observations[index].MetricKey; got != metric.Key {
			t.Errorf("observation %d is %s, want %s", index, got, metric.Key)
		}
	}
}

// A window that has already reset counts as no time left to wait rather than
// as a negative duration.
func TestNormalizeCountsAPastResetAsZero(t *testing.T) {
	snapshot, err := normalize(t, fixture(t, "usage_window_past.json"))
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if got, _ := observation(t, snapshot, codexdomain.MetricSessionResetsInSeconds).Value.DurationSeconds(); got != 0 {
		t.Errorf("sessionResetsInSeconds = %d, want 0", got)
	}
	if got, _ := observation(t, snapshot, codexdomain.MetricSessionRemainingPercent).Value.Number(); got != 94 {
		t.Errorf("sessionRemainingPercent = %d, want the window still read", got)
	}
}

// A window that is over its limit reports nothing left rather than a negative
// percentage, and one that reports less than nothing used is read as full.
func TestNormalizeClampsThePercentage(t *testing.T) {
	cases := map[string]int64{"usage_over_limit.json": 0, "usage_negative.json": 100}
	for name, want := range cases {
		snapshot, err := normalize(t, fixture(t, name))
		if err != nil {
			t.Fatalf("%s: Normalize failed: %v", name, err)
		}
		if got, _ := observation(t, snapshot, codexdomain.MetricSessionRemainingPercent).Value.Number(); got != want {
			t.Errorf("%s: sessionRemainingPercent = %d, want %d", name, got, want)
		}
	}
}

// A response that is not the shape this adapter knows is refused: a renamed
// field must not become "0% left", which is what a bar would draw.
func TestNormalizeRefusesMalformedResponses(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixturesDir, "usage_malformed_*.json"))
	if err != nil {
		t.Fatalf("glob the fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("there is no malformed fixture to check")
	}
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			snapshot, err := normalize(t, fixture(t, name))
			if err == nil {
				t.Fatalf("Normalize accepted %s and produced %+v", name, snapshot.Observations)
			}
			if !errors.Is(err, domain.ErrProviderBadResponse) {
				t.Errorf("error = %v, want ErrProviderBadResponse", err)
			}
			if len(snapshot.Observations) != 0 {
				t.Errorf("a refused response still produced %+v", snapshot.Observations)
			}
		})
	}
}

// The refusal says what was asked and keeps a trimmed snippet: a renamed
// field is meant to be diagnosable from the error alone.
func TestBadResponseCarriesWhatWasAsked(t *testing.T) {
	_, err := normalize(t, fixture(t, "usage_malformed_no_rate_limit.json"))
	if err == nil {
		t.Fatal("the malformed fixture was accepted")
	}
	var bad *domain.BadResponseError
	if !errors.As(err, &bad) {
		t.Fatalf("error = %v, want a BadResponseError", err)
	}
	if bad.Endpoint == "" || bad.HTTPStatus != 200 {
		t.Errorf("the error does not say what was asked: %+v", bad)
	}
	if bad.ProviderID != codexdomain.ProviderID {
		t.Errorf("ProviderID = %q, want %q", bad.ProviderID, codexdomain.ProviderID)
	}
	if len(bad.Snippet) > domain.MaxSnippetBytes {
		t.Errorf("the snippet is %d bytes, want at most %d", len(bad.Snippet), domain.MaxSnippetBytes)
	}
}

// A plan the response does not name is shown as no plan rather than as a
// failure: the bar and the reset time are still worth having.
func TestNormalizeWithoutAPlan(t *testing.T) {
	snapshot, err := normalize(t, fixture(t, "usage_no_plan.json"))
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if got, ok := observation(t, snapshot, codexdomain.MetricPlanName).Value.Text(); !ok || got != "" {
		t.Errorf("planName = %q (%v), want an empty text", got, observation(t, snapshot, codexdomain.MetricPlanName).Value)
	}
	if got, _ := observation(t, snapshot, codexdomain.MetricSessionRemainingPercent).Value.Number(); got != 94 {
		t.Errorf("sessionRemainingPercent = %d, want the window still read", got)
	}
}

// The panel the adapter is given is the one it fills: a panel that lists a
// metric this provider does not report is caught rather than guessed at.
func TestNormalizeRefusesAMetricItDoesNotReport(t *testing.T) {
	panel := codexdomain.Panel()
	panel.MetricKeys = append(panel.MetricKeys, "balanceMinor")
	if _, err := NewAdapter().Normalize(fixture(t, "usage_ok.json"), panel); err == nil {
		t.Fatal("a metric the provider does not report was accepted")
	}
}

func TestProviderID(t *testing.T) {
	if got := NewAdapter().ProviderID(); got != codexdomain.ProviderID {
		t.Errorf("ProviderID() = %q, want %q", got, codexdomain.ProviderID)
	}
}
