package usecase

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

// The translate service is the tool's rules: which translator is used, what
// happens when nothing is configured, which language a text goes into, and
// what the user's own service was configured with. It is tested over fakes,
// with no network and no window.

// fakeTranslator is a translator whose answer the test decides.
type fakeTranslator struct {
	id         string
	descriptor domain.TranslatorDescriptor
	// baseURL is where a translator the user points at their own service
	// reads its address from, when the descriptor says the address is a
	// setting. The real one reads the same settings, and the use case must
	// not be able to tell the difference.
	baseURL func() string

	mu       sync.Mutex
	calls    int
	requests []domain.TranslationRequest
	result   domain.TranslationResult
	err      error
	gate     chan struct{}
}

func newFakeTranslator(id, displayName string) *fakeTranslator {
	return &fakeTranslator{
		id: id,
		descriptor: domain.TranslatorDescriptor{
			ID:             id,
			DisplayName:    displayName,
			CredentialKeys: []string{"apiKey"},
			DocsURL:        "https://example.com/keys",
			Endpoint:       "https://api.example.com/v1/chat/completions",
		},
		result: domain.TranslationResult{
			SourceText:     "hello",
			TranslatedText: "你好",
			SourceLang:     domain.LangEnglish,
			TargetLang:     domain.LangChinese,
			ProviderID:     id,
			CreatedAt:      time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC),
		},
	}
}

func (f *fakeTranslator) ID() string { return f.id }

func (f *fakeTranslator) Descriptor() domain.TranslatorDescriptor {
	descriptor := f.descriptor
	if descriptor.BaseURLSetting != "" && f.baseURL != nil {
		// The address is computed from the settings the way the real
		// translator computes it.
		if base := f.baseURL(); base != "" {
			descriptor.Endpoint = base + "/chat/completions"
		}
	}
	return descriptor
}

func (f *fakeTranslator) Translate(ctx context.Context, creds domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	f.mu.Lock()
	f.calls++
	f.requests = append(f.requests, req)
	gate, err, result := f.gate, f.err, f.result
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return domain.TranslationResult{}, ctx.Err()
		}
	}
	if err != nil {
		return domain.TranslationResult{}, err
	}
	result.SourceText = req.Text
	result.SourceLang = req.SourceLang
	result.TargetLang = req.TargetLang
	result.ProviderID = f.id
	return result, nil
}

func (f *fakeTranslator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeTranslator) lastRequest() domain.TranslationRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return domain.TranslationRequest{}
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeTranslator) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeTranslator) hold() chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = make(chan struct{})
	return f.gate
}

// fakeTranslators is a domain.TranslatorRegistry over the fakes.
type fakeTranslators struct {
	byID map[string]*fakeTranslator
}

func newFakeTranslators(replies ...*fakeTranslator) *fakeTranslators {
	registry := &fakeTranslators{byID: map[string]*fakeTranslator{}}
	for _, reply := range replies {
		registry.byID[reply.id] = reply
	}
	return registry
}

func (r *fakeTranslators) Translator(id string) (domain.Translator, error) {
	reply, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: translator %s", domain.ErrProviderNotFound, id)
	}
	return reply, nil
}

func (r *fakeTranslators) TranslatorIDs() []string {
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	return ids
}

// translateFixture is the service over the fakes, with the credential store
// and the settings a test may look at.
type translateFixture struct {
	translate *Translate
	device    *fakeDevice
	selection *fakeSelection
	tools     *fakeTools
	secrets   *memoryStore
	deepseek  *fakeTranslator
	custom    *fakeTranslator
	clock     *testClock
}

// fakeSelection is the platform's answer to what is selected, which a test
// decides: the text, nothing, or a platform that cannot say.
type fakeSelection struct {
	mu     sync.Mutex
	text   string
	err    error
	asked  int
	answer func() (string, error)
}

func (s *fakeSelection) SelectedText(context.Context) (string, error) {
	s.mu.Lock()
	s.asked++
	answer, text, err := s.answer, s.text, s.err
	s.mu.Unlock()
	if answer != nil {
		return answer()
	}
	return text, err
}

// asks is how many times the platform was asked what is selected.
func (s *fakeSelection) asks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked
}

