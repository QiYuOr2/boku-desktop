package custom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

// The address is the user's to name, so the descriptor reports what they
// typed — and nothing when they typed nothing, rather than an address the
// application made up.
func TestDescriptorFollowsTheSettings(t *testing.T) {
	settings := domain.CustomTranslatorSettings{}
	reply := New(httpc.New(httpc.Options{}), func() domain.CustomTranslatorSettings { return settings })
	if got := reply.Descriptor().Endpoint; got != "" {
		t.Errorf("endpoint = %q, want nothing before an address is configured", got)
	}
	if err := reply.Descriptor().Validate(); err != nil {
		t.Fatalf("the descriptor does not validate: %v", err)
	}
	if reply.Descriptor().BaseURLSetting == "" {
		t.Error("the descriptor does not say its address is a setting")
	}

	settings = domain.CustomTranslatorSettings{BaseURL: "https://api.example.com/v1"}
	if got := reply.Descriptor().Endpoint; got != "https://api.example.com/v1/chat/completions" {
		t.Errorf("endpoint = %q", got)
	}
	// The settings may change while the application runs, and the next
	// translation uses the new address.
	settings = domain.CustomTranslatorSettings{BaseURL: "http://127.0.0.1:11434/v1"}
	if got := reply.Descriptor().Endpoint; got != "http://127.0.0.1:11434/v1/chat/completions" {
		t.Errorf("endpoint after a change = %q", got)
	}
}

// An address nobody typed is a configuration problem, and it is named as
// one before anything is sent.
func TestTranslateWithoutAnAddress(t *testing.T) {
	asked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
	}))
	defer server.Close()
	reply := New(httpc.New(httpc.Options{}), func() domain.CustomTranslatorSettings {
		return domain.CustomTranslatorSettings{}
	})
	_, err := reply.Translate(context.Background(), domain.Credentials{}, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if err == nil {
		t.Fatal("a translation without an address succeeded")
	}
	if asked {
		t.Error("the translator asked somewhere")
	}
}

// A service the user runs themselves may need no key at all: no
// Authorization header is sent rather than one carrying an empty key.
func TestTranslateWithoutAKeySendsNoAuthorization(t *testing.T) {
	var auth string
	var hasAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, hasAuth = r.Header.Get("Authorization"), r.Header.Get("Authorization") != ""
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}]}`))
	}))
	defer server.Close()
	reply := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	result, err := reply.Translate(context.Background(), domain.Credentials{}, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if hasAuth {
		t.Errorf("the request carried an authorization header: %q", auth)
	}
	if result.TranslatedText != "你好" || result.ProviderID != ProviderID {
		t.Errorf("result = %+v", result)
	}
}

func TestTranslateSendsTheKeyWhenThereIsOne(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}]}`))
	}))
	defer server.Close()
	reply := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	creds := domain.Credentials{Secrets: map[string]string{APIKeyCredential: "sk-test-0002"}}
	if _, err := reply.Translate(context.Background(), creds, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if auth != "Bearer sk-test-0002" {
		t.Errorf("authorization = %q", auth)
	}
}

// A service that rejects the key says so through the domain's own error, and
// the window then says "check the key" rather than showing a status code.
func TestTranslateTranslatesARejectedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key","key":"sk-live-111122223333"}}`))
	}))
	defer server.Close()
	reply := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	creds := domain.Credentials{Secrets: map[string]string{APIKeyCredential: "sk-live-111122223333"}}
	_, err := reply.Translate(context.Background(), creds, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if !errors.Is(err, domain.ErrProviderUnauthorized) {
		t.Fatalf("error = %v, want ErrProviderUnauthorized", err)
	}
	if strings.Contains(err.Error(), "sk-live-111122223333") {
		t.Errorf("the error carries the credential: %v", err)
	}
}

// The address is cleaned the way the settings clean it, so a user who typed
// a trailing slash still gets one request path rather than two.
func TestTranslateNormalizesTheAddress(t *testing.T) {
	var path string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}]}`))
	}))
	defer server.Close()
	reply := New(httpc.New(httpc.Options{HTTPClient: server.Client()}), func() domain.CustomTranslatorSettings {
		// A trailing slash is what a person pastes; the request must not
		// end up with two.
		return domain.CustomTranslatorSettings{BaseURL: server.URL + "/v1/"}
	})
	if _, err := reply.Translate(context.Background(), domain.Credentials{}, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %q", path)
	}
}

// The annotation settings reach the request, which is what makes the
// Japanese switches mean anything.
func TestTranslateAsksForAnnotations(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"s\":\"機械語\",\"r\":\"キカイゴ\",\"t\":\"机器语言\"}"}}]}`))
	}))
	defer server.Close()
	reply := NewWithBaseURL(httpc.New(httpc.Options{}), server.URL)
	result, err := reply.Translate(context.Background(), domain.Credentials{}, domain.TranslationRequest{
		Text:       "機械語",
		TargetLang: domain.LangChinese,
		Annotation: domain.AnnotationOptions{Furigana: true},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if len(result.Segments) != 1 || result.Segments[0].Reading != "キカイゴ" {
		t.Fatalf("segments = %+v", result.Segments)
	}
	messages, _ := body["messages"].([]any)
	system, _ := messages[0].(map[string]any)
	prompt, _ := system["content"].(string)
	if !strings.Contains(prompt, `"r"`) {
		t.Errorf("the prompt does not ask for readings:\n%s", prompt)
	}
	if strings.Contains(prompt, `"g"`) {
		t.Errorf("the prompt asks for glosses that were not requested:\n%s", prompt)
	}
}

var _ domain.Translator = (*Translator)(nil)
