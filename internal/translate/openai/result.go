package openai

import (
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// ImageSystemPrompt is the instruction both OpenAI-compatible image translators
// send. It stays here so a change to the envelope is one change.
const ImageSystemPrompt = `You translate text visible in an image. Return exactly one JSON object and no Markdown. For a translation return {"status":"success","sourceLang":"ja","sourceText":"original visible text","targetLang":"zh","translatedText":"translation","failure":null}. Use only zh,en,ja,ko,ru,ar,th for language codes and preserve line breaks. If the image has no readable text, is unsupported, or must be refused, return {"status":"failure","sourceLang":"","sourceText":"","targetLang":"","translatedText":"","failure":{"code":"no_readable_text"}}; code must be exactly one of no_readable_text, unsupported_image, refused.`

// Prepare cuts a request to the shared ceiling and fills in an unknown source
// language from the text that will actually be sent.
func Prepare(req domain.TranslationRequest) (text string, source domain.Language, cut bool) {
	text, cut = Truncate(req.Text, DefaultMaxChars)
	source = req.SourceLang
	if source == domain.LangUnknown {
		source = domain.Language(kit.DetectLanguage(text))
	}
	return text, source, cut
}

// TruncationNote is what a result says when the text was too long to send whole.
func TruncationNote(cut bool) string {
	if !cut {
		return ""
	}
	return "原文过长，只翻译了前 " + strconv.Itoa(DefaultMaxChars) + " 个字。"
}

// TextResult turns one chat answer into a translation. A plain answer is kept
// as the model wrote it. When readings were asked for, the annotation replaces
// that text, and a shortened source overwrites the annotation note: the missing
// half of the paragraph is the thing the window has to say.
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

// ImageResult maps a successful image envelope onto a translation. The caller
// chose the target, so a model that relabels it is ignored. ParseImageResponse
// has already refused a failure status.
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
