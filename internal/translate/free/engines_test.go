package free

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

// Each free engine is a translator of its own: it is asked when it is the one
// the user picked, and nothing else is asked in its place. These tests drive
// one engine each, over an httptest server — no test in this package reaches
// the network.

// chatAnswer wraps a model's own answer in the chat-completions envelope the
// endpoint answers with, escaping the content the way the wire format does.
func chatAnswer(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic(err)
	}
	return `{"choices":[{"message":{"content":` + string(encoded) + `}}]}`
}

// microsoftAnswer is the shape the Edge endpoint answers with, as
// §3.1 of the free-translate research measured it.
const microsoftAnswer = `[{"detectedLanguage":{"language":"en","score":0.9},
  "translations":[{"text":"你好，世界。","to":"zh-Hans","sentLen":{"srcSentLen":[12],"transSentLen":[6]}}]}]`

func plainRequest(text string) domain.TranslationRequest {
	return domain.TranslationRequest{
		Text:       text,
		SourceLang: domain.LangEnglish,
		TargetLang: domain.LangChinese,
	}
}

// The engine is a translator under its own id, which is what the settings file
// stores and what the picker marks.
func TestTheEnginesCarryTheirOwnIdentity(t *testing.T) {
	client := httpc.New(httpc.Options{})
	microsoft := NewMicrosoft(client)
	bilibili := NewBilibili(client)

	if microsoft.ID() != contract.MicrosoftEdge {
		t.Errorf("microsoft id = %q, want %q", microsoft.ID(), contract.MicrosoftEdge)
	}
	if bilibili.ID() != contract.Bilibili {
		t.Errorf("bilibili id = %q, want %q", bilibili.ID(), contract.Bilibili)
	}
	// One engine, one id: two free engines sharing an id would be one entry in
	// the settings file with two meanings.
	if microsoft.ID() == bilibili.ID() {
		t.Error("the two free engines share an id")
	}
	// Both are usable with nothing configured at all, which is the whole point
	// of them.
	for _, reply := range []domain.Translator{microsoft, bilibili} {
		descriptor := reply.Descriptor()
		if err := descriptor.Validate(); err != nil {
			t.Fatalf("%s does not validate: %v", reply.ID(), err)
		}
		if !descriptor.AllowEmptyCredential {
			t.Errorf("%s is reported as needing a credential", reply.ID())
		}
		if descriptor.NeedsCredential(contract.APIKeyCredential) {
			t.Errorf("%s requires the placeholder key it names", reply.ID())
		}
		if descriptor.DisplayName == "" {
			t.Errorf("%s has no name to draw", reply.ID())
		}
		if strings.TrimSpace(descriptor.Endpoint) == "" {
			t.Errorf("%s does not say where it asks", reply.ID())
		}
	}
}

func TestBilibiliDoesNotAdvertiseImageTranslation(t *testing.T) {
	bilibili := NewBilibili(httpc.New(httpc.Options{}))
	if bilibili.Descriptor().SupportsImage {
		t.Error("Bilibili must not advertise image support")
	}
	if _, ok := any(bilibili).(domain.ImageTranslator); ok {
		t.Error("Bilibili must not implement image translation")
	}
}

// The names are the design's own: the settings page draws them as the two free
// options, and they have to be what the user recognises.
func TestTheEnginesAreNamedAsTheSettingsPageDrawsThem(t *testing.T) {
	client := httpc.New(httpc.Options{})
	if got := NewMicrosoft(client).Descriptor().DisplayName; got != "微软 Edge 翻译" {
		t.Errorf("microsoft is called %q", got)
	}
	if got := NewBilibili(client).Descriptor().DisplayName; got != "B站 Index-Translate" {
		t.Errorf("bilibili is called %q", got)
	}
}

func TestMicrosoftAsksAndReadsTheAnswer(t *testing.T) {
	var (
		path  string
		query string
		body  []string
		auth  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()

	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"/translate/translatetext?from=&to=%s&isEnterpriseClient=false")
	result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello world."))
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好，世界。" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.SourceText != "Hello world." || result.TargetLang != domain.LangChinese {
		t.Errorf("result = %+v", result)
	}
	if result.CreatedAt.IsZero() {
		t.Error("the result has no time")
	}
	// The body is a bare array of strings, and the target language is spelled
	// the way this endpoint spells it.
	if len(body) != 1 || body[0] != "Hello world." {
		t.Errorf("body = %v, want one string", body)
	}
	if path != "/translate/translatetext" {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(query, "to=zh-Hans") {
		t.Errorf("query = %q, want the simplified-Chinese code", query)
	}
	// The engine sends no credential, because there is none to send: a free
	// endpoint given an empty bearer token would be told about a key nobody
	// has.
	if auth != "" {
		t.Errorf("authorization = %q, want none", auth)
	}
}

