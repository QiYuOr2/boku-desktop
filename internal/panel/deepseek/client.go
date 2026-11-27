// 本文件向 DeepSeek 查询账户余额。它只做 HTTP 与错误翻译，别的什么都不做：
// 数字的含义、变成哪些指标，是 adapter.go 的事。
package deepseek

import (
	"context"
	"strings"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	// defaultBaseURL 是 DeepSeek 自己的地址。应用不猜服务地址：自建或代理
	// 地址是另一个 provider，而不是这里的设置。
	defaultBaseURL = "https://api.deepseek.com"
	balancePath    = "/user/balance"
	// minRefreshSeconds 是端点能容忍的最短刷新间隔。余额请求开销小，因此
	// 远低于全局下限。
	minRefreshSeconds = 60
)

// Provider 读取余额端点。
type Provider struct {
	client  *httpc.Client
	baseURL string
}

// New 返回直连 DeepSeek 的 provider。
func New(client *httpc.Client) *Provider {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL 返回指向另一地址的 provider。测试用它指向 httptest 服务。
func NewWithBaseURL(client *httpc.Client, baseURL string) *Provider {
	return &Provider{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

// ID 返回 provider id。
func (p *Provider) ID() string { return contract.ProviderID }

// Descriptor 说明 provider 的名称和所需凭据。
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

// Fetch 请求余额。没有 key 时完全不发请求：未认证的请求只会让端点
// 记住这个 key 是错的。
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

// 在定义处而非调用处确保接口被满足。
var _ domain.Provider = (*Provider)(nil)
