// Package secret stores the application's credentials.
//
// Today they are plain text in a JSON file that the store asks the operating
// system to keep at 0600. On Windows that permission bit does not decide
// access — an ACL does — so this is not a security boundary, and the
// settings window says so. The store has nothing to unlock, which is why
// Unlock accepts anything and Unlocked always answers true: pretending
// otherwise would make a plain-text file look protected.
//
// docs/security.md §4 keeps this implementation and the encrypted vault of
// Task 008 behind one domain.SecretStore interface, so moving to the vault
// changes this package and nothing above it.
//
// A value never appears in an error or a log: the store hands values out one
// name at a time, and lists names only.
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
)

// fileVersion is the shape this build reads and writes. A file from another
// version is refused rather than partly understood.
const fileVersion = 1

// fileMode is what the store asks the file system for. Directories get 0700.
const (
	fileMode = 0o600
	dirMode  = 0o700
)

// Store keeps credentials in one file. It is safe for concurrent use: the
// file is written whole, through a temporary file and a rename, so a crash
// mid-write cannot leave a half-written secret.
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

// Open reads the store at path. A file that does not exist yet is not an
// error: the store starts empty and creates the file on the first write.
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
		// The parser's message describes the shape, not the values.
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

// Path returns the file the store keeps, for the interface to name it.
func (s *Store) Path() string { return s.path }

// Unlock accepts any passphrase: a plain-text file has nothing to unlock.
func (s *Store) Unlock(ctx context.Context, _ string) error { return contextError(ctx) }

// Lock does nothing, for the same reason: the values are readable whether it
// is called or not.
func (s *Store) Lock() {}

// Unlocked is always true here; see the package comment.
func (s *Store) Unlocked() bool { return true }

// Put stores a value, replacing an existing one and keeping the creation
// time of its first version.
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
		// The store keeps what it had: memory and file stay in step.
		if existed {
			s.secrets[name] = previous
		} else {
			delete(s.secrets, name)
		}
		return err
	}
	return nil
}

// Get returns one secret, or ErrSecretNotFound.
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

// Delete removes a value, or returns ErrSecretNotFound.
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

// List returns every name, sorted, and no values.
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

// Meta returns one name's metadata, or ErrSecretNotFound.
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

// save writes the whole file through a temporary file. The caller holds the
// lock.
func (s *Store) save() error {
	data, err := json.MarshalIndent(document{Version: fileVersion, Secrets: s.secrets}, "", "  ")
	if err != nil {
		return fmt.Errorf("secret: encode the credential file: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("secret: create %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("secret: create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer func() {
		if name != "" {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secret: write %s: %w", name, err)
	}
	if err := temp.Chmod(fileMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secret: restrict %s: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secret: flush %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("secret: close %s: %w", name, err)
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("secret: move %s into place: %w", name, err)
	}
	name = ""
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

var _ domain.SecretStore = (*Store)(nil)
