package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Language 是工具可命名的语言，用两字母代码表示。空值表示“未知”：请求不带源语言
// 即交给翻译器检测；结果未命名语言则表示尚未分类。
type Language string

const (
	LangUnknown  Language = ""
	LangChinese  Language = "zh"
	LangEnglish  Language = "en"
	LangJapanese Language = "ja"
	LangKorean   Language = "ko"
	LangRussian  Language = "ru"
	LangArabic   Language = "ar"
	LangThai     Language = "th"
)

// translationLanguages 是允许的代码集合，用于廉价地拒绝设置文件或模型返回的非法值。
var translationLanguages = map[Language]string{
	LangChinese:  "中文",
	LangEnglish:  "English",
	LangJapanese: "日本語",
	LangKorean:   "한국어",
	LangRussian:  "Русский",
	LangArabic:   "العربية",
	LangThai:     "ไทย",
}

func (l Language) Valid() bool {
	_, ok := translationLanguages[l]
	return ok
}

// Name 返回界面显示的语言名，用该语言自身的写法。未知代码回退为代码本身，
// 以免翻译器返回本版本不认识的语音时窗口空白。
func (l Language) Name() string {
	if name, ok := translationLanguages[l]; ok {
		return name
	}
	return string(l)
}

type TranslationRequest struct {
	Text string
	// SourceLang 是调用方已知的源语言；为空时要求翻译器自行检测。
	SourceLang Language
	// TargetLang 必填：不说明目标语言的请求无法处理。
	TargetLang Language
	// Annotation 是翻译之外额外要求的内容，由调用方按用户设置决定。
	Annotation AnnotationOptions
}

// AnnotationOptions 是翻译之外可选要求的内容：逐词读音与外来词的原词。二者都针对
// 日语；不支持标注的翻译器会忽略它们，只返回译文。
type AnnotationOptions struct {
	// Furigana 要求给出日语汉字的假名读音。
	Furigana bool
	// LoanwordGloss 要求给出片假名外来词对应的英文原词。
	LoanwordGloss bool
}

func (o AnnotationOptions) Wants() bool { return o.Furigana || o.LoanwordGloss }

// Validate 在发起请求前检查缺项：缺文本或缺目标语言属于调用方错误，而非服务失败。
func (r TranslationRequest) Validate() error {
	if strings.TrimSpace(r.Text) == "" {
		return fmt.Errorf("%w: there is no text to translate", ErrInvalidArgument)
	}
	if !r.TargetLang.Valid() {
		return fmt.Errorf("%w: the target language %q is not one this build knows", ErrInvalidArgument, r.TargetLang)
	}
	if r.SourceLang != LangUnknown && !r.SourceLang.Valid() {
		return fmt.Errorf("%w: the source language %q is not one this build knows", ErrInvalidArgument, r.SourceLang)
	}
	return nil
}

// Segment 是源文本中的一个词或片段，及其可标注的信息：读音，以及片假名外来词的来源词。
// 各字段均可选；翻译器对某词无所说时仍交出该词。
type Segment struct {
	// Text 是源文本片段本身。
	Text string
	// Reading 是读音（日语为假名）；翻译器未提供时为空。
	Reading string
	// Gloss 是片假名外来词的来源词，如 コンパイラ → compiler。
	Gloss string
	// Target 是该片段自身的译文；若所有片段都无译文，则使用整体译文。
	Target string
}

func (s Segment) Validate() error {
	if strings.TrimSpace(s.Text) == "" {
		return fmt.Errorf("%w: a segment has no text", ErrInvalidArgument)
	}
	return nil
}

type TranslationResult struct {
	SourceText     string
	TranslatedText string
	// SourceLang 是翻译器判定的源语言；请求未指定源语言时即为检测结果。
	SourceLang Language
	TargetLang Language
	ProviderID string
	// Segments 是逐片段标注；为空表示翻译器不提供，此时按整体处理。
	Segments []Segment
	// Note 是翻译器对答案的提醒（被截短的文本、无法标注的片段）；答案完整时为空。
	Note      string
	CreatedAt time.Time
}