// An engine that answers one sentence per text has nothing to say about the
// words in it, and says so rather than failing: the user gets the translation
// and is told exactly what it does not have.
func TestMicrosoftAnswersARequestForReadingsWithANote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()

	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")
	request := plainRequest("【回想モード完備】")
	request.Annotation = domain.AnnotationOptions{Furigana: true, LoanwordGloss: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好，世界。" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.Segments != nil {
		t.Errorf("an engine that cannot annotate produced segments: %+v", result.Segments)
	}
	if result.Note != noReadingsNote {
		t.Errorf("note = %q, want the word about missing readings", result.Note)
	}
}

func TestMicrosoftFailuresAreDomainErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		payload string
		want    error
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":"too many"}`, domain.ErrProviderRateLimited},
		{"unauthorized", http.StatusUnauthorized, `{}`, domain.ErrProviderUnauthorized},
		{"down", http.StatusBadGateway, ``, domain.ErrProviderUnavailable},
		{"not JSON", http.StatusOK, `<html>`, domain.ErrProviderBadResponse},
		{"empty array", http.StatusOK, `[]`, domain.ErrProviderBadResponse},
		{"no translations", http.StatusOK, `[{"detectedLanguage":{"language":"en"}}]`, domain.ErrProviderBadResponse},
		{"empty translation", http.StatusOK, `[{"translations":[{"text":"  ","to":"zh-Hans"}]}]`, domain.ErrProviderBadResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer server.Close()

			engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")
			result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello world."))
			if err == nil {
				t.Fatalf("a %s answer was accepted", tc.name)
			}
			if !errorsIs(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if result.TranslatedText != "" {
				t.Errorf("a failure produced a translation: %+v", result)
			}
		})
	}
}

// A text that is not there is the caller's mistake, and nothing is asked.
func TestTheEnginesRefuseAnEmptyRequest(t *testing.T) {
	asked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
	}))
	defer server.Close()

	client := httpc.New(httpc.Options{})
	for _, reply := range []domain.Translator{
		newMicrosoft(client, server.URL+"?to=%s"),
		newBilibili(client, server.URL, "m"),
	} {
		if _, err := reply.Translate(t.Context(), domain.Credentials{}, plainRequest("   ")); err == nil {
			t.Errorf("%s answered a request with no text", reply.ID())
		}
	}
	if asked {
		t.Error("an engine was asked for a request with no text")
	}
}

// A text that was cut says so, and one that was not is silent about it.
func TestMicrosoftNotesALongText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()
	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")

	request := plainRequest(strings.Repeat("あ", DefaultMaxChars+10))
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if !strings.Contains(result.Note, "原文过长") {
		t.Errorf("note = %q, want the word about the cut", result.Note)
	}
	if len([]rune(result.SourceText)) != DefaultMaxChars {
		t.Errorf("the text sent is %d characters, want the ceiling", len([]rune(result.SourceText)))
	}
}

