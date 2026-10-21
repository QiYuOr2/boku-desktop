package ui

import (
	"boku/internal/clipboard"
	contractdeepseek "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/panel"
	"boku/internal/panel/deepseek"
	"boku/internal/translate"
	"context"
	"errors"
	"fmt"
	"github.com/egoist/mygo"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

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
			CredentialKeys:    []string{contractdeepseek.APIKeyCredential},
			DocsURL:           "https://platform.deepseek.com/api_keys",
			UsageURL:          deepseek.UsageURL,
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

// testStateWith is testState with the translation fixture handed back, for
// the tests that drive the translation tool: what it answers, what failed,
// and what the shortcut registrar was told.
func testStateWith(t testing.TB, provider *testProvider, store *testStore, version string) (*State, *testTranslateFixture, *testShortcuts) {
	t.Helper()
	device := &testDevice{}
	translation := testTranslate(t, store, device)
	shortcuts := &testShortcuts{}
	state := New(Config{Service: translation.service, Clipboard: testClipboard(t, nil, nil), Shortcuts: shortcuts})
	return state, translation, shortcuts
}

// testTranslateFixture is the translation tool with the fakes it was built
// over, so that a test can change what it answers.
type testTranslateFixture struct {
	service   *translate.Translate
	deepseek  *testTranslator
	microsoft *testTranslator
	bilibili  *testTranslator
	// own is the registry, as the assembly holds it: a test adds a service the
	// way the assembly does, through own.addOwnService.
	own *ownRegistry
	// registerOwn is what the window calls after the user adds a service: the
	// assembly's own "put a translator in the registry for every service the
	// settings hold", over the fake registry above.
	registerOwn func() error
	// addService adds one of the user's own services the whole way — settings
	// and registry — which is what the window's 添加 does in production.
	addService func(translate.CustomSettings) string
	tools      *testToolStore
	device     *testDevice
	selection  *testSelection
	// timeline is what happened, in the order it happened. The selection
	// device, the card and the translator all write to it, which is how a
	// test says that the text to translate was read before the card took
	// the keyboard.
	timeline *timeline
}

// timeline is an ordered record the fakes write to, for the tests that are
// about an order rather than about a value.
type timeline struct {
	mu     sync.Mutex
	events []string
}

func (l *timeline) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

// all is everything that happened, oldest first.
func (l *timeline) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// testSelection is the platform's answer to what is selected: the text a test
// decides, and a note in the timeline that it was asked.
type testSelection struct {
	mu       sync.Mutex
	text     string
	err      error
	asked    int
	timeline *timeline
}

func (s *testSelection) SelectedText(context.Context) (string, error) {
	if s.timeline != nil {
		s.timeline.add("read the selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked++
	return s.text, s.err
}

// set is what the platform says is selected, from now on.
func (s *testSelection) set(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text, s.err = text, nil
}

// asks is how many times the platform was asked.
func (s *testSelection) asks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked
}

// testTranslate assembles the translation tool over the fakes: a translator
// whose answer the test decides, the credential store the window writes to,
// and a clipboard device that never touches the system's.
func testTranslate(t testing.TB, store *testStore, device *testDevice) *testTranslateFixture {
	t.Helper()
	timeline := &timeline{}
	reply := &testTranslator{id: "deepseek", name: "DeepSeek", timeline: timeline, descriptor: domain.TranslatorDescriptor{
		Endpoint: "https://api.deepseek.com/chat/completions",
	}}
	// The sections start as a fresh installation has them: the file store
	// fills the defaults in when the file has no such key, so the fake stands
	// in for a store that has already been read once.
	tools := newTestToolStore()
	own := translatorRegistry(tools.translation)
	// Registering the user's own services is what the window calls after the
	// user adds one. The assembly is what really does it, through the registry
	// it built; a test does the same thing through the fake registry.
	// registerOwn is what the window calls after the user adds a service or
	// removes one: the assembly's own "make the registry match the settings",
	// over the fake registry. Both directions, because they are one job.
	registerOwn := func() error {
		present := map[string]bool{}
		for _, id := range tools.translation.Current().CustomServiceIDs() {
			present[id] = true
			if !own.Has(id) {
				own.addOwnService(id)
			}
		}
		for _, id := range own.TranslatorIDs() {
			if present[id] || !translate.IsCustomService(id) {
				continue
			}
			own.Remove(id)
		}
		return nil
	}
	// The free engines: two translators, not one. They are what a fresh
	// installation starts on and what the picker draws in its own group, so a
	// window test that never saw them would be testing a build nobody ships.
	microsoft := &testTranslator{id: freeMicrosoftID, name: "微软 Edge 翻译", descriptor: domain.TranslatorDescriptor{
		AllowEmptyCredential: true,
		Endpoint:             "edge.microsoft.com/translate/translatetext",
	}}
	bilibili := &testTranslator{id: freeBilibiliID, name: "B站 Index-Translate", descriptor: domain.TranslatorDescriptor{
		AllowEmptyCredential: true,
		Endpoint:             "index-translate.bilibili.com/v1/chat/completions",
	}}
	own.add(microsoft, bilibili, reply)
	selection := &testSelection{timeline: timeline}
	service, err := translate.NewTranslate(translate.TranslateOptions{
		Translators: own,
		Secrets:     store,
		Section:     tools.translation,
		Settings:    tools,
		// The platform's own answer is faked here, so that a test can say
		// what the user has selected in another application — and, through
		// the timeline, when it was read.
		Selection: selection,
		Clipboard: device,
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	// addService is one of the user's own services added the whole way: the
	// settings hold it, and a translator for it is in the registry.
	//
	// It is what the window does when 添加 is pressed, for the tests that are
	// about what the window draws afterwards rather than about the button. The
	// two steps are in the order production takes them, because that order is
	// the reason it works: the settings first, so the translator has an
	// address to read, and the registry second, so there is one to read it.
	addService := func(settings translate.CustomSettings) string {
		id, err := service.AddCustomTranslator(context.Background(), settings)
		if err != nil {
			t.Fatalf("AddCustomTranslator failed: %v", err)
		}
		if err := registerOwn(); err != nil {
			t.Fatalf("registering the service failed: %v", err)
		}
		return id
	}
	return &testTranslateFixture{
		service:     service,
		own:         own,
		registerOwn: registerOwn,
		addService:  addService,
		tools:       tools,
		device:      device,
		selection:   selection,
		timeline:    timeline,
	}
}

// testTranslator is a translator whose answer the test decides, so that a
// view test can reach the state a translation puts the window in without a
// network.
//
// It answers two requests differently, because the card makes two: the
// translation, and the annotation under it. A test that sets only one answer
// gets it for both.
type testTranslator struct {
	id         string
	name       string
	descriptor domain.TranslatorDescriptor
	// baseURL, when set, is where a translator whose address is a setting
	// reads that address from: the fake's stand-in for the real backend's
	// closure over the settings.
	baseURL func() string

	mu      sync.Mutex
	result  domain.TranslationResult
	err     error
	replies int

	// annotated is the answer to a request that asks for the annotation, and
	// hasAnnotation says whether a test ever set it.
	annotated     domain.TranslationResult
	hasAnnotation bool
	// requests is every request the translator was given, in order, so a
	// test can say what was asked and how often.
	requests []domain.TranslationRequest
	// keepProviderID makes the translator answer with the result's own
	// provider id rather than its registered one. It is how a test stands in
	// for the free tier, whose answer names the engine that served it
	// ("免费（微软）") rather than the tier.
	keepProviderID bool
	// nameFromService, when set, is what a translator of the user's own is
	// called: their name for the service, read from the settings, rather than
	// a name written down in a test.
	nameFromService func() string
	// timeline, when a test sets one, records that the request went out —
	// which is what says whether the card had the keyboard before it did.
	timeline *timeline
}

func (t *testTranslator) ID() string { return t.id }

func (t *testTranslator) Descriptor() domain.TranslatorDescriptor {
	descriptor := t.descriptor
	descriptor.ID = t.id
	// What a translator says about itself is written down, the way the real
	// ones write it: a fake that filled its own fields in on the way out
	// would agree with code that had forgotten to.
	// A translator of the user's own is called what the user called the
	// service: the settings are where that name lives, and reading it here is
	// what makes the window show it.
	name := t.name
	if t.nameFromService != nil {
		name = t.nameFromService()
	}
	descriptor.DisplayName = name
	if len(descriptor.CredentialKeys) == 0 {
		descriptor.CredentialKeys = []string{"apiKey"}
	}
	if descriptor.DocsURL == "" {
		descriptor.DocsURL = "https://example.com/keys"
	}
	// A translator whose address is a setting computes it from that setting,
	// the way the real custom backend does. It is what makes the endpoint the
	// window shows the address the user saved rather than one written down in
	// a test.
	if descriptor.BaseURLSetting != "" && t.baseURL != nil {
		if base := strings.TrimRight(t.baseURL(), "/"); base != "" {
			descriptor.Endpoint = base + "/chat/completions"
		}
	}
	return descriptor
}

func (t *testTranslator) Translate(_ context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if t.timeline != nil {
		t.timeline.add("asked the translator")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.replies++
	t.requests = append(t.requests, req)
	if t.err != nil {
		return domain.TranslationResult{}, t.err
	}
	result := t.result
	if req.Annotation.Wants() && t.hasAnnotation {
		result = t.annotated
	}
	result.SourceText = req.Text
	result.SourceLang = req.SourceLang
	result.TargetLang = req.TargetLang
	if !t.keepProviderID {
		result.ProviderID = t.id
	}
	if result.TranslatedText == "" && len(result.Segments) == 0 {
		result.TranslatedText = "译文"
	}
	return result, nil
}

// answer makes the translator answer with this from now on.
func (t *testTranslator) answer(result domain.TranslationResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result = result
}

// annotate makes the translator answer a request for the annotation with this
// from now on: the pieces the card's second request is for.
func (t *testTranslator) annotate(result domain.TranslationResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.annotated, t.hasAnnotation = result, true
}

// fail makes the translator fail with this from now on.
func (t *testTranslator) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

// requestCount is how many requests the translator was given.
func (t *testTranslator) requestCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

// lastRequest is the last one it was given.
func (t *testTranslator) lastRequest() domain.TranslationRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.requests) == 0 {
		return domain.TranslationRequest{}
	}
	return t.requests[len(t.requests)-1]
}

// ownRegistry is a domain.TranslatorRegistry that can grow: it stands in for the
// registry the assembly builds, over which a translator is registered for every
// service the settings hold.
//
// It is a registry a test can add to because that is what production does: the
// settings window adds a service and the assembly registers the translator that
// asks it, in that order. A test whose registry could not grow could never see
// a service the user added.
type ownRegistry struct {
	mu       sync.Mutex
	entries  map[string]*testTranslator
	services *translate.Section
}

// translatorRegistry returns an empty registry whose translators of the user's
// own resolve their addresses from a section.
func translatorRegistry(services *translate.Section) *ownRegistry {
	return &ownRegistry{entries: map[string]*testTranslator{}, services: services}
}

// add puts translators in the registry, as the assembly does at startup.
func (r *ownRegistry) add(replies ...*testTranslator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, reply := range replies {
		r.entries[reply.id] = reply
	}
}

// addOwnService registers the translator for one of the user's own services,
// reading its address and its name from the settings the way the real one does.
func (r *ownRegistry) addOwnService(id string) {
	services := r.services
	reply := &testTranslator{
		id:   id,
		name: translate.CustomSettings{}.DisplayName,
		descriptor: domain.TranslatorDescriptor{
			BaseURLSetting:       "custom.baseURL",
			AllowEmptyCredential: true,
		},
	}
	reply.baseURL = func() string {
		service, _ := services.Current().CustomService(id)
		return service.BaseURL
	}
	reply.nameFromService = func() string {
		service, ok := services.Current().CustomService(id)
		if !ok || strings.TrimSpace(service.DisplayName) == "" {
			return "自定义"
		}
		return service.DisplayName
	}
	r.add(reply)
}

func (r *ownRegistry) Translator(id string) (domain.Translator, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reply, ok := r.entries[id]
	if !ok {
		return nil, fmt.Errorf("%w: translator %s", domain.ErrProviderNotFound, id)
	}
	return reply, nil
}

func (r *ownRegistry) TranslatorIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Has reports whether a translator is registered, which is what the assembly
// asks before registering one the user just added.
func (r *ownRegistry) Has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.entries[id]
	return ok
}

// Remove takes a translator out, which is what the assembly does when the user
// removes one of their own services.
func (r *ownRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}

// translator is one of the registered translators, by id. It is how a test
// reaches a fake it did not build itself — one the registry holds because the
// assembly put it there.
func (f *testTranslateFixture) translator(t *testing.T, id string) *testTranslator {
	t.Helper()
	f.own.mu.Lock()
	defer f.own.mu.Unlock()
	reply, ok := f.own.entries[id]
	if !ok {
		t.Fatalf("no translator is registered under %s", id)
	}
	return reply
}

// ensure the registry is what the tool asks for.
var _ domain.TranslatorRegistry = (*ownRegistry)(nil)

// testShortcuts is a ShortcutRegistrar the test drives: it records what was
// registered, so that the rules around a global shortcut — bind it, show a
// failure, release it on the way out — are checked without a graphical
// session.
type testShortcuts struct {
	mu            sync.Mutex
	accelerator   string
	fn            func()
	registrations int
	releases      int
	refusals      int
	err           error
	// working is how many more registrations are taken before err applies:
	// it is how a test says "the first one works, the second is refused",
	// which is the shape of a combination another application already holds.
	working int
}

func (s *testShortcuts) Register(accelerator string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrations++
	switch {
	case s.working > 0:
		s.working--
	case s.err != nil:
		s.refusals++
		return s.err
	}
	s.accelerator, s.fn = accelerator, fn
	return nil
}

// refuseAfter lets this many more registrations through before the failure
// applies: the test's way of saying a combination is already taken, and then
// letting the binding that was there be put back.
func (s *testShortcuts) refuseAfter(taken int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.working = taken
	s.err = errors.New("the combination is already registered")
}

// refused counts how many registrations were turned down.
func (s *testShortcuts) refused() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refusals
}

func (s *testShortcuts) UnregisterAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	s.accelerator, s.fn = "", nil
}

