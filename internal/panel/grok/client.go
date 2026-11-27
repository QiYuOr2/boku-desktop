// 本文件向 Grok 请求当前账号的订阅额度，只做 HTTP 与错误翻译。
//
// 端点即 Grok CLI 调用的地址，并非公开契约：形状不符的响应一律视为坏响应，不做猜测。
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
	// minRefreshSeconds 是该端点能接受的最短刷新间隔。
	minRefreshSeconds = 60
	// tokenAuth 与 clientVersion 是 Grok CLI 发送的请求头；版本号在无法读取更新版本时使用。
	tokenAuth     = "xai-grok-cli"
	clientVersion = "1.0.41"
	// ClientID 是 Grok CLI 的公开 OAuth client id，不是 Boku 的机密。
	ClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	// DeviceEndpoint 与 TokenEndpoint 是 xAI 的公开设备码接口。
	DeviceEndpoint = "https://auth.x.ai/oauth2/device/code"
	TokenEndpoint  = "https://auth.x.ai/oauth2/token"
	scopes         = "openid profile email offline_access grok-cli:access api:access"
	deviceGrant    = "urn:ietf:params:oauth:grant-type:device_code"
)

// Provider 读取账单端点。
type Provider struct {
	client     *httpc.Client
	billingURL string
}

// New 返回指向 Grok 正式地址的 provider。
func New(client *httpc.Client) *Provider {
	return NewWithBillingURL(client, billingURL)
}

// NewWithBillingURL 返回指向另一账单地址的 provider；测试用它指向 httptest 服务器。
func NewWithBillingURL(client *httpc.Client, billing string) *Provider {
	return &Provider{client: client, billingURL: billing}
}

func (p *Provider) ID() string { return ProviderID }

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

// Fetch 请求账号额度；没有 token 时不发请求。
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

// subject 读取 JWT 的 sub 而不校验签名：token 真伪由端点判定，此头只用来标明账号。
func subject(token string) string {
	claims := jwtClaims(token)
	userID, _ := claims["sub"].(string)
	return strings.TrimSpace(userID)
}

// expiresAt 读取 JWT 自述的过期时间；读不到时返回零值，调用方视为「无意见」。
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
