package domain

import "context"

// CredentialSource hands a provider what it needs when the credential is not
// Boku's to keep: the Codex quota reads the ChatGPT login the Codex CLI
// already made, and copying that token into Boku's own store would be both
// pointless and worse (docs/security.md §9).
//
// A source is registered by the assembly for one provider id. It is what the
// use case asks instead of the secret store, and it is the only other place a
// provider's credentials may come from: the alternative is not "look it up
// yourself", it is "ask for a source".
type CredentialSource interface {
	// ProviderID is the provider this source supplies.
	ProviderID() string
	// Credentials returns what that provider needs, in memory, for one
	// request. A credential that is not there is ErrCredentialMissing — the
	// provider is then never called, and the interface says why rather than
	// showing a number.
	Credentials(ctx context.Context) (Credentials, error)
}

// LoginStarter is a CredentialSource whose credential comes from a tool the
// user has to log into, and whose login can be started from here.
//
// It is a capability rather than a method on every source: a source that is
// already logged in has no login to start, and the interface then offers no
// button for one.
type LoginStarter interface {
	CredentialSource
	// StartLogin starts that tool's own login. Boku does not implement the
	// flow — the login is the tool's — so all this saves is finding a
	// terminal. ErrLoginCommandMissing and ErrLoginUnsupported say when it
	// could not be started.
	StartLogin() error
}
