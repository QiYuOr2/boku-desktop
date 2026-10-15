// Package panel is the panel feature: reading the numbers a provider-backed
// panel shows, and the registry that pairs every provider with the adapter
// that interprets it.
//
// This file is the registry. The two are registered together and checked at
// startup, because a provider without an adapter can only fail later, in
// front of the user, as a card that says nothing. panels.go is the service:
// caching, single-flight, and a failure that keeps the last good reading.
//
// Nothing here knows a concrete provider: the assembly in main.go registers
// them, and each vendor's own code lives in a package under this one
// (panel/deepseek, panel/codex).
package panel

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"boku/internal/domain"
)

// Registry holds the providers and their adapters by provider id.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]domain.Provider
	adapters  map[string]domain.Adapter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]domain.Provider),
		adapters:  make(map[string]domain.Adapter),
	}
}

// Register adds a provider and the adapter that maps it.
//
// It refuses duplicates and mismatches instead of overwriting: two
// providers answering to one id means two answers to the same question, and
// only the application's assembly can decide which is right.
func (r *Registry) Register(provider domain.Provider, adapter domain.Adapter) error {
	if provider == nil {
		return fmt.Errorf("%w: a provider is nil", domain.ErrInvalidArgument)
	}
	if adapter == nil {
		return fmt.Errorf("%w: the adapter of provider %s is nil", domain.ErrInvalidArgument, provider.ID())
	}
	id := strings.TrimSpace(provider.ID())
	if id == "" {
		return fmt.Errorf("%w: a provider has no id", domain.ErrInvalidArgument)
	}
	if err := provider.Descriptor().Validate(); err != nil {
		return err
	}
	if adapter.ProviderID() != id {
		return fmt.Errorf("%w: adapter maps %s but provider is %s", domain.ErrInvalidArgument, adapter.ProviderID(), id)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return fmt.Errorf("%w: provider %s is registered twice", domain.ErrAlreadyExists, id)
	}
	if _, exists := r.adapters[id]; exists {
		return fmt.Errorf("%w: adapter for provider %s is registered twice", domain.ErrAlreadyExists, id)
	}
	r.providers[id] = provider
	r.adapters[id] = adapter
	return nil
}

// Provider returns the provider registered under an id, or
// ErrProviderNotFound.
func (r *Registry) Provider(id string) (domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return provider, nil
}

// Adapter returns the adapter registered under an id, or
// ErrProviderNotFound.
func (r *Registry) Adapter(id string) (domain.Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return adapter, nil
}

// IDs returns the registered provider ids, sorted.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Validate reports a provider without an adapter, or an adapter without a
// provider. The application calls it once the assembly is done: a
// configuration error is worth failing at startup, not on the first panel
// the user opens.
func (r *Registry) Validate() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id := range r.providers {
		if _, ok := r.adapters[id]; !ok {
			return fmt.Errorf("%w: provider %s has no adapter", domain.ErrProviderNotFound, id)
		}
	}
	for id := range r.adapters {
		if _, ok := r.providers[id]; !ok {
			return fmt.Errorf("%w: adapter %s has no provider", domain.ErrProviderNotFound, id)
		}
	}
	return nil
}