// Validate 在边界处检查结果是否是有效答案，把空答案变成可见失败而非空白窗口。
func (r TranslationResult) Validate() error {
	if strings.TrimSpace(r.SourceText) == "" {
		return fmt.Errorf("%w: the translation has no source text", ErrProviderBadResponse)
	}
	if strings.TrimSpace(r.TranslatedText) == "" && len(r.Segments) == 0 {
		return fmt.Errorf("%w: the translation is empty", ErrProviderBadResponse)
	}
	if !r.TargetLang.Valid() {
		return fmt.Errorf("%w: the translation is into %q", ErrProviderBadResponse, r.TargetLang)
	}
	if r.SourceLang != LangUnknown && !r.SourceLang.Valid() {
		return fmt.Errorf("%w: the translation is from %q", ErrProviderBadResponse, r.SourceLang)
	}
	for _, segment := range r.Segments {
		if err := segment.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Annotated 报告结果除文本外是否带有逐片段读音。
func (r TranslationResult) Annotated() bool {
	for _, segment := range r.Segments {
		if segment.Reading != "" || segment.Gloss != "" {
			return true
		}
	}
	return false
}

type TranslatorDescriptor struct {
	ID          string
	DisplayName string
	// CredentialKeys 是翻译器所需的凭据 key（不含 provider 前缀），如 "apiKey"。
	CredentialKeys []string
	// DocsURL 是获取凭据的页面；无需凭据时为服务自身页面。
	DocsURL string
	// Endpoint 是按当前设置实际请求的地址；未配置地址时为空。
	Endpoint string
	// BaseURLSetting 指向保存地址的设置键，适用于用户指向自有服务的翻译器；固定地址时为空。
	BaseURLSetting string
	// AllowEmptyCredential 由无需凭据的本地服务翻译器设置。
	AllowEmptyCredential bool
	// SupportsImage 表示该翻译器可从图片同时返回识别文本与译文。
	SupportsImage bool
}

// Validate 检查描述符缺项，装配时在启动阶段调用：配置不了的翻译器应启动失败。
func (d TranslatorDescriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("%w: a translator has no id", ErrInvalidArgument)
	}
	if strings.TrimSpace(d.DisplayName) == "" {
		return fmt.Errorf("%w: translator %s has no display name", ErrInvalidArgument, d.ID)
	}
	seen := make(map[string]bool, len(d.CredentialKeys))
	for _, key := range d.CredentialKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: translator %s has an empty credential key", ErrInvalidArgument, d.ID)
		}
		if seen[key] {
			return fmt.Errorf("%w: translator %s asks for %q twice", ErrInvalidArgument, d.ID, key)
		}
		seen[key] = true
	}
	if len(d.CredentialKeys) == 0 {
		return fmt.Errorf("%w: translator %s asks for no credentials", ErrInvalidArgument, d.ID)
	}
	if strings.TrimSpace(d.DocsURL) == "" {
		return fmt.Errorf("%w: translator %s does not say where to get credentials", ErrInvalidArgument, d.ID)
	}
	// 地址要么固定要么由设置提供，两者皆无则无处发送文本。
	if strings.TrimSpace(d.Endpoint) == "" && strings.TrimSpace(d.BaseURLSetting) == "" {
		return fmt.Errorf("%w: translator %s does not say where it asks", ErrInvalidArgument, d.ID)
	}
	return nil
}

// NeedsCredential 报告缺少指定凭据时翻译器是否无法使用。
func (d TranslatorDescriptor) NeedsCredential(key string) bool {
	if d.AllowEmptyCredential {
		return false
	}
	for _, asked := range d.CredentialKeys {
		if asked == key {
			return true
		}
	}
	return false
}

// Translator 将文本翻译为另一种语言，负责 HTTP 与错误翻译，不涉及窗口或面板。
type Translator interface {
	ID() string
	Descriptor() TranslatorDescriptor
	Translate(ctx context.Context, creds Credentials, req TranslationRequest) (TranslationResult, error)
}

// ImageTranslator 是可选扩展，供直接接受图片字节的翻译器实现；本功能刻意不做本地 OCR。
type ImageTranslator interface {
	TranslateImage(ctx context.Context, creds Credentials, image []byte, target Language) (TranslationResult, error)
}

// TranslatorRegistry 是用例视角的已注册翻译器集合：按 id 查找，不关心存在哪些。
// 具体实现在 internal/data；用例只依赖本接口，不依赖该包。
type TranslatorRegistry interface {
	// Translator 返回以 id 注册的翻译器，不存在时返回 ErrProviderNotFound。
	Translator(id string) (Translator, error)
	// TranslatorIDs 返回已排序的注册 id。
	TranslatorIDs() []string
}
