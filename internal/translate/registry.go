// Package translate is the translation tool: turning text the user selected
// into another language, the registry of translators it may ask, and the
// settings that decide which one and how.
//
// This file is the registry. It is the same idea as the provider registry
// next door, and deliberately not the same type: a provider and an adapter
// are registered together because a raw payload needs an interpreter, while a
// translator answers in the domain's own words and stands alone. Nothing here
// knows a concrete translator: the assembly in main.go registers them, and
// each backend lives in a package under this one (translate/deepseek,
// translate/custom, and translate/openai for the protocol they share).
package translate

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"boku/internal/domain"
)

// Registry holds the translators by id.
type Registry struct {
	mu    sync.RWMutex
	byIDs map[string]domain.Translator
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byIDs: make(map[string]domain.Translator)}
}

// Register adds a translator.
//
// It refuses a duplicate rather than overwriting: two translators answering
// to one id means two answers to the same question, and only the assembly
// can decide which is right. A descriptor that does not validate is refused
// too — a translator nobody can configure is a mistake worth failing on at
// startup, not on the first press of the shortcut.
func (r *Registry) Register(reply domain.Translator) error {
	if reply == nil {
		return fmt.Errorf("%w: a translator is nil", domain.ErrInvalidArgument)
	}
	id := strings.TrimSpace(reply.ID())
	if id == "" {
		return fmt.Errorf("%w: a translator has no id", domain.ErrInvalidArgument)
	}
	if err := reply.Descriptor().Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byIDs[id]; exists {
		return fmt.Errorf("%w: translator %s is registered twice", domain.ErrAlreadyExists, id)
	}
	r.byIDs[id] = reply
	return nil
}

// Translator returns the translator registered under an id, or
// ErrProviderNotFound.
func (r *Registry) Translator(id string) (domain.Translator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reply, ok := r.byIDs[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("%w: translator %s", domain.ErrProviderNotFound, id)
	}
	return reply, nil
}

// TranslatorIDs returns the registered ids, sorted, which is the order the
// settings window offers them in.
func (r *Registry) TranslatorIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.byIDs))
	for id := range r.byIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Len is how many translators are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byIDs)
}

// ensure the interface is satisfied where it is defined, not at the call
// site.
var _ domain.TranslatorRegistry = (*Registry)(nil)
