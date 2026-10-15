package panel

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
// by the assembly in main.go from packages such as internal/panel/deepseek,
// so this package never names a vendor.
type PanelDef struct {
	Panel   domain.Panel
	Metrics []domain.Metric
	// UsageURL is where the provider keeps the account this panel reads, in
	// full. It is left empty by an assembly that names none, and then the
	// provider's own descriptor decides.
	UsageURL string
	// Optional marks a panel the user can turn off in the settings window:
	// one whose data is not Boku's own to fetch, or one that reads something
	// of the user's. A panel that is not optional has no switch, and the
	// settings window cannot offer one.
	Optional bool
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
	// tools is where the choices about the optional panels live, or nil in
	// an assembly that has no settings file: the switches then work for this
	// run only.
	tools domain.ToolStore
	// sources are the providers whose credentials come from somewhere other
	// than the store, by provider id. A provider named here is asked of its
	// source and never of the store. The map is filled once, in NewPanels,
	// and only read afterwards.
	sources map[string]domain.CredentialSource
	now     func() time.Time

	mu       sync.Mutex
	defs     []PanelDef
	byID     map[string]PanelDef
	cache    map[string]cached
	lastErr  map[string]error
	inflight map[string]*inflight
	// choices are the panels the user turned off, by id, as they were read
	// at assembly and as SetPanelOn changes them. A panel with no entry is
	// on. It is read on every frame, which is why it is not read from the
	// file there.
	choices map[string]bool
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
//
// A source is registered per provider for a credential that is not Boku's to
// keep: the Codex panel reads the login the Codex CLI already made
// (docs/security.md §9). A source is optional, and the store is asked for
// every provider that has none.
//
// tools is where the choices about the optional panels are read and written,
// or nil for an assembly with no settings file.
func NewPanels(registry domain.ProviderRegistry, secrets domain.SecretStore, tools domain.ToolStore, defs []PanelDef, sources ...domain.CredentialSource) (*Panels, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: no provider registry", domain.ErrInvalidArgument)
	}
	if secrets == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	panels := &Panels{
		registry: registry,
		secrets:  secrets,
		tools:    tools,
		sources:  make(map[string]domain.CredentialSource, len(sources)),
		now:      func() time.Time { return time.Now().UTC() },
		byID:     make(map[string]PanelDef, len(defs)),
		cache:    make(map[string]cached),
		lastErr:  make(map[string]error),
		inflight: make(map[string]*inflight),
		choices:  make(map[string]bool),
	}
	for _, source := range sources {
		if source == nil || strings.TrimSpace(source.ProviderID()) == "" {
			return nil, fmt.Errorf("%w: a credential source names no provider", domain.ErrInvalidArgument)
		}
		if _, exists := panels.sources[source.ProviderID()]; exists {
			return nil, fmt.Errorf("%w: two credential sources for provider %s", domain.ErrAlreadyExists, source.ProviderID())
		}
		panels.sources[source.ProviderID()] = source
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

// List returns the enabled panels, in the order they are shown. A panel the
// user turned off is not listed at all: it is not drawn, and nothing fetches
// it — which for the panels whose credential is somebody else's login is also
// the promise that the login file is not read.
func (p *Panels) List() []domain.Panel {
	p.mu.Lock()
	defer p.mu.Unlock()
	panels := make([]domain.Panel, 0, len(p.defs))
	for _, def := range p.defs {
		if p.onLocked(def) {
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

// onLocked reports whether a panel is shown: its definition must say so, and
// the user must not have turned it off.
func (p *Panels) onLocked(def PanelDef) bool {
	if !def.Panel.Enabled {
		return false
	}
	on, chosen := p.choices[def.Panel.ID]
	return !chosen || on
}

// OptionalPanels returns the panels the settings window offers a switch for:
// the ones the assembly marked optional, whether or not they are on, in the
// order they are shown.
func (p *Panels) OptionalPanels() []OptionalPanel {
	p.mu.Lock()
	defer p.mu.Unlock()
	panels := make([]OptionalPanel, 0, len(p.defs))
	for _, def := range p.defs {
		if !def.Optional {
			continue
		}
		panels = append(panels, OptionalPanel{
			Panel: def.Panel,
			On:    p.onLocked(def),
		})
	}
	sort.SliceStable(panels, func(i, j int) bool {
		if panels[i].Panel.SortOrder != panels[j].Panel.SortOrder {
			return panels[i].Panel.SortOrder < panels[j].Panel.SortOrder
		}
		return panels[i].Panel.ID < panels[j].Panel.ID
	})
	return panels
}

// OptionalPanel is one panel the user can turn off, with the answer.
type OptionalPanel struct {
	Panel domain.Panel
	On    bool
}

// LoadChoices reads the panels the user turned off. It is called once, by the
// assembly: a frame must not read a file, and the choices change only through
// SetPanelOn, which remembers them here as well as in the file.
//
// A store that cannot be read is not a reason to fail the start: the panels
// are all on, which is what a user who never turned one off has.
func (p *Panels) LoadChoices(ctx context.Context) error {
	if p.tools == nil {
		return nil
	}
	settings, err := p.tools.Load(ctx)
	if err != nil {
		return err
	}
	choices := make(map[string]bool, len(settings.Panels))
	for id, on := range settings.Panels {
		choices[id] = on
	}
	p.mu.Lock()
	p.choices = choices
	p.mu.Unlock()
	return nil
}

// SetPanelOn turns an optional panel on or off and stores the choice.
//
// The file is read, changed and written rather than written from a copy of
// the settings this service holds: every part of the application writes the
// same file, and a copy would write back the other switches as they were when
// this service started. A store that cannot be written is reported, and the
// switch has already taken effect for this run.
func (p *Panels) SetPanelOn(ctx context.Context, panelID string, on bool) error {
	p.mu.Lock()
	def, known := p.byID[panelID]
	p.mu.Unlock()
	if !known {
		return fmt.Errorf("%w: panel %s", domain.ErrNotFound, panelID)
	}
	if !def.Optional {
		return fmt.Errorf("%w: panel %s cannot be turned off", domain.ErrInvalidArgument, panelID)
	}

	var failed error
	if p.tools != nil {
		settings, err := p.tools.Load(ctx)
		if err != nil {
			failed = err
		} else {
			if settings.Panels == nil {
				settings.Panels = make(map[string]bool)
			}
			settings.Panels[panelID] = on
			if err := p.tools.Save(ctx, settings); err != nil {
				failed = err
			}
		}
	}

	p.mu.Lock()
	if p.choices == nil {
		p.choices = make(map[string]bool)
	}
	p.choices[panelID] = on
	p.mu.Unlock()
	return failed
}

// PanelOn reports whether one panel is on, by id.
func (p *Panels) PanelOn(panelID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, known := p.byID[panelID]
	if !known {
		return false
	}
	return p.onLocked(def)
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
// again, and a panel the user turned off is never asked.
func (p *Panels) NeedsRefresh(panelID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok || !p.onLocked(def) {
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
//
// A panel the user turned off is not asked at all, even if something calls
// here: off means no request, and for the panels whose credential is somebody
// else's login it means the login file is not read.
func (p *Panels) Refresh(ctx context.Context, panelID string) (domain.Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	def, known := p.byID[panelID]
	running := p.inflight[panelID]
	on := known && p.onLocked(def)
	p.mu.Unlock()
	if !known || !on {
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
	on := known && p.onLocked(def)
	running := p.inflight[panelID] != nil
	entry, cached := p.cache[panelID]
	_, failed := p.lastErr[panelID]
	fresh := false
	if known && cached && !failed {
		fresh = !p.isStale(def, entry)
	}
	p.mu.Unlock()

	if !known || !on {
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

// StartCredentialLogin starts the login a provider's credential comes from,
// for a provider whose credential is not Boku's to keep: the panel has
// nothing to read until the user has logged the tool in, and this is what
// saves them finding a terminal.
//
// A provider with no source, or whose source has no login of its own, says
// so: the interface only offers the action when the source has one.
func (p *Panels) StartCredentialLogin(providerID string) error {
	source, ok := p.sources[providerID]
	if !ok {
		return fmt.Errorf("%w: provider %s has no credential source", domain.ErrProviderNotFound, providerID)
	}
	starter, ok := source.(domain.LoginStarter)
	if !ok {
		return fmt.Errorf("%w: the credential source of %s has no login to start", domain.ErrInvalidArgument, providerID)
	}
	return starter.StartLogin()
}

// credentials collects what the provider asked for. A missing one is
// ErrCredentialMissing before the provider is called at all: no request is
// worth making with a key that is not there.
//
// A provider with a source of its own is asked of that source: its
// credential is not in the store — and must not be copied into it — so the
// store is not consulted for it at all.
func (p *Panels) credentials(ctx context.Context, descriptor domain.ProviderDescriptor) (domain.Credentials, error) {
	if source, ok := p.sources[descriptor.ID]; ok {
		return source.Credentials(ctx)
	}
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
		// Only an amount has a currency to resolve: a percentage, a duration
		// and a text are not amounts, and asking for theirs would refuse
		// every one of them.
		if metric.Unit == domain.UnitCurrency {
			if _, err := snapshot.MetricCurrency(metric); err != nil {
				return err
			}
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