// newTranslate builds the fixture with a key already stored for deepseek,
// which is the state a user reaches after configuring the panel.
func newTranslate(t *testing.T) *translateFixture {
	t.Helper()
	deepseek := newFakeTranslator("deepseek", "DeepSeek")
	customReply := newFakeTranslator("custom", "自定义")
	customReply.descriptor.BaseURLSetting = "custom.baseURL"
	customReply.descriptor.Endpoint = ""
	customReply.descriptor.AllowEmptyCredential = true

	device := &fakeDevice{}
	// The settings start as a fresh installation has them: the store fills
	// the translation defaults in when the file has no such key, so the fake
	// stands in for a store that has already been read once.
	tools := &fakeTools{settings: domain.ToolSettings{Translation: domain.TranslationDefaults()}}
	// The user's own translator reads its address from those settings.
	customReply.baseURL = func() string { return tools.settings.Translation.Custom.BaseURL }
	secrets := newMemoryStore()
	if err := secrets.Put(context.Background(),
		domain.CredentialKey("deepseek", "apiKey"), []byte("sk-test-0001"), domain.SecretMeta{}); err != nil {
		t.Fatalf("store the credential: %v", err)
	}
	clock := newTestClock()
	selection := &fakeSelection{err: domain.ErrSelectionUnavailable}
	service, err := NewTranslate(TranslateOptions{
		Translators: newFakeTranslators(deepseek, customReply),
		Secrets:     secrets,
		Tools:       tools,
		Selection:   selection,
		Clipboard:   device,
		Clock:       clock,
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	return &translateFixture{
		translate: service,
		device:    device,
		selection: selection,
		tools:     tools,
		secrets:   secrets,
		deepseek:  deepseek,
		custom:    customReply,
		clock:     clock,
	}
}

func TestTranslateNormal(t *testing.T) {
	f := newTranslate(t)
	f.device.set(textReading("A compiler converts source code."))
	result, err := f.translate.Translate(context.Background(), Request{FromSelection: true})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.SourceText != "A compiler converts source code." {
		t.Errorf("source = %q", result.SourceText)
	}
	if result.ProviderID != "deepseek" || result.ProviderName != "DeepSeek" {
		t.Errorf("provider = %q / %q", result.ProviderID, result.ProviderName)
	}
	if result.SourceLang != domain.LangEnglish || result.TargetLang != domain.LangChinese {
		t.Errorf("languages = %q -> %q", result.SourceLang, result.TargetLang)
	}
	if result.At.IsZero() {
		t.Error("the result has no time")
	}
	if f.deepseek.count() != 1 {
		t.Errorf("the translator was asked %d times", f.deepseek.count())
	}
	// The clipboard is read once, and only because the request asked for it.
	if f.device.readCount() != 1 {
		t.Errorf("the clipboard was read %d times", f.device.readCount())
	}
}

// What the user has selected is what the tool translates: the platform is
// asked first, and the clipboard — which holds what was copied last, and may
// be something else entirely — is not consulted at all when it can answer.
func TestTranslatePrefersTheSelection(t *testing.T) {
	f := newTranslate(t)
	f.device.set(textReading("what was copied an hour ago"))
	f.selection.err, f.selection.text = nil, "コンパイラはソフトです。"

	result, err := f.translate.Translate(context.Background(), Request{FromSelection: true})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.SourceText != "コンパイラはソフトです。" {
		t.Errorf("source = %q", result.SourceText)
	}
	if f.selection.asks() != 1 {
		t.Errorf("the platform was asked %d times what is selected", f.selection.asks())
	}
	if f.device.readCount() != 0 {
		t.Errorf("the clipboard was read although the selection answered")
	}
}

// A platform that cannot say what is selected, one that says nothing is
// selected, one whose call failed and a build with no such device at all are
// the same thing to the user: the clipboard answers, as it did before there
// was a selection to read.
func TestTranslateFallsBackToTheClipboard(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*translateFixture)
	}{
		{"the platform cannot say", func(f *translateFixture) {
			f.selection.err = domain.ErrSelectionUnavailable
		}},
		{"the platform says nothing is selected", func(f *translateFixture) {
			f.selection.err, f.selection.text = nil, "   "
		}},
		{"the platform call failed", func(f *translateFixture) {
			f.selection.err = errors.New("the provider answered nothing")
		}},
		{"there is no selection device", func(f *translateFixture) {
			f.translate.selection = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranslate(t)
			f.device.set(textReading("A compiler converts source code."))
			tc.prepare(f)

			result, err := f.translate.Translate(context.Background(), Request{FromSelection: true})
			if err != nil {
				t.Fatalf("Translate failed: %v", err)
			}
			if result.SourceText != "A compiler converts source code." {
				t.Errorf("source = %q", result.SourceText)
			}
			if f.device.readCount() != 1 {
				t.Errorf("the clipboard was read %d times", f.device.readCount())
			}
		})
	}
}

