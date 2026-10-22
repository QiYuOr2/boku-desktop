package panel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

type registry struct {
	providers map[string]domain.Provider
	adapters  map[string]domain.Adapter
}

func newRegistry() *registry {
	return &registry{providers: map[string]domain.Provider{}, adapters: map[string]domain.Adapter{}}
}

func (r *registry) add(provider domain.Provider, adapter domain.Adapter) {
	r.providers[provider.ID()] = provider
	r.adapters[provider.ID()] = adapter
}

func (r *registry) Provider(id string) (domain.Provider, error) {
	provider, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return provider, nil
}

func (r *registry) Adapter(id string) (domain.Adapter, error) {
	adapter, ok := r.adapters[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return adapter, nil
}

// fakeProvider is a provider whose answer the test decides.
type fakeProvider struct {
	id         string
	descriptor domain.ProviderDescriptor

	mu     sync.Mutex
	calls  int
	last   domain.Credentials
	result domain.FetchResult
	err    error
	// gate, when set, holds every fetch until the test lets it go, so
	// concurrent callers really do overlap.
	gate chan struct{}
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		id: "fake",
		descriptor: domain.ProviderDescriptor{
			ID:                "fake",
			DisplayName:       "Fake",
			CredentialKeys:    []string{"apiKey"},
			DocsURL:           "https://example.com/keys",
			Endpoint:          "https://example.com/balance",
			MinRefreshSeconds: 60,
		},
		result: domain.FetchResult{
			ProviderID: "fake",
			Endpoint:   "https://example.com/balance",
			FetchedAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			Payload:    []byte(`{}`),
			HTTPStatus: 200,
		},
	}
}

func (f *fakeProvider) ID() string { return f.id }

func (f *fakeProvider) Descriptor() domain.ProviderDescriptor { return f.descriptor }

func (f *fakeProvider) Fetch(ctx context.Context, creds domain.Credentials) (domain.FetchResult, error) {
	f.mu.Lock()
	f.calls++
	f.last = creds
	gate := f.gate
	result, err := f.result, f.err
	f.mu.Unlock()
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
	return result, nil
}

func (f *fakeProvider) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProvider) credentials() domain.Credentials {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func (f *fakeProvider) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeProvider) succeed() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = nil
}

func (f *fakeProvider) hold() chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = make(chan struct{})
	return f.gate
}

// fakeAdapter maps a fetch result to a snapshot of whatever the panel asks
// for, without inventing a vendor shape.
type fakeAdapter struct{ id string }

func (a fakeAdapter) ProviderID() string { return a.id }

func (a fakeAdapter) Normalize(result domain.FetchResult, panel domain.Panel) (domain.Snapshot, error) {
	observations := make([]domain.Observation, 0, len(panel.MetricKeys))
	for _, key := range panel.MetricKeys {
		observations = append(observations, domain.Observation{
			MetricKey:  key,
			Value:      domain.NumberValue(100),
			ObservedAt: result.FetchedAt,
		})
	}
	return domain.Snapshot{
		PanelID:      panel.ID,
		ProviderID:   a.id,
		ObservedAt:   result.FetchedAt,
		Status:       domain.StatusOK,
		Observations: observations,
		Currency:     "CNY",
	}, nil
}

// brokenAdapter answers with whatever it was given on purpose.
type brokenAdapter struct {
	id       string
	snapshot domain.Snapshot
	err      error
}

func (a brokenAdapter) ProviderID() string { return a.id }

func (a brokenAdapter) Normalize(domain.FetchResult, domain.Panel) (domain.Snapshot, error) {
	if a.err != nil {
		return domain.Snapshot{}, a.err
	}
	return a.snapshot, nil
}

func testDef() PanelDef {
	return PanelDef{
		Panel: domain.Panel{
			ID: "panel-1", Kind: domain.PanelKindProvider, Title: "API 余额",
			ProviderID: "fake", MetricKeys: []string{"balanceMinor", "toppedUpMinor"},
			RefreshEvery: 600, SortOrder: 10, Enabled: true,
		},
		Metrics: []domain.Metric{
			{
				Key: "balanceMinor", Label: "总余额",
				Unit: domain.UnitCurrency, ValueType: domain.ValueTypeNumber,
				Precision: 2, Currency: "CNY", Comparison: domain.ComparisonLowerIsBetter,
			},
			{
				Key: "toppedUpMinor", Label: "充值余额",
				Unit: domain.UnitCurrency, ValueType: domain.ValueTypeNumber,
				Precision: 2, Currency: "CNY", Comparison: domain.ComparisonHigherIsBetter,
			},
		},
	}
}

