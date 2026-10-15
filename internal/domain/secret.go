package domain

import (
	"context"
	"time"
)

// SecretMeta describes one stored secret without saying what it is. A store
// keeps it beside the value, so that a window can list which keys exist and
// who they belong to without reading any value.
type SecretMeta struct {
	ProviderID string
	// Account is a non-sensitive label, such as which account a key
	// belongs to; it is shown, so it never holds a credential.
	Account   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Secret is one credential, in memory, for as long as it is being used.
//
// Value is the plain text and it stays here: a Secret has no String method
// on purpose, and nothing formats one — a struct printed with %v would show
// its value, which is exactly what must never reach a log.
type Secret struct {
	Name  string
	Value []byte
	Meta  SecretMeta
}

// Text returns the value as the text a request needs.
func (s Secret) Text() string { return string(s.Value) }

// SecretStore keeps credentials. The database stores only these names, never
// a value. Only this interface stands between the application and how the
// values are protected, so the file behind it can change without touching
// the layers above.
//
// The locked state is part of the interface and not of this build. A store
// that needs a passphrase before it can be read answers ErrVaultLocked —
// which is why Put, Get and the rest are written to be branchable on it —
// but the store this application assembles keeps its values in a plain-text
// file and answers true to Unlocked always. §5 of the security design says
// why, and says what protects the values instead.
type SecretStore interface {
	// Unlock makes the values readable. A store with nothing to unlock
	// accepts any passphrase — which is itself the honest answer for a
	// plain-text file.
	Unlock(ctx context.Context, passphrase string) error
	// Lock forgets what it can, so that the values are not readable again
	// until the next Unlock.
	Lock()
	// Unlocked reports whether values can be read right now.
	Unlocked() bool
	// Put stores a value under a name, replacing it and keeping the
	// creation time of the first version.
	Put(ctx context.Context, name string, value []byte, meta SecretMeta) error
	// Get returns one secret, or ErrSecretNotFound.
	Get(ctx context.Context, name string) (Secret, error)
	// Delete removes a name, or returns ErrSecretNotFound.
	Delete(ctx context.Context, name string) error
	// List returns every name, sorted, and no values.
	List(ctx context.Context) ([]string, error)
	// Meta returns one name's metadata, or ErrSecretNotFound.
	Meta(ctx context.Context, name string) (SecretMeta, error)
}
