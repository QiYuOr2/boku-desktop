package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/domain"
)

// The vectors live inside this module (testdata/providers) so it can be built
// and tested on its own. They mirror the repository-level fixtures/ directory,
// which both ends share: both ends must read the same response the service
// sends. Change both copies.
const fixturesDir = "../../../testdata/providers/codex"

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
		ProviderID: ProviderID,
		Endpoint:   "https://chatgpt.com/backend-api/wham/usage",
		FetchedAt:  fetchedAt,
		Payload:    payload,
		HTTPStatus: 200,
	}
}

func normalize(t *testing.T, result domain.FetchResult) (domain.Snapshot, error) {
	t.Helper()
	return NewAdapter().Normalize(result, Panel())
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
	if snapshot.ProviderID != ProviderID || snapshot.PanelID != PanelID {
		t.Errorf("snapshot belongs to %s/%s", snapshot.ProviderID, snapshot.PanelID)
	}
	if !snapshot.ObservedAt.Equal(fetchedAt) {
		t.Errorf("ObservedAt = %s, want the moment it was fetched", snapshot.ObservedAt)
	}

	// 6% used leaves 94% of the five-hour window.
	remaining := observation(t, snapshot, MetricSessionRemainingPercent)
	if got, ok := remaining.Value.Number(); !ok || got != 94 {
		t.Errorf("sessionRemainingPercent = %v (%v), want 94", got, remaining.Value)
	}
	// The reset time is absolute in the response and a duration in the
	// snapshot: two hours after the moment it was fetched.
	resetsIn := observation(t, snapshot, MetricSessionResetsInSeconds)
	if got, ok := resetsIn.Value.DurationSeconds(); !ok || got != sessionResetAt-fetchedAt.Unix() {
		t.Errorf("sessionResetsInSeconds = %v (%v), want %d", got, resetsIn.Value, sessionResetAt-fetchedAt.Unix())
	}
	if got, ok := observation(t, snapshot, MetricPlanName).Value.Text(); !ok || got != "plus" {
		t.Errorf("planName = %q (%v), want plus", got, observation(t, snapshot, MetricPlanName).Value)
	}
	if got, _ := observation(t, snapshot, MetricLongWindowRemainingPercent).Value.Number(); got != 76 {
		t.Errorf("longWindowRemainingPercent = %d, want 76", got)
	}
	if got, _ := observation(t, snapshot, MetricLongWindowResetsInSeconds).Value.DurationSeconds(); got != 1792400000-fetchedAt.Unix() {
		t.Errorf("longWindowResetsInSeconds = %d, want %d", got, 1792400000-fetchedAt.Unix())
	}
	// The metrics come out in the panel's own order, so the bar's number is
	// first.
	if len(snapshot.Observations) != len(Metrics()) {
		t.Fatalf("observations = %d, want the panel's %d", len(snapshot.Observations), len(Metrics()))
	}
	for index, metric := range Metrics() {
		if got := snapshot.Observations[index].MetricKey; got != metric.Key {
			t.Errorf("observation %d is %s, want %s", index, got, metric.Key)
		}
	}
}