// newTestPanels builds the service over a fake provider; store may be nil,
// in which case one key is stored under "fake.apiKey".
func newTestPanels(t *testing.T, provider *fakeProvider, store domain.SecretStore, defs ...PanelDef) *Panels {
	t.Helper()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: provider.id})
	if store == nil {
		memory := newMemoryStore()
		if err := memory.Put(context.Background(), domain.CredentialKey(provider.id, "apiKey"), []byte("sk-test"), domain.SecretMeta{ProviderID: provider.id}); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		store = memory
	}
	if len(defs) == 0 {
		defs = []PanelDef{testDef()}
	}
	panels, err := NewPanels(reg, store, NewSection(), nil, defs)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	return panels
}

// An optional panel can be turned off. It leaves the list — so nothing draws
// it and nothing fetches it — and the choice is written where the other
// switches live, in the file every part of the application writes.
func TestAnOptionalPanelCanBeTurnedOff(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: "fake"})
	tools := newFakeTools()
	optional := testDef()
	optional.Optional = true
	always := testDef()
	always.Panel.ID = "panel-2"
	always.Panel.Title = "一直显示"
	always.Panel.SortOrder = 20

	// A credential, so that a refresh of a panel that is on has something to
	// fetch with.
	store := newMemoryStore()
	if err := store.Put(context.Background(), domain.CredentialKey("fake", "apiKey"), []byte("sk-test"), domain.SecretMeta{ProviderID: "fake"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	panels, err := NewPanels(reg, store, tools.section, tools, []PanelDef{always, optional})
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if err := panels.LoadChoices(context.Background()); err != nil {
		t.Fatalf("LoadChoices failed: %v", err)
	}

	// Nothing has been chosen: both are listed.
	if listed := panels.List(); len(listed) != 2 {
		t.Fatalf("List returned %d panels, want both before any choice: %+v", len(listed), listed)
	}
	if choices := panels.OptionalPanels(); len(choices) != 1 || !choices[0].On {
		t.Errorf("the switches are %+v, want the optional panel on", choices)
	}

	if err := panels.SetPanelOn(context.Background(), optional.Panel.ID, false); err != nil {
		t.Fatalf("SetPanelOn failed: %v", err)
	}
	listed := panels.List()
	if len(listed) != 1 || listed[0].ID != always.Panel.ID {
		t.Errorf("List returned %+v, want only the panel nobody turned off", listed)
	}
	if panels.PanelOn(optional.Panel.ID) {
		t.Error("the panel reports itself on after being turned off")
	}
	if choices := panels.OptionalPanels(); len(choices) != 1 || choices[0].On {
		t.Errorf("the switches are %+v, want the optional panel off", choices)
	}
	// The choice is in the section, not only in memory.
	if on, chosen := tools.section.Choices()[optional.Panel.ID]; !chosen || on {
		t.Errorf("the stored choices are %v, want the panel off", tools.section.Choices())
	}
	if tools.saves != 1 {
		t.Errorf("the settings were saved %d times, want once", tools.saves)
	}

	// A panel that is off is not asked at all, even by a caller that insists.
	if _, err := panels.Refresh(context.Background(), optional.Panel.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Refresh of a panel that is off = %v, want ErrNotFound", err)
	}
	if panels.NeedsRefresh(optional.Panel.ID) {
		t.Error("a panel that is off says it needs a refresh")
	}
	if provider.count() != 0 {
		t.Errorf("the provider was called %d times, want none while the panel is off", provider.count())
	}

	// And back on: it is listed again, and it is asked.
	if err := panels.SetPanelOn(context.Background(), optional.Panel.ID, true); err != nil {
		t.Fatalf("SetPanelOn failed: %v", err)
	}
	if listed := panels.List(); len(listed) != 2 {
		t.Errorf("List returned %+v, want both panels again", listed)
	}
	if _, err := panels.Refresh(context.Background(), optional.Panel.ID); err != nil {
		t.Fatalf("Refresh failed after the panel was turned back on: %v", err)
	}
	if provider.count() != 1 {
		t.Errorf("the provider was called %d times, want once", provider.count())
	}
}

// A panel that is not optional has no switch, and one that is not defined has
// nothing to switch: both are refused rather than stored.
func TestOnlyOptionalPanelsCanBeTurnedOff(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: "fake"})
	panels, err := NewPanels(reg, newMemoryStore(), NewSection(), newFakeTools(), []PanelDef{testDef()})
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if err := panels.SetPanelOn(context.Background(), "panel-1", false); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("SetPanelOn of a panel that is not optional = %v, want ErrInvalidArgument", err)
	}
	if err := panels.SetPanelOn(context.Background(), "ghost", false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("SetPanelOn of a panel that does not exist = %v, want ErrNotFound", err)
	}
	if choices := panels.OptionalPanels(); len(choices) != 0 {
		t.Errorf("the switches are %+v, want none", choices)
	}
}

