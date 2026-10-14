package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"boku/internal/data/httpx"
	"boku/internal/domain"
	codexdomain "boku/internal/domain/codex"
)

func newProvider(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewWithBaseURL(httpx.New(httpx.Options{HTTPClient: server.Client()}), server.URL), server
}

func credentials(token, account string) domain.Credentials {
	return domain.Credentials{
		ProviderID: codexdomain.ProviderID,
		Secrets:    map[string]string{codexdomain.AccessTokenCredential: token},
		Settings:   map[string]string{codexdomain.AccountIDSetting: account},
	}
}

func TestFetchAsksTheUsageEndpointWithTheToken(t *testing.T) {
	var path, authorization, account string
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		account = r.Header.Get("ChatGPT-Account-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":6}}}`))
	})

	result, err := provider.Fetch(context.Background(), credentials("token-abc", "account-1"))
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if path != usagePath {
		t.Errorf("path = %q, want %q", path, usagePath)
	}
	if authorization != "Bearer token-abc" {
		t.Errorf("Authorization = %q", authorization)
	}
	if account != "account-1" {
		t.Errorf("ChatGPT-Account-Id = %q, want the login's account", account)
	}
	if result.ProviderID != codexdomain.ProviderID {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, codexdomain.ProviderID)
	}
	if result.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", result.HTTPStatus)
	}
}

// A login that names no account sends no account header: the endpoint decides
// which account a token belongs to when it is the only one.
func TestFetchWithoutAnAccountSendsNoAccountHeader(t *testing.T) {
	var account string
	var present bool
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["Chatgpt-Account-Id"]
		account = r.Header.Get("ChatGPT-Account-Id")
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := provider.Fetch(context.Background(), credentials("token-abc", "  ")); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if present || account != "" {
		t.Errorf("the request carried ChatGPT-Account-Id = %q", account)
	}
}

func TestFetchWithoutATokenDoesNotAsk(t *testing.T) {
	var calls atomic.Int32
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	})
	_, err := provider.Fetch(context.Background(), credentials("", ""))
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("the provider made %d request(s) without a token, want 0", got)
	}
	if !strings.Contains(err.Error(), "codex.accessToken") {
		t.Errorf("the error does not name the credential: %v", err)
	}
}

func TestFetchTranslatesFailures(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{401, domain.ErrProviderUnauthorized},
		{403, domain.ErrProviderUnauthorized},
		{429, domain.ErrProviderRateLimited},
		{500, domain.ErrProviderUnavailable},
	}
	for _, c := range cases {
		provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(`{"detail":"nope"}`))
		})
		_, err := provider.Fetch(context.Background(), credentials("token-abc", ""))
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: error = %v, want %v", c.status, err, c.want)
		}
	}
}

func TestFetchTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := httpx.New(httpx.Options{HTTPClient: server.Client(), Timeout: 20 * time.Millisecond})
	provider := NewWithBaseURL(client, server.URL)
	if _, err := provider.Fetch(context.Background(), credentials("token-abc", "")); !errors.Is(err, domain.ErrProviderTimeout) {
		t.Errorf("error = %v, want ErrProviderTimeout", err)
	}
}

func TestFetchRefusesAnOversizedAnswer(t *testing.T) {
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"plan_type":"` + strings.Repeat("x", httpx.DefaultMaxBytes) + `"}`))
	})
	if _, err := provider.Fetch(context.Background(), credentials("token-abc", "")); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("error = %v, want ErrProviderBadResponse", err)
	}
}

