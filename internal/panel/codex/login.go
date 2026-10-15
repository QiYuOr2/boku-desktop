// This file reads the ChatGPT login the Codex CLI already made, and exchanges
// its refresh token for a new access token when that login has expired.
//
// It is the one place in the application that opens another tool's
// credential file. Nothing is copied into Boku's own store, nothing is
// written back to the CLI's file, and the token only ever leaves this process
// for ChatGPT (docs/security.md §9). It is a domain.CredentialSource, which
// is how the panel feature is handed a credential Boku does not keep.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	// authFileName is the file the Codex CLI writes its login to.
	authFileName = "auth.json"
	// codexHomeEnv is the CLI's own override for where that file lives.
	codexHomeEnv = "CODEX_HOME"
	// staleLogin is how old the file's own "last refresh" may be before the
	// login is refreshed even when its token still looks alive. It is the
	// same eight days the CLI works to.
	staleLogin = 8 * 24 * time.Hour
	// refreshSkew is how long before its expiry a token is replaced: an
	// answer that arrives late must not be answered with a token that died
	// on the way.
	refreshSkew = 5 * time.Minute
)

// LoginCommand is what the user would type themselves. It is here so that
// the message the interface shows and the documentation say the same thing.
const LoginCommand = "codex login"

// LoginOptions configures a Login. The zero value is usable: the real home
// directory, the real environment, and Codex's own token endpoint.
type LoginOptions struct {
	// Client makes the one request a refresh needs. The zero value is a
	// client of the application's own defaults.
	Client *httpc.Client
	// Home overrides the user's home directory: a test points it at a
	// directory of its own, and nothing else reads it.
	Home string
	// CodexHome overrides CODEX_HOME, which the CLI itself honours.
	CodexHome string
	// TokenEndpoint overrides where a refresh is sent.
	TokenEndpoint string
	// Now overrides the clock.
	Now func() time.Time
}

// Login is the credential source for the Codex panel. It is safe to use from
// several goroutines: the panels ask it off the main thread.
type Login struct {
	client        *httpc.Client
	home          string
	codexHome     string
	tokenEndpoint string
	now           func() time.Time

	mu sync.Mutex
	// remembered is a token this process exchanged for the one in the file.
	// Without it every refresh cycle would exchange the same refresh token
	// again, because the file it comes from is the CLI's and is never
	// updated by Boku.
	remembered remembered
}

type remembered struct {
	// from is the access token the file held when this one was exchanged
	// for it: a user who logs in again has a different token, and that one
	// wins.
	from  string
	token Token
}

// NewLogin returns the login source.
func NewLogin(opts LoginOptions) *Login {
	client := opts.Client
	if client == nil {
		client = httpc.New(httpc.Options{})
	}
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		if found, err := os.UserHomeDir(); err == nil {
			home = found
		}
	}
	codexHome := strings.TrimSpace(opts.CodexHome)
	if codexHome == "" {
		codexHome = strings.TrimSpace(os.Getenv(codexHomeEnv))
	}
	endpoint := strings.TrimSpace(opts.TokenEndpoint)
	if endpoint == "" {
		endpoint = TokenEndpoint
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Login{
		client:        client,
		home:          home,
		codexHome:     codexHome,
		tokenEndpoint: endpoint,
		now:           now,
	}
}

// ProviderID is the provider this source supplies.
func (l *Login) ProviderID() string { return ProviderID }

// authFile is the CLI's own login file, as it is on disk.
type authFile struct {
	// APIKey is the CLI's other login mode. It is read only to say which
	// mode the user is in: an API key cannot read a ChatGPT plan's quota.
	APIKey *string `json:"OPENAI_API_KEY"`
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// Credentials returns what the Codex provider needs: the token the CLI is
// logged in with, and the account it belongs to.
func (l *Login) Credentials(ctx context.Context) (domain.Credentials, error) {
	file, _, err := l.read()
	if err != nil {
		return domain.Credentials{}, err
	}
	if strings.TrimSpace(file.Tokens.AccessToken) == "" {
		if file.APIKey != nil && strings.TrimSpace(*file.APIKey) != "" {
			return domain.Credentials{}, fmt.Errorf("%w: the Codex CLI is logged in with an API key, and the plan's quota is only there for a ChatGPT login", domain.ErrCredentialMissing)
		}
		return domain.Credentials{}, fmt.Errorf("%w: the Codex login has no access token; run %s", domain.ErrCredentialMissing, LoginCommand)
	}
	token, err := l.token(ctx, file)
	if err != nil {
		return domain.Credentials{}, err
	}
	return domain.Credentials{
		ProviderID: ProviderID,
		Secrets:    map[string]string{AccessTokenCredential: token.AccessToken},
		Settings:   map[string]string{AccountIDSetting: strings.TrimSpace(file.Tokens.AccountID)},
	}, nil
}

// StartLogin starts the CLI's own login flow in a terminal window. The flow
// is the CLI's, not Boku's: this only saves the user finding a terminal, and
// the next reading picks the login up.
func (l *Login) StartLogin() error {
	command, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("%w: %s is not installed: %v", domain.ErrLoginCommandMissing, LoginCommand, err)
	}
	return startLogin(command)
}