// The B 站 engine asks the OpenAI-compatible endpoint the same way the paid
// backends do, and reads the annotation out of a model's answer. It is the free
// engine that can do this, which is what makes it the one to pick for Japanese.
func TestBilibiliAsksForAnAnnotatedAnswer(t *testing.T) {
	var (
		path string
		body map[string]any
		auth string
	)
	lines := []string{
		`{"s":"【","t":"【"}`,
		`{"s":"回想","r":"カイソウ","t":"回想"}`,
		`{"s":"モード","g":"mode","t":"模式"}`,
	}
	answer := chatAnswer(strings.Join(lines, "\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "Index-Translate-35B-A3B")
	request := plainRequest("【回想モード】")
	request.SourceLang = domain.LangJapanese
	request.Annotation = domain.AnnotationOptions{Furigana: true, LoanwordGloss: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if path != "/chat/completions" {
		t.Errorf("path = %q", path)
	}
	if auth != "" {
		t.Errorf("authorization = %q, want none", auth)
	}
	if body["model"] != "Index-Translate-35B-A3B" {
		t.Errorf("model = %v", body["model"])
	}
	if len(result.Segments) != 3 {
		t.Fatalf("segments = %+v, want the pieces the model answered", result.Segments)
	}
	if result.Segments[1].Reading != "カイソウ" {
		t.Errorf("the reading is not carried: %+v", result.Segments[1])
	}
	if result.Segments[2].Gloss != "mode" {
		t.Errorf("the loanword gloss is not carried: %+v", result.Segments[2])
	}
}

// A model that answers a paragraph when pieces were asked for is not a
// failure: the paragraph is the translation, and the note says what is missing,
// exactly as the paid backends do.
func TestBilibiliNotesAnAnswerWithNoPieces(t *testing.T) {
	answer := chatAnswer("【回忆模式已完备】")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
	request := plainRequest("【回想モード完備】")
	request.Annotation = domain.AnnotationOptions{Furigana: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "【回忆模式已完备】" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.Segments != nil {
		t.Errorf("a plain answer produced segments: %+v", result.Segments)
	}
	if result.Note != noReadingsNote {
		t.Errorf("note = %q, want the word about missing readings", result.Note)
	}
}

// A model that wraps its answer in a fenced code block — the most common way a
// model disobeys "no code fences" — is read the same way: the parser unwraps
// the fence, and what it cannot read as a piece is the translation.
func TestBilibiliReadsAModelThatIgnoresTheFormat(t *testing.T) {
	answer := chatAnswer("```json\n" + `{"s":"こんにちは","t":"你好"}` + "\n```")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
	request := plainRequest("こんにちは")
	request.SourceLang = domain.LangJapanese
	request.Annotation = domain.AnnotationOptions{Furigana: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	// The fence is not a translation, and it is not drawn: the piece inside it
	// is.
	if len(result.Segments) != 1 || result.Segments[0].Text != "こんにちは" {
		t.Errorf("segments = %+v, want the piece inside the fence", result.Segments)
	}
}

// A service that fails is reported as a failure and never as an empty
// translation — and, because the engines are separate translators, the failure
// is the one the user's engine reported: nothing else is asked in its place.
func TestBilibiliReportsItsOwnFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"rate limited", http.StatusTooManyRequests, domain.ErrProviderRateLimited},
		{"down", http.StatusInternalServerError, domain.ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asked := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"no"}`))
			}))
			defer server.Close()

			engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
			result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello."))
			if err == nil {
				t.Fatal("a failed engine answered")
			}
			if !errorsIs(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if result.TranslatedText != "" {
				t.Errorf("a failure produced a translation: %+v", result)
			}
			// The engine was asked once and only once: there is no pool to hand
			// the text to.
			if asked != 1 {
				t.Errorf("the endpoint was asked %d times, want once", asked)
			}
		})
	}
}

// The endpoints are the ones the design measured, written once each.
func TestTheEndpointsAreTheOnesTheDesignMeasured(t *testing.T) {
	if !strings.HasPrefix(microsoftEndpoint, "https://edge.microsoft.com/translate/translatetext?") {
		t.Errorf("endpoint = %q", microsoftEndpoint)
	}
	if !strings.Contains(microsoftEndpoint, "isEnterpriseClient=false") {
		t.Errorf("endpoint = %q, want the parameter Edge sends", microsoftEndpoint)
	}
	if !strings.HasPrefix(bilibiliEndpoint, "https://index-translate.bilibili.com/v1") {
		t.Errorf("endpoint = %q", bilibiliEndpoint)
	}
}

// Each engine stamps its answers through its own clock, which is how a test
// decides when an answer happened.
func TestTheEnginesStampTheirAnswers(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()

	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")
	engine.now = func() time.Time { return at }
	result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello."))
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if !result.CreatedAt.Equal(at) {
		t.Errorf("created at %s, want %s", result.CreatedAt, at)
	}
}

// The OpenAPI-compatible engine keeps the shared protocol honest: it asks for
// the same prompt the paid backends ask for, which is what the annotation
// depends on.
func TestBilibiliSendsTheToolOwnPrompt(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(chatAnswer("你好")))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
	if _, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("こんにちは")); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %+v, want a system prompt and the text", messages)
	}
	system, _ := messages[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "翻译引擎") {
		t.Errorf("the system prompt is %q, want the tool's own", system)
	}
	user, _ := messages[1].(map[string]any)["content"].(string)
	if user != "こんにちは" {
		t.Errorf("the text sent is %q", user)
	}
}

// ensure the openai package is the one both the prompt and the parser come
// from, so that this test file cannot drift from the protocol.
var _ = openai.DefaultMaxChars
