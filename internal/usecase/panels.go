package usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
)

// PanelDef is one panel and the metrics it shows. The definitions are built
// by the assembly in main.go from packages such as internal/domain/deepseek,
// so this package never names a vendor.
type PanelDef struct {
	Panel   domain.Panel
	Metrics []domain.Metric
	// UsageURL is where the provider keeps the account this panel reads, in
	// full. It is left empty by an assembly that names none, and then the
	// provider's own descriptor decides.
	UsageURL string
}

// Panels reads provider-backed panels: it keeps the last good snapshot of
// each, merges concurrent refreshes into one request, and never replaces a
// good snapshot with a failure.
//
// The interface reads View while drawing and Refresh from a goroutine; both
// are safe to call at any time from any goroutine.
type Panels struct {
	registry domain.ProviderRegistry
	secrets  domain.SecretStore
	now      func() time.Time

	mu       sync.Mutex
	defs     []PanelDef
	byID     map[string]PanelDef
	cache    map[string]cached
	lastErr  map[string]error
	inflight map[string]*inflight
}

type cached struct {
	snapshot  domain.Snapshot
	fetchedAt time.Time
}

// inflight is one refresh other callers wait for: the same panel must not
// ask its provider twice at once.
type inflight struct {
	done chan struct{}
}

// View is everything the interface needs to draw one panel, read from what
// is already in memory: building a frame never touches the network or the
// disk.
type View struct {
	Panel   domain.Panel
	Metrics []domain.Metric
	// UsageURL is where the provider keeps this account in full, or empty
	// when the provider names no such page. It is resolved once, when the
	// panel is added, so a frame does not look it up.
	UsageURL string
	// Snapshot is the last good one; HasSnapshot says whether there is one.
	// A failed refresh leaves it in place, marked stale, rather than
	// showing 0.
	Snapshot    domain.Snapshot
	HasSnapshot bool
	FetchedAt   time.Time
	// Stale says the numbers on screen are not current: they are older
	// than the panel's interval, or the last attempt to refresh them
	// failed.
	Stale bool
	// Loading is true while a refresh of this panel is running.
	Loading bool
	// Err is the failure of the last attempt, and its kind decides what
	// the card offers: Missing credentials ask for configuration, others
	// for a retry.
	Err error
}

// NeedsCredentials reports whether the panel failed because nobody has
// stored the credentials it needs.
func (v View) NeedsCredentials() bool {
	return errors.Is(v.Err, domain.ErrCredentialMissing)
}

// NewPanels checks the definitions and returns the service. It fails on a
// bad definition — an unknown provider, a metric the panel does not list, a
// refresh interval the provider does not tolerate — because those are
// assembly mistakes, not something to discover later in a card.
func NewPanels(registry domain.ProviderRegistry, secrets domain.SecretStore, defs []PanelDef) (*Panels, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: no provider registry", domain.ErrInvalidArgument)
	}
	if secrets == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	panels := &Panels{
		registry: registry,
		secrets:  secrets,
		now:      func() time.Time { return time.Now().UTC() },
		byID:     make(map[string]PanelDef, len(defs)),
		cache:    make(map[string]cached),
		lastErr:  make(map[string]error),
		inflight: make(map[string]*inflight),
	}
	for _, def := range defs {
		if err := panels.add(def); err != nil {
			return nil, err
		}
	}
	return panels, nil
}

