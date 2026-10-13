package app

import (
	"context"
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
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: testClipboard(t, nil, nil),
		Version:   version,
		Quit:      func() {},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return state
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
	if d.reading.Kind == "" {
		return domain.ClipContent{}, domain.ErrUnsupportedClipboardFormat
	}
	if d.reading.CapturedAt.IsZero() {
		d.reading = d.stamp(d.reading)
	}
	return d.reading, nil
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
