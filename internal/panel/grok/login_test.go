package grok

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *memorySecrets) Get(_ context.Context, providerID, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[domain.CredentialKey(providerID, key)]
	if !ok {
		return "", domain.ErrSecretNotFound
	}
	return value, nil
}

func (s *memorySecrets) Set(_ context.Context, providerID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[domain.CredentialKey(providerID, key)] = value
	return nil
}

func (s *memorySecrets) Delete(_ context.Context, providerID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := domain.CredentialKey(providerID, key)
	if _, ok := s.values[name]; !ok {
		return domain.ErrSecretNotFound
	}
	delete(s.values, name)
	return nil
}

func (s *memorySecrets) get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[domain.CredentialKey(ProviderID, key)]
}

func TestDeviceLoginSavesTokens(t *testing.T) {
	access := token(t, time.Now().Add(time.Hour), "user-synthetic")
	var pauses []time.Duration
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		switch r.URL.Path {
		case "/device":
			if form.Get("client_id") != ClientID || form.Get("scope") != scopes || form.Get("referrer") != "boku" {
				t.Errorf("device form = %v", form)
			}
			_, _ = w.Write([]byte(`{"device_code":"device-synthetic","user_code":"ABCD-EFGH","verification_uri":"https://auth.x.ai/device","verification_uri_complete":"https://auth.x.ai/device?user_code=ABCD-EFGH","expires_in":120,"interval":"1"}`))
		case "/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			if form.Get("grant_type") != deviceGrant || form.Get("device_code") != "device-synthetic" {
				t.Errorf("poll form = %v", form)
			}
			_, _ = w.Write([]byte(`{"access_token":"` + access + `","refresh_token":"refresh-synthetic"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	secrets := &memorySecrets{}
	login := NewLogin(LoginOptions{
		Client:         httpc.New(httpc.Options{HTTPClient: server.Client()}),
		Secrets:        secrets,
		DeviceEndpoint: server.URL + "/device",
		TokenEndpoint:  server.URL + "/token",
		Now:            func() time.Time { return time.Unix(1_000, 0).UTC() },
		Pause: func(_ context.Context, wait time.Duration) error {
			pauses = append(pauses, wait)
			return nil
		},
	})
	prompt, err := login.BeginDeviceLogin(context.Background())
	if err != nil {
		t.Fatalf("begin failed: %v", err)
	}
	if prompt.UserCode != "ABCD-EFGH" || !strings.Contains(prompt.VerificationURL, "user_code=ABCD-EFGH") {
		t.Fatalf("prompt = %+v", prompt)
	}
	if err := login.FinishDeviceLogin(context.Background()); err != nil {
		t.Fatalf("finish failed: %v", err)
	}
	if secrets.get(AccessTokenCredential) != access || secrets.get(RefreshTokenCredential) != "refresh-synthetic" {
		t.Fatalf("stored access %q refresh %q", secrets.get(AccessTokenCredential), secrets.get(RefreshTokenCredential))
	}
	if len(pauses) != 1 || pauses[0] != time.Second {
		t.Fatalf("pauses = %v", pauses)
	}
	if !login.LoggedIn() {
		t.Fatal("login was not remembered")
	}
	creds, err := login.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.Secrets[AccessTokenCredential] != access {
		t.Fatalf("credentials = %+v", creds.Secrets)
	}
}

func TestExpiredAccessTokenRefreshesAndKeepsTheOldRefreshToken(t *testing.T) {
	now := time.Now().UTC()
	expired := token(t, now.Add(time.Minute), "")
	fresh := token(t, now.Add(time.Hour), "user-new")
	secrets := &memorySecrets{}
	if err := secrets.Set(context.Background(), ProviderID, AccessTokenCredential, expired); err != nil {
		t.Fatal(err)
	}
	if err := secrets.Set(context.Background(), ProviderID, RefreshTokenCredential, "refresh-old"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "refresh-old" {
			t.Errorf("form = %v", form)
		}
		_, _ = w.Write([]byte(`{"access_token":"` + fresh + `"}`))
	}))
	defer server.Close()
	login := NewLogin(LoginOptions{
		Client:        httpc.New(httpc.Options{HTTPClient: server.Client()}),
		Secrets:       secrets,
		TokenEndpoint: server.URL,
		Now:           func() time.Time { return now },
	})
	creds, err := login.Credentials(context.Background())
	if err != nil {
		t.Fatalf("credentials failed: %v", err)
	}
	if creds.Secrets[AccessTokenCredential] != fresh || secrets.get(RefreshTokenCredential) != "refresh-old" {
		t.Fatalf("access %q refresh %q", creds.Secrets[AccessTokenCredential], secrets.get(RefreshTokenCredential))
	}
}

func TestRejectedRefreshBecomesAMissingLogin(t *testing.T) {
	now := time.Now().UTC()
	secrets := &memorySecrets{}
	_ = secrets.Set(context.Background(), ProviderID, AccessTokenCredential, token(t, now.Add(time.Minute), ""))
	_ = secrets.Set(context.Background(), ProviderID, RefreshTokenCredential, "refresh-old")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()
	login := NewLogin(LoginOptions{Client: httpc.New(httpc.Options{HTTPClient: server.Client()}), Secrets: secrets, TokenEndpoint: server.URL, Now: func() time.Time { return now }})
	_, err := login.Credentials(context.Background())
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v", err)
	}
}

func TestRememberReportsAStoredLogin(t *testing.T) {
	now := time.Now().UTC()
	secrets := &memorySecrets{}
	_ = secrets.Set(context.Background(), ProviderID, AccessTokenCredential, token(t, now.Add(time.Hour), ""))
	_ = secrets.Set(context.Background(), ProviderID, RefreshTokenCredential, "refresh-old")
	login := NewLogin(LoginOptions{Secrets: secrets, Now: func() time.Time { return now }})
	if login.LoggedIn() {
		t.Fatal("logged in before reading the store")
	}
	if err := login.Remember(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !login.LoggedIn() {
		t.Fatal("a stored login was forgotten")
	}
}

func TestQuotaWithoutLoginDoesNotRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("a missing login was sent")
	}))
	defer server.Close()
	login := NewLogin(LoginOptions{Client: httpc.New(httpc.Options{HTTPClient: server.Client()}), Secrets: &memorySecrets{}, TokenEndpoint: server.URL})
	if err := login.Remember(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := login.Credentials(context.Background())
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v", err)
	}
}
