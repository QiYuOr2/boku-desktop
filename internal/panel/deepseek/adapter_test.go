package deepseek

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
)

// 样本放在本模块内（testdata/providers），以便单独构建测试。它与仓库级
// fixtures/ 互为副本，两端共享：同样的原始响应必须映射成同样的快照，
// 改动时两处都要改。
const fixturesDir = "../../../testdata/providers/deepseek"

var fetchedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func result(payload []byte, status int) domain.FetchResult {
	return domain.FetchResult{
		ProviderID: contract.ProviderID,
		Endpoint:   "https://api.deepseek.com/user/balance",
		FetchedAt:  fetchedAt,
		Payload:    payload,
		HTTPStatus: status,
	}
}

func number(t *testing.T, snapshot domain.Snapshot, key string) int64 {
	t.Helper()
	observation, ok := snapshot.Observation(key)
	if !ok {
		t.Fatalf("the snapshot has no observation of %s: %+v", key, snapshot.Observations)
	}
	value, ok := observation.Value.Number()
	if !ok {
		t.Fatalf("observation %s is not a number: %+v", key, observation.Value)
	}
	return value
}

func TestNormalizeHappyPath(t *testing.T) {
	adapter := NewAdapter()
	snapshot, err := adapter.Normalize(result(readFixture(t, "balance_ok.json"), 200), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if snapshot.Status != domain.StatusOK {
		t.Errorf("status = %q, want ok (message %q)", snapshot.Status, snapshot.Message)
	}
	if snapshot.Currency != "CNY" {
		t.Errorf("currency = %q, want CNY", snapshot.Currency)
	}
	if snapshot.PanelID != PanelID {
		t.Errorf("panel id = %q", snapshot.PanelID)
	}
	if !snapshot.ObservedAt.Equal(fetchedAt) {
		t.Errorf("observedAt = %s, want %s", snapshot.ObservedAt, fetchedAt)
	}
	if snapshot.Message != "" {
		t.Errorf("message = %q, want none", snapshot.Message)
	}
	want := map[string]int64{
		MetricBalanceMinor:  4210,
		MetricGrantedMinor:  1000,
		MetricToppedUpMinor: 3210,
	}
	for key, value := range want {
		if got := number(t, snapshot, key); got != value {
			t.Errorf("%s = %d, want %d", key, got, value)
		}
	}
	// 观测项按面板顺序排列，主指标在最前。
	if got := snapshot.Observations[0].MetricKey; got != MetricBalanceMinor {
		t.Errorf("the first observation is %s, want %s", got, MetricBalanceMinor)
	}
}

// 服务标记为不可用的账户是一种有效答复而非失败：余额确实为零，卡片上
// 说明账户不可用。
func TestNormalizeUnavailableAccountIsPartialNotAFailure(t *testing.T) {
	adapter := NewAdapter()
	snapshot, err := adapter.Normalize(result(readFixture(t, "balance_unavailable.json"), 200), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if snapshot.Status != domain.StatusPartial {
		t.Errorf("status = %q, want partial", snapshot.Status)
	}
	if !strings.Contains(snapshot.Message, "不可用") {
		t.Errorf("message = %q, want an explanation", snapshot.Message)
	}
	if got := number(t, snapshot, MetricBalanceMinor); got != 0 {
		t.Errorf("balance = %d, want 0", got)
	}
}

func TestNormalizeRejectsAnEmptyBalanceList(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Normalize(result(readFixture(t, "balance_empty.json"), 200), Panel())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "no balance") {
		t.Errorf("the error does not explain itself: %v", err)
	}
	assertErrorIsTraceable(t, err)
}

func TestNormalizeRejectsAMalformedAmount(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Normalize(result(readFixture(t, "balance_malformed.json"), 200), Panel())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "total_balance") {
		t.Errorf("the error does not name the field: %v", err)
	}
	assertErrorIsTraceable(t, err)
}

func TestNormalizePrefersCNYAndSaysWhatItIgnored(t *testing.T) {
	adapter := NewAdapter()
	snapshot, err := adapter.Normalize(result(readFixture(t, "balance_multi_currency.json"), 200), Panel())
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if snapshot.Currency != "CNY" {
		t.Errorf("currency = %q, want CNY", snapshot.Currency)
	}
	if got := number(t, snapshot, MetricBalanceMinor); got != 1230 {
		t.Errorf("balance = %d, want 1230", got)
	}
	if !strings.Contains(snapshot.Message, "USD") {
		t.Errorf("message = %q, want it to mention the ignored USD balance", snapshot.Message)
	}
}

// 错误体不是余额：即使服务以 200 返回它，卡片也不能显示 0。
func TestNormalizeRejectsAnErrorBody(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Normalize(result(readFixture(t, "error_unauthorized.json"), 401), Panel())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	assertErrorIsTraceable(t, err)
}

func TestNormalizeIgnoresUnknownFields(t *testing.T) {
	adapter := NewAdapter()
	payload := []byte(`{
		"is_available": true,
		"balance_infos": [{
			"currency": "CNY",
			"total_balance": "42.10",
			"granted_balance": "10.00",
			"topped_up_balance": "32.10",
			"voucher_balance": "1.00"
		}],
		"request_id": "abc",
		"server_version": "2"
	}`)
	snapshot, err := adapter.Normalize(result(payload, 200), Panel())
	if err != nil {
		t.Fatalf("a response with new fields was rejected: %v", err)
	}
	if got := number(t, snapshot, MetricBalanceMinor); got != 4210 {
		t.Errorf("balance = %d, want 4210", got)
	}
}

func TestNormalizeRequiresIsAvailable(t *testing.T) {
	adapter := NewAdapter()
	payload := []byte(`{"balance_infos":[{"currency":"CNY","total_balance":"1.00","granted_balance":"0.00","topped_up_balance":"1.00"}]}`)
	_, err := adapter.Normalize(result(payload, 200), Panel())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "available") {
		t.Errorf("the error does not explain itself: %v", err)
	}
}

func TestNormalizeRejectsAnUnknownMetricInThePanel(t *testing.T) {
	adapter := NewAdapter()
	panel := Panel()
	panel.MetricKeys = append(panel.MetricKeys, "voucherMinor")
	_, err := adapter.Normalize(result(readFixture(t, "balance_ok.json"), 200), panel)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "voucherMinor") {
		t.Errorf("the error does not name the metric: %v", err)
	}
}

func TestNormalizeWithoutAPanelReportsEveryMetric(t *testing.T) {
	adapter := NewAdapter()
	snapshot, err := adapter.Normalize(result(readFixture(t, "balance_ok.json"), 200), domain.Panel{})
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	if len(snapshot.Observations) != len(Metrics()) {
		t.Errorf("the snapshot has %d observations, want %d", len(snapshot.Observations), len(Metrics()))
	}
}

// 坏响应必须可诊断：哪个厂商、哪个地址、什么状态码，以及裁剪后的响应片段。
func assertErrorIsTraceable(t *testing.T, err error) {
	t.Helper()
	var bad *domain.BadResponseError
	if !errors.As(err, &bad) {
		t.Fatalf("error %v is not a BadResponseError", err)
	}
	if bad.ProviderID != contract.ProviderID {
		t.Errorf("provider = %q", bad.ProviderID)
	}
	if bad.Endpoint == "" {
		t.Error("the error does not name the endpoint")
	}
	if bad.Snippet == "" {
		t.Error("the error carries no response snippet")
	}
	if len(bad.Snippet) > domain.MaxSnippetBytes {
		t.Errorf("the snippet is %d bytes", len(bad.Snippet))
	}
}
