package deepseek

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func newProvider(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewWithBaseURL(httpc.New(httpc.Options{HTTPClient: server.Client()}), server.URL), server
}

func credentials(key string) domain.Credentials {
	return domain.Credentials{
		ProviderID: contract.ProviderID,
		Secrets:    map[string]string{contract.APIKeyCredential: key},
	}
}

func TestFetchAsksTheBalanceEndpointWithTheKey(t *testing.T) {
	var path, authorization string
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_available":true,"balance_infos":[]}`))
	})

	result, err := provider.Fetch(context.Background(), credentials("sk-test-key"))
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if path != balancePath {
		t.Errorf("path = %q, want %q", path, balancePath)
	}
	if authorization != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q", authorization)
	}
	if result.ProviderID != contract.ProviderID {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, contract.ProviderID)
	}
	if result.HTTPStatus != 200 {
		t.Errorf("HTTPStatus = %d, want 200", result.HTTPStatus)
	}
}

func TestFetchWithoutAKeyDoesNotAsk(t *testing.T) {
	var calls atomic.Int32
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	})
	_, err := provider.Fetch(context.Background(), credentials(""))
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("the provider made %d request(s) without a key, want 0", got)
	}
	if !strings.Contains(err.Error(), "deepseek.apiKey") {
		t.Errorf("the error does not name the credential: %v", err)
	}
}

func TestFetchTranslatesStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{401, domain.ErrProviderUnauthorized},
		{403, domain.ErrProviderUnauthorized},
		{429, domain.ErrProviderRateLimited},
		{500, domain.ErrProviderUnavailable},
		{503, domain.ErrProviderUnavailable},
	}
	for _, c := range cases {
		provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails"}}`))
		})
		_, err := provider.Fetch(context.Background(), credentials("sk-test-key"))
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: error = %v, want %v", c.status, err, c.want)
		}
	}
}

func TestFetchErrorsNeverLeakTheKey(t *testing.T) {
	provider, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails"}}`))
	})
	const key = "sk-super-secret-value"
	_, err := provider.Fetch(context.Background(), credentials(key))
	if err == nil {
		t.Fatal("the request did not fail")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the error leaks the key: %v", err)
	}
}

func TestDescriptor(t *testing.T) {
	provider := New(httpc.New(httpc.Options{}))
	descriptor := provider.Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("the descriptor is invalid: %v", err)
	}
	if descriptor.ID != provider.ID() {
		t.Errorf("descriptor id %q and provider id %q differ", descriptor.ID, provider.ID())
	}
	if descriptor.DocsURL == "" {
		t.Error("the descriptor does not say where to get a key")
	}
	// The page the panel's headline opens is the provider's own account
	// page, and it is https: the interface hands it to the system to open.
	if descriptor.UsageURL != UsageURL {
		t.Errorf("UsageURL = %q, want %q", descriptor.UsageURL, UsageURL)
	}
	if !strings.HasPrefix(descriptor.UsageURL, "https://") {
		t.Errorf("UsageURL = %q, want an https address", descriptor.UsageURL)
	}
	if !strings.HasSuffix(descriptor.Endpoint, balancePath) {
		t.Errorf("Endpoint = %q, want the balance address", descriptor.Endpoint)
	}
	if got := descriptor.MinRefreshInterval(); got != time.Minute {
		t.Errorf("MinRefreshInterval() = %s, want 1m", got)
	}
	// The default base URL is what a real run talks to.
	if !strings.HasPrefix(New(httpc.New(httpc.Options{})).baseURL, "https://api.deepseek.com") {
		t.Errorf("the default base URL is %q", New(httpc.New(httpc.Options{})).baseURL)
	}
}
