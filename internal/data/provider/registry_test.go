package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

type fakeProvider struct {
	id         string
	descriptor domain.ProviderDescriptor
}

func (f fakeProvider) ID() string { return f.id }

func (f fakeProvider) Descriptor() domain.ProviderDescriptor { return f.descriptor }

func (f fakeProvider) Fetch(context.Context, domain.Credentials) (domain.FetchResult, error) {
	return domain.FetchResult{ProviderID: f.id, Payload: []byte(`{}`)}, nil
}

type fakeAdapter struct{ id string }

func (f fakeAdapter) ProviderID() string { return f.id }

func (f fakeAdapter) Normalize(domain.FetchResult, domain.Panel) (domain.Snapshot, error) {
	return domain.Snapshot{ProviderID: f.id, Status: domain.StatusOK}, nil
}

func providerWithID(id string) fakeProvider {
	return fakeProvider{
		id: id,
		descriptor: domain.ProviderDescriptor{
			ID:             id,
			DisplayName:    id,
			CredentialKeys: []string{"apiKey"},
			DocsURL:        "https://example.com/keys",
			Endpoint:       "https://example.com/balance",
		},
	}
}

func TestRegisterAndGetAPair(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(providerWithID("deepseek"), fakeAdapter{id: "deepseek"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if _, err := registry.Provider("deepseek"); err != nil {
		t.Errorf("Provider(deepseek) failed: %v", err)
	}
	if _, err := registry.Adapter("deepseek"); err != nil {
		t.Errorf("Adapter(deepseek) failed: %v", err)
	}
	if ids := registry.IDs(); len(ids) != 1 || ids[0] != "deepseek" {
		t.Errorf("IDs() = %v, want [deepseek]", ids)
	}
}

func TestUnknownProviderIsNotFound(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Provider("nope"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("Provider(nope) error = %v, want ErrProviderNotFound", err)
	}
	if _, err := registry.Adapter("nope"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("Adapter(nope) error = %v, want ErrProviderNotFound", err)
	}
}

func TestRegisterRefusesDuplicates(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(providerWithID("deepseek"), fakeAdapter{id: "deepseek"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	err := registry.Register(providerWithID("deepseek"), fakeAdapter{id: "deepseek"})
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second Register error = %v, want ErrAlreadyExists", err)
	}
	if !strings.Contains(err.Error(), "deepseek") {
		t.Errorf("the error does not name the provider: %v", err)
	}
}

func TestRegisterRefusesAMismatch(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(providerWithID("deepseek"), fakeAdapter{id: "steam"})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "steam") || !strings.Contains(err.Error(), "deepseek") {
		t.Errorf("the error does not name both sides: %v", err)
	}
}

func TestRegisterRefusesAnInvalidDescriptor(t *testing.T) {
	registry := NewRegistry()
	provider := fakeProvider{id: "deepseek", descriptor: domain.ProviderDescriptor{ID: "deepseek"}}
	if err := registry.Register(provider, fakeAdapter{id: "deepseek"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestRegisterRefusesNils(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil, fakeAdapter{id: "x"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("nil provider: error = %v", err)
	}
	if err := registry.Register(providerWithID("x"), nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("nil adapter: error = %v", err)
	}
}

// Validate is the startup check: an unpaired registry must fail loudly and
// say which id is incomplete, not surprise the user later.
func TestValidateReportsUnpairedEntries(t *testing.T) {
	broken := &Registry{
		providers: map[string]domain.Provider{"deepseek": providerWithID("deepseek")},
		adapters:  map[string]domain.Adapter{},
	}
	err := broken.Validate()
	if !errors.Is(err, domain.ErrProviderNotFound) {
		t.Fatalf("error = %v, want ErrProviderNotFound", err)
	}
	if !strings.Contains(err.Error(), "deepseek") {
		t.Errorf("the error does not name the id: %v", err)
	}

	orphan := &Registry{
		providers: map[string]domain.Provider{},
		adapters:  map[string]domain.Adapter{"ghost": fakeAdapter{id: "ghost"}},
	}
	if err := orphan.Validate(); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Fatalf("orphan adapter: error = %v, want ErrProviderNotFound", err)
	}
}
