package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"

	"boku/internal/clipboard"
	contractdeepseek "boku/internal/contract/deepseek"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/panel"
	"boku/internal/panel/deepseek"
	"boku/internal/platform/httpc"
	"boku/internal/settings"
	"boku/internal/translate"
	customtranslator "boku/internal/translate/custom"
	deepseektranslator "boku/internal/translate/deepseek"
	"boku/internal/translate/free"
	"boku/internal/translate/openai"
)

type testStore struct {
	*credential.Store
	t testing.TB
}

func newTestStore(t testing.TB) *testStore {
	t.Helper()
	store, err := credential.Open(filepath.Join(t.TempDir(), credential.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return &testStore{Store: store, t: t}
}

func (s *testStore) set(key, value string) {
	s.t.Helper()
	if err := s.Put(context.Background(), key, []byte(value), domain.SecretMeta{}); err != nil {
		s.t.Fatal(err)
	}
}

func withCredential(store *testStore) *testStore {
	store.set(domain.CredentialKey("deepseek", contractdeepseek.APIKeyCredential), "sk-test-0001")
	return store
}

type testProvider struct {
	*deepseek.Provider
	mu    sync.Mutex
	calls int
	gate  chan struct{}
}

func newTestProvider(t testing.TB) *testProvider {
	t.Helper()
	payload, err := os.ReadFile("../../testdata/providers/deepseek/balance_ok.json")
	if err != nil {
		t.Fatal(err)
	}
	provider := &testProvider{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.mu.Lock()
		provider.calls++
		gate := provider.gate
		provider.mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	provider.Provider = deepseek.NewWithBaseURL(httpc.New(httpc.Options{HTTPClient: server.Client()}), server.URL)
	return provider
}

func (p *testProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *testProvider) hold() chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gate = make(chan struct{})
	return p.gate
}

func testPanelRegistry(t testing.TB, provider domain.Provider) *panel.Registry {
	t.Helper()
	registry := panel.NewRegistry()
	if err := registry.Register(provider, deepseek.NewAdapter()); err != nil {
		t.Fatal(err)
	}
	return registry
}

func testState(t testing.TB, provider *testProvider, store *testStore, version string) *State {
	t.Helper()
	state, _ := testStateWith(t, provider, store, version)
	return state
}

func testStateWith(t testing.TB, provider *testProvider, store *testStore, version string) (*State, *testTranslateFixture) {
	t.Helper()
	panels, err := panel.NewPanels(testPanelRegistry(t, provider), store, panel.NewSection(), nil,
		[]panel.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := credential.NewSecrets(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture := testTranslate(t, store)
	state, err := New(mygo.App, Deps{
		Panels: panels, Secrets: secrets, Clipboard: testClipboard(t), Translate: fixture.service,
		Version: version, Quit: func() {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return state, fixture
}

type testTranslateFixture struct {
	service    *translate.Translate
	registry   *translate.Registry
	addService func(translate.CustomSettings) string
}

func testTranslate(t testing.TB, store *testStore) *testTranslateFixture {
	t.Helper()
	section := translate.NewSection()
	tools, err := settings.New(filepath.Join(t.TempDir(), "settings.json"), section)
	if err != nil {
		t.Fatal(err)
	}
	registry := translate.NewRegistry()
	for _, backend := range []domain.Translator{free.NewMicrosoft(nil), free.NewBilibili(nil), deepseektranslator.New(nil)} {
		if err := registry.Register(&testTranslator{Translator: backend}); err != nil {
			t.Fatal(err)
		}
	}
	service, err := translate.NewTranslate(translate.TranslateOptions{Translators: registry, Secrets: store, Section: section, Settings: tools})
	if err != nil {
		t.Fatal(err)
	}
	return &testTranslateFixture{
		service:  service,
		registry: registry,
		addService: func(config translate.CustomSettings) string {
			id, err := service.AddCustomTranslator(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			backend := customtranslator.New(nil, id, func() openai.CustomService {
				current, _ := section.Current().CustomService(id)
				return openai.CustomService{BaseURL: current.BaseURL, DisplayName: current.DisplayName, Model: current.Model, SupportsImage: current.SupportsImage}
			})
			if err := registry.Register(backend); err != nil {
				t.Fatal(err)
			}
			return id
		},
	}
}

// Only the network response is controlled; descriptors come from the real backends.
type testTranslator struct {
	domain.Translator
	mu       sync.Mutex
	result   domain.TranslationResult
	err      error
	requests []domain.TranslationRequest
}

func (t *testTranslator) Translate(_ context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = append(t.requests, req)
	if t.err != nil {
		return domain.TranslationResult{}, t.err
	}
	result := t.result
	result.SourceText, result.SourceLang, result.TargetLang, result.ProviderID = req.Text, req.SourceLang, req.TargetLang, t.ID()
	return result, nil
}

func (t *testTranslator) answer(result domain.TranslationResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result = result
}

func (t *testTranslator) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

func (t *testTranslator) requestCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

func (f *testTranslateFixture) translator(t *testing.T, id string) *testTranslator {
	t.Helper()
	backend, err := f.registry.Translator(id)
	if err != nil {
		t.Fatal(err)
	}
	return backend.(*testTranslator)
}

type testDevice struct{}

func (*testDevice) Read(context.Context) (domain.ClipContent, error) {
	return domain.ClipContent{}, nil
}
func (*testDevice) WriteText(context.Context, string) error    { return nil }
func (*testDevice) WriteFiles(context.Context, []string) error { return nil }
func (*testDevice) WriteImage(context.Context, []byte) error   { return nil }

func testClipboard(t testing.TB) *clipboard.Clipboard {
	t.Helper()
	history, err := clipboard.NewHistory(filepath.Join(t.TempDir(), "clipboard_history.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := clipboard.NewClipboard(clipboard.ClipboardOptions{Device: &testDevice{}, Store: history, Section: clipboard.NewSection()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	return service
}

func translateState(t testing.TB) (*State, *testTranslateFixture) {
	t.Helper()
	return testStateWith(t, newTestProvider(t), withCredential(newTestStore(t)), "0.1.0")
}

func contains(texts []string, want string) bool {
	for _, text := range texts {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}