// A request that carries its own text asks the platform nothing: the text the
// caller has is the text to translate.
func TestTranslateDoesNotAskForASelectionWithItsOwnText(t *testing.T) {
	f := newTranslate(t)
	if _, err := f.translate.Translate(context.Background(), Request{Text: "hello"}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if f.selection.asks() != 0 {
		t.Errorf("the platform was asked %d times for a request with its own text", f.selection.asks())
	}
}

// A Chinese selection is translated into English rather than being a dead
// end, and the language is decided from the text itself.
func TestTranslatePicksTheTargetFromTheSource(t *testing.T) {
	f := newTranslate(t)
	result, err := f.translate.Translate(context.Background(), Request{Text: "编译器把源代码转换成机器语言。"})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.SourceLang != domain.LangChinese {
		t.Errorf("source language = %q", result.SourceLang)
	}
	if result.TargetLang != domain.LangEnglish {
		t.Errorf("target language = %q, want the other direction", result.TargetLang)
	}
	if f.device.readCount() != 0 {
		t.Errorf("the clipboard was read for a request that carried its own text")
	}
}

// The annotation is asked for by the request that wants it, and by no other:
// the translation goes out asking for a paragraph, and the switches decide
// what the request for the words asks for.
func TestTheAnnotationIsItsOwnRequest(t *testing.T) {
	f := newTranslate(t)
	f.tools.settings.Translation = domain.TranslationDefaults()

	// A translation asks for no annotation, whatever the switches say.
	if err := f.translate.SetFurigana(context.Background(), true); err != nil {
		t.Fatalf("SetFurigana failed: %v", err)
	}
	if _, err := f.translate.Translate(context.Background(), Request{Text: "コンパイラ"}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if request := f.deepseek.lastRequest(); request.Annotation.Wants() {
		t.Errorf("the translation asked for the annotation: %+v", request.Annotation)
	}

	// The request for the words carries the switches, which is what makes
	// them mean anything.
	if err := f.translate.SetLoanwordGloss(context.Background(), false); err != nil {
		t.Fatalf("SetLoanwordGloss failed: %v", err)
	}
	if _, err := f.translate.Translate(context.Background(), Request{Text: "コンパイラ", Annotate: true}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	request := f.deepseek.lastRequest()
	if !request.Annotation.Furigana || request.Annotation.LoanwordGloss {
		t.Errorf("the annotation request does not carry the switches: %+v", request.Annotation)
	}
	if request.Text != "コンパイラ" {
		t.Errorf("the annotation request is for %q", request.Text)
	}
	if request.SourceLang != domain.LangJapanese {
		t.Errorf("source language = %q", request.SourceLang)
	}
}

// The line breaks the user copied survive the request: Windows hands a
// selection over with \r\n, the paragraphs the window draws are split on \n,
// and neither a carriage return nor a lost line is something to translate.
func TestTheCopiedLineBreaksSurvive(t *testing.T) {
	f := newTranslate(t)
	text := "一行目\r\n二行目\r三行目\n\n四行目"
	if _, err := f.translate.Translate(context.Background(), Request{Text: text}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	want := "一行目\n二行目\n三行目\n\n四行目"
	if got := f.deepseek.lastRequest().Text; got != want {
		t.Errorf("the text sent is %q, want %q", got, want)
	}
}

// A tool that is switched off says so, and does not read the clipboard or
// ask a service.
func TestTranslateWhenTheToolIsOff(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.SetEnabled(context.Background(), false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{FromSelection: true})
	if !errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Fatalf("error = %v, want ErrTranslateUnavailable", err)
	}
	if !strings.Contains(err.Error(), "已关闭") {
		t.Errorf("the error does not say the tool is off: %v", err)
	}
	if f.deepseek.count() != 0 || f.device.readCount() != 0 {
		t.Error("a switched-off tool did work")
	}
}

// A tool with nothing configured says where to go, and does not fail: the
// window shows "去配置" rather than an error.
func TestTranslateWithNothingConfigured(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.SaveSettings(context.Background(), func(s *domain.TranslationSettings) {
		s.Provider = "nobody"
	}); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Fatalf("error = %v, want ErrTranslateUnavailable", err)
	}
	if !strings.Contains(err.Error(), "还没有配置") {
		t.Errorf("the error does not offer configuration: %v", err)
	}
}

// A missing credential is its own answer: no request is worth making with a
// key that is not there.
func TestTranslateWithoutACredential(t *testing.T) {
	f := newTranslate(t)
	if err := f.secrets.Delete(context.Background(), domain.CredentialKey("deepseek", "apiKey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if f.deepseek.count() != 0 {
		t.Error("the translator was asked without a credential")
	}
	// The error names the credential and never its value.
	if !strings.Contains(err.Error(), "deepseek.apiKey") {
		t.Errorf("the error does not name the credential: %v", err)
	}
}

// An empty clipboard is a user problem with its own words: they have to copy
// something first.
func TestTranslateWithAnEmptyClipboard(t *testing.T) {
	f := newTranslate(t)
	_, err := f.translate.Translate(context.Background(), Request{FromSelection: true})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "复制") {
		t.Errorf("the error does not tell the user what to do: %v", err)
	}
	if f.deepseek.count() != 0 {
		t.Error("an empty clipboard reached the translator")
	}
}

// A clipboard holding a file list or an image is not a text to translate.
func TestTranslateWithANonTextClipboard(t *testing.T) {
	for _, reading := range []domain.ClipContent{
		{Kind: domain.ClipKindFile, Files: []string{"/tmp/a.txt"}},
		{Kind: domain.ClipKindImage, ImageBytes: 1024},
	} {
		f := newTranslate(t)
		f.device.set(reading)
		_, err := f.translate.Translate(context.Background(), Request{FromSelection: true})
		if !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("kind %s = %v, want ErrInvalidArgument", reading.Kind, err)
		}
		if f.deepseek.count() != 0 {
			t.Errorf("kind %s reached the translator", reading.Kind)
		}
	}
}

// A service that fails is reported as a failure, never as an empty
// translation.
func TestTranslateReportsServiceFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"unauthorized", fmt.Errorf("%w: deepseek.apiKey was refused", domain.ErrProviderUnauthorized)},
		{"rate limited", fmt.Errorf("%w: too many requests", domain.ErrProviderRateLimited)},
		{"unavailable", fmt.Errorf("%w: no answer", domain.ErrProviderUnavailable)},
		{"timeout", fmt.Errorf("%w: took too long", domain.ErrProviderTimeout)},
		{"bad response", fmt.Errorf("%w: the answer is not JSON", domain.ErrProviderBadResponse)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranslate(t)
			f.deepseek.fail(tc.err)
			result, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if result.TranslatedText != "" {
				t.Errorf("a failure produced a translation: %+v", result)
			}
		})
	}
}

