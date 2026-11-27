package openai

import (
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// ImageSystemPrompt 是两个 OpenAI 兼容图片翻译器共用的指令。放在这里，改动信封只需一处。
const ImageSystemPrompt = `You translate text visible in an image. Return exactly one JSON object and no Markdown. For a translation return {"status":"success","sourceLang":"ja","sourceText":"original visible text","targetLang":"zh","translatedText":"translation","failure":null}. Use only zh,en,ja,ko,ru,ar,th for language codes and preserve line breaks. If the image has no readable text, is unsupported, or must be refused, return {"status":"failure","sourceLang":"","sourceText":"","targetLang":"","translatedText":"","failure":{"code":"no_readable_text"}}; code must be exactly one of no_readable_text, unsupported_image, refused.`

// Prepare 把请求截到公共上限，并从实际将发送的文本补齐未知的源语言。
func Prepare(req domain.TranslationRequest) (text string, source domain.Language, cut bool) {
	text, cut = Truncate(req.Text, DefaultMaxChars)
	source = req.SourceLang
	if source == domain.LangUnknown {
		source = domain.Language(kit.DetectLanguage(text))
	}
	return text, source, cut
}

// TruncationNote 是文本过长无法整段发送时结果所说的话。
func TruncationNote(cut bool) string {
	if !cut {
		return ""
	}
	return "原文过长，只翻译了前 " + strconv.Itoa(DefaultMaxChars) + " 个字。"
}

// TextResult 把一次 chat 回答转成译文。纯回答按模型所写保留。要求了读音时，标注替换该
// 文本；而截短的原文会覆盖标注提示——段落缺失的那一半才是窗口要说的。
func TextResult(content, text string, source, target domain.Language, providerID string, at time.Time, opts AnnotationOptions, cut bool) (domain.TranslationResult, error) {
	result := domain.TranslationResult{
		SourceText: text,
		SourceLang: source,
		TargetLang: target,
		ProviderID: providerID,
		CreatedAt:  at,
	}
	if opts.Wants() {
		annotation := ParseAnnotation(content)
		if annotation.Segmented {
			result.Segments = annotation.Segments
		}
		result.TranslatedText = annotation.TranslatedText
		result.Note = AnnotationNote(annotation, opts)
	} else {
		result.TranslatedText = content
	}
	if cut {
		result.Note = TruncationNote(true)
	}
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
}

// ImageResult 把成功的图片信封映射成译文。目标语言由调用方选定，模型若改标则忽略。
// ParseImageResponse 已拒绝失败状态。
func ImageResult(answer ImageResponse, target domain.Language, providerID string, at time.Time) (domain.TranslationResult, error) {
	sourceText := strings.TrimSpace(answer.SourceText)
	result := domain.TranslationResult{
		SourceText:     sourceText,
		TranslatedText: strings.TrimSpace(answer.TranslatedText),
		SourceLang:     kit.ImageSourceLanguage(answer.SourceLang, sourceText),
		TargetLang:     target,
		ProviderID:     providerID,
		CreatedAt:      at,
	}
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
}