// ensure the ports are satisfied where they are defined, not at the call
// site.
var (
	_ domain.CredentialSource = (*Login)(nil)
	_ domain.LoginStarter     = (*Login)(nil)
)

// paths returns where the CLI's login may be, in the order it is looked for:
// the CLI's own override first, then the two places it uses by default.
func (l *Login) paths() []string {
	if l.codexHome != "" {
		return []string{filepath.Join(l.codexHome, authFileName)}
	}
	if l.home == "" {
		return nil
	}
	return []string{
		filepath.Join(l.home, ".config", "codex", authFileName),
		filepath.Join(l.home, ".codex", authFileName),
	}
}

// read returns the first login it finds, and where it was. A machine with no
// Codex login at all is ErrCredentialMissing: it is the state before the user
// logs in, not a failure of a service.
func (l *Login) read() (authFile, string, error) {
	paths := l.paths()
	if len(paths) == 0 {
		return authFile{}, "", fmt.Errorf("%w: this machine has no home directory to look for a Codex login in", domain.ErrCredentialMissing)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return authFile{}, path, fmt.Errorf("read the Codex login at %s: %w", path, err)
		}
		var file authFile
		if err := json.Unmarshal(data, &file); err != nil {
			return authFile{}, path, fmt.Errorf("%w: the Codex login at %s is not readable: %v", domain.ErrCredentialMissing, path, err)
		}
		return file, path, nil
	}
	return authFile{}, "", fmt.Errorf("%w: no Codex login at %s (run %s)",
		domain.ErrCredentialMissing, strings.Join(paths, ", "), LoginCommand)
}

// token returns a token worth sending: the file's own while it is good, one
// this process already exchanged for it, or a new one.
//
// Nothing is written back to the file: it belongs to the CLI, and a Boku that
// rewrote it could only make the CLI's own state wrong.
func (l *Login) token(ctx context.Context, file authFile) (Token, error) {
	access := strings.TrimSpace(file.Tokens.AccessToken)
	refresh := strings.TrimSpace(file.Tokens.RefreshToken)
	expiry := ExpiresAt(access)
	lastRefresh, _ := time.Parse(time.RFC3339, strings.TrimSpace(file.LastRefresh))
	now := l.now()

	l.mu.Lock()
	cached := l.remembered
	l.mu.Unlock()
	if cached.from == access && cached.from != "" && !cached.token.ExpiresAt.IsZero() &&
		now.Before(cached.token.ExpiresAt.Add(-refreshSkew)) {
		return cached.token, nil
	}

	needsRefresh := false
	switch {
	case !expiry.IsZero():
		needsRefresh = !now.Before(expiry.Add(-refreshSkew))
	case !lastRefresh.IsZero():
		// A token that does not say when it expires is replaced on the
		// CLI's own rule: eight days after the login was last refreshed.
		needsRefresh = now.Sub(lastRefresh) > staleLogin
	}
	if !needsRefresh {
		return Token{
			AccessToken:  access,
			RefreshToken: refresh,
			IDToken:      strings.TrimSpace(file.Tokens.IDToken),
			ExpiresAt:    expiry,
		}, nil
	}

	fresh, err := RefreshAccessToken(ctx, l.client, l.tokenEndpoint, refresh)
	if err != nil {
		return Token{}, loginRefused(err)
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = refresh
	}
	if fresh.IDToken == "" {
		fresh.IDToken = strings.TrimSpace(file.Tokens.IDToken)
	}
	l.mu.Lock()
	l.remembered = remembered{from: access, token: fresh}
	l.mu.Unlock()
	return fresh, nil
}

// loginRefused turns what the token endpoint said into the one thing the
// user can act on: the login is no longer good, so log in again. A network
// failure is not that, and is passed on as it is.
func loginRefused(err error) error {
	var bad *domain.BadResponseError
	switch {
	case errors.Is(err, domain.ErrProviderUnauthorized), errors.As(err, &bad):
		return fmt.Errorf("%w: the Codex login was refused (%v); run %s", domain.ErrCredentialMissing, err, LoginCommand)
	default:
		return err
	}
}
