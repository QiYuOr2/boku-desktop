// This file is the action the interface takes on credentials: list the names
// that exist, mask a value down to its last characters, store one, forget
// one. The file they are kept in is store.go, and the package comment there
// says what the protection is worth today.
package credential

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// Secrets stores and reads the credentials providers need. It owns the
// naming rule — a credential is stored as "<providerId>.<key>" — so the rest
// of the application never spells a name out.
//
// A value goes in, and comes back out only as the one value asked for or
// masked to its last four characters.
type Secrets struct {
	store domain.SecretStore
	now   func() time.Time
}

// NewSecrets returns the service over a store.
func NewSecrets(store domain.SecretStore) (*Secrets, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	return &Secrets{store: store, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Path names the file the credentials live in, for the interface to show.
// An empty path means the store keeps them somewhere without a name of its
// own.
func (s *Secrets) Path() string {
	named, ok := s.store.(interface{ Path() string })
	if !ok {
		return ""
	}
	return named.Path()
}

// Set stores one credential of a provider. The creation time of a first
// version survives an update: the store keeps it.
func (s *Secrets) Set(ctx context.Context, providerID, credentialKey, value string) error {
	providerID = strings.TrimSpace(providerID)
	credentialKey = strings.TrimSpace(credentialKey)
	if providerID == "" || credentialKey == "" {
		return fmt.Errorf("%w: a credential needs a provider and a name", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: credential %s is empty", domain.ErrInvalidArgument, domain.CredentialKey(providerID, credentialKey))
	}
	return s.store.Put(ctx, domain.CredentialKey(providerID, credentialKey), []byte(value), domain.SecretMeta{
		ProviderID: providerID,
		UpdatedAt:  s.now(),
	})
}

// Get returns one credential. Its value is for a request, not for a view.
func (s *Secrets) Get(ctx context.Context, providerID, credentialKey string) (string, error) {
	secret, err := s.store.Get(ctx, domain.CredentialKey(providerID, credentialKey))
	if err != nil {
		return "", err
	}
	return secret.Text(), nil
}

// Delete removes one credential.
func (s *Secrets) Delete(ctx context.Context, providerID, credentialKey string) error {
	return s.store.Delete(ctx, domain.CredentialKey(providerID, credentialKey))
}

// Names returns the names of a provider's stored credentials, sorted.
func (s *Secrets) Names(ctx context.Context, providerID string) ([]string, error) {
	all, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(providerID) + "."
	names := make([]string, 0, len(all))
	for _, name := range all {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, nil
}

// Masked returns a credential as the interface may show it: its last four
// characters. A credential that is not stored is not an error — it is the
// normal state before the user configures anything — and answers with "",
// false.
func (s *Secrets) Masked(ctx context.Context, providerID, credentialKey string) (string, bool, error) {
	secret, err := s.store.Get(ctx, domain.CredentialKey(providerID, credentialKey))
	if errors.Is(err, domain.ErrSecretNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(secret.Text()) == "" {
		return "", false, nil
	}
	return kit.MaskSecret(secret.Text()), true, nil
}
