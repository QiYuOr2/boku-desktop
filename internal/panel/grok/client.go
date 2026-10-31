// This file asks Grok for the subscription quota of the account Boku logged
// in. It does HTTP and error translation and nothing else.
//
// The endpoint is the one the Grok CLI calls. It is not a published contract,
// so a response that is not the shape the adapter knows is a bad response
// rather than a guess.
package grok

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	billingURL = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	docsURL    = "https://x.ai/cli"
	// minRefreshSeconds is the shortest interval the endpoint tolerates.
	minRefreshSeconds = 60
	// tokenAuth and clientVersion are the headers the Grok CLI sends. The
	// version is the one used when a newer CLI version cannot be read.
	tokenAuth     = "xai-grok-cli"
	clientVersion = "1.0.41"
	// ClientID is Grok CLI's public OAuth client id. It is not a secret of Boku's.
	ClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	// DeviceEndpoint and TokenEndpoint are xAI's public device-code endpoints.
	DeviceEndpoint = "https://auth.x.ai/oauth2/device/code"
	TokenEndpoint  = "https://auth.x.ai/oauth2/token"
	scopes         = "openid profile email offline_access grok-cli:access api:access"
	deviceGrant    = "urn:ietf:params:oauth:grant-type:device_code"
)

// Provider reads the billing endpoint.
type Provider struct {
	client     *httpc.Client
	billingURL string
}

// New returns the provider, talking to Grok itself.
func New(client *httpc.Client) *Provider {
	return NewWithBillingURL(client, billingURL)
}

// NewWithBillingURL returns the provider talking to another billing address.
// Tests point it at an httptest server.
func NewWithBillingURL(client *httpc.Client, billing string) *Provider {
	return &Provider{client: client, billingURL: billing}
}

// ID returns the provider id.
func (p *Provider) ID() string { return ProviderID }

// Descriptor says what the provider is called and what it needs.
func (p *Provider) Descriptor() domain.ProviderDescriptor {
	return domain.ProviderDescriptor{
		ID:                ProviderID,
		DisplayName:       "Grok",
		CredentialKeys:    []string{AccessTokenCredential},
		DocsURL:           docsURL,
		UsageURL:          UsageURL,
		Endpoint:          p.billingURL,
		MinRefreshSeconds: minRefreshSeconds,
	}
}

// Fetch asks for the account's quota. Without a token it does not ask at all.
func (p *Provider) Fetch(ctx context.Context, creds domain.Credentials) (domain.FetchResult, error) {
	token, err := creds.Secret(AccessTokenCredential)
	if err != nil {
		return domain.FetchResult{}, err
	}
	headers := map[string]string{
		"Authorization":         "Bearer " + token,
		"X-XAI-Token-Auth":      tokenAuth,
		"x-grok-client-version": clientVersion,
	}
	if userID := subject(token); userID != "" {
		headers["x-userid"] = userID
	}
	result, err := p.client.GetJSON(ctx, p.billingURL, headers)
	if err != nil {
		return result, err
	}
	result.ProviderID = ProviderID
	return result, nil
}

// subject reads the JWT sub without verifying the signature. The endpoint
// decides whether the token is genuine; the header only names the account.
func subject(token string) string {
	claims := jwtClaims(token)
	userID, _ := claims["sub"].(string)
	return strings.TrimSpace(userID)
}

// expiresAt reads the expiry a JWT states about itself. A token whose expiry
// cannot be read reports the zero time, which callers treat as "no opinion".
func expiresAt(token string) time.Time {
	raw, ok := jwtClaims(token)["exp"]
	if !ok {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(rawString(raw)), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

func rawString(value any) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return ""
	}
}

func jwtClaims(token string) map[string]any {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		claims, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}
	decoder := json.NewDecoder(strings.NewReader(string(claims)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil
	}
	return payload
}

var _ domain.Provider = (*Provider)(nil)
