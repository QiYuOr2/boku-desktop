// 本文件向 ChatGPT 查询 Codex CLI 所登录账户的配额。它只做 HTTP 与错误翻译：
// 数字的含义、变成哪些指标，属于 adapter.go 的职责。
//
// 该接口 OpenAI 未公开，是 Codex CLI 自己调用的地址（厂商契约 §4.6）。因此不符合
// 下方结构的响应一律报为坏响应而非猜测，被改名的字段由 adapter 捕获。
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
	// defaultBaseURL 是 ChatGPT 自己的主机。应用不为任何服务猜测地址；用代理会
	// 成为另一个 provider。
	defaultBaseURL = "https://chatgpt.com"
	usagePath      = "/backend-api/wham/usage"
	// docsURL 是用户阅读 CLI 登录方法的地方。这个登录属于 CLI 而非 Boku：见安全设计 §9。
	docsURL = "https://github.com/openai/codex"
	// minRefreshSeconds 是该接口能接受的最短刷新间隔。
	minRefreshSeconds = 60
)

// Codex CLI 自己使用的一组 OAuth 参数：CLI 的公开 client id 与刷新所用的 token
// 端点；这里没有任何 Boku 的密钥。
const (
	// TokenEndpoint 是用 refresh token 换取 access token 的地址。
	TokenEndpoint = "https://auth.openai.com/oauth/token"
	// ClientID 是 Codex 的公开 OAuth client id。
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// Provider 读取用量接口。
type Provider struct {
	client  *httpc.Client
	baseURL string
}

// New 返回访问 ChatGPT 本体的 provider。
func New(client *httpc.Client) *Provider {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL 返回访问另一地址的 provider，测试用它指向 httptest 服务器。
func NewWithBaseURL(client *httpc.Client, baseURL string) *Provider {
	return &Provider{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

func (p *Provider) ID() string { return ProviderID }

// Descriptor 说明 provider 的名称与需求。凭据是 CLI 的 token 而非 Boku 的密钥，
// 但 provider 以相同方式索取，使装配、错误信息与跨端契约只命名它一次。
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

// Fetch 查询账户配额。没有 token 时根本不发请求：答案只会是拒绝。
func (p *Provider) Fetch(ctx context.Context, creds domain.Credentials) (domain.FetchResult, error) {
	token, err := creds.Secret(AccessTokenCredential)
	if err != nil {
		return domain.FetchResult{}, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	// 账户 id 可选，只决定多账户登录里配额属于哪个账户。
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

// Token 是 token 端点返回的一次登录。答案未给出有效期且 token 自身也不带时，
// ExpiresAt 为零值。
type Token struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    time.Time
}

// tokenResponse 是 token 端点自身的结构。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// RefreshAccessToken 用 refresh token 换取新的 access token。
//
// endpoint 用参数而非常量传入，是为了让测试能指向自己的服务器；调用方传 TokenEndpoint。
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
		// 端点给了有效期就用它；没给的 token 一直用到端点拒绝为止。
		token.ExpiresAt = ExpiresAt(token.AccessToken)
	}
	return token, nil
}

// ExpiresAt 读取 JWT 自我声明的过期时间，但不校验签名：它回答的是「还值不值得发送」，
// 而非「是否真实」——真伪由端点判断。读不到过期时间的 token 返回零值，调用方将其视为
// 「无意见」。
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

// 在接口定义处满足接口，而不是在调用点。
var _ domain.Provider = (*Provider)(nil)

// base64URL 解码一个 JWT 段，即通常省略填充的 base64url。
func base64URL(text string) ([]byte, error) {
	if data, err := base64.RawURLEncoding.DecodeString(text); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(text)
}