// A cancelled context stops the request: a user who closed the window should
// not wait out a service.
func TestTranslateHonoursCancellation(t *testing.T) {
	f := newTranslate(t)
	gate := f.deepseek.hold()
	defer close(gate)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.translate.Translate(ctx, Request{Text: "hello"})
		done <- err
	}()
	waitFor(t, "the translator to be asked", func() bool { return f.deepseek.count() == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled translation did not return")
	}
}

// Every switch is written through the same path, and what is stored is what
// the settings window read back.
func TestSettingsRoundTrip(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()
	f.tools.settings.Translation = domain.TranslationDefaults()

	if err := f.translate.SetProvider(ctx, "custom"); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	if err := f.translate.SetPinned(ctx, true); err != nil {
		t.Fatalf("SetPinned failed: %v", err)
	}
	if err := f.translate.SetCloseDelay(ctx, 1200*time.Millisecond); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
	}
	if err := f.translate.SetCustomTranslator(ctx, domain.CustomTranslatorSettings{
		BaseURL:     "api.example.com/v1/",
		DisplayName: "  本地 Ollama  ",
	}); err != nil {
		t.Fatalf("SetCustomTranslator failed: %v", err)
	}

	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.ProviderOr() != "custom" {
		t.Errorf("provider = %q", settings.ProviderOr())
	}
	if !settings.Pinned {
		t.Error("pinned was not stored")
	}
	if settings.CloseDelayOr() != 1200*time.Millisecond {
		t.Errorf("close delay = %s", settings.CloseDelayOr())
	}
	if settings.Custom.BaseURL != "https://api.example.com/v1" {
		t.Errorf("custom base URL = %q, want it cleaned", settings.Custom.BaseURL)
	}
	if settings.Custom.DisplayName != "本地 Ollama" {
		t.Errorf("custom name = %q, want it trimmed", settings.Custom.DisplayName)
	}
	// The clipboard switches are untouched: the two tools share one file,
	// and writing one must not lose the other.
	f.tools.settings.ClipboardMonitoring = true
	if _, err := f.translate.Settings(); err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if !f.tools.settings.ClipboardMonitoring {
		t.Error("saving the translation settings lost the clipboard switches")
	}
}