func TestFetchErrorsNeverLeakTheToken(t *testing.T) {
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"token expired"}`))
	})
	const token = "ey-super-secret-token"
	_, err := provider.Fetch(context.Background(), credentials(token, ""))
	if err == nil {
		t.Fatal("the request did not fail")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("the error leaks the token: %v", err)
	}
}

func TestDescriptor(t *testing.T) {
	provider := New(httpx.New(httpx.Options{}))
	descriptor := provider.Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("the descriptor is invalid: %v", err)
	}
	if descriptor.ID != provider.ID() {
		t.Errorf("descriptor id %q and provider id %q differ", descriptor.ID, provider.ID())
	}
	if descriptor.DocsURL == "" {
		t.Error("the descriptor does not say where to read about the login")
	}
	if descriptor.UsageURL != codexdomain.UsageURL {
		t.Errorf("UsageURL = %q, want %q", descriptor.UsageURL, codexdomain.UsageURL)
	}
	if !strings.HasPrefix(descriptor.UsageURL, "https://") {
		t.Errorf("UsageURL = %q, want an https address", descriptor.UsageURL)
	}
	if !strings.HasSuffix(descriptor.Endpoint, usagePath) {
		t.Errorf("Endpoint = %q, want the usage address", descriptor.Endpoint)
	}
	if !strings.HasPrefix(descriptor.Endpoint, "https://chatgpt.com/") {
		t.Errorf("Endpoint = %q, want ChatGPT's own host", descriptor.Endpoint)
	}
	if got := descriptor.MinRefreshInterval(); got != time.Minute {
		t.Errorf("MinRefreshInterval() = %s, want 1m", got)
	}
}

func TestRefreshAccessTokenPostsTheCodexForm(t *testing.T) {
	var form url.Values
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	t.Cleanup(server.Close)
	client := httpx.New(httpx.Options{HTTPClient: server.Client()})

	token, err := RefreshAccessToken(context.Background(), client, server.URL, "old-refresh")
	if err != nil {
		t.Fatalf("RefreshAccessToken failed: %v", err)
	}
	if got := form.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q", got)
	}
	if got := form.Get("client_id"); got != ClientID {
		t.Errorf("client_id = %q, want Codex's own", got)
	}
	if got := form.Get("refresh_token"); got != "old-refresh" {
		t.Errorf("refresh_token = %q", got)
	}
	if !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q", contentType)
	}
	if token.AccessToken != "new-token" || token.RefreshToken != "new-refresh" {
		t.Errorf("token = %+v", token)
	}
	if token.ExpiresAt.IsZero() || time.Until(token.ExpiresAt) > time.Hour+time.Minute {
		t.Errorf("ExpiresAt = %s, want about an hour from now", token.ExpiresAt)
	}
}

func TestRefreshAccessTokenWithoutARefreshTokenDoesNotAsk(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	t.Cleanup(server.Close)
	client := httpx.New(httpx.Options{HTTPClient: server.Client()})

	_, err := RefreshAccessToken(context.Background(), client, server.URL, "  ")
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("the exchange was attempted %d time(s) without a refresh token", got)
	}
}

func TestRefreshAccessTokenRefusals(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"an expired login", http.StatusBadRequest, `{"error":"invalid_grant"}`},
		{"a rejected token", http.StatusUnauthorized, `{"error":"invalid_token"}`},
		{"an empty answer", http.StatusOK, `{}`},
		{"not a token answer", http.StatusOK, `{"plan_type":"plus"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			t.Cleanup(server.Close)
			client := httpx.New(httpx.Options{HTTPClient: server.Client()})
			if _, err := RefreshAccessToken(context.Background(), client, server.URL, "refresh"); err == nil {
				t.Fatal("the exchange succeeded on an answer that carries no token")
			}
		})
	}
}

func TestExpiresAtReadsTheTokensOwnClaim(t *testing.T) {
	// A JWT is three dot-separated base64url segments; only the payload is
	// read, and its signature is never checked: the question is "is this
	// worth sending", not "is this genuine".
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1738300000}`))
	token := "header." + payload + ".signature"
	want := time.Unix(1738300000, 0).UTC()
	if got := ExpiresAt(token); !got.Equal(want) {
		t.Errorf("ExpiresAt = %s, want %s", got, want)
	}
	for _, bad := range []string{"", "not-a-jwt", "a.b", "a." + base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".c"} {
		if got := ExpiresAt(bad); !got.IsZero() {
			t.Errorf("ExpiresAt(%q) = %s, want the zero time", bad, got)
		}
	}
}

func TestTokenResponseNumbersAreReadAsNumbers(t *testing.T) {
	// expires_in is a number in a JSON document whose fields are otherwise
	// strings; decoding it as text keeps the expiry arithmetic honest.
	var payload tokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"t","expires_in":3600}`), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d", payload.ExpiresIn)
	}
}
