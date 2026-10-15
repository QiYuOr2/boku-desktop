package app

import (
	"context"
	"errors"
	"fmt"
	"image"
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

// testState builds the presentation state over the fakes. version is what
// the footer shows; the credential store starts empty unless it is given.
func testState(t testing.TB, provider *testProvider, store *testStore, version string) *State {
	t.Helper()
	state, _, _ := testStateWith(t, provider, store, version)
	return state
}

// testStateWith is testState with the translation fixture handed back, for
// the tests that drive the translation tool: what it answers, what failed,
// and what the shortcut registrar was told.
func testStateWith(t testing.TB, provider *testProvider, store *testStore, version string) (*State, *testTranslateFixture, *testShortcuts) {
	t.Helper()
	if store == nil {
		store = newTestStore()
	}
	panels, err := usecase.NewPanels(
		testRegistry{provider: provider, adapter: testAdapter{provider: provider}},
		store,
		nil,
		[]usecase.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	secrets, err := usecase.NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	device := &testDevice{}
	translation := testTranslate(t, store, device)
	shortcuts := &testShortcuts{}
	state, err := New(mygo.App, Deps{
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: testClipboard(t, nil, nil),
		Translate: translation.service,
		Shortcuts: shortcuts,
		Version:   version,
		Quit:      func() {},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return state, translation, shortcuts
}

// testTranslateFixture is the translation tool with the fakes it was built
// over, so that a test can change what it answers.
type testTranslateFixture struct {
	service   *usecase.Translate
	deepseek  *testTranslator
	custom    *testTranslator
	tools     *testToolStore
	device    *testDevice
	selection *testSelection
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
	custom := &testTranslator{id: "custom", name: "自定义", descriptor: domain.TranslatorDescriptor{
		BaseURLSetting:       "custom.baseURL",
		AllowEmptyCredential: true,
	}}
	// The store starts as a fresh installation has it: the file store fills
	// the translation defaults in when the file has no such key, so the fake
	// stands in for a store that has already been read once.
	tools := &testToolStore{settings: domain.ToolSettings{Translation: domain.TranslationDefaults()}}
	selection := &testSelection{timeline: timeline}
	service, err := usecase.NewTranslate(usecase.TranslateOptions{
		Translators: testTranslators{reply, custom},
		Secrets:     store,
		Tools:       tools,
		// The platform's own answer is faked here, so that a test can say
		// what the user has selected in another application — and, through
		// the timeline, when it was read.
		Selection: selection,
		Clipboard: device,
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	return &testTranslateFixture{
		service:   service,
		deepseek:  reply,
		custom:    custom,
		tools:     tools,
		device:    device,
		selection: selection,
		timeline:  timeline,
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
	descriptor.DisplayName = t.name
	if len(descriptor.CredentialKeys) == 0 {
		descriptor.CredentialKeys = []string{"apiKey"}
	}
	if descriptor.DocsURL == "" {
		descriptor.DocsURL = "https://example.com/keys"
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
	result.ProviderID = t.id
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

// testTranslators is a domain.TranslatorRegistry over the fakes.
type testTranslators []*testTranslator

func (r testTranslators) Translator(id string) (domain.Translator, error) {
	for _, reply := range r {
		if reply.id == id {
			return reply, nil
		}
	}
	return nil, fmt.Errorf("%w: translator %s", domain.ErrProviderNotFound, id)
}

func (r testTranslators) TranslatorIDs() []string {
	ids := make([]string, 0, len(r))
	for _, reply := range r {
		ids = append(ids, reply.id)
	}
	sort.Strings(ids)
	return ids
}

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
func testClipboard(t testing.TB, device *testDevice, tools *testToolStore) *usecase.Clipboard {
	t.Helper()
	if device == nil {
		device = &testDevice{}
	}
	if tools == nil {
		tools = &testToolStore{}
	}
	// One clock for the service and for what the device hands back: the
	// history is ordered by the times the device reports.
	clock := &testClock{at: time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)}
	device.now = clock.now
	clipboard, err := usecase.NewClipboard(usecase.ClipboardOptions{
		Device: device,
		Store:  &testClipStore{},
		Tools:  tools,
		Clock:  clock,
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

// testToolStore is a domain.ToolStore in memory.
type testToolStore struct {
	mu       sync.Mutex
	settings domain.ToolSettings
}

func (s *testToolStore) Load(context.Context) (domain.ToolSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, nil
}

func (s *testToolStore) Save(_ context.Context, settings domain.ToolSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	return nil
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
		nil,
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	return panels
}

// loaded refreshes the panel once, so that the view has a snapshot to draw.
func loaded(t testing.TB, state *State) {
	t.Helper()
	if _, err := state.panelService.Refresh(context.Background(), deepseek.PanelID); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
}

// findText returns where one text of the frame sits. The tester matches a
// text in full, so a label and the value beside it can both be found.
func findText(t *testing.T, tt *ui.Tester, want string) ui.Rect {
	t.Helper()
	rect, ok := tt.Find(want)
	if !ok {
		t.Fatalf("no text is %q; the frame shows %q", want, tt.Texts())
	}
	return rect
}

// findTextPrefix returns where the text starting with prefix sits. It is for
// the lines whose value the test does not decide, such as the clock.
func findTextPrefix(t *testing.T, tt *ui.Tester, prefix string) ui.Rect {
	t.Helper()
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, prefix) {
			return findText(t, tt, text)
		}
	}
	t.Fatalf("no text starts with %q; the frame shows %q", prefix, tt.Texts())
	return ui.Rect{}
}

// textWithPrefix returns the whole of the text starting with prefix, for a
// test that wants to read what a line says rather than where it is.
func textWithPrefix(texts []string, prefix string) (string, bool) {
	for _, text := range texts {
		if strings.HasPrefix(text, prefix) {
			return text, true
		}
	}
	return "", false
}

// fetchedClock is the clock the quick window writes for its first panel: the
// moment those numbers were taken, as the block shows it. The design writes
// the clock alone beside the refresh action, so a test that looks for that
// line has to know which moment it was.
func fetchedClock(t *testing.T, state *State) string {
	t.Helper()
	listed := state.panelService.List()
	if len(listed) == 0 {
		t.Fatal("the state has no panel to take a clock from")
	}
	view := state.panelService.View(listed[0].ID)
	if !view.HasSnapshot {
		t.Fatal("the panel has no snapshot to take a clock from")
	}
	return clock(view.FetchedAt)
}

func contains(texts []string, want string) bool {
	for _, text := range texts {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

// dividerRows returns the y of every row of the frame that is drawn edge to
// edge in one color, which is how a divider looks in a rendered frame.
func dividerRows(img *image.RGBA, c ui.Color, width int) []int {
	var rows []int
	for y := 0; y < img.Rect.Dy(); y++ {
		row := 0
		for x := 0; x < width; x++ {
			i := img.PixOffset(x, y)
			if img.Pix[i] == c.R && img.Pix[i+1] == c.G && img.Pix[i+2] == c.B {
				row++
			}
		}
		if row == width {
			rows = append(rows, y)
		}
	}
	return rows
}

// clipboardState is a state whose clipboard tool the test owns.
func clipboardState(t testing.TB, device *testDevice, tools *testToolStore) *State {
	t.Helper()
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.clipboard = testClipboard(t, device, tools)
	loaded(t, state)
	return state
}