// press runs what is bound, the way the platform would.
func (s *testShortcuts) press() {
	s.mu.Lock()
	fn := s.fn
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *testShortcuts) bound() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accelerator
}

// callback is what the bound key runs, or nil when nothing is bound.
func (s *testShortcuts) callback() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fn
}

func (s *testShortcuts) counts() (registrations, releases int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registrations, s.releases
}

func (s *testShortcuts) failWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// testClipboard is a clipboard tool over a device and a store the test owns,
// so that a view test never touches the system clipboard.
func testClipboard(t testing.TB, device *testDevice, tools *testToolStore) *clipboard.Clipboard {
	t.Helper()
	if device == nil {
		device = &testDevice{}
	}
	if tools == nil {
		tools = newTestToolStore()
	}
	// One clock for the service and for what the device hands back: the
	// history is ordered by the times the device reports.
	clock := &testClock{at: time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)}
	device.now = clock.now
	clipboard, err := clipboard.NewClipboard(clipboard.ClipboardOptions{
		Device:   device,
		Store:    &testClipStore{},
		Section:  tools.clipboard,
		Settings: tools,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	return clipboard
}

// testClock is a clock a view test can move: copies made through the frame
// are ordered by it.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// now is Now as a function value, for the device that stamps readings.
func (c *testClock) now() time.Time { return c.Now() }

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// testDevice is a domain.ClipboardDevice whose content the test decides.
//
// It stamps what it hands back with the same clock the service reads, so that
// a test compares one timeline: an entry copied back is newest, and the
// entries around it keep the order they were copied in.
type testDevice struct {
	// now stamps a reading, and is set by testClipboard to the clock the
	// service was given.
	now func() time.Time

	mu      sync.Mutex
	reading domain.ClipContent
	// readErr, when set, is what the platform answers instead of a reading:
	// a clipboard another application is holding answers that way.
	readErr error
	writes  []string
	files   [][]string
	images  [][]byte
}

func (d *testDevice) stamp(reading domain.ClipContent) domain.ClipContent {
	if d.now != nil {
		reading.CapturedAt = d.now()
	}
	return reading
}

func (d *testDevice) Read(context.Context) (domain.ClipContent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.readErr != nil {
		return domain.ClipContent{}, d.readErr
	}
	if d.reading.CapturedAt.IsZero() {
		d.reading = d.stamp(d.reading)
	}
	// A clipboard with nothing this application can show is not an error of
	// the platform's: it is the state before anything was copied, and the
	// reader answers with an empty reading.
	return d.reading, nil
}

// failRead makes the next reading fail, the way a platform that refuses the
// format does.
func (d *testDevice) failRead(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readErr = err
}

func (d *testDevice) WriteText(_ context.Context, text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, text)
	d.reading = d.stamp(domain.ClipContent{Kind: domain.ClipKindText, Text: text})
	return nil
}