// The choices are read from the file at assembly: a panel the user turned off
// is off from the first frame, without anything having to ask.
func TestTheStoredChoicesAreReadAtAssembly(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: "fake"})
	def := testDef()
	def.Optional = true
	tools := newFakeTools()
	tools.section.Set(def.Panel.ID, false)

	panels, err := NewPanels(reg, newMemoryStore(), tools.section, tools, []PanelDef{def})
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	// Before the choices are read, nothing is off: a service that has not
	// read the file lists what it was built with.
	if listed := panels.List(); len(listed) != 1 {
		t.Fatalf("List returned %+v, want the panel before the choices are read", listed)
	}
	if err := panels.LoadChoices(context.Background()); err != nil {
		t.Fatalf("LoadChoices failed: %v", err)
	}
	if listed := panels.List(); len(listed) != 0 {
		t.Errorf("List returned %+v, want nothing: the file says the panel is off", listed)
	}
}

// A store that cannot be written still flips the switch for this run: the
// user's click is not refused because a file is read-only.
func TestASwitchThatCannotBeStoredStillWorks(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: "fake"})
	def := testDef()
	def.Optional = true
	tools := newFakeTools()
	tools.err = errors.New("the settings file is read-only")

	panels, err := NewPanels(reg, newMemoryStore(), tools.section, tools, []PanelDef{def})
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if err := panels.SetPanelOn(context.Background(), def.Panel.ID, false); err == nil {
		t.Error("SetPanelOn reported no error for a store that cannot be written")
	}
	if listed := panels.List(); len(listed) != 0 {
		t.Errorf("List returned %+v, want the panel off for this run anyway", listed)
	}
}

// With no settings store at all the switches still work, for this run.
func TestSwitchesWithoutAStore(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: "fake"})
	def := testDef()
	def.Optional = true
	panels, err := NewPanels(reg, newMemoryStore(), NewSection(), nil, []PanelDef{def})
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if err := panels.LoadChoices(context.Background()); err != nil {
		t.Fatalf("LoadChoices without a store failed: %v", err)
	}
	if err := panels.SetPanelOn(context.Background(), def.Panel.ID, false); err != nil {
		t.Fatalf("SetPanelOn without a store failed: %v", err)
	}
	if listed := panels.List(); len(listed) != 0 {
		t.Errorf("List returned %+v, want the panel off", listed)
	}
}

func TestNewPanelsValidatesDefinitions(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	reg.add(provider, fakeAdapter{id: "fake"})
	store := newMemoryStore()

	cases := []struct {
		name string
		defs []PanelDef
		want error
	}{
		{
			name: "a panel with no provider registered",
			defs: func() []PanelDef {
				def := testDef()
				def.Panel.ProviderID = "ghost"
				return []PanelDef{def}
			}(),
			want: domain.ErrProviderNotFound,
		},
		{
			name: "a metric the panel lists but does not define",
			defs: func() []PanelDef {
				def := testDef()
				def.Panel.MetricKeys = append(def.Panel.MetricKeys, "ghostMinor")
				return []PanelDef{def}
			}(),
			want: domain.ErrInvalidArgument,
		},
		{
			name: "a metric the panel defines but does not list",
			defs: func() []PanelDef {
				def := testDef()
				def.Metrics = def.Metrics[:1]
				return []PanelDef{def}
			}(),
			want: domain.ErrInvalidArgument,
		},
		{
			name: "a refresh interval the provider does not tolerate",
			defs: func() []PanelDef {
				def := testDef()
				def.Panel.RefreshEvery = 30
				return []PanelDef{def}
			}(),
			want: domain.ErrInvalidArgument,
		},
		{
			name: "an invalid panel",
			defs: func() []PanelDef {
				def := testDef()
				def.Panel.Title = ""
				return []PanelDef{def}
			}(),
			want: domain.ErrInvalidArgument,
		},
		{
			name: "an invalid metric",
			defs: func() []PanelDef {
				def := testDef()
				def.Metrics[0].Currency = ""
				return []PanelDef{def}
			}(),
			want: domain.ErrInvalidArgument,
		},
		{
			name: "the same panel twice",
			defs: []PanelDef{testDef(), testDef()},
			want: domain.ErrAlreadyExists,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewPanels(reg, store, NewSection(), nil, c.defs); !errors.Is(err, c.want) {
				t.Fatalf("error = %v, want %v", err, c.want)
			}
		})
	}

	if _, err := NewPanels(nil, store, NewSection(), nil, nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("a nil registry: error = %v", err)
	}
	if _, err := NewPanels(reg, nil, NewSection(), nil, nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("a nil store: error = %v", err)
	}
}

