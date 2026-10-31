package grok

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func token(t *testing.T, exp time.Time, subject string) string {
	t.Helper()
	encode := func(value any) string {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(payload)
	}
	claims := map[string]any{"exp": exp.Unix()}
	if subject != "" {
		claims["sub"] = subject
	}
	return encode(map[string]string{"alg": "none"}) + "." + encode(claims) + ".signature"
}

func TestFetchSendsTheCliHeaders(t *testing.T) {
	access := token(t, time.Now().Add(time.Hour), "user-synthetic")
	var got http.Header
	var path, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"config":{"creditUsagePercent":1}}`))
	}))
	defer server.Close()
	provider := NewWithBillingURL(httpc.New(httpc.Options{HTTPClient: server.Client()}), server.URL+"/v1/billing?format=credits")
	result, err := provider.Fetch(context.Background(), domain.Credentials{ProviderID: ProviderID, Secrets: map[string]string{AccessTokenCredential: access}})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if path != "/v1/billing" || query != "format=credits" {
		t.Fatalf("asked %s?%s", path, query)
	}
	if got.Get("Authorization") != "Bearer "+access {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("X-XAI-Token-Auth") != tokenAuth || got.Get("x-grok-client-version") != clientVersion {
		t.Fatalf("headers = %v", got)
	}
	if got.Get("x-userid") != "user-synthetic" {
		t.Fatalf("user = %q", got.Get("x-userid"))
	}
	if result.ProviderID != ProviderID {
		t.Fatalf("provider = %s", result.ProviderID)
	}
}

func TestFetchWithoutATokenDoesNotAsk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("a missing token was sent")
	}))
	defer server.Close()
	provider := NewWithBillingURL(httpc.New(httpc.Options{HTTPClient: server.Client()}), server.URL)
	_, err := provider.Fetch(context.Background(), domain.Credentials{ProviderID: ProviderID})
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v", err)
	}
}