func (p *Panels) add(def PanelDef) error {
	if err := def.Panel.Validate(); err != nil {
		return err
	}
	if _, exists := p.byID[def.Panel.ID]; exists {
		return fmt.Errorf("%w: panel %s is defined twice", domain.ErrAlreadyExists, def.Panel.ID)
	}
	provider, err := p.registry.Provider(def.Panel.ProviderID)
	if err != nil {
		return err
	}
	if _, err := p.registry.Adapter(def.Panel.ProviderID); err != nil {
		return err
	}
	byKey := make(map[string]domain.Metric, len(def.Metrics))
	for _, metric := range def.Metrics {
		if err := metric.Validate(); err != nil {
			return err
		}
		if _, exists := byKey[metric.Key]; exists {
			return fmt.Errorf("%w: panel %s defines metric %s twice", domain.ErrAlreadyExists, def.Panel.ID, metric.Key)
		}
		byKey[metric.Key] = metric
	}
	for _, key := range def.Panel.MetricKeys {
		if _, ok := byKey[key]; !ok {
			return fmt.Errorf("%w: panel %s lists metric %s, which it does not define", domain.ErrInvalidArgument, def.Panel.ID, key)
		}
	}
	if len(def.Panel.MetricKeys) != len(byKey) {
		return fmt.Errorf("%w: panel %s defines %d metrics and lists %d", domain.ErrInvalidArgument, def.Panel.ID, len(byKey), len(def.Panel.MetricKeys))
	}
	if every := def.Panel.RefreshEvery; every > 0 {
		if floor := int(provider.Descriptor().MinRefreshInterval().Seconds()); every < floor {
			return fmt.Errorf("%w: panel %s refreshes every %ds, and provider %s tolerates no less than %ds",
				domain.ErrInvalidArgument, def.Panel.ID, every, def.Panel.ProviderID, floor)
		}
	}
	// The link a panel's headline opens is the provider's to name: an
	// assembly may write one down, and otherwise the descriptor is asked,
	// which keeps the address beside the provider that owns it.
	if strings.TrimSpace(def.UsageURL) == "" {
		def.UsageURL = provider.Descriptor().UsageURL
	}
	p.defs = append(p.defs, def)
	p.byID[def.Panel.ID] = def
	return nil
}

// List returns the enabled panels, in the order they are shown.
func (p *Panels) List() []domain.Panel {
	p.mu.Lock()
	defer p.mu.Unlock()
	panels := make([]domain.Panel, 0, len(p.defs))
	for _, def := range p.defs {
		if def.Panel.Enabled {
			panels = append(panels, def.Panel)
		}
	}
	sort.SliceStable(panels, func(i, j int) bool {
		if panels[i].SortOrder != panels[j].SortOrder {
			return panels[i].SortOrder < panels[j].SortOrder
		}
		return panels[i].ID < panels[j].ID
	})
	return panels
}

// Metrics returns one panel's metrics, in the order they are shown.
func (p *Panels) Metrics(panelID string) []domain.Metric {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok {
		return nil
	}
	return append([]domain.Metric(nil), def.Metrics...)
}

// Descriptor returns what a provider is called and what it asks for, so an
// interface can offer to configure it without knowing which provider it is.
func (p *Panels) Descriptor(providerID string) (domain.ProviderDescriptor, error) {
	provider, err := p.registry.Provider(providerID)
	if err != nil {
		return domain.ProviderDescriptor{}, err
	}
	return provider.Descriptor(), nil
}

// View returns what is known about a panel right now.
func (p *Panels) View(panelID string) View {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok {
		return View{}
	}
	view := View{
		Panel:    def.Panel,
		Metrics:  append([]domain.Metric(nil), def.Metrics...),
		UsageURL: def.UsageURL,
	}
	if entry, ok := p.cache[panelID]; ok {
		view.Snapshot = entry.snapshot
		view.HasSnapshot = true
		view.FetchedAt = entry.fetchedAt
		view.Stale = p.isStale(def, entry) || p.lastErr[panelID] != nil
	}
	view.Loading = p.inflight[panelID] != nil
	view.Err = p.lastErr[panelID]
	return view
}

// NeedsRefresh reports whether the panel's snapshot is missing, stale or
// from a failed attempt. A fresh snapshot means the provider is not asked
// again.
func (p *Panels) NeedsRefresh(panelID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok {
		return false
	}
	if _, failed := p.lastErr[panelID]; failed {
		return true
	}
	entry, cached := p.cache[panelID]
	if !cached {
		return true
	}
	return p.isStale(def, entry)
}