// A provider no translator answers to is refused rather than stored.
func TestSetProviderRefusesAnUnknownID(t *testing.T) {
	f := newTranslate(t)
	err := f.translate.SetProvider(context.Background(), "nobody")
	if !errors.Is(err, domain.ErrProviderNotFound) {
		t.Fatalf("error = %v, want ErrProviderNotFound", err)
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.ProviderOr() != domain.DefaultTranslationProvider {
		t.Errorf("provider = %q, want the stored one kept", settings.ProviderOr())
	}
}

// A switch that cannot be written is reported, and the value the
// application runs on is the one on disk rather than the one that was meant
// to be written.
func TestSettingsReportAWriteFailure(t *testing.T) {
	f := newTranslate(t)
	f.tools.settings.Translation = domain.TranslationDefaults()
	f.tools.err = errors.New("the disk is full")
	err := f.translate.SetPinned(context.Background(), true)
	if err == nil {
		t.Fatal("a failed write was reported as success")
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.Pinned {
		t.Error("the switch is on although nothing was written")
	}
}

// The settings window asks for the list of translators, what each is
// configured with, and whether it can be used now.
func TestProvidersDescribeWhatIsUsable(t *testing.T) {
	f := newTranslate(t)
	f.tools.settings.Translation = domain.TranslationDefaults()
	views, err := f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %+v, want both translators", views)
	}
	// The one in use comes first.
	if views[0].ID != "deepseek" {
		t.Errorf("the first translator is %q, want the one in use", views[0].ID)
	}
	if !views[0].Configured {
		t.Errorf("the configured translator is not reported as configured: %+v", views[0])
	}
	if views[0].CredentialKey != "apiKey" || !views[0].NeedsCredential {
		t.Errorf("view = %+v", views[0])
	}
	// The user's own service is not configured until an address is there.
	if views[1].ID != "custom" || views[1].Configured {
		t.Errorf("custom = %+v, want it unconfigured before an address", views[1])
	}
	if views[1].NeedsCredential {
		t.Error("a local service is reported as needing a key")
	}
	if views[1].Endpoint != "" {
		t.Errorf("endpoint = %q, want it empty before an address", views[1].Endpoint)
	}

	if err := f.translate.SetCustomTranslator(context.Background(), domain.CustomTranslatorSettings{
		BaseURL: "https://api.example.com/v1",
	}); err != nil {
		t.Fatalf("SetCustomTranslator failed: %v", err)
	}
	views, err = f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	var custom TranslatorView
	for _, view := range views {
		if view.ID == "custom" {
			custom = view
		}
	}
	if !custom.Configured {
		t.Error("a service with an address is still reported as unconfigured")
	}
	if custom.Endpoint != "https://api.example.com/v1/chat/completions" {
		t.Errorf("endpoint = %q", custom.Endpoint)
	}
}

// A credential that is not stored makes the translator unusable, which is
// what the window shows.
func TestProvidersFollowTheCredential(t *testing.T) {
	f := newTranslate(t)
	if err := f.secrets.Delete(context.Background(), domain.CredentialKey("deepseek", "apiKey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	views, err := f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	for _, view := range views {
		if view.ID == "deepseek" && view.Configured {
			t.Error("a translator with no key is reported as configured")
		}
	}
}

// Testing a configuration translates a short text: what the user typed is
// used, and nothing is stored.
func TestTestTranslator(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	// What is stored is tested when the user typed nothing.
	if err := f.translate.TestTranslator(ctx, "deepseek", nil); err != nil {
		t.Fatalf("TestTranslator failed: %v", err)
	}
	request := f.deepseek.lastRequest()
	if !strings.Contains(request.Text, "connection test") {
		t.Errorf("the test translated %q, want a test sentence", request.Text)
	}
	if request.Annotation.Wants() {
		t.Errorf("the test asked for an annotation: %+v", request.Annotation)
	}

	// What the user typed is tested when they did type something.
	before := f.deepseek.count()
	if err := f.translate.TestTranslator(ctx, "deepseek", map[string]string{"apiKey": "sk-typed-0002"}); err != nil {
		t.Fatalf("TestTranslator failed: %v", err)
	}
	if f.deepseek.count() != before+1 {
		t.Errorf("the translator was asked %d times, want one more", f.deepseek.count())
	}

	// A service with no address has nothing to test: the caller is told to
	// configure it rather than waiting for a request that cannot happen.
	if err := f.translate.TestTranslator(ctx, "custom", nil); err == nil {
		t.Error("testing a service with no address succeeded")
	}

	// A translator nobody registered is not something to test.
	if err := f.translate.TestTranslator(ctx, "nobody", nil); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("error = %v, want ErrProviderNotFound", err)
	}

	// A service that refuses the key says so.
	f.deepseek.fail(fmt.Errorf("%w: deepseek.apiKey was refused", domain.ErrProviderUnauthorized))
	if err := f.translate.TestTranslator(ctx, "deepseek", nil); !errors.Is(err, domain.ErrProviderUnauthorized) {
		t.Errorf("error = %v, want ErrProviderUnauthorized", err)
	}
}

// A translator that needs a key which is neither stored nor typed is a
// missing credential, and it is never asked.
func TestTestTranslatorWithoutAKey(t *testing.T) {
	f := newTranslate(t)
	if err := f.secrets.Delete(context.Background(), domain.CredentialKey("deepseek", "apiKey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	err := f.translate.TestTranslator(context.Background(), "deepseek", nil)
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if f.deepseek.count() != 0 {
		t.Error("the translator was asked without a credential")
	}
}

// The test never stores what it was given: testing a key is not saving it.
func TestTestTranslatorDoesNotStore(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.TestTranslator(context.Background(), "deepseek", map[string]string{"apiKey": "sk-typed-0002"}); err != nil {
		t.Fatalf("TestTranslator failed: %v", err)
	}
	secret, err := f.secrets.Get(context.Background(), domain.CredentialKey("deepseek", "apiKey"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if secret.Text() != "sk-test-0001" {
		t.Errorf("the stored credential is %q, want the original", secret.Text())
	}
}

// A registry with nothing in it is a tool that says "go and configure one",
// which is what a fresh installation with no translator reaches.
func TestTranslateWithoutAnyTranslator(t *testing.T) {
	service, err := NewTranslate(TranslateOptions{
		Translators: newFakeTranslators(),
		Secrets:     newMemoryStore(),
		Tools:       &fakeTools{},
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	if _, err := service.Translate(context.Background(), Request{Text: "hello"}); !errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Errorf("error = %v, want ErrTranslateUnavailable", err)
	}
	views, err := service.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("views = %+v, want none", views)
	}
}

func TestNewTranslateRequiresItsDependencies(t *testing.T) {
	if _, err := NewTranslate(TranslateOptions{Secrets: newMemoryStore()}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("without a registry = %v", err)
	}
	if _, err := NewTranslate(TranslateOptions{Translators: newFakeTranslators()}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("without a secret store = %v", err)
	}
}
