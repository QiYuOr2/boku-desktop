// This file asks ChatGPT for the quota of the account the Codex CLI is logged
// in with. It does HTTP and error translation and nothing else: what the
// numbers mean, and which metrics they become, is adapter.go's business.
//
// The endpoint is not documented by OpenAI: it is what the Codex CLI itself
// calls (docs/providers.md §4.6). A response that is not the shape below is
// therefore reported as a bad response rather than guessed at, and the
// adapter is where a renamed field is caught.
package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/httpc"
)

const (
	// defaultBaseURL is ChatGPT's own host. Nothing in the application
	// guesses a service's address; a proxy would be a provider of its own.
	defaultBaseURL = "https://chatgpt.com"
	usagePath      = "/backend-api/wham/usage"
	// docsURL is where the user reads how to log the CLI in. The login is
	// the CLI's, not Boku's: docs/security.md §9.
	docsURL = "https://github.com/openai/codex"
	// minRefreshSeconds is the shortest interval the endpoint tolerates.
	minRefreshSeconds = 60
)

// The OAuth parameters the Codex CLI itself uses. They are the CLI's public
// client id and the token endpoint its refresh goes to; nothing here is a
// secret of Boku's.
const (
	// TokenEndpoint is where a refresh token is exchanged for an access
	// token.
	TokenEndpoint = "https://auth.openai.com/oauth/token"
	// ClientID is Codex's public OAuth client id.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// Provider reads the usage endpoint.
type Provider struct {
	client  *httpc.Client
	baseURL string
}

// New returns the provider, talking to ChatGPT itself.
func New(client *httpc.Client) *Provider {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL returns the provider talking to another address. Tests use
// it to point at an httptest server.
func NewWithBaseURL(client *httpc.Client, baseURL string) *Provider {
	return &Provider{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

// ID returns the provider id.
func (p *Provider) ID() string { return ProviderID }

// Descriptor says what the provider is called and what it needs. The
// credential is the CLI's token rather than one of Boku's keys, but the
// provider asks for it the same way, so that the assembly, the error
// messages and the cross-end contract name it once.
func (p *Provider) Descriptor() domain.ProviderDescriptor {
	return domain.ProviderDescriptor{
		ID:                ProviderID,
		DisplayName:       "Codex",
		CredentialKeys:    []string{AccessTokenCredential},
		SettingsSchema:    map[string]string{AccountIDSetting: "string"},
		DocsURL:           docsURL,
		UsageURL:          UsageURL,
		Endpoint:          p.baseURL + usagePath,
		MinRefreshSeconds: minRefreshSeconds,
	}
}

// Fetch asks for the account's quota. Without a token it does not ask at
// all: the answer would only be a refusal.
func (p *Provider) Fetch(ctx context.Context, creds domain.Credentials) (domain.FetchResult, error) {
	token, err := creds.Secret(AccessTokenCredential)
	if err != nil {
		return domain.FetchResult{}, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	// The account id is optional and only decides which account of a
	// multi-account login the quota belongs to.
	if account := strings.TrimSpace(creds.Setting(AccountIDSetting)); account != "" {
		headers["ChatGPT-Account-Id"] = account
	}
	result, err := p.client.GetJSON(ctx, p.baseURL+usagePath, headers)
	if err != nil {
		return result, err
	}
	result.ProviderID = ProviderID
	return result, nil
}

// Token is a login as the token endpoint hands it back. ExpiresAt is zero
// when the answer named no lifetime and the token carried none.
type Token struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    time.Time
}

// tokenResponse is the token endpoint's own shape.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// RefreshAccessToken exchanges a refresh token for a new access token.
//
// endpoint is a parameter rather than the constant so that a test can point
// it at its own server; the caller passes TokenEndpoint.
func RefreshAccessToken(ctx context.Context, client *httpc.Client, endpoint, refreshToken string) (Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Token{}, &domain.BadResponseError{
			Endpoint: kit.Redact(endpoint),
			Reason:   "the login carries no refresh token",
		}
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = TokenEndpoint
	}
	result, err := client.PostForm(ctx, endpoint, nil, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return Token{}, err
	}
	var payload tokenResponse
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return Token{}, &domain.BadResponseError{
			ProviderID: ProviderID,
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the token endpoint did not answer with a token: " + err.Error(),
			Snippet:    domain.Snippet(result.Payload),
		}
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		reason := "the token endpoint answered no access token"
		if payload.Error != "" {
			reason = "the token endpoint refused the refresh: " + payload.Error
		}
		return Token{}, &domain.BadResponseError{
			ProviderID: ProviderID,
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     reason,
			Snippet:    domain.Snippet(result.Payload),
		}
	}
	token := Token{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		IDToken:      strings.TrimSpace(payload.IDToken),
	}
	switch {
	case payload.ExpiresIn > 0:
		token.ExpiresAt = result.FetchedAt.Add(time.Duration(payload.ExpiresIn) * time.Second)
	default:
		// A token that says when it expires is asked; one that does not is
		// used until the endpoint refuses it.
		token.ExpiresAt = ExpiresAt(token.AccessToken)
	}
	return token, nil
}

// ExpiresAt reads the expiry a JWT states about itself, without verifying
// its signature: the question it answers is "is this worth sending", not
// "is this genuine" — the endpoint decides that. A token whose expiry cannot
// be read reports the zero time, which callers read as "no opinion".
func ExpiresAt(token string) time.Time {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	claims, err := base64URL(parts[1])
	if err != nil {
		return time.Time{}
	}
	var payload struct {
		ExpiresAt json.Number `json:"exp"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(claims)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(payload.ExpiresAt.String(), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

// ensure the interface is satisfied where it is defined, not at the call
// site.
var _ domain.Provider = (*Provider)(nil)

// base64URL decodes one JWT segment, which is base64url with the padding
// usually left off.
func base64URL(text string) ([]byte, error) {
	if data, err := base64.RawURLEncoding.DecodeString(text); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(text)
}