// WriteFiles records a file list written back, and puts it on the clipboard
// the way the platform would: as files, with their paths as text.
func (d *testDevice) WriteFiles(_ context.Context, paths []string) error {
	if len(paths) == 0 {
		return domain.ErrUnsupportedClipboardFormat
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files = append(d.files, append([]string(nil), paths...))
	d.reading = d.stamp(domain.ClipContent{
		Kind:  domain.ClipKindFile,
		Files: append([]string(nil), paths...),
	})
	return nil
}

func (d *testDevice) WriteImage(_ context.Context, png []byte) error {
	if len(png) == 0 {
		return domain.ErrUnsupportedClipboardFormat
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.images = append(d.images, append([]byte(nil), png...))
	d.reading = d.stamp(domain.ClipContent{
		Kind:       domain.ClipKindImage,
		HasImage:   true,
		ImageBytes: len(png),
		Image:      append([]byte(nil), png...),
	})
	return nil
}

func (d *testDevice) set(reading domain.ClipContent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// A test that sets a time of its own keeps it; one that does not gets
	// the clock the service uses.
	if reading.CapturedAt.IsZero() {
		reading = d.stamp(reading)
	}
	d.reading = reading
}

func (d *testDevice) written() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.writes...)
}

func (d *testDevice) writtenFiles() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]string, 0, len(d.files))
	for _, paths := range d.files {
		out = append(out, append([]string(nil), paths...))
	}
	return out
}

