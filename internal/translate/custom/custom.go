// Package custom 用用户自选的服务翻译文本，前提是它兼容 OpenAI chat-completions 协议。
//
// 地址是设置而非常量，因为不存在固定地址；密钥也是可选的，本地服务不需要密钥。
package custom

import (
	"context"
	"fmt"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

const (
	// ProviderID 是用户自有服务注册的第一个翻译器 id，即裸 "custom"；
	// 多服务时依次为 "custom-2"、"custom-3" 等，具体 id 由翻译功能决定。
	ProviderID = "custom"
	// DefaultDisplayName 是用户未命名服务时的显示名。
	DefaultDisplayName = "自定义"
	// DefaultModel 兼容在自定义服务存储模型之前写下的旧配置文件，
	// 新服务须在设置窗口里指定模型。
	DefaultModel = "gpt-4o-mini"
	// APIKeyCredential 是密钥的存储键名，不含 provider 前缀。
	APIKeyCredential = "apiKey"
	// docsURL 是托管网关用户获取密钥的地址；本地服务不需要密钥。
	docsURL = "https://platform.openai.com/api-keys"
)

// Translator 请求用户自有服务进行翻译。
//
// 每个服务各建一个实例（可能同时有多个，各有自己的 id、地址和名称）。
type Translator struct {
	client *httpc.Client
	// id 是本实例注册的翻译器 id（"custom"、"custom-2" …），
	// 同时也是其设置的存储键，因此地址和名称互不干扰。
	id string
	// settings 使用时才读取，翻译采用请求发出那一刻的配置。
	settings func() openai.CustomService
	now      func() time.Time
}

// New 返回某个用户自有服务的翻译器。settings 用函数而非值，因为运行中用户可能改地址，
// 下次翻译应使用新地址。
func New(client *httpc.Client, id string, settings func() openai.CustomService) *Translator {
	return &Translator{
		client:   client,
		id:       id,
		settings: settings,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// NewWithBaseURL 返回使用固定地址的翻译器，忽略设置。测试用它指向 httptest 服务器。
func NewWithBaseURL(client *httpc.Client, baseURL string) *Translator {
	return New(client, ProviderID, func() openai.CustomService {
		return openai.CustomService{BaseURL: baseURL}
	})
}

// ID 返回本实例构建时所指定的 id。
func (t *Translator) ID() string { return t.id }

// Descriptor 描述服务的名称与依赖。
//
// 名称优先用用户指定的；未填写地址时 endpoint 为空，界面据此显示无处可发，
// 而不是编造一个地址。
func (t *Translator) Descriptor() domain.TranslatorDescriptor {
	current := t.current()
	return domain.TranslatorDescriptor{
		ID:                   t.id,
		DisplayName:          displayNameOf(current),
		CredentialKeys:       []string{APIKeyCredential},
		DocsURL:              docsURL,
		Endpoint:             openai.EndpointOf(current.BaseURL),
		BaseURLSetting:       "custom.baseURL",
		AllowEmptyCredential: true,
		SupportsImage:        current.SupportsImage,
	}
}

// displayNameOf 返回服务的显示名，未命名时用默认名。
func displayNameOf(service openai.CustomService) string {
	if name := strings.TrimSpace(service.DisplayName); name != "" {
		return name
	}
	return DefaultDisplayName
}

// current 返回当前设置中的配置。
//
// 地址按本功能的方式规范化，保证请求地址与存储一致，后端不再处理。
func (t *Translator) current() openai.CustomService {
	if t.settings == nil {
		return openai.CustomService{}
	}
	raw := t.settings()
	return openai.CustomService{
		BaseURL:       openai.NormalizeBaseURL(raw.BaseURL),
		DisplayName:   strings.TrimSpace(raw.DisplayName),
		Model:         modelOrDefault(raw.Model),
		SupportsImage: raw.SupportsImage,
	}
}

func modelOrDefault(model string) string {
	if value := strings.TrimSpace(model); value != "" {
		return value
	}
	// 旧配置文件早于模型字段，沿用原默认值以免静默破坏原本可用的文本服务。
	return DefaultModel
}

// TranslateImage 使用与文本翻译相同的模型和端点。它故意在 Descriptor 声明支持图片之前
// 就可供能力探测使用；常规图片翻译仍受已验证的 descriptor 标志位控制。
func (t *Translator) TranslateImage(ctx context.Context, creds domain.Credentials, image []byte, target domain.Language) (domain.TranslationResult, error) {
	if len(image) == 0 {
		return domain.TranslationResult{}, fmt.Errorf("%w: there is no image to translate", domain.ErrInvalidArgument)
	}
	if !target.Valid() {
		target = domain.LangChinese
	}
	service := t.current()
	if service.BaseURL == "" {
		return domain.TranslationResult{}, fmt.Errorf("%w: custom image translator has no address", domain.ErrInvalidArgument)
	}
	content, err := openai.TranslateImage(ctx, openai.ImageOptions{
		Client: t.client, BaseURL: service.BaseURL, Model: service.Model,
		APIKey: strings.TrimSpace(creds.Secrets[APIKeyCredential]), Target: target, Image: image,
		System: openai.ImageSystemPrompt,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	answer, err := openai.ParseImageResponse(content)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	return openai.ImageResult(answer, target, t.id, t.now())
}

// Translate 请求翻译。
//
// 未填地址在发送前拒绝，视为配置问题而非服务失败；缺少密钥不拒绝——
// 本地服务可能不需要，需要的会返回 401，界面译为“检查密钥”。
func (t *Translator) Translate(ctx context.Context, creds domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	settings := t.current()
	if settings.BaseURL == "" {
		return domain.TranslationResult{}, &domain.BadResponseError{
			ProviderID: t.id,
			Reason:     "这个自定义翻译服务还没有填写地址",
		}
	}
	// 密钥可选，缺失即不发 Authorization 头，而不是带空值；需要密钥的服务返回 401。
	key := strings.TrimSpace(creds.Secrets[APIKeyCredential])
	text, source, cut := openai.Prepare(req)
	options := openai.AnnotationOptions{Furigana: req.Annotation.Furigana, LoanwordGloss: req.Annotation.LoanwordGloss}
	content, err := openai.Translate(ctx, openai.Options{
		Client:  t.client,
		BaseURL: settings.BaseURL,
		Model:   settings.Model,
		APIKey:  key,
		System:  openai.SystemPrompt(source, req.TargetLang, options),
		Source:  source,
		Target:  req.TargetLang,
		Text:    text,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	return openai.TextResult(content, text, source, req.TargetLang, t.id, t.now(), options, cut)
}

// 在接口定义处确认实现，而非调用处。
var _ domain.Translator = (*Translator)(nil)
var _ domain.ImageTranslator = (*Translator)(nil)