func TestMissingCredentialStopsBeforeTheNetwork(t *testing.T) {
	provider := newFakeProvider()
	panels := newTestPanels(t, provider, newMemoryStore())

	_, err := panels.Refresh(context.Background(), "panel-1")
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if provider.count() != 0 {
		t.Errorf("the provider was called %d times without a credential", provider.count())
	}
	view := panels.View("panel-1")
	if !view.NeedsCredentials() {
		t.Error("the view does not report missing credentials")
	}
	if view.HasSnapshot {
		t.Error("the view claims a snapshot it never fetched")
	}
	if view.Snapshot.Status == domain.StatusOK {
		t.Error("the view shows a snapshot that does not exist")
	}
}

func TestRefreshStoresTheSnapshot(t *testing.T) {
	provider := newFakeProvider()
	panels := newTestPanels(t, provider, nil)

	snapshot, err := panels.Refresh(context.Background(), "panel-1")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if snapshot.Status != domain.StatusOK {
		t.Errorf("status = %q", snapshot.Status)
	}
	if provider.count() != 1 {
		t.Errorf("the provider was called %d times, want 1", provider.count())
	}
	if got := provider.credentials().Secrets["apiKey"]; got != "sk-test" {
		t.Errorf("the provider saw key %q, want the stored one", got)
	}

	view := panels.View("panel-1")
	if !view.HasSnapshot || view.Stale || view.Loading || view.Err != nil {
		t.Errorf("view = %+v, want a fresh snapshot and no error", view)
	}
	if view.FetchedAt.IsZero() {
		t.Error("the view does not say when the snapshot was fetched")
	}
	if len(view.Metrics) != 2 {
		t.Errorf("the view carries %d metrics, want 2", len(view.Metrics))
	}
}

func TestAFailureKeepsTheLastGoodSnapshot(t *testing.T) {
	provider := newFakeProvider()
	panels := newTestPanels(t, provider, nil)
	ctx := context.Background()

	if _, err := panels.Refresh(ctx, "panel-1"); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	provider.fail(fmt.Errorf("%w: fake answered 401", domain.ErrProviderUnauthorized))

	snapshot, err := panels.Refresh(ctx, "panel-1")
	if !errors.Is(err, domain.ErrProviderUnauthorized) {
		t.Fatalf("error = %v, want ErrProviderUnauthorized", err)
	}
	if len(snapshot.Observations) == 0 {
		t.Error("the failed refresh did not return the previous snapshot")
	}

	view := panels.View("panel-1")
	if !view.HasSnapshot {
		t.Error("the failure cleared the cached snapshot")
	}
	if !errors.Is(view.Err, domain.ErrProviderUnauthorized) {
		t.Errorf("the view does not report the failure: %v", view.Err)
	}

	provider.succeed()
	if _, err := panels.Refresh(ctx, "panel-1"); err != nil {
		t.Fatalf("Refresh after recovery failed: %v", err)
	}
	if err := panels.View("panel-1").Err; err != nil {
		t.Errorf("the view still reports a failure after success: %v", err)
	}
}

func TestAFailureWithoutACacheShowsNothing(t *testing.T) {
	provider := newFakeProvider()
	provider.fail(fmt.Errorf("%w: fake is down", domain.ErrProviderUnavailable))
	panels := newTestPanels(t, provider, nil)

	_, err := panels.Refresh(context.Background(), "panel-1")
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
	view := panels.View("panel-1")
	if view.HasSnapshot {
		t.Error("the view claims a snapshot the provider never gave")
	}
	if view.NeedsCredentials() {
		t.Error("an unavailable provider is reported as a missing credential")
	}
	if !strings.Contains(view.Err.Error(), "down") {
		t.Errorf("the view does not carry the reason: %v", view.Err)
	}
}

