package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

func TestSecretsSetGetAndMask(t *testing.T) {
	store := newMemoryStore()
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
	if _, ok := store.values["deepseek.apiKey"]; !ok {
		t.Errorf("the store holds %v, want deepseek.apiKey", store.values)
	}
}

func TestSecretsRefuseEmptyValues(t *testing.T) {
	secrets, err := NewSecrets(newMemoryStore())
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
	store := newMemoryStore()
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

// The masked form is what the interface shows; the store must still hold
// the value itself.
func TestSecretsMaskedKeepsTheValueStored(t *testing.T) {
	store := newMemoryStore()
	secrets, err := NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	ctx := context.Background()
	const value = "sk-very-secret-value-9a8b"
	if err := secrets.Set(ctx, "deepseek", "apiKey", value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	masked, _, err := secrets.Masked(ctx, "deepseek", "apiKey")
	if err != nil {
		t.Fatalf("Masked failed: %v", err)
	}
	stored, err := secrets.Get(ctx, "deepseek", "apiKey")
	if err != nil || stored != value {
		t.Fatalf("Get = %q, %v; want the stored value", stored, err)
	}
	if strings.Contains(masked, "secret") {
		t.Errorf("Masked = %q", masked)
	}
}

func TestNewSecretsNeedsAStore(t *testing.T) {
	if _, err := NewSecrets(nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
}
