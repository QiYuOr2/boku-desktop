package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/domain/deepseek"
	"boku/internal/usecase"
)

// The presentation layer is tested without a window and without a network:
// a provider the test decides, an adapter that answers from it, and a
// credential store in memory. Nothing here imports internal/data, which is
// what keeps the layers honest.

// testStore is a domain.SecretStore in memory.
type testStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newTestStore() *testStore { return &testStore{values: map[string]string{}} }

func (s *testStore) set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

func (s *testStore) Get(_ context.Context, name string) (domain.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[name]
	if !ok {
		return domain.Secret{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return domain.Secret{Name: name, Value: []byte(value)}, nil
}

func (s *testStore) Unlock(context.Context, string) error { return nil }

func (s *testStore) Lock() {}

func (s *testStore) Unlocked() bool { return true }

func (s *testStore) Put(_ context.Context, name string, value []byte, _ domain.SecretMeta) error {
	s.set(name, string(value))
	return nil
}

func (s *testStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[name]; !ok {
		return fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	delete(s.values, name)
	return nil
}

func (s *testStore) List(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.values))
	for name := range s.values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *testStore) Meta(_ context.Context, name string) (domain.SecretMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[name]; !ok {
		return domain.SecretMeta{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return domain.SecretMeta{}, nil
}

// testProvider is a provider whose answer the test decides.
type testProvider struct {
	id         string
	descriptor domain.ProviderDescriptor

	mu       sync.Mutex
	calls    int
	err      error
	status   domain.SnapshotStatus
	currency string
	message  string
	values   map[string]int64
	gate     chan struct{}
}

func newTestProvider() *testProvider {
	return &testProvider{
		id: "deepseek",
		descriptor: domain.ProviderDescriptor{
			ID:                "deepseek",
			DisplayName:       "DeepSeek",
			CredentialKeys:    []string{deepseek.APIKeyCredential},
			DocsURL:           "https://platform.deepseek.com/api_keys",
			Endpoint:          "https://api.deepseek.com/user/balance",
			MinRefreshSeconds: 60,
		},
		status:   domain.StatusOK,
		currency: "CNY",
		values: map[string]int64{
			deepseek.MetricBalanceMinor:  4210,
			deepseek.MetricGrantedMinor:  1000,
			deepseek.MetricToppedUpMinor: 3210,
		},
	}
}

func (p *testProvider) ID() string { return p.id }

func (p *testProvider) Descriptor() domain.ProviderDescriptor { return p.descriptor }

func (p *testProvider) Fetch(ctx context.Context, creds domain.Credentials) (domain.FetchResult, error) {
	p.mu.Lock()
	p.calls++
	gate := p.gate
	err := p.err
	p.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return domain.FetchResult{}, ctx.Err()
		}
	}
	if err != nil {
		return domain.FetchResult{}, err
	}
	return domain.FetchResult{
		ProviderID: p.id,
		Endpoint:   p.descriptor.Endpoint,
		FetchedAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Payload:    []byte(`{}`),
		HTTPStatus: 200,
	}, nil
}

func (p *testProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *testProvider) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *testProvider) hold() chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gate = make(chan struct{})
	return p.gate
}

func (p *testProvider) setStatus(status domain.SnapshotStatus, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status, p.message = status, message
}

// testSnapshot is the answer the adapter builds from the provider.
type testAdapter struct{ provider *testProvider }

func (a testAdapter) ProviderID() string { return a.provider.id }

func (a testAdapter) Normalize(result domain.FetchResult, panel domain.Panel) (domain.Snapshot, error) {
	a.provider.mu.Lock()
	values := make(map[string]int64, len(a.provider.values))
	for key, value := range a.provider.values {
		values[key] = value
	}
	currency, status, message := a.provider.currency, a.provider.status, a.provider.message
	a.provider.mu.Unlock()

	observations := make([]domain.Observation, 0, len(panel.MetricKeys))
	for _, key := range panel.MetricKeys {
		value, ok := values[key]
		if !ok {
			return domain.Snapshot{}, fmt.Errorf("%w: no value for %s", domain.ErrProviderBadResponse, key)
		}
		observations = append(observations, domain.Observation{
			MetricKey:  key,
			Value:      domain.NumberValue(value),
			ObservedAt: result.FetchedAt,
		})
	}
	return domain.Snapshot{
		PanelID:      panel.ID,
		ProviderID:   a.ProviderID(),
		ObservedAt:   result.FetchedAt,
		Status:       status,
		Observations: observations,
		Message:      message,
		Currency:     currency,
	}, nil
}

// testRegistry is a domain.ProviderRegistry with one provider.
type testRegistry struct {
	provider domain.Provider
	adapter  domain.Adapter
}

