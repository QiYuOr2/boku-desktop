package credential

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"boku/internal/domain"
)

// memoryStore is a domain.SecretStore in memory: the service tests do not
// depend on the file store beside them, and a store that fails on demand is
// how the error paths are reached.
type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	meta   map[string]domain.SecretMeta
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[string]string{}, meta: map[string]domain.SecretMeta{}}
}

func (s *memoryStore) Get(_ context.Context, name string) (domain.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[name]
	if !ok {
		return domain.Secret{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return domain.Secret{Name: name, Value: []byte(value), Meta: s.meta[name]}, nil
}

func (s *memoryStore) Unlock(context.Context, string) error { return nil }

func (s *memoryStore) Lock() {}

func (s *memoryStore) Unlocked() bool { return true }

func (s *memoryStore) Put(_ context.Context, name string, value []byte, meta domain.SecretMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.meta[name]; ok && meta.CreatedAt.IsZero() {
		meta.CreatedAt = existing.CreatedAt
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	s.values[name] = string(value)
	s.meta[name] = meta
	return nil
}

func (s *memoryStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.values[name]; !exists {
		return fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	delete(s.values, name)
	delete(s.meta, name)
	return nil
}

func (s *memoryStore) List(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.values))
	for name := range s.values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *memoryStore) Meta(_ context.Context, name string) (domain.SecretMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.meta[name]
	if !ok {
		return domain.SecretMeta{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return meta, nil
}