func TestNormalizeOptionalLongWindow(t *testing.T) {
	for _, name := range []string{"usage_free.json", "usage_no_secondary.json", "usage_no_plan.json"} {
		t.Run(name, func(t *testing.T) {
			snapshot, err := normalize(t, fixture(t, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{MetricLongWindowRemainingPercent, MetricLongWindowResetsInSeconds} {
				if _, ok := snapshot.Observation(key); ok {
					t.Errorf("unexpected observation %s", key)
				}
			}
			if _, ok := snapshot.Observation(MetricSessionRemainingPercent); !ok {
				t.Fatal("primary quota was lost")
			}
		})
	}
}

func TestNormalizeWindowLengths(t *testing.T) {
	for _, tc := range []struct {
		file          string
		session, long int64
	}{
		{"usage_ok.json", 18000, 604800},
		{"usage_free.json", 2592000, 0},
		{"usage_window_lengths.json", 7200, 1209600},
		{"usage_no_secondary.json", 18000, 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			snapshot, err := normalize(t, fixture(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]int64{MetricSessionWindowSeconds: tc.session, MetricLongWindowSeconds: tc.long} {
				got, exists := snapshot.Observation(key)
				if want == 0 {
					if exists {
						t.Errorf("unexpected %s", key)
					}
					continue
				}
				if seconds, ok := got.Value.DurationSeconds(); !exists || !ok || seconds != want {
					t.Errorf("%s = %+v, want %d seconds", key, got, want)
				}
			}
		})
	}
}

func TestNormalizeOmitsUnavailableWindowLength(t *testing.T) {
	zero, negative := int64(0), int64(-1)
	for _, length := range []*int64{nil, &zero, &negative} {
		result := fixture(t, "usage_ok.json")
		var response usageResponse
		if err := json.Unmarshal(result.Payload, &response); err != nil {
			t.Fatal(err)
		}
		response.RateLimit.PrimaryWindow.LimitWindowSeconds = length
		response.RateLimit.SecondaryWindow.LimitWindowSeconds = length
		var err error
		result.Payload, err = json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := normalize(t, result)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{MetricSessionWindowSeconds, MetricLongWindowSeconds} {
			if _, exists := snapshot.Observation(key); exists {
				t.Errorf("unexpected %s", key)
			}
		}
		if len(snapshot.Observations) != 5 {
			t.Fatalf("quotas lost when period unavailable: %+v", snapshot.Observations)
		}
	}
}

func TestNormalizeLongWindowBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, plan          string
		used                int
		reset               int64
		wantLeft, wantReset int64
		wantWindow          bool
	}{
		{"zero used", "pro", 0, 1792400000, 100, 1792400000 - fetchedAt.Unix(), true},
		{"over limit", "business", 120, 1792400000, 0, 1792400000 - fetchedAt.Unix(), true},
		{"negative used and past reset", "enterprise", -5, fetchedAt.Unix() - 1, 100, 0, true},
		{"unknown paid tier", "future-plan", 24, 1792400000, 76, 1792400000 - fetchedAt.Unix(), true},
		{"free with secondary", " FrEe ", 24, 1792400000, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := fixture(t, "usage_ok.json")
			var response usageResponse
			if err := json.Unmarshal(result.Payload, &response); err != nil {
				t.Fatal(err)
			}
			response.PlanType = tc.plan
			response.RateLimit.SecondaryWindow.UsedPercent = &tc.used
			response.RateLimit.SecondaryWindow.ResetAt = &tc.reset
			var err error
			result.Payload, err = json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := normalize(t, result)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := snapshot.Observation(MetricLongWindowRemainingPercent); ok != tc.wantWindow {
				t.Fatalf("long window present = %v, want %v", ok, tc.wantWindow)
			}
			if !tc.wantWindow {
				return
			}
			if got, _ := observation(t, snapshot, MetricLongWindowRemainingPercent).Value.Number(); got != tc.wantLeft {
				t.Errorf("remaining = %d, want %d", got, tc.wantLeft)
			}
			if got, _ := observation(t, snapshot, MetricLongWindowResetsInSeconds).Value.DurationSeconds(); got != tc.wantReset {
				t.Errorf("resets in = %d, want %d", got, tc.wantReset)
			}
		})
	}
}

// A window that has already reset counts as no time left to wait rather than
// as a negative duration.
func TestNormalizeCountsAPastResetAsZero(t *testing.T) {
	snapshot, err := normalize(t, fixture(t, "usage_window_past.json"))
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if got, _ := observation(t, snapshot, MetricSessionResetsInSeconds).Value.DurationSeconds(); got != 0 {
		t.Errorf("sessionResetsInSeconds = %d, want 0", got)
	}
	if got, _ := observation(t, snapshot, MetricSessionRemainingPercent).Value.Number(); got != 94 {
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
		if got, _ := observation(t, snapshot, MetricSessionRemainingPercent).Value.Number(); got != want {
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
	if bad.ProviderID != ProviderID {
		t.Errorf("ProviderID = %q, want %q", bad.ProviderID, ProviderID)
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
	if got, ok := observation(t, snapshot, MetricPlanName).Value.Text(); !ok || got != "" {
		t.Errorf("planName = %q (%v), want an empty text", got, observation(t, snapshot, MetricPlanName).Value)
	}
	if got, _ := observation(t, snapshot, MetricSessionRemainingPercent).Value.Number(); got != 94 {
		t.Errorf("sessionRemainingPercent = %d, want the window still read", got)
	}
}

// The panel the adapter is given is the one it fills: a panel that lists a
// metric this provider does not report is caught rather than guessed at.
func TestNormalizeRefusesAMetricItDoesNotReport(t *testing.T) {
	panel := Panel()
	panel.MetricKeys = append(panel.MetricKeys, "balanceMinor")
	if _, err := NewAdapter().Normalize(fixture(t, "usage_ok.json"), panel); err == nil {
		t.Fatal("a metric the provider does not report was accepted")
	}
}

func TestProviderID(t *testing.T) {
	if got := NewAdapter().ProviderID(); got != ProviderID {
		t.Errorf("ProviderID() = %q, want %q", got, ProviderID)
	}
}