func (r testRegistry) Provider(id string) (domain.Provider, error) {
	if id != r.provider.ID() {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return r.provider, nil
}

func (r testRegistry) Adapter(id string) (domain.Adapter, error) {
	if id != r.provider.ID() {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return r.adapter, nil
}

// testState builds the presentation state over the fakes. version is what
// the footer shows; the credential store starts empty unless it is given.
func testState(t *testing.T, provider *testProvider, store *testStore, version string) *State {
	t.Helper()
	if store == nil {
		store = newTestStore()
	}
	panels, err := usecase.NewPanels(
		testRegistry{provider: provider, adapter: testAdapter{provider: provider}},
		store,
		[]usecase.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	secrets, err := usecase.NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	state, err := New(mygo.App, Deps{
		Panels:  panels,
		Secrets: secrets,
		Version: version,
		Quit:    func() {},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return state
}

// withCredential adds the DeepSeek key the panel needs.
func withCredential(store *testStore) *testStore {
	store.set(domain.CredentialKey("deepseek", deepseek.APIKeyCredential), "sk-test-0001")
	return store
}

// emptyPanelService is a service with no panels defined.
func emptyPanelService(t *testing.T) *usecase.Panels {
	t.Helper()
	provider := newTestProvider()
	panels, err := usecase.NewPanels(
		testRegistry{provider: provider, adapter: testAdapter{provider: provider}},
		newTestStore(),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	return panels
}

// loaded refreshes the panel once, so that the view has a snapshot to draw.
func loaded(t *testing.T, state *State) {
	t.Helper()
	if _, err := state.panelService.Refresh(context.Background(), deepseek.PanelID); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
}

func TestNewRequiresItsDependencies(t *testing.T) {
	secrets, err := usecase.NewSecrets(newTestStore())
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := usecase.NewPanels(
		testRegistry{provider: newTestProvider(), adapter: testAdapter{provider: newTestProvider()}},
		newTestStore(),
		[]usecase.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if _, err := New(mygo.App, Deps{Secrets: secrets}); err == nil {
		t.Error("New accepted a state without a panel service")
	}
	if _, err := New(mygo.App, Deps{Panels: panels}); err == nil {
		t.Error("New accepted a state without a secret service")
	}
	if _, err := New(nil, Deps{Panels: panels, Secrets: secrets}); err == nil {
		t.Error("New accepted a state without an application")
	}
}

// The quick window has no header: what it is about is said by the panel's
// own title, and a window this small does not spend its top on repeating
// the application's name. What it keeps is the panel, and the footer with
// the version and the actions.
func TestQuickWindowHasNoHeaderAndKeepsItsFooter(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	for _, want := range []string{"DeepSeek 余额", "刷新", "退出", "设置", "Boku 0.1.0"} {
		if !tt.HasText(want) {
			t.Errorf("the quick window does not show %q; it shows %q", want, tt.Texts())
		}
	}
	// "Boku" itself is in the footer, as the first half of the version
	// line; what is gone is the header's subtitle.
	if tt.HasText("看见状态，用完就走。") {
		t.Errorf("the quick window still shows the header's subtitle: %q", tt.Texts())
	}
	// The panel comes first: the window is its block, not a header's worth
	// of padding followed by one.
	title, ok := tt.Find("DeepSeek 余额")
	if !ok {
		t.Fatalf("the panel title was not found; the window shows %q", tt.Texts())
	}
	if title.Y > 40 {
		t.Errorf("the panel starts at y=%.0f: the header was not taken away", title.Y)
	}
}

func TestFooterSitsBelowTheContentAndIsSmall(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	content, ok := tt.Find("DeepSeek 余额")
	if !ok {
		t.Fatalf("the content was not found; the window shows %q", tt.Texts())
	}
	quit, ok := tt.Find("退出")
	if !ok {
		t.Fatalf("the quit action was not found; the window shows %q", tt.Texts())
	}
	version, ok := tt.Find("Boku 0.1.0")
	if !ok {
		t.Fatalf("the version was not found; the window shows %q", tt.Texts())
	}
	if quit.Y <= content.Y {
		t.Errorf("the quit action at y=%.0f is not below the content at y=%.0f", quit.Y, content.Y)
	}
	if quit.H > 26 {
		t.Errorf("the quit action is %.0f points tall: the footer should keep it small", quit.H)
	}
	if quit.W > 60 {
		t.Errorf("the quit action is %.0f points wide", quit.W)
	}
	if diff := version.Y - quit.Y; diff > 8 || diff < -8 {
		t.Errorf("the version at y=%.1f and the actions at y=%.1f are not on one footer row", version.Y, quit.Y)
	}
	if version.X >= quit.X {
		t.Errorf("the version at x=%.0f is not left of the actions at x=%.0f", version.X, quit.X)
	}
}

// The tray application has no main window, so the quit entry has to work.
func TestQuitActionQuits(t *testing.T) {
	quit := 0
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.quit = func() { quit++ }
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	if err := tt.Click("退出"); err != nil {
		t.Fatalf("click the quit action: %v", err)
	}
	if quit != 1 {
		t.Errorf("quit called %d times after one click, want 1", quit)
	}
}

// MyGo rebuilds the view for every input and repaint, so a view that
// changed state would fight itself.
func TestViewIsIdempotent(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	first := tt.Texts()
	tt.Frame()
	second := tt.Texts()
	if len(first) != len(second) {
		t.Fatalf("redrawing changed the number of texts: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("text %d changed on redraw: %q then %q", i, first[i], second[i])
		}
	}
	// Drawing must not fetch anything: the view is built several times per
	// frame.
	if view := state.panelService.View(deepseek.PanelID); view.Loading {
		t.Error("drawing the view started a refresh")
	}
}

func TestViewInNarrowWindow(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, 160, panelHeight)
	if !tt.HasText("退出") {
		t.Errorf("the quick window does not show the footer when narrow; it shows %q", tt.Texts())
	}
}

// Without a version the footer still has to say something.
func TestVersionLineFallsBack(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "")
	if got := state.versionLine(); got != "Boku dev" {
		t.Errorf("versionLine = %q, want Boku dev", got)
	}
	state.version = "1.2.3"
	if got := state.versionLine(); got != "Boku 1.2.3" {
		t.Errorf("versionLine = %q, want Boku 1.2.3", got)
	}
}

// refreshAsync is what the interface calls; it must never block the frame,
// and it must ask only when there is something to ask for.
func TestRefreshAsyncDoesNotBlockAndOnlyAsksWhenNeeded(t *testing.T) {
	provider := newTestProvider()
	gate := provider.hold()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")

	start := time.Now()
	state.refreshAsync(false)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("refreshAsync blocked for %s", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for provider.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if provider.count() != 1 {
		t.Fatalf("the provider was called %d times, want 1", provider.count())
	}
	close(gate)

	// Wait for the fetch to settle, then a second call must not ask again.
	deadline = time.Now().Add(2 * time.Second)
	for state.panelService.View(deepseek.PanelID).Loading && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state.refreshAsync(false)
	time.Sleep(20 * time.Millisecond)
	if got := provider.count(); got != 1 {
		t.Errorf("a fresh panel was fetched again: %d calls, want 1", got)
	}
}

func TestCredentialLabel(t *testing.T) {
	cases := map[string]string{
		"apiKey":   "API Key",
		"adminKey": "Admin Key",
		"steamId":  "Steam ID",
		"":         "凭据",
		"other":    "other",
	}
	for key, want := range cases {
		if got := credentialLabel(key); got != want {
			t.Errorf("credentialLabel(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestFriendlyErrorSpeaksTheUsersLanguage(t *testing.T) {
	cases := []struct {
		err      error
		contains string
	}{
		{fmt.Errorf("%w: deepseek.apiKey", domain.ErrCredentialMissing), "凭据"},
		{fmt.Errorf("%w: 401", domain.ErrProviderUnauthorized), "被拒绝"},
		{fmt.Errorf("%w: 429", domain.ErrProviderRateLimited), "频繁"},
		{fmt.Errorf("%w: timeout", domain.ErrProviderTimeout), "超时"},
		{fmt.Errorf("%w: 500", domain.ErrProviderUnavailable), "不可用"},
		{fmt.Errorf("%w: deepseek", domain.ErrProviderNotFound), "找不到"},
		{&domain.BadResponseError{ProviderID: "deepseek", Reason: "balance_infos is empty"}, "balance_infos is empty"},
	}
	for _, c := range cases {
		message := friendlyError(c.err)
		if !strings.Contains(message, c.contains) {
			t.Errorf("friendlyError(%v) = %q, want it to mention %q", c.err, message, c.contains)
		}
	}
	if friendlyError(nil) != "" {
		t.Error("friendlyError(nil) is not empty")
	}
	if err := errors.New("something else"); friendlyError(err) != err.Error() {
		t.Errorf("friendlyError(%v) = %q", err, friendlyError(err))
	}
}

func TestFormatMetricNeverInventsAZero(t *testing.T) {
	metrics := deepseek.Metrics()
	empty := domain.Snapshot{ProviderID: "deepseek", Status: domain.StatusOK}
	if got := formatMetric(empty, metrics[0]); got != "—" {
		t.Errorf("formatMetric of an empty snapshot = %q, want a dash", got)
	}
	snapshot := domain.Snapshot{
		ProviderID: "deepseek",
		Status:     domain.StatusOK,
		Currency:   "CNY",
		Observations: []domain.Observation{
			{MetricKey: deepseek.MetricBalanceMinor, Value: domain.NumberValue(4210)},
			{MetricKey: deepseek.MetricGrantedMinor, Value: domain.TextValue("plenty")},
		},
	}
	if got := formatMetric(snapshot, metrics[0]); got != "¥42.10" {
		t.Errorf("formatMetric(balance) = %q, want ¥42.10", got)
	}
	if got := formatMetric(snapshot, metrics[1]); got != "—" {
		t.Errorf("formatMetric of a text where a number belongs = %q, want a dash", got)
	}
	noCurrency := snapshot
	noCurrency.Currency = ""
	if got := formatMetric(noCurrency, metrics[0]); got != "—" {
		t.Errorf("formatMetric without a currency = %q, want a dash", got)
	}
}

func TestClock(t *testing.T) {
	if got := clock(time.Time{}); got != "—" {
		t.Errorf("clock of the zero time = %q", got)
	}
	at := time.Date(2026, 1, 2, 15, 4, 5, 0, time.Local)
	if got := clock(at); got != "15:04" {
		t.Errorf("clock = %q, want 15:04", got)
	}
}
