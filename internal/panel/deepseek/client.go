// This file asks DeepSeek for an account's balance. It does HTTP and error
// translation and nothing else: what the numbers mean, and which metrics they
// become, is adapter.go's business.
package deepseek

import (
	"context"
	"strings"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	// defaultBaseURL is DeepSeek's own endpoint. Nothing in the application
	// guesses a service's address; a self-hosted or proxied address is a
	// provider of its own, not a setting here.
	defaultBaseURL = "https://api.deepseek.com"
	balancePath    = "/user/balance"
	// minRefreshSeconds is the shortest interval the endpoint tolerates.
	// The balance call is cheap, so it is far below the global floor.
	minRefreshSeconds = 60
)

// Provider reads the balance endpoint.
type Provider struct {
	client  *httpc.Client
	baseURL string
}

// New returns the provider, talking to DeepSeek itself.
func New(client *httpc.Client) *Provider {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL returns the provider talking to another address. Tests use
// it to point at an httptest server.
func NewWithBaseURL(client *httpc.Client, baseURL string) *Provider {
	return &Provider{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

// ID returns the provider id.
func (p *Provider) ID() string { return contract.ProviderID }

// Descriptor says what the provider is called and what it needs.
func (p *Provider) Descriptor() domain.ProviderDescriptor {
	return domain.ProviderDescriptor{
		ID:                contract.ProviderID,
		DisplayName:       "DeepSeek",
		CredentialKeys:    []string{contract.APIKeyCredential},
		DocsURL:           contract.DocsURL,
		UsageURL:          UsageURL,
		Endpoint:          p.baseURL + balancePath,
		MinRefreshSeconds: minRefreshSeconds,
	}
}

// Fetch asks for the balance. Without a key it does not ask at all: an
// unauthenticated request would only teach the endpoint that the key is
// wrong.
func (p *Provider) Fetch(ctx context.Context, creds domain.Credentials) (domain.FetchResult, error) {
	key, err := creds.Secret(contract.APIKeyCredential)
	if err != nil {
		return domain.FetchResult{}, err
	}
	endpoint := p.baseURL + balancePath
	result, err := p.client.GetJSON(ctx, endpoint, map[string]string{
		"Authorization": "Bearer " + key,
	})
	if err != nil {
		return result, err
	}
	result.ProviderID = contract.ProviderID
	return result, nil
}

// ensure the interface is satisfied where it is defined, not at the call
// site.
var _ domain.Provider = (*Provider)(nil)