func (d *testDevice) writtenImages() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]byte, 0, len(d.images))
	for _, png := range d.images {
		out = append(out, append([]byte(nil), png...))
	}
	return out
}

// testClipStore is a domain.ClipboardStore in memory.
type testClipStore struct {
	mu      sync.Mutex
	entries []domain.ClipboardEntry
	// images is the image store beside the history.
	images map[string][]byte
}

func (s *testClipStore) SaveImage(_ context.Context, ref string, png []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.images == nil {
		s.images = map[string][]byte{}
	}
	s.images[ref] = append([]byte(nil), png...)
	return nil
}

func (s *testClipStore) LoadImage(_ context.Context, ref string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	png, ok := s.images[ref]
	if !ok {
		return nil, domain.ErrUnsupportedClipboardFormat
	}
	return append([]byte(nil), png...), nil
}

func (s *testClipStore) DeleteImage(_ context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.images, ref)
	return nil
}

func (s *testClipStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ClipboardEntry(nil), s.entries...), nil
}

func (s *testClipStore) SaveEntries(_ context.Context, entries []domain.ClipboardEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append([]domain.ClipboardEntry(nil), entries...)
	return nil
}

// testToolStore is the settings store, in memory: the real sections, with the
// file replaced by a map.
//
// The sections are the real ones — a test that changed a clipboard switch
// through a stand-in would not be testing what ships — and what is faked is
// only the file underneath them, so that a test never touches a user's data.
type testToolStore struct {
	mu          sync.Mutex
	clipboard   *clipboard.Section
	translation *translate.Section
	panels      *panel.Section
}

