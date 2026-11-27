package free

import (
	"context"
	"strings"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

// Bilibili 是 B 站的 Index-Translate 引擎：public OpenAI-compatible 端点上
// 的开放翻译模型。
//
// 它是唯一能做标注的免费引擎：对工具自己的标注 prompt 逐片返回一个 JSON 对象，
// 因而日语读音也不需要 key。这是选它翻译日语的原因。
//
// 端点是 OpenAI-compatible 的，请求复用 openai.Translate，与付费后端共用一个
// chat-completions 请求，避免维护第二份副本。
type Bilibili struct {
	client  *httpc.Client
	baseURL string
	model   string
	now     func() time.Time
}

const (
	bilibiliEndpoint = "https://index-translate.bilibili.com/v1"
	bilibiliModel    = "Index-Translate-35B-A3B"
)

func NewBilibili(client *httpc.Client) *Bilibili {
	return newBilibili(client, bilibiliEndpoint, bilibiliModel)
}

func newBilibili(client *httpc.Client, baseURL, model string) *Bilibili {
	return &Bilibili{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (b *Bilibili) ID() string { return contract.Bilibili }

func (b *Bilibili) Descriptor() domain.TranslatorDescriptor {
	return domain.TranslatorDescriptor{
		ID:                   contract.Bilibili,
		DisplayName:          "B站 Index-Translate",
		CredentialKeys:       []string{contract.APIKeyCredential},
		DocsURL:              contract.DocsURL,
		Endpoint:             "index-translate.bilibili.com/v1/chat/completions",
		AllowEmptyCredential: true,
	}
}

// Translate 不读取凭据，也不要求凭据：端点公开，省略认证头而非发送空值。
func (b *Bilibili) Translate(ctx context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	text := prepare(req)
	options := openai.AnnotationOptions{
		Furigana:      req.Annotation.Furigana,
		LoanwordGloss: req.Annotation.LoanwordGloss,
	}
	content, err := openai.Translate(ctx, openai.Options{
		Client:  b.client,
		BaseURL: b.baseURL,
		Model:   b.model,
		System:  openai.SystemPrompt(text.source, req.TargetLang, options),
		Source:  text.source,
		Target:  req.TargetLang,
		Text:    text.text,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	result := annotatedResult(content, req, text.source, text.text, b.now())
	result.Note = firstNote(truncationNote(text.cut), result.Note)
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
}
