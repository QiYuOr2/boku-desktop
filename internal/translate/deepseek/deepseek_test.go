package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

// The translator reuses the balance panel's credential: one account, one
// key, configured once.
func TestDescriptorReusesThePanelCredential(t *testing.T) {
	reply := New(httpc.New(httpc.Options{}))
	if reply.ID() != contract.ProviderID {
		t.Errorf("ID() = %q, want %q", reply.ID(), contract.ProviderID)
	}
	descriptor := reply.Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("the descriptor does not validate: %v", err)
	}
	if len(descriptor.CredentialKeys) != 1 || descriptor.CredentialKeys[0] != contract.APIKeyCredential {
		t.Errorf("credential keys = %v, want the panel's own key", descriptor.CredentialKeys)
	}
	if descriptor.Endpoint != "https://api.deepseek.com/chat/completions" {
		t.Errorf("endpoint = %q", descriptor.Endpoint)
	}
	if descriptor.BaseURLSetting != "" {
		t.Error("the built-in translator claims its address is a setting")
	}
	if descriptor.NeedsCredential(contract.APIKeyCredential) != true {
		t.Error("the translator does not require the key it asked for")
	}
}

// Without a key nothing is asked: an unauthenticated request would only
// teach the endpoint that the key is wrong.
func TestTranslateWithoutAKeyDoesNotAsk(t *testing.T) {
	asked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
	}))
	defer server.Close()
	reply := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	_, err := reply.Translate(context.Background(), domain.Credentials{ProviderID: ProviderID}, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("error = %v, want ErrCredentialMissing", err)
	}
	if asked {
		t.Error("the translator asked without a key")
	}
}

func TestTranslateAsksAndReadsTheAnswer(t *testing.T) {
	var (
		body  map[string]any
		auth  string
		reply string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + mustJSON(reply) + `}}]}`))
	}))
	defer server.Close()

	translated := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	translated.now = func() time.Time { return time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC) }
	creds := domain.Credentials{
		ProviderID: ProviderID,
		Secrets:    map[string]string{contract.APIKeyCredential: "sk-test-0001"},
	}
	request := domain.TranslationRequest{
		Text:       "A compiler converts source code.",
		TargetLang: domain.LangChinese,
	}

	// A plain translation: the prompt asks for nothing but the text, and the
	// answer is one line.
	reply = "编译器把源代码转换成机器语言。"
	result, err := translated.Translate(context.Background(), creds, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != reply {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.SourceLang != domain.LangEnglish {
		t.Errorf("source language = %q, want the script's own answer", result.SourceLang)
	}
	if result.TargetLang != domain.LangChinese || result.ProviderID != ProviderID {
		t.Errorf("result = %+v", result)
	}
	if result.CreatedAt.IsZero() {
		t.Error("the result has no time")
	}
	if result.Segments != nil {
		t.Errorf("a plain answer produced segments: %+v", result.Segments)
	}
	if auth != "Bearer sk-test-0001" {
		t.Errorf("authorization = %q", auth)
	}
	if body["model"] != defaultModel {
		t.Errorf("model = %v, want %q", body["model"], defaultModel)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	system, _ := messages[0].(map[string]any)
	if prompt, _ := system["content"].(string); strings.Contains(prompt, "JSON") {
		t.Errorf("a plain translation asked for annotations:\n%s", prompt)
	}

	// An annotated answer: the segments are read, and the source the result
	// carries is the source the segments add up to.
	reply = `{"s":"コンパイラ","r":"コンパイラ","g":"compiler","t":"编译器"}` + "\n" +
		`{"s":"は","r":"ハ","t":"是"}`
	annotated := request
	annotated.Text = "コンパイラは"
	annotated.Annotation = domain.AnnotationOptions{Furigana: true, LoanwordGloss: true}
	result, err = translated.Translate(context.Background(), creds, annotated)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("segments = %+v", result.Segments)
	}
	if result.TranslatedText != "编译器是" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.SourceText != "コンパイラは" {
		t.Errorf("source = %q, want it built from the segments", result.SourceText)
	}
	if result.SourceLang != domain.LangJapanese {
		t.Errorf("source language = %q", result.SourceLang)
	}
	if result.Note != "" {
		t.Errorf("note = %q, want nothing to report", result.Note)
	}
	messages, _ = body["messages"].([]any)
	system, _ = messages[0].(map[string]any)
	if prompt, _ := system["content"].(string); !strings.Contains(prompt, `"r"`) {
		t.Errorf("the annotated prompt does not ask for readings:\n%s", prompt)
	}
}

// A model that answers nothing is a failure at the boundary, never an empty
// window.
func TestTranslateRefusesAnEmptyAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer server.Close()
	translated := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	_, err := translated.Translate(context.Background(), credentials(), domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("error = %v, want ErrProviderBadResponse", err)
	}
}

// A text longer than one request should carry is cut, and the result says
// so rather than pretending the whole thing was translated.
func TestTranslateReportsATruncatedText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"译文"}}]}`))
	}))
	defer server.Close()
	translated := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	long := strings.Repeat("日", openai.DefaultMaxChars+50)
	result, err := translated.Translate(context.Background(), credentials(), domain.TranslationRequest{
		Text:       long,
		TargetLang: domain.LangChinese,
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got := len([]rune(result.SourceText)); got != openai.DefaultMaxChars {
		t.Errorf("the source sent is %d characters, want %d", got, openai.DefaultMaxChars)
	}
	if !strings.Contains(result.Note, "只翻译了前") {
		t.Errorf("note = %q, want it to say the text was cut", result.Note)
	}
}

func TestTranslateRefusesABadRequest(t *testing.T) {
	translated := NewWithBaseURL(httpc.New(httpc.Options{}), "https://example.com")
	_, err := translated.Translate(context.Background(), credentials(), domain.TranslationRequest{
		Text: "hello",
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("a request with no target = %v, want ErrInvalidArgument", err)
	}
}

func credentials() domain.Credentials {
	return domain.Credentials{
		ProviderID: ProviderID,
		Secrets:    map[string]string{contract.APIKeyCredential: "sk-test-0001"},
	}
}

// mustJSON encodes a string as a JSON literal for a test server's answer.
func mustJSON(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

var _ domain.Translator = (*Translator)(nil)
