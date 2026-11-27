// Package credential 负责应用的凭据：存放它们的文件（本文件）与界面能对它们做的操作（service.go）。
//
// 值以明文 JSON 保存，按 0600 请求权限。这是有意的选择：没有用户输入的
// 口令，就不存在一个既能解密、又不会被能读到该目录的攻击者读到的地方放密钥。
// 把文件用旁边的密钥加密只是看起来像保护，实际并没有，比如实说明更糟。
//
// 因此保护值靠的不是密码学，而是能真正保证的事：
//
//   - 文件按 0600 写入；Windows 上由 ACL 决定访问（仅所有者、管理员与 SYSTEM 可读目录）；
//   - 值不出现在错误或日志中，也不进数据库，数据库只存名字；
//   - 设置窗口明确告知凭据是明文文件，不误导用户。
package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
)

// FileName 是凭据文件名，位于用户数据目录下。
const FileName = "secrets.json"

// fileVersion 是当前构建读写的数据结构版本；版本不符的文件直接拒绝，不尝试部分解析。
const fileVersion = 1

// Store 把凭据保存在单个文件中，可并发使用：整份文件经临时文件加改名写入，
// 写到一半崩溃也不会留下残缺的密钥。
type Store struct {
	path string

	mu      sync.Mutex
	secrets map[string]stored
}

type stored struct {
	Value      string    `json:"value"`
	ProviderID string    `json:"providerId"`
	Account    string    `json:"account,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type document struct {
	Version int               `json:"version"`
	Secrets map[string]stored `json:"secrets"`
}

// Open 读取 path 处的存储。文件尚不存在不算错误：存储为空，首次写入时创建。
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: the secret store has no path", domain.ErrInvalidArgument)
	}
	store := &Store{path: path, secrets: make(map[string]stored)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secret: read %s: %w", path, err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		// 解析器的消息只描述结构，不涉及值。
		return nil, fmt.Errorf("secret: %s is not a readable credential file: %w", path, err)
	}
	if doc.Version != fileVersion {
		return nil, fmt.Errorf("secret: %s is version %d, this build reads version %d", path, doc.Version, fileVersion)
	}
	if doc.Secrets != nil {
		store.secrets = doc.Secrets
	}
	return store, nil
}

// Path 返回存储的文件路径，供界面展示。
func (s *Store) Path() string { return s.path }

// Unlock 接受任意口令：明文文件没有可解锁的东西。
func (s *Store) Unlock(ctx context.Context, _ string) error { return contextError(ctx) }

// Lock 什么也不做：值无论是否调用它都可读。
func (s *Store) Lock() {}

// Unlocked 恒为 true，见包注释。
func (s *Store) Unlocked() bool { return true }

// Put 写入一个值，覆盖已有值并保留首次创建的 createdAt。
func (s *Store) Put(ctx context.Context, name string, value []byte, meta domain.SecretMeta) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: a credential needs a name", domain.ErrInvalidArgument)
	}
	if len(strings.TrimSpace(string(value))) == 0 {
		return fmt.Errorf("%w: credential %s is empty", domain.ErrInvalidArgument, name)
	}

	now := time.Now().UTC()
	entry := stored{
		Value:      string(value),
		ProviderID: meta.ProviderID,
		Account:    meta.Account,
		CreatedAt:  meta.CreatedAt,
		UpdatedAt:  meta.UpdatedAt,
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = now
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.secrets[name]
	if existed && previous.CreatedAt.Before(entry.CreatedAt) {
		entry.CreatedAt = previous.CreatedAt
	}
	s.secrets[name] = entry
	if err := s.save(); err != nil {
		// 回滚：内存与文件保持一致。
		if existed {
			s.secrets[name] = previous
		} else {
			delete(s.secrets, name)
		}
		return err
	}
	return nil
}

func (s *Store) Get(ctx context.Context, name string) (domain.Secret, error) {
	if err := contextError(ctx); err != nil {
		return domain.Secret{}, err
	}
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.secrets[name]
	if !ok {
		return domain.Secret{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return domain.Secret{Name: name, Value: []byte(entry.Value), Meta: entry.meta()}, nil
}

func (s *Store) Delete(ctx context.Context, name string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.secrets[name]
	if !ok {
		return fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	delete(s.secrets, name)
	if err := s.save(); err != nil {
		s.secrets[name] = previous
		return err
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]string, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.secrets))
	for name := range s.secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *Store) Meta(ctx context.Context, name string) (domain.SecretMeta, error) {
	if err := contextError(ctx); err != nil {
		return domain.SecretMeta{}, err
	}
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.secrets[name]
	if !ok {
		return domain.SecretMeta{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return entry.meta(), nil
}

func (s stored) meta() domain.SecretMeta {
	return domain.SecretMeta{
		ProviderID: s.ProviderID,
		Account:    s.Account,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}
}

// save 经临时文件写入整份文件；调用方持有锁。
func (s *Store) save() error {
	data, err := json.MarshalIndent(document{Version: fileVersion, Secrets: s.secrets}, "", "  ")
	if err != nil {
		return fmt.Errorf("secret: encode the credential file: %w", err)
	}
	data = append(data, '\n')

	if err := atomicfile.Write(s.path, data); err != nil {
		return fmt.Errorf("secret: %w", err)
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

var _ domain.SecretStore = (*Store)(nil)
