package credential

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"boku/internal/domain"
)

func TestSecretsSetGetAndMask(t *testing.T) {
	store := newServiceStore(t)
	secrets, err := NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	ctx := context.Background()

	if _, found, err := secrets.Masked(ctx, "deepseek", "apiKey"); err != nil || found {
		t.Fatalf("Masked before anything was stored = %q, %v, %v", "", found, err)
	}

	if err := secrets.Set(ctx, "deepseek", "apiKey", "sk-abcdef1234"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if value, err := secrets.Get(ctx, "deepseek", "apiKey"); err != nil || value != "sk-abcdef1234" {
		t.Fatalf("Get = %q, %v", value, err)
	}
	masked, found, err := secrets.Masked(ctx, "deepseek", "apiKey")
	if err != nil || !found {
		t.Fatalf("Masked = %q, %v, %v", masked, found, err)
	}
	if masked != "…1234" {
		t.Errorf("Masked = %q, want only the last four characters", masked)
	}
	if strings.Contains(masked, "abcdef") {
		t.Errorf("Masked leaked the value: %q", masked)
	}

	// The key is stored under the provider's namespace, not under "apiKey".
	stored, err := store.Get(ctx, "deepseek.apiKey")
	if err != nil || stored.Text() != "sk-abcdef1234" {
		t.Fatalf("stored credential after masking = %q, %v", stored.Text(), err)
	}
}

func TestSecretsRefuseEmptyValues(t *testing.T) {
	secrets, err := NewSecrets(newServiceStore(t))
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	ctx := context.Background()
	cases := []struct {
		name          string
		provider, key string
		value         string
	}{
		{"no provider", " ", "apiKey", "sk-value"},
		{"no key", "deepseek", "", "sk-value"},
		{"empty value", "deepseek", "apiKey", "   "},
	}
	for _, c := range cases {
		if err := secrets.Set(ctx, c.provider, c.key, c.value); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("%s: error = %v, want ErrInvalidArgument", c.name, err)
		}
	}
}

func TestSecretsNamesAndMasked(t *testing.T) {
	store := newServiceStore(t)
	secrets, err := NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	ctx := context.Background()
	if err := secrets.Set(ctx, "deepseek", "apiKey", "sk-d"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := secrets.Set(ctx, "weread", "apiKey", "wrk-w"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	names, err := secrets.Names(ctx, "deepseek")
	if err != nil {
		t.Fatalf("Names failed: %v", err)
	}
	if len(names) != 1 || names[0] != "deepseek.apiKey" {
		t.Fatalf("Names = %v, want only deepseek's key", names)
	}
	meta, err := store.Meta(ctx, "deepseek.apiKey")
	if err != nil {
		t.Fatalf("Meta failed: %v", err)
	}
	if meta.ProviderID != "deepseek" {
		t.Errorf("meta = %+v", meta)
	}
	if meta.CreatedAt.IsZero() {
		t.Error("the credential has no creation time")
	}

	if err := secrets.Delete(ctx, "deepseek", "apiKey"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := secrets.Get(ctx, "deepseek", "apiKey"); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("Get after Delete = %v, want ErrSecretNotFound", err)
	}
	if _, found, err := secrets.Masked(ctx, "deepseek", "apiKey"); err != nil || found {
		t.Errorf("Masked after Delete = %v, %v", found, err)
	}
	if err := secrets.Delete(ctx, "deepseek", "apiKey"); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("second Delete = %v, want ErrSecretNotFound", err)
	}
}

func TestNewSecretsNeedsAStore(t *testing.T) {
	if _, err := NewSecrets(nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestSecretsSurviveReopeningTheFile(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	secrets, err := NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	ctx := context.Background()

	if err := secrets.Set(ctx, "deepseek", "apiKey", "sk-test-0001"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	reopened, err := Open(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err = NewSecrets(reopened)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := secrets.Get(ctx, "deepseek", "apiKey"); err != nil || value != "sk-test-0001" {
		t.Errorf("Get = %q, %v", value, err)
	}
	names, err := secrets.Names(ctx, "deepseek")
	if err != nil || len(names) != 1 || names[0] != "deepseek.apiKey" {
		t.Errorf("Names = %v, %v", names, err)
	}
	if path := secrets.Path(); filepath.Base(path) != FileName {
		t.Errorf("Path = %q, want the file the store was opened with", path)
	}
}

func newServiceStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
