package panel

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"boku/internal/domain"
)

// fakeTools 是内存中的设置存储：面板功能通过它读写可选面板的开关。
//
// 它用来代替 internal/settings 的真实存储，因为这些测试针对的是面板功能，
// 而一个可按需失败的存储是触达其错误路径的方式；对功能用到的部分，行为与真实
// 存储一致。
type fakeTools struct {
	mu      sync.Mutex
	section *Section
	saves   int
	err     error
}

func newFakeTools() *fakeTools {
	return &fakeTools{section: NewSection()}
}

func (f *fakeTools) Load(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *fakeTools) Update(_ context.Context, _ string, change func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.err != nil {
		return f.err
	}
	return change()
}

// memoryStore 是内存中的 domain.SecretStore：测试不依赖文件存储，
// 需要凭据的面板直接拿到一份。
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