func TestRefreshIfNeededOnlyAsksWhenItHasTo(t *testing.T) {
	provider := newFakeProvider()
	panels := newTestPanels(t, provider, nil)
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	panels.now = func() time.Time { return now }

	if !panels.NeedsRefresh("panel-1") {
		t.Error("a panel with no snapshot does not need a refresh")
	}
	if _, fetched, err := panels.RefreshIfNeeded(ctx, "panel-1"); err != nil || !fetched {
		t.Fatalf("RefreshIfNeeded = %v, %v; want a fetch", fetched, err)
	}
	if provider.count() != 1 {
		t.Fatalf("the provider was called %d times, want 1", provider.count())
	}

	if panels.NeedsRefresh("panel-1") {
		t.Error("a fresh snapshot still needs a refresh")
	}
	if _, fetched, err := panels.RefreshIfNeeded(ctx, "panel-1"); err != nil || fetched {
		t.Fatalf("RefreshIfNeeded = %v, %v; want no fetch inside the interval", fetched, err)
	}
	if provider.count() != 1 {
		t.Errorf("the provider was called %d times, want 1", provider.count())
	}

	// Ten minutes later the snapshot is stale by the panel's own interval.
	panels.now = func() time.Time { return now.Add(11 * time.Minute) }
	view := panels.View("panel-1")
	if !view.Stale {
		t.Error("an old snapshot is not marked stale")
	}
	if !panels.NeedsRefresh("panel-1") {
		t.Error("a stale snapshot does not need a refresh")
	}
	if _, fetched, err := panels.RefreshIfNeeded(ctx, "panel-1"); err != nil || !fetched {
		t.Fatalf("RefreshIfNeeded = %v, %v; want a fetch after the interval", fetched, err)
	}
	if provider.count() != 2 {
		t.Errorf("the provider was called %d times, want 2", provider.count())
	}
}

func TestRefreshIfNeededRetriesAfterAFailure(t *testing.T) {
	provider := newFakeProvider()
	provider.fail(fmt.Errorf("%w: fake is down", domain.ErrProviderUnavailable))
	panels := newTestPanels(t, provider, nil)

	if _, _, err := panels.RefreshIfNeeded(context.Background(), "panel-1"); err == nil {
		t.Fatal("RefreshIfNeeded did not report the failure")
	}
	if !panels.NeedsRefresh("panel-1") {
		t.Error("a failed attempt does not need another one")
	}
	if _, _, err := panels.RefreshIfNeeded(context.Background(), "panel-1"); err == nil {
		t.Fatal("RefreshIfNeeded did not retry after a failure")
	}
	if provider.count() != 2 {
		t.Errorf("the provider was called %d times, want 2", provider.count())
	}
}

func TestConcurrentRefreshesBecomeOneRequest(t *testing.T) {
	provider := newFakeProvider()
	gate := provider.hold()
	panels := newTestPanels(t, provider, nil)

	const callers = 8
	var wg sync.WaitGroup
	results := make([]domain.Snapshot, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = panels.Refresh(context.Background(), "panel-1")
		}(i)
	}

	// Wait until exactly one fetch is running, then let it answer.
	deadline := time.Now().Add(2 * time.Second)
	for provider.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()

	if got := provider.count(); got != 1 {
		t.Errorf("the provider was called %d times, want 1", got)
	}
	for i := range results {
		if errs[i] != nil {
			t.Errorf("caller %d failed: %v", i, errs[i])
			continue
		}
		if len(results[i].Observations) != 2 {
			t.Errorf("caller %d got %d observations, want 2", i, len(results[i].Observations))
		}
	}
}

