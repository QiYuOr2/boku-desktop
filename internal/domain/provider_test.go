package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCredentialsNeverLeakAValue(t *testing.T) {
	creds := Credentials{ProviderID: "deepseek", Secrets: map[string]string{}}
	if _, err := creds.Secret("apiKey"); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	} else if !strings.Contains(err.Error(), "deepseek.apiKey") {
		t.Errorf("the error does not name the missing credential: %v", err)
	}

	creds.Secrets["apiKey"] = "  sk-secret-value  "
	value, err := creds.Secret("apiKey")
	if err != nil {
		t.Fatalf("Secret(apiKey) failed: %v", err)
	}
	if value != "sk-secret-value" {
		t.Errorf("Secret(apiKey) = %q, want the trimmed value", value)
	}
	if _, err := creds.Secret("adminKey"); err == nil || strings.Contains(err.Error(), "sk-secret-value") {
		t.Errorf("the error for a missing credential leaked a value: %v", err)
	}
}

func TestProviderDescriptorValidate(t *testing.T) {
	ok := ProviderDescriptor{
		ID: "deepseek", DisplayName: "DeepSeek",
		CredentialKeys: []string{"apiKey"},
		DocsURL:        "https://platform.deepseek.com/api_keys",
		Endpoint:       "https://api.deepseek.com/user/balance",
	}
	if err := ok.Validate(); err != nil {
		t.Errorf("a valid descriptor was rejected: %v", err)
	}
	if got := ok.MinRefreshInterval(); got != time.Duration(DefaultMinRefreshSeconds)*time.Second {
		t.Errorf("MinRefreshInterval() = %s, want the default", got)
	}
	ok.MinRefreshSeconds = 60
	if got := ok.MinRefreshInterval(); got != time.Minute {
		t.Errorf("MinRefreshInterval() = %s, want 1m", got)
	}

	bad := []ProviderDescriptor{
		{DisplayName: "x", CredentialKeys: []string{"k"}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", CredentialKeys: []string{"k"}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{""}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k", "k"}, DocsURL: "u", Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k"}, Endpoint: "e"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k"}, DocsURL: "u"},
		{ID: "x", DisplayName: "y", CredentialKeys: []string{"k"}, DocsURL: "u", Endpoint: "e", MinRefreshSeconds: -1},
	}
	for _, d := range bad {
		if err := d.Validate(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("descriptor %+v: error = %v, want ErrInvalidArgument", d, err)
		}
	}
}
