// Package deepseek 用 DeepSeek 的 chat completions 翻译文本，复用余额面板的同一把 API key：
// 同一个账号只配置一次，避免让用户重复填写同一把 key。
package deepseek

import (
	"context"
	"fmt"
	"strings"
	"time"

	contract "boku/internal/contract/deepseek"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

const (
	// ProviderID 既是翻译器的 id，也是余额 provider 的 id：同一服务、同一把 key，故同名。
	ProviderID = contract.ProviderID
	// defaultBaseURL 是 DeepSeek 自己的地址，不做任何猜测。
	defaultBaseURL = "https://api.deepseek.com"
	// defaultModel 是文本模型，集中写在这里便于一处修改。
	defaultModel = "deepseek-chat"
	// imageModel 是多模态模型；deepseek-chat 只接受文本，图片请求必须指定这个模型。
	imageModel = "deepseek-flash"
)

// Translator 用 DeepSeek 做翻译。
type Translator struct {
	client     *httpc.Client
	baseURL    string
	model      string
	imageModel string
	now        func() time.Time
}

// New 返回直接访问 DeepSeek 的翻译器。
func New(client *httpc.Client) *Translator {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL 让翻译器访问指定地址；测试用它指向 httptest 服务器。
func NewWithBaseURL(client *httpc.Client, baseURL string) *Translator {
	return &Translator{
		client:     client,
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      defaultModel,
		imageModel: imageModel,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// ID 返回翻译器的 id。
func (t *Translator) ID() string { return ProviderID }

// Descriptor 描述翻译器的名称与所需凭据；Endpoint 取当前设置下的地址，
// 便于界面在发送前显示文本将发往何处。
func (t *Translator) Descriptor() domain.TranslatorDescriptor {
	return domain.TranslatorDescriptor{
		ID:             ProviderID,
		DisplayName:    "DeepSeek",
		CredentialKeys: []string{contract.APIKeyCredential},
		DocsURL:        contract.DocsURL,
		Endpoint:       openai.EndpointOf(t.baseURL),
		SupportsImage:  true,
	}
}

// TranslateImage 让 DeepSeek 多模态模型在一次请求中识别并翻译图中文字。
// 图片仅停留在内存，不经过本地 OCR，也不落盘。
func (t *Translator) TranslateImage(ctx context.Context, creds domain.Credentials, image []byte, target domain.Language) (domain.TranslationResult, error) {
	if len(image) == 0 {
		return domain.TranslationResult{}, fmt.Errorf("%w: there is no image to translate", domain.ErrInvalidArgument)
	}
	if !target.Valid() {
		target = domain.LangChinese
	}
	key, err := creds.Secret(contract.APIKeyCredential)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	content, err := openai.TranslateImage(ctx, openai.ImageOptions{
		Client:  t.client,
		BaseURL: t.baseURL,
		Model:   t.imageModel,
		APIKey:  key,
		Target:  target,
		Image:   image,
		System:  openai.ImageSystemPrompt,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	answer, err := openai.ParseImageResponse(content)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	return openai.ImageResult(answer, target, ProviderID, t.now())
}

// Translate 发起翻译。
//
// 没有 key 时完全不发请求：未认证请求只会让接口判定 key 有误，
// 而界面应提示「请配置 key」而非「服务拒绝」。
func (t *Translator) Translate(ctx context.Context, creds domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	key, err := creds.Secret(contract.APIKeyCredential)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	return t.translate(ctx, key, req)
}

// translate 执行一次请求并得到一个回答。
func (t *Translator) translate(ctx context.Context, key string, req domain.TranslationRequest) (domain.TranslationResult, error) {
	// 选中文本可能超出单次请求上限。截断必须显式上报：只说半段却不提示少了半段，
	// 比明确说明被截断更糟。
	text, source, cut := openai.Prepare(req)
	options := openai.AnnotationOptions{Furigana: req.Annotation.Furigana, LoanwordGloss: req.Annotation.LoanwordGloss}
	content, err := openai.Translate(ctx, openai.Options{
		Client:  t.client,
		BaseURL: t.baseURL,
		Model:   t.model,
		APIKey:  key,
		System:  openai.SystemPrompt(source, req.TargetLang, options),
		Source:  source,
		Target:  req.TargetLang,
		Text:    text,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	return openai.TextResult(content, text, source, req.TargetLang, ProviderID, t.now(), options, cut)
}

// 在接口定义处而非调用处确认接口已实现。
var _ domain.Translator = (*Translator)(nil)
var _ domain.ImageTranslator = (*Translator)(nil)
