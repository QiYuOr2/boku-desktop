package translate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	contractfree "boku/internal/contract/freetranslate"
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

func (f *fakeTranslator) TranslateImage(ctx context.Context, creds domain.Credentials, image []byte, target domain.Language) (domain.TranslationResult, error) {
	if len(image) == 0 {
		return domain.TranslationResult{}, domain.ErrInvalidArgument
	}
	return f.Translate(ctx, creds, domain.TranslationRequest{Text: "image probe", SourceLang: domain.LangEnglish, TargetLang: target})
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

// TranslatorIDs returns the registered ids, sorted, exactly as the real
// registry does: the settings window draws its rows in this order, and a fake
// that handed them back in map order would let a test pass while the shipped
// list rearranged itself between frames.
func (r *fakeTranslators) TranslatorIDs() []string {
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
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
	microsoft *fakeTranslator
	bilibili  *fakeTranslator
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
	// The free engines: what a fresh installation starts on, and what these
	// tests therefore have to cover the way the shipped build has it — two
	// translators that ask for no credential at all.
	microsoft := newFakeTranslator(contractfree.MicrosoftEdge, "微软 Edge 翻译")
	microsoft.descriptor.AllowEmptyCredential = true
	microsoft.descriptor.DocsURL = "https://example.com/free"
	microsoft.descriptor.Endpoint = "edge.microsoft.com/translate/translatetext"
	bilibili := newFakeTranslator(contractfree.Bilibili, "B站 Index-Translate")
	bilibili.descriptor.AllowEmptyCredential = true
	bilibili.descriptor.DocsURL = "https://example.com/free"
	bilibili.descriptor.Endpoint = "index-translate.bilibili.com/v1/chat/completions"

	device := &fakeDevice{}
	// The settings start as a fresh installation has them: the store fills
	// the translation defaults in when the file has no such key, so the fake
	// stands in for a store that has already been read once.
	tools := newFakeTools()
	// The user's own translator reads its address from those settings.
	customReply.baseURL = func() string {
		service, _ := tools.section.Current().CustomService(customReply.id)
		return service.BaseURL
	}
	secrets := newMemoryStore()
	if err := secrets.Put(context.Background(),
		domain.CredentialKey("deepseek", "apiKey"), []byte("sk-test-0001"), domain.SecretMeta{}); err != nil {
		t.Fatalf("store the credential: %v", err)
	}
	clock := newTestClock()
	selection := &fakeSelection{err: domain.ErrSelectionUnavailable}
	service, err := NewTranslate(TranslateOptions{
		Translators: newFakeTranslators(deepseek, customReply, microsoft, bilibili),
		Secrets:     secrets,
		Section:     tools.section,
		Settings:    tools,
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
		microsoft: microsoft,
		bilibili:  bilibili,
		clock:     clock,
	}
}

// useDeepSeek picks the paid translator, which is the state a user reaches
// after configuring their own key. The tests that are about credentials, or
// about a service that fails, are about that translator and say so: the
// default is a free engine, and a test that assumed otherwise would be
// testing the wrong thing.
func useDeepSeek(t *testing.T, f *translateFixture) {
	t.Helper()
	if err := f.translate.SetProvider(context.Background(), "deepseek"); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
}

// source reads what the user chose, the way the window does before the card
// takes the keyboard: the platform first, the clipboard when it cannot say.
func source(t *testing.T, f *translateFixture) string {
	t.Helper()
	text, err := f.translate.Source(context.Background())
	if err != nil {
		t.Fatalf("Source failed: %v", err)
	}
	return text
}

func TestTranslateNormal(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	f.device.set(textReading("A compiler converts source code."))
	result, err := f.translate.Translate(context.Background(), Request{Text: source(t, f)})
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
	// The clipboard answers once, and once only: the read that comes before
	// the request. The request itself asks no device anything.
	if f.device.readCount() != 1 {
		t.Errorf("the clipboard was read %d times", f.device.readCount())
	}
}

// What the user has selected is what the tool translates: the platform is
// asked first, and the clipboard — which holds what was copied last, and may
// be something else entirely — is not consulted at all when it can answer.
func TestSourcePrefersTheSelection(t *testing.T) {
	f := newTranslate(t)
	f.device.set(textReading("what was copied an hour ago"))
	f.selection.err, f.selection.text = nil, "コンパイラはソフトです。"

	if got := source(t, f); got != "コンパイラはソフトです。" {
		t.Errorf("source = %q", got)
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
func TestSourceFallsBackToTheClipboard(t *testing.T) {
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

			if got := source(t, f); got != "A compiler converts source code." {
				t.Errorf("source = %q", got)
			}
			if f.device.readCount() != 1 {
				t.Errorf("the clipboard was read %d times", f.device.readCount())
			}
		})
	}
}

// A clipboard that cannot be read at all is a fault rather than an empty
// answer: the window shows it, rather than telling the user to copy something
// they may already have copied.
func TestSourceReportsAClipboardThatCannotBeRead(t *testing.T) {
	f := newTranslate(t)
	f.device.fail(errors.New("the clipboard is held by another application"))
	if _, err := f.translate.Source(context.Background()); err == nil {
		t.Fatal("Source answered nothing for a clipboard it could not read")
	}
}

// A request that carries its own text asks the platform nothing: the text the
// caller has is the text to translate.
func TestTranslateDoesNotAskForASelectionWithItsOwnText(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	if _, err := f.translate.Translate(context.Background(), Request{Text: "hello"}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if f.selection.asks() != 0 {
		t.Errorf("the platform was asked %d times for a request with its own text", f.selection.asks())
	}
	if f.device.readCount() != 0 {
		t.Errorf("the clipboard was read %d times for a request with its own text", f.device.readCount())
	}
}

// A Chinese selection is translated into English rather than being a dead
// end, and the language is decided from the text itself.
func TestTranslatePicksTheTargetFromTheSource(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
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
	f.tools.section.Set(Defaults())
	useDeepSeek(t, f)

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
	useDeepSeek(t, f)
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
//
// It is its own error rather than "translation unavailable": the two are
// different sentences to the user, and the window draws them differently. A
// switched-off tool is not a fault — the user asked for it — while a tool with
// nowhere to send a text is one they have to fix.
func TestTranslateWhenTheToolIsOff(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.SetEnabled(context.Background(), false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrTranslateDisabled) {
		t.Fatalf("error = %v, want ErrTranslateDisabled", err)
	}
	// The two are distinct: a caller that only handles the configured case
	// must not catch this one.
	if errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Error("a switched-off tool is reported as an unconfigured one")
	}
	if !strings.Contains(err.Error(), "已关闭") {
		t.Errorf("the error does not say the tool is off: %v", err)
	}
	if f.deepseek.count() != 0 || f.device.readCount() != 0 {
		t.Error("a switched-off tool did work")
	}
}

// A tool whose settings name a translator this build does not have says which
// one, so that the user knows what to pick instead.
func TestTranslateWithNothingConfigured(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.SaveSettings(context.Background(), func(s *Settings) {
		s.Provider = "nobody"
	}); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Fatalf("error = %v, want ErrTranslateUnavailable", err)
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("the error does not name the translator that is missing: %v", err)
	}
	if !strings.Contains(err.Error(), "设置") {
		t.Errorf("the error does not say where to fix it: %v", err)
	}
}

// A missing credential is its own answer: no request is worth making with a
// key that is not there.
func TestTranslateWithoutACredential(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
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

// Nothing selected and nothing usable on the clipboard is a user problem with
// its own words: they have to select or copy something first.
func TestTranslateWithNothingToTranslate(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	_, err := f.translate.Translate(context.Background(), Request{Text: source(t, f)})
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
		useDeepSeek(t, f)
		f.device.set(reading)
		_, err := f.translate.Translate(context.Background(), Request{Text: source(t, f)})
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
			useDeepSeek(t, f)
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
	useDeepSeek(t, f)
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
	f.tools.section.Set(Defaults())

	// The service is stored first, because it has to exist before it can be
	// the one in use: a provider naming one of the user's own services is
	// brought back to the default when that service is not there.
	if err := f.translate.SetCustomTranslator(ctx, "custom", CustomSettings{
		BaseURL:     "api.example.com/v1/",
		DisplayName: "  本地 Ollama  ",
	}); err != nil {
		t.Fatalf("SetCustomTranslator failed: %v", err)
	}
	if err := f.translate.SetProvider(ctx, "custom"); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	if err := f.translate.SetPinned(ctx, true); err != nil {
		t.Fatalf("SetPinned failed: %v", err)
	}
	if err := f.translate.SetCloseDelay(ctx, 1200*time.Millisecond); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
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
	service, ok := settings.CustomService("custom")
	if !ok {
		t.Fatalf("the service that was saved is not in the settings: %+v", settings.Custom)
	}
	if service.BaseURL != "https://api.example.com/v1" {
		t.Errorf("custom base URL = %q, want it cleaned", service.BaseURL)
	}
	if service.DisplayName != "本地 Ollama" {
		t.Errorf("custom name = %q, want it trimmed", service.DisplayName)
	}
	// Reading the translation settings does not disturb them. The two tools
	// share one file, so what keeps one from losing the other's switches is
	// that each section owns its own keys — a translation section has no
	// clipboard field to write, which is what this used to check by hand.
	before := f.tools.section.Current()
	if _, err := f.translate.Settings(); err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	after := f.tools.section.Current()
	if after.Provider != before.Provider || !sameServices(after.Custom, before.Custom) {
		t.Errorf("reading the translation settings changed them: %+v then %+v", before, after)
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
	if settings.ProviderOr() != DefaultProvider {
		t.Errorf("provider = %q, want the stored one kept", settings.ProviderOr())
	}
}

// A switch that cannot be written is reported, and the value the
// application runs on is the one on disk rather than the one that was meant
// to be written.
func TestSettingsReportAWriteFailure(t *testing.T) {
	f := newTranslate(t)
	f.tools.section.Set(Defaults())
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
	f.tools.section.Set(Defaults())
	views, err := f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	if len(views) != 4 {
		t.Fatalf("views = %+v, want every translator", views)
	}
	byID := map[string]TranslatorView{}
	for _, view := range views {
		byID[view.ID] = view
	}
	// A free engine is usable with nothing stored at all, which is the whole
	// point of it.
	for _, id := range []string{contractfree.MicrosoftEdge, contractfree.Bilibili} {
		view := byID[id]
		if !view.Configured {
			t.Errorf("%s is not reported as configured: %+v", id, view)
		}
		if view.NeedsCredential {
			t.Errorf("%s is reported as needing a key: %+v", id, view)
		}
	}
	// A translator whose key is stored is configured, and one whose key is
	// not is shown but cannot be picked.
	deepseek := byID["deepseek"]
	if !deepseek.Configured || !deepseek.NeedsCredential {
		t.Errorf("deepseek = %+v", deepseek)
	}
	// The user's own service is not configured until an address is there.
	if custom := byID["custom"]; custom.Configured || custom.Endpoint != "" {
		t.Errorf("custom = %+v, want it unconfigured before an address", custom)
	}
	if byID["custom"].NeedsCredential {
		t.Error("a local service is reported as needing a key")
	}

	if _, err := f.translate.AddCustomTranslator(context.Background(), CustomSettings{
		BaseURL: "https://api.example.com/v1",
	}); err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
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

// The user may keep several services of their own, and each one is its own
// translator: adding one does not disturb the ones already there, and each
// answers for its own address.
func TestSeveralOwnServicesLiveSideBySide(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	first, err := f.translate.AddCustomTranslator(ctx, CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	second, err := f.translate.AddCustomTranslator(ctx, CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "远端网关",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	if first != "custom" || second != "custom-2" {
		t.Fatalf("ids = %q / %q, want custom and custom-2", first, second)
	}

	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if len(settings.Custom) != 2 {
		t.Fatalf("services = %+v, want both", settings.Custom)
	}
	// Each keeps its own address and its own name: the second one did not
	// overwrite the first.
	if service, _ := settings.CustomService(first); service.DisplayName != "本地 Ollama" {
		t.Errorf("the first service is %+v", service)
	}
	if service, _ := settings.CustomService(second); service.DisplayName != "远端网关" {
		t.Errorf("the second service is %+v", service)
	}

	// Removing one leaves the other exactly where it was.
	if err := f.translate.RemoveCustomTranslator(ctx, first); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	settings, err = f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if _, still := settings.CustomService(first); still {
		t.Error("the removed service is still there")
	}
	if service, ok := settings.CustomService(second); !ok || service.DisplayName != "远端网关" {
		t.Errorf("removing one disturbed the other: %+v", settings.Custom)
	}
}

// A service of the user's own is refused an address that is not one, and one
// with no address at all is refused rather than stored: there would be nothing
// to ask and nothing to show.
func TestAddingAnOwnServiceChecksTheAddress(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	if _, err := f.translate.AddCustomTranslator(ctx, CustomSettings{DisplayName: "没地址"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
	if _, err := f.translate.AddCustomTranslator(ctx, CustomSettings{BaseURL: "file:///etc/passwd"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if len(settings.Custom) != 0 {
		t.Errorf("a refused service was stored anyway: %+v", settings.Custom)
	}
}

// An id that is not one of the user's own services is refused by both the
// setter and the remover: a built-in translator's address is not the user's to
// rewrite, and removing it would take a service the application knows away.
func TestOwnServiceActionsRefuseABuiltInID(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	if err := f.translate.SetCustomTranslator(ctx, "deepseek", CustomSettings{BaseURL: "https://x.example.com/v1"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("SetCustomTranslator(deepseek) = %v, want ErrInvalidArgument", err)
	}
	if err := f.translate.RemoveCustomTranslator(ctx, "deepseek"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("RemoveCustomTranslator(deepseek) = %v, want ErrInvalidArgument", err)
	}
}

// Removing the service that is in use brings the tool back to the default one,
// rather than leaving it pointing at an id that is gone.
//
// It is what the user sees after deleting the service they had selected: the
// next press has to translate, not report a service that no longer exists.
func TestRemovingTheServiceInUseFallsBackToTheDefault(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	id, err := f.translate.AddCustomTranslator(ctx, CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "要删掉的服务",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	if err := f.translate.SetProvider(ctx, id); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	if got := providerOf(t, f); got != id {
		t.Fatalf("provider = %q, want the service that was just added", got)
	}

	if err := f.translate.RemoveCustomTranslator(ctx, id); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	if got := providerOf(t, f); got != DefaultProvider {
		t.Errorf("provider = %q after removing the one in use, want %q", got, DefaultProvider)
	}
	// And the tool answers rather than reporting a missing service.
	useDeepSeek(t, f)
	if _, err := f.translate.Translate(ctx, Request{Text: "hello"}); err != nil {
		t.Errorf("Translate failed after the removal: %v", err)
	}
}

// providerOf is the translator the tool is set to use, as the settings hold it.
func providerOf(t *testing.T, f *translateFixture) string {
	t.Helper()
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	return settings.ProviderOr()
}

// A provider that names a service which is not there is brought back to the
// default, whether it went missing by hand or by a removal.
//
// A hand-edited file is the other way in, and the rule is the same one: the
// tool must keep working, and the provider has to name something that answers.
func TestAProviderWithNoServiceFallsBackToTheDefault(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	// Written straight into the settings, the way a hand-edited file arrives.
	if err := f.translate.SaveSettings(ctx, func(s *Settings) { s.Provider = "custom-9" }); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	if got := providerOf(t, f); got != DefaultProvider {
		t.Errorf("provider = %q, want the default %q", got, DefaultProvider)
	}

	// A built-in id is left alone: whether this build knows it is the
	// registry's question, and the window says so when it does not.
	if err := f.translate.SaveSettings(ctx, func(s *Settings) { s.Provider = "nobody" }); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	if got := providerOf(t, f); got != "nobody" {
		t.Errorf("provider = %q, want it left as written", got)
	}
}

// The list of translators never rearranges itself: the same ids come back in
// the same order no matter which one is in use.
//
// It is what the settings window draws its rows from, and a row that moves
// when the user picks one is a row the user cannot aim at — the press they made
// lands on whatever took its place.
func TestProviderOrderDoesNotDependOnWhatIsInUse(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()
	ids := func() []string {
		t.Helper()
		views, err := f.translate.Providers(ctx)
		if err != nil {
			t.Fatalf("Providers failed: %v", err)
		}
		order := make([]string, 0, len(views))
		for _, view := range views {
			order = append(order, view.ID)
		}
		return order
	}

	before := ids()
	if len(before) != 4 {
		t.Fatalf("the list is %v, want every translator", before)
	}
	// Pick each one in turn, and the order is the same every time.
	for _, pick := range append(append([]string{}, before...), DefaultProvider) {
		if err := f.translate.SetProvider(ctx, pick); err != nil {
			t.Fatalf("SetProvider(%s) failed: %v", pick, err)
		}
		after := ids()
		if !sameOrder(before, after) {
			t.Fatalf("picking %s reordered the list: %v then %v", pick, before, after)
		}
	}
}

// sameOrder reports whether two lists hold the same ids in the same places.
func sameOrder(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
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
	tools := newFakeTools()
	service, err := NewTranslate(TranslateOptions{
		Translators: newFakeTranslators(),
		Secrets:     newMemoryStore(),
		Section:     tools.section,
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	// The default provider is one this registry does not have, which is the
	// same answer as a registry that is empty: the tool says where to
	// configure a service rather than reaching for a network.
	tools.section.Set(Defaults())
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

func TestDetectCustomImageCapabilityPersistsOnlyTheVerifiedOutcome(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()
	if err := f.translate.SaveSettings(ctx, func(s *Settings) {
		s.Custom["custom"] = CustomSettings{BaseURL: "https://api.example.com/v1"}
	}); err != nil {
		t.Fatal(err)
	}
	supported, err := f.translate.DetectCustomImageCapability(ctx, "custom")
	if err != nil || !supported {
		t.Fatalf("DetectCustomImageCapability = %v, %v", supported, err)
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Custom["custom"].SupportsImage {
		t.Error("successful probe was not persisted")
	}

	f.custom.fail(domain.ErrProviderUnavailable)
	supported, err = f.translate.DetectCustomImageCapability(ctx, "custom")
	if err == nil || supported {
		t.Fatalf("failed probe = %v, %v", supported, err)
	}
	settings, _ = f.translate.Settings()
	if settings.Custom["custom"].SupportsImage {
		t.Error("failed probe left a stale image capability")
	}
	if _, err := f.translate.DetectCustomImageCapability(ctx, "deepseek"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("non-custom probe = %v, want invalid argument", err)
	}
}
