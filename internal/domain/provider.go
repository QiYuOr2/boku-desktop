package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultMinRefreshSeconds 是 provider 未另行说明时的默认最小刷新间隔。
// 接口成本低的 provider 会调低它。
const DefaultMinRefreshSeconds = 900

// ProviderDescriptor 说明 provider 叫什么、需要用户提供什么、以及去哪里获取。
type ProviderDescriptor struct {
	ID          string
	DisplayName string
	// CredentialKeys 是 provider 需要的密钥，不带 provider 前缀，例如
	// "apiKey"。
	CredentialKeys []string
	// SettingsSchema 描述 provider 的非敏感设置，以设置名映射到类型。
	SettingsSchema map[string]string
	// DocsURL 是用户获取凭据的地方。
	DocsURL string
	// UsageURL 是 provider 自己的、面板所读账户的页面：面板汇总的数字在
	// 那里的完整来源。它是面板标题点击后打开的链接。没有此类页面的
	// provider 留空，界面随即只画一个纯数字。
	UsageURL string
	// Endpoint 是 provider 请求的地址，界面据此在用户交出凭据前展示凭据
	// 将被发往何处。
	Endpoint string
	// MinRefreshSeconds 是该 provider 接口能容忍的最短间隔；0 表示
	// DefaultMinRefreshSeconds。
	MinRefreshSeconds int
}

// MinRefreshInterval 是 provider 能容忍的最短间隔。
func (d ProviderDescriptor) MinRefreshInterval() time.Duration {
	seconds := d.MinRefreshSeconds
	if seconds <= 0 {
		seconds = DefaultMinRefreshSeconds
	}
	return time.Duration(seconds) * time.Second
}

// Validate 报告描述符缺少什么。应用在启动时校验它：没人能配置的
// provider 是装配错误，而不是等面板失败时才发现的。
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

// CredentialKey 是 provider 某个凭据的存储名，在 secret store 与数据库中
// 一致："deepseek.apiKey"。
func CredentialKey(providerID, credentialKey string) string {
	return providerID + "." + credentialKey
}

// Credentials 是 provider 允许看到的东西：它索要的密钥，只在一次请求的内存
// 里存在。provider 无法命名它未索要的密钥。
type Credentials struct {
	ProviderID string
	Secrets    map[string]string
	Settings   map[string]string
}

// Secret 返回其中一个凭据，或 ErrCredentialMissing。错误只含凭据名，绝不含值。
func (c Credentials) Secret(key string) (string, error) {
	value := strings.TrimSpace(c.Secrets[key])
	if value == "" {
		return "", fmt.Errorf("%w: provider %s has no %s", ErrCredentialMissing, c.ProviderID, CredentialKey(c.ProviderID, key))
	}
	return value, nil
}

// Setting 返回一个非敏感设置，或 ""。
func (c Credentials) Setting(key string) string { return c.Settings[key] }

// FetchResult 是 provider 的原始应答，尚未被 adapter 解释。
type FetchResult struct {
	ProviderID string
	// Endpoint 是实际请求的地址，其中任何敏感内容已移除，供错误信息与审计。
	Endpoint  string
	FetchedAt time.Time
	Payload   []byte
	// Cookies 是应答携带的 Set-Cookie 键值对，形如 name=value，不含属性。
	// 发放匿名身份 cookie 的服务需要在下次请求带回它们；这里没有登录凭据，
	// 调用方不得当作凭据。
	Cookies    []string
	HTTPStatus int
}

// Provider 向一个服务索取原始应答。它做 HTTP 与错误翻译，不认识面板、
// 快照或指标。
type Provider interface {
	ID() string
	Descriptor() ProviderDescriptor
	Fetch(ctx context.Context, creds Credentials) (FetchResult, error)
}

// Adapter 把一个 provider 的原始应答转换成应用其它部分认识的唯一形状。
// 它是纯映射：无网络、无自身时钟，可用 fixture 离线测试。
type Adapter interface {
	ProviderID() string
	Normalize(result FetchResult, panel Panel) (Snapshot, error)
}

// ProviderRegistry 是用例视角下已注册 provider 的集合：按 id 查找，且不知道
// 有哪些 provider 存在。实现该接口的注册表位于 internal/data；用例依赖此接口
// 而非那个包。
type ProviderRegistry interface {
	// Provider 返回以某个 id 注册的 provider，或 ErrProviderNotFound。
	Provider(id string) (Provider, error)
	// Adapter 返回以某个 id 注册的 adapter，或 ErrProviderNotFound。
	Adapter(id string) (Adapter, error)
}
