// Package free 提供免费翻译引擎：无需 API key、账号与配置的公开端点。
//
// 两个引擎各自是独立的 domain.Translator，而非一个带后备池的翻译器：界面把它们
// 作为独立选项呈现，用户选哪个就只问哪个；某个引擎失败只报自身错误，不会悄悄把
// 文本转给另一个。
package free

import (
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/translate/openai"
)

// DefaultMaxChars 是单次请求的最大字符数，与付费后端共用的协议一致。
const DefaultMaxChars = openai.DefaultMaxChars

func truncationNote(cut bool) string { return openai.TruncationNote(cut) }

// noReadingsNote 是不能标注的引擎对读音请求的提示。措辞与 openai.AnnotationNote
// 在同样情形（答复未分段）下一致，让用户无法分辨是哪个引擎给出的提示。
const noReadingsNote = "翻译服务没有逐词分段，这次没有读音与外来语标注。"

type prepared struct {
	text   string
	source domain.Language
	cut    bool
}

func prepare(req domain.TranslationRequest) prepared {
	text, cut := openai.Truncate(req.Text, DefaultMaxChars)
	source := req.SourceLang
	if source == domain.LangUnknown {
		source = domain.Language(kit.DetectLanguage(text))
	}
	return prepared{text: text, source: source, cut: cut}
}

// annotatedResult 把 OpenAI-compatible 答复转成结果：请求标注时给标注，否则给
// 段落，并附上说明答复缺少内容的提示。
//
// 模型在请求分段时只给译文不算失败：窗口画段落并提示没有读音，这与付费后端
// openai.AnnotationNote 的语义相同。只有 B 站引擎会走到这条路径，因为它是唯一被
// 请求标注的免费引擎。
func annotatedResult(content string, req domain.TranslationRequest, source domain.Language, text string, at time.Time) domain.TranslationResult {
	result := domain.TranslationResult{
		SourceText: text,
		SourceLang: source,
		TargetLang: req.TargetLang,
		CreatedAt:  at,
	}
	options := openai.AnnotationOptions{
		Furigana:      req.Annotation.Furigana,
		LoanwordGloss: req.Annotation.LoanwordGloss,
	}
	if !options.Wants() {
		result.TranslatedText = strings.TrimSpace(content)
		return result
	}
	annotation := openai.ParseAnnotation(content)
	if annotation.Segmented {
		result.Segments = annotation.Segments
	}
	result.TranslatedText = annotation.TranslatedText
	result.Note = openai.AnnotationNote(annotation, options)
	return result
}

// plainResult 把句级答复转成结果。
//
// 每次只答一句的引擎（如 Edge）无法提供词内信息，请求读音时因此只给译文与
// 「没有读音」的提示，与模型未分段答复产生的提示相同。
func plainResult(translated string, req domain.TranslationRequest, source domain.Language, text string, at time.Time) domain.TranslationResult {
	result := domain.TranslationResult{
		SourceText:     text,
		TranslatedText: strings.TrimSpace(translated),
		SourceLang:     source,
		TargetLang:     req.TargetLang,
		CreatedAt:      at,
	}
	if req.Annotation.Wants() {
		result.Note = noReadingsNote
	}
	return result
}

var _ domain.Translator = (*Microsoft)(nil)
var _ domain.Translator = (*Bilibili)(nil)