// Refresh fetches a panel, whatever its snapshot says, and is what the
// interface calls when the user asks for a refresh.
//
// Concurrent calls for the same panel become one request; a failure keeps
// the last good snapshot and is remembered until an attempt succeeds. The
// snapshot returned with an error is that previous one, or nothing.
func (p *Panels) Refresh(ctx context.Context, panelID string) (domain.Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	def, known := p.byID[panelID]
	running := p.inflight[panelID]
	p.mu.Unlock()
	if !known {
		return domain.Snapshot{}, fmt.Errorf("%w: panel %s", domain.ErrNotFound, panelID)
	}

	if running != nil {
		select {
		case <-running.done:
		case <-ctx.Done():
			return domain.Snapshot{}, fmt.Errorf("%w: waiting for the refresh of panel %s", domain.ErrProviderTimeout, panelID)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.cache[panelID].snapshot, p.lastErr[panelID]
	}

	call := &inflight{done: make(chan struct{})}
	p.mu.Lock()
	if p.inflight[panelID] != nil {
		// Another caller started one between the two locks: let them do
		// the work and wait for it.
		existing := p.inflight[panelID]
		p.mu.Unlock()
		select {
		case <-existing.done:
		case <-ctx.Done():
			return domain.Snapshot{}, fmt.Errorf("%w: waiting for the refresh of panel %s", domain.ErrProviderTimeout, panelID)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.cache[panelID].snapshot, p.lastErr[panelID]
	}
	p.inflight[panelID] = call
	p.mu.Unlock()

	snapshot, err := p.fetch(ctx, def)

	p.mu.Lock()
	delete(p.inflight, panelID)
	result := snapshot
	if err != nil {
		p.lastErr[panelID] = err
		result = p.cache[panelID].snapshot
	} else {
		p.cache[panelID] = cached{snapshot: snapshot, fetchedAt: p.now()}
		delete(p.lastErr, panelID)
	}
	close(call.done)
	p.mu.Unlock()
	return result, err
}

// RefreshIfNeeded fetches a panel only when it has to: no snapshot yet, one
// that is older than the panel's refresh interval, or a last attempt that
// failed. It reports whether it asked the provider.
func (p *Panels) RefreshIfNeeded(ctx context.Context, panelID string) (domain.Snapshot, bool, error) {
	p.mu.Lock()
	def, known := p.byID[panelID]
	running := p.inflight[panelID] != nil
	entry, cached := p.cache[panelID]
	_, failed := p.lastErr[panelID]
	fresh := false
	if known && cached && !failed {
		fresh = !p.isStale(def, entry)
	}
	p.mu.Unlock()

	if !known {
		return domain.Snapshot{}, false, fmt.Errorf("%w: panel %s", domain.ErrNotFound, panelID)
	}
	if fresh && !running {
		return entry.snapshot, false, nil
	}
	snapshot, err := p.Refresh(ctx, panelID)
	return snapshot, true, err
}

// TestProvider checks credentials without storing them: it fetches the
// panel of that provider once and maps the answer. The settings window uses
// it to tell the user whether a key works before saving it.
func (p *Panels) TestProvider(ctx context.Context, providerID string, secrets map[string]string) error {
	p.mu.Lock()
	var def PanelDef
	found := false
	for _, candidate := range p.defs {
		if candidate.Panel.ProviderID == providerID {
			def, found = candidate, true
			break
		}
	}
	p.mu.Unlock()
	if !found {
		return fmt.Errorf("%w: no panel uses provider %s", domain.ErrProviderNotFound, providerID)
	}
	provider, err := p.registry.Provider(providerID)
	if err != nil {
		return err
	}
	adapter, err := p.registry.Adapter(providerID)
	if err != nil {
		return err
	}
	// A candidate that is not there is answered here, so the provider is
	// only ever asked with a credential in hand.
	for _, key := range provider.Descriptor().CredentialKeys {
		if strings.TrimSpace(secrets[key]) == "" {
			return fmt.Errorf("%w: %s is empty", domain.ErrCredentialMissing, domain.CredentialKey(providerID, key))
		}
	}
	creds := domain.Credentials{
		ProviderID: providerID,
		Secrets:    secrets,
		Settings:   map[string]string{},
	}
	result, err := provider.Fetch(ctx, creds)
	if err != nil {
		return err
	}
	if result.ProviderID == "" {
		result.ProviderID = providerID
	}
	if _, err := adapter.Normalize(result, def.Panel); err != nil {
		return err
	}
	return nil
}

// fetch asks the provider and maps the answer.
func (p *Panels) fetch(ctx context.Context, def PanelDef) (domain.Snapshot, error) {
	providerID := def.Panel.ProviderID
	provider, err := p.registry.Provider(providerID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	adapter, err := p.registry.Adapter(providerID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	creds, err := p.credentials(ctx, provider.Descriptor())
	if err != nil {
		return domain.Snapshot{}, err
	}
	result, err := provider.Fetch(ctx, creds)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if result.ProviderID == "" {
		result.ProviderID = providerID
	}
	snapshot, err := adapter.Normalize(result, def.Panel)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if err := checkSnapshot(def, snapshot); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

// credentials collects what the provider asked for. A missing one is
// ErrCredentialMissing before the provider is called at all: no request is
// worth making with a key that is not there.
func (p *Panels) credentials(ctx context.Context, descriptor domain.ProviderDescriptor) (domain.Credentials, error) {
	creds := domain.Credentials{
		ProviderID: descriptor.ID,
		Secrets:    make(map[string]string, len(descriptor.CredentialKeys)),
		Settings:   map[string]string{},
	}
	for _, key := range descriptor.CredentialKeys {
		ref := domain.CredentialKey(descriptor.ID, key)
		secret, err := p.secrets.Get(ctx, ref)
		if errors.Is(err, domain.ErrSecretNotFound) {
			return creds, fmt.Errorf("%w: %s is not configured", domain.ErrCredentialMissing, ref)
		}
		if err != nil {
			return creds, err
		}
		value := strings.TrimSpace(secret.Text())
		if value == "" {
			return creds, fmt.Errorf("%w: %s is empty", domain.ErrCredentialMissing, ref)
		}
		creds.Secrets[key] = value
	}
	return creds, nil
}

// checkSnapshot holds every adapter to the panel's contract: the panel's
// metrics, in the panel's order, each with the value type it declared.
func checkSnapshot(def PanelDef, snapshot domain.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	metrics := make(map[string]domain.Metric, len(def.Metrics))
	for _, metric := range def.Metrics {
		metrics[metric.Key] = metric
	}
	seen := make(map[string]bool, len(snapshot.Observations))
	for _, observation := range snapshot.Observations {
		metric, ok := metrics[observation.MetricKey]
		if !ok {
			return fmt.Errorf("%w: the snapshot of panel %s has metric %s, which the panel does not define",
				domain.ErrInvalidMetricValue, def.Panel.ID, observation.MetricKey)
		}
		if !observation.Value.Matches(metric.ValueType) {
			return fmt.Errorf("%w: observation of %s does not match its value type %s",
				domain.ErrInvalidMetricValue, observation.MetricKey, metric.ValueType)
		}
		if _, err := snapshot.MetricCurrency(metric); err != nil {
			return err
		}
		seen[observation.MetricKey] = true
	}
	for _, key := range def.Panel.MetricKeys {
		if !seen[key] {
			return fmt.Errorf("%w: the snapshot of panel %s has no value for metric %s",
				domain.ErrInvalidMetricValue, def.Panel.ID, key)
		}
	}
	return nil
}

// isStale reports whether a snapshot is older than the panel's interval.
// The caller holds the lock.
func (p *Panels) isStale(def PanelDef, entry cached) bool {
	if def.Panel.RefreshEvery <= 0 {
		return false
	}
	return p.now().Sub(entry.fetchedAt) > time.Duration(def.Panel.RefreshEvery)*time.Second
}
