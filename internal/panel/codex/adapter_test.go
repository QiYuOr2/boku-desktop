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

// 这些向量位于本模块内（testdata/providers），使本模块可独立构建与测试。它们镜像
// 仓库级的 fixtures/ 目录，两端共享同一份，因而必须读到服务发出的同一份响应。两处
// 副本都要改。
const fixturesDir = "../../../testdata/providers/codex"

// fetchedAt 是样本被视为的取数时刻，sessionResetAt 是 usage_ok.json 携带的重置时间：
// adapter 由前者推算出后者，因此两者都写在这里。
var fetchedAt = time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)

const sessionResetAt = 1791943200 // 2026-10-14T04:00:00Z，两小时之后

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

// observation 返回快照中的一个指标，缺失时使测试失败。
func observation(t *testing.T, snapshot domain.Snapshot, key string) domain.Observation {
	t.Helper()
	found, ok := snapshot.Observation(key)
	if !ok {
		t.Fatalf("the snapshot has no %s; it has %+v", key, snapshot.Observations)
	}
	return found
}

// 用量响应变成面板展示的三个数字：窗口剩余多少、多久后重置、账户属于哪个套餐。
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

	// 已用 6%，五小时窗口还剩 94%。
	remaining := observation(t, snapshot, MetricSessionRemainingPercent)
	if got, ok := remaining.Value.Number(); !ok || got != 94 {
		t.Errorf("sessionRemainingPercent = %v (%v), want 94", got, remaining.Value)
	}
	// 响应里的重置时间绝对值，快照里是时长：取数时刻之后两小时。
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
	// 指标按面板自身的顺序输出，因此进度条的数字排在最前。
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

// 已重置的窗口按「无需再等」计，而不是负时长。
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

// 超出上限的窗口报告剩余为零而不是负百分比；已用量少于零的按满额读。
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

// 不是本 adapter 已知结构的响应会被拒绝：字段被改名时不能变成「剩余 0%」，那正是
// 进度条会画出的样子。
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

// 拒绝信息说明所请求的内容并保留裁剪过的片段：字段被改名应当仅凭错误即可诊断。
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

// 响应未指明套餐时显示为无套餐，而不是失败：进度条与重置时间仍值得保留。
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

// 传给 adapter 的面板就是它要填充的面板：面板列出 provider 不上报的指标会被捕获，
// 而不是猜测。
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