func newTestToolStore() *testToolStore {
	return &testToolStore{
		clipboard:   clipboard.NewSection(),
		translation: translate.NewSection(),
		panels:      panel.NewSection(),
	}
}

// Load is a file read that finds nothing: every section keeps its defaults.
func (s *testToolStore) Load(context.Context) error { return nil }

// Update runs the change against the section named, the way the file store
// does — it is the section that decides what the change means.
func (s *testToolStore) Update(_ context.Context, section string, change func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return change()
}

// withCredential adds the DeepSeek key the panel needs.
func withCredential(store *testStore) *testStore {
	store.set(domain.CredentialKey("deepseek", contractdeepseek.APIKeyCredential), "sk-test-0001")
	return store
}

// translateState is a state whose translation card is a fake, so that a test
// can ask what the card was told without a window existing.
func translateState(t testing.TB) (*State, *testTranslateFixture, *testCard) {
	t.Helper()
	state, translate, _ := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	card := &testCard{timeline: translate.timeline}
	state.cardWindows = func(onClosed, onBlur, onFocus func()) CardWindow {
		card.onClosed, card.onBlur, card.onFocus = onClosed, onBlur, onFocus
		return card
	}
	state.cursor, state.cursorSet = mygo.Point{X: 400, Y: 300}, true
	return state, translate, card
}