func TestRefreshUnknownPanel(t *testing.T) {
	panels := newTestPanels(t, newFakeProvider(), nil)
	if _, err := panels.Refresh(context.Background(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if _, _, err := panels.RefreshIfNeeded(context.Background(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RefreshIfNeeded error = %v, want ErrNotFound", err)
	}
	if view := panels.View("nope"); view.HasSnapshot || view.Panel.ID != "" {
		t.Errorf("View of an unknown panel = %+v", view)
	}
}

func TestListOrdersEnabledPanels(t *testing.T) {
	second := testDef()
	second.Panel = domain.Panel{
		ID: "panel-2", Kind: domain.PanelKindProvider, Title: "第二个",
		ProviderID: "fake", MetricKeys: []string{"balanceMinor"},
		RefreshEvery: 0, SortOrder: 1, Enabled: true,
	}
	second.Metrics = second.Metrics[:1]
	disabled := testDef()
	disabled.Panel.ID = "panel-3"
	disabled.Panel.SortOrder = 0
	disabled.Panel.Enabled = false

	panels := newTestPanels(t, newFakeProvider(), nil, testDef(), second, disabled)
	panels1 := panels.List()
	if len(panels1) != 2 {
		t.Fatalf("List returned %d panels, want 2 (a disabled one is left out)", len(panels1))
	}
	if panels1[0].ID != "panel-2" || panels1[1].ID != "panel-1" {
		t.Errorf("List order = %s, %s; want panel-2 first by sort order", panels1[0].ID, panels1[1].ID)
	}
	if got := panels.Metrics("panel-2"); len(got) != 1 {
		t.Errorf("Metrics(panel-2) = %+v", got)
	}
	if got := panels.Metrics("nope"); got != nil {
		t.Errorf("Metrics of an unknown panel = %+v", got)
	}
}

// A refresh interval of zero means "only when asked", so a snapshot never
// goes stale by itself.
func TestAZeroIntervalNeverGoesStale(t *testing.T) {
	def := testDef()
	def.Panel.RefreshEvery = 0
	panels := newTestPanels(t, newFakeProvider(), nil, def)
	panels.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := panels.Refresh(context.Background(), "panel-1"); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if view := panels.View("panel-1"); view.Stale {
		t.Error("a panel that only refreshes when asked is stale")
	}
}

func TestAnAdapterThatBreaksTheContractIsCaught(t *testing.T) {
	provider := newFakeProvider()
	reg := newRegistry()
	store := newMemoryStore()
	if err := store.Put(context.Background(), domain.CredentialKey("fake", "apiKey"), []byte("sk-test"), domain.SecretMeta{ProviderID: "fake"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	cases := []struct {
		name     string
		adapter  domain.Adapter
		def      *PanelDef
		contains string
	}{
		{
			name: "a snapshot without the panel's metrics",
			adapter: brokenAdapter{id: "fake", snapshot: domain.Snapshot{
				PanelID: "panel-1", ProviderID: "fake", Status: domain.StatusOK,
			}},
			contains: "no value for metric",
		},
		{
			name: "a snapshot with a metric the panel does not define",
			adapter: brokenAdapter{id: "fake", snapshot: domain.Snapshot{
				PanelID: "panel-1", ProviderID: "fake", Status: domain.StatusOK,
				Observations: []domain.Observation{
					{MetricKey: "ghost", Value: domain.NumberValue(1)},
					{MetricKey: "balanceMinor", Value: domain.NumberValue(1)},
					{MetricKey: "toppedUpMinor", Value: domain.NumberValue(1)},
				},
			}},
			contains: "does not define",
		},
		{
			name: "a value of the wrong shape",
			adapter: brokenAdapter{id: "fake", snapshot: domain.Snapshot{
				PanelID: "panel-1", ProviderID: "fake", Status: domain.StatusOK,
				Observations: []domain.Observation{
					{MetricKey: "balanceMinor", Value: domain.TextValue("plenty")},
					{MetricKey: "toppedUpMinor", Value: domain.NumberValue(1)},
				},
			}},
			contains: "does not match its value type",
		},
		{
			name: "a metric that takes its currency from the provider and a snapshot without one",
			adapter: brokenAdapter{id: "fake", snapshot: domain.Snapshot{
				PanelID: "panel-1", ProviderID: "fake", Status: domain.StatusOK,
				Observations: []domain.Observation{
					{MetricKey: "balanceMinor", Value: domain.NumberValue(1)},
					{MetricKey: "toppedUpMinor", Value: domain.NumberValue(1)},
				},
			}},
			def: func() *PanelDef {
				def := testDef()
				def.Metrics[0].Currency = ""
				def.Metrics[0].CurrencyFromProvider = true
				return &def
			}(),
			contains: "takes its currency from the provider",
		},
		{
			name:     "an adapter that fails",
			adapter:  brokenAdapter{id: "fake", err: fmt.Errorf("%w: mapped nothing", domain.ErrProviderBadResponse)},
			contains: "mapped nothing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg.providers = map[string]domain.Provider{"fake": provider}
			reg.adapters = map[string]domain.Adapter{"fake": c.adapter}
			def := testDef()
			if c.def != nil {
				def = *c.def
			}
			panels, err := NewPanels(reg, store, NewSection(), nil, []PanelDef{def})
			if err != nil {
				t.Fatalf("NewPanels failed: %v", err)
			}
			_, err = panels.Refresh(context.Background(), "panel-1")
			if err == nil {
				t.Fatal("the refresh succeeded although the adapter broke the contract")
			}
			if !strings.Contains(err.Error(), c.contains) {
				t.Errorf("error = %v, want it to mention %q", err, c.contains)
			}
		})
	}
}

func TestOptionalMetricsAreExplicitAndStillValidated(t *testing.T) {
	def := testDef()
	def.Panel.MetricKeys = def.Panel.MetricKeys[:1]
	def.OptionalMetricKeys = []string{"toppedUpMinor"}
	panels := newTestPanels(t, newFakeProvider(), nil, def)
	if len(panels.View(def.Panel.ID).Metrics) != 2 {
		t.Fatal("optional metric definition was lost")
	}
	for _, tc := range []struct {
		name     string
		optional []string
		wantErr  bool
	}{
		{"declared", []string{"toppedUpMinor"}, false},
		{"undeclared", nil, true},
		{"undefined", []string{"ghost"}, true},
		{"required overlap", []string{"balanceMinor"}, true},
		{"duplicate", []string{"toppedUpMinor", "toppedUpMinor"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := def
			candidate.OptionalMetricKeys = tc.optional
			reg := newRegistry()
			reg.add(newFakeProvider(), fakeAdapter{id: "fake"})
			_, err := NewPanels(reg, newMemoryStore(), NewSection(), nil, []PanelDef{candidate})
			if errors.Is(err, domain.ErrInvalidArgument) != tc.wantErr {
				t.Fatalf("NewPanels = %v, want invalid argument = %v", err, tc.wantErr)
			}
		})
	}
	for _, tc := range []struct {
		name         string
		observations []domain.Observation
		wantErr      bool
	}{
		{"optional absent", []domain.Observation{{MetricKey: "balanceMinor", Value: domain.NumberValue(10)}}, false},
		{"optional present", []domain.Observation{{MetricKey: "balanceMinor", Value: domain.NumberValue(10)}, {MetricKey: "toppedUpMinor", Value: domain.NumberValue(5)}}, false},
		{"optional wrong type", []domain.Observation{{MetricKey: "balanceMinor", Value: domain.NumberValue(10)}, {MetricKey: "toppedUpMinor", Value: domain.TextValue("bad")}}, true},
		{"required absent", []domain.Observation{{MetricKey: "toppedUpMinor", Value: domain.NumberValue(5)}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := domain.Snapshot{PanelID: def.Panel.ID, ProviderID: "fake", Status: domain.StatusOK, Observations: tc.observations}
			if err := checkSnapshot(def, snapshot); (err != nil) != tc.wantErr {
				t.Fatalf("checkSnapshot = %v, want error = %v", err, tc.wantErr)
			}
		})
	}
}

func TestTestProviderChecksWithoutStoring(t *testing.T) {
	provider := newFakeProvider()
	panels := newTestPanels(t, provider, nil)
	ctx := context.Background()

	if err := panels.TestProvider(ctx, "fake", map[string]string{"apiKey": "sk-candidate"}); err != nil {
		t.Fatalf("TestProvider failed: %v", err)
	}
	if got := provider.credentials().Secrets["apiKey"]; got != "sk-candidate" {
		t.Errorf("the provider saw %q, want the candidate key", got)
	}

	provider.fail(fmt.Errorf("%w: the key was rejected", domain.ErrProviderUnauthorized))
	if err := panels.TestProvider(ctx, "fake", map[string]string{"apiKey": "sk-bad"}); !errors.Is(err, domain.ErrProviderUnauthorized) {
		t.Errorf("error = %v, want ErrProviderUnauthorized", err)
	}

	if err := panels.TestProvider(ctx, "ghost", map[string]string{"apiKey": "x"}); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("unknown provider: error = %v", err)
	}
	if err := panels.TestProvider(ctx, "fake", nil); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("no credentials: error = %v", err)
	}
}
