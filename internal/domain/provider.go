package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultMinRefreshSeconds is how rarely a provider is asked again unless it
// says otherwise. Providers whose endpoints are cheap lower it.
const DefaultMinRefreshSeconds = 900

// ProviderDescriptor says what a provider is called, what it needs from the
// user, and where to send them for it.
type ProviderDescriptor struct {
	ID          string
	DisplayName string
	// CredentialKeys are the secrets the provider needs, without the
	// provider prefix, such as "apiKey".
	CredentialKeys []string
	// SettingsSchema describes the provider's non-sensitive settings, as
	// setting name to type.
	SettingsSchema map[string]string
	// DocsURL is where the user gets the credentials.
	DocsURL string
	// UsageURL is the provider's own page for the account a panel reads:
	// where the numbers a panel summarizes are kept in full. It is the
	// link a panel's headline opens. A provider that has no such page
	// leaves it empty, and the interface then draws a number that is only
	// a number.
	UsageURL string
	// Endpoint is the address the provider asks, so that the interface can
	// show where a credential is going before the user hands it over.
	Endpoint string
	// MinRefreshSeconds is the shortest interval this provider's endpoint
	// tolerates; 0 means DefaultMinRefreshSeconds.
	MinRefreshSeconds int
}

// MinRefreshInterval is the shortest interval the provider tolerates.
func (d ProviderDescriptor) MinRefreshInterval() time.Duration {
	seconds := d.MinRefreshSeconds
	if seconds <= 0 {
		seconds = DefaultMinRefreshSeconds
	}
	return time.Duration(seconds) * time.Second
}

// Validate reports what is missing from the descriptor. The application
// checks it at startup: a provider nobody can configure is a configuration
// error, not something to discover when a panel fails.
func (d ProviderDescriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("%w: a provider has no id", ErrInvalidArgument)
	}
	if strings.TrimSpace(d.DisplayName) == "" {
		return fmt.Errorf("%w: provider %s has no display name", ErrInvalidArgument, d.ID)
	}
	if len(d.CredentialKeys) == 0 {
		return fmt.Errorf("%w: provider %s asks for no credentials", ErrInvalidArgument, d.ID)
	}
	seen := make(map[string]bool, len(d.CredentialKeys))
	for _, key := range d.CredentialKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: provider %s has an empty credential key", ErrInvalidArgument, d.ID)
		}
		if seen[key] {
			return fmt.Errorf("%w: provider %s asks for %q twice", ErrInvalidArgument, d.ID, key)
		}
		seen[key] = true
	}
	if strings.TrimSpace(d.DocsURL) == "" {
		return fmt.Errorf("%w: provider %s does not say where to get credentials", ErrInvalidArgument, d.ID)
	}
	if strings.TrimSpace(d.Endpoint) == "" {
		return fmt.Errorf("%w: provider %s does not say where it asks", ErrInvalidArgument, d.ID)
	}
	if d.MinRefreshSeconds < 0 {
		return fmt.Errorf("%w: provider %s has a negative refresh floor", ErrInvalidArgument, d.ID)
	}
	return nil
}

// CredentialKey is the name one credential of a provider is stored under,
// in the secret store and in the database alike: "deepseek.apiKey".
func CredentialKey(providerID, credentialKey string) string {
	return providerID + "." + credentialKey
}

// Credentials is what a provider is allowed to see: the secrets it asked
// for, in memory, for one request. It never reaches a provider's caller the
// other way round: a provider cannot name a secret it did not ask for.
type Credentials struct {
	ProviderID string
	Secrets    map[string]string
	Settings   map[string]string
}

// Secret returns one of the credentials, or ErrCredentialMissing. The error
// names the credential and never its value.
func (c Credentials) Secret(key string) (string, error) {
	value := strings.TrimSpace(c.Secrets[key])
	if value == "" {
		return "", fmt.Errorf("%w: provider %s has no %s", ErrCredentialMissing, c.ProviderID, CredentialKey(c.ProviderID, key))
	}
	return value, nil
}

// Setting returns a non-sensitive setting, or "".
func (c Credentials) Setting(key string) string { return c.Settings[key] }

// FetchResult is a provider's raw answer, before an adapter interprets it.
type FetchResult struct {
	ProviderID string
	// Endpoint is the address actually asked, with anything sensitive in it
	// already removed, for error messages and audit.
	Endpoint   string
	FetchedAt  time.Time
	Payload    []byte
	HTTPStatus int
}

// Provider asks one service for its raw answer. It does HTTP and error
// translation, and it knows nothing about panels, snapshots or metrics.
type Provider interface {
	ID() string
	Descriptor() ProviderDescriptor
	Fetch(ctx context.Context, creds Credentials) (FetchResult, error)
}

// Adapter turns one provider's raw answer into the one shape the rest of the
// application knows. It is a pure mapping: no network, no clock of its own,
// testable offline from a fixture.
type Adapter interface {
	ProviderID() string
	Normalize(result FetchResult, panel Panel) (Snapshot, error)
}

// ProviderRegistry is a use case's view of the registered providers: it
// looks one up by id and never knows which ones exist. The registry that
// implements it lives in internal/data; a use case depends on this
// interface, not on that package.
type ProviderRegistry interface {
	// Provider returns the provider registered under an id, or
	// ErrProviderNotFound.
	Provider(id string) (Provider, error)
	// Adapter returns the adapter registered under an id, or
	// ErrProviderNotFound.
	Adapter(id string) (Adapter, error)
}