func start(t *testing.T, state *State) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		state.onTranslateShortcut()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the shortcut callback did not return: something in it waits")
	}
}

type testCard struct {
	mu     sync.Mutex
	shown  bool
	focuse int
	hides  int
	bounds mygo.Rectangle

	// timeline, when a test sets one, records the two moments that matter to
	// a translation: the card appearing without the keyboard, and taking it.
	timeline *timeline

	onClosed func()
	onBlur   func()
	onFocus  func()
}

func (c *testCard) ShowInactive() {
	c.mu.Lock()
	c.shown = true
	c.mu.Unlock()
	if c.timeline != nil {
		c.timeline.add("the card appeared without the keyboard")
	}
}

func (c *testCard) Focus() {
	c.mu.Lock()
	c.focuse++
	c.mu.Unlock()
	if c.timeline != nil {
		c.timeline.add("the card took the keyboard")
	}
}

func (c *testCard) Hide() { c.mu.Lock(); c.hides++; c.shown = false; c.mu.Unlock() }

func (c *testCard) Update(fn func()) {
	// The card has no frame of its own, so the update runs where it is
	// asked for: that is the state the next frame would draw.
	fn()
}

func (c *testCard) SetContentBounds(r mygo.Rectangle) { c.mu.Lock(); c.bounds = r; c.mu.Unlock() }

func (c *testCard) ContentBounds() mygo.Rectangle { c.mu.Lock(); defer c.mu.Unlock(); return c.bounds }

func (c *testCard) OnClosed(fn func()) func() { c.onClosed = fn; return func() {} }

func (c *testCard) OnBlur(fn func()) func() { c.onBlur = fn; return func() {} }

func (c *testCard) OnFocus(fn func()) func() { c.onFocus = fn; return func() {} }

// visible is whether the card is on screen.
func (c *testCard) visible() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.shown }

// hidden is how many times the card was taken off screen.
func (c *testCard) hidden() int { c.mu.Lock(); defer c.mu.Unlock(); return c.hides }

// blur and focus are what the platform reports.
func (c *testCard) blur() {
	c.mu.Lock()
	fn := c.onBlur
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *testCard) focus() {
	c.mu.Lock()
	fn := c.onFocus
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}
