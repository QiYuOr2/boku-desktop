// Package free holds the free translation engines: public endpoints that need
// no API key, no account and no configuration.
//
// There are two of them, and each one is a domain.Translator of its own:
//
//	free.microsoft   Edge's own translate endpoint (§3.1 of the free-translate research)
//	free.bilibili    B 站 Index-Translate (§3.2)
//
// They are separate translators rather than one translator with a pool behind
// it, because the interface offers them as separate choices: the translation
// section draws one option per engine, the user picks the one they want, and
// that engine is the one asked. An engine that fails reports its failure and
// does not quietly hand the text to the other — "I picked 微软, why did my text
// go to B 站" is not a question a settings page should raise.
//
// What the two share is here: the descriptor they carry, the discipline around
// one request, and the result they build. What each one asks, and what counts
// as an answer, is its own file.
package free

import (
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/translate/openai"
)

// DefaultMaxChars is the most one request carries, taken from the protocol the
// paid backends share: a selection is a sentence or a paragraph, and the
// ceiling is the same wherever the text goes.
const DefaultMaxChars = openai.DefaultMaxChars

func truncationNote(cut bool) string { return openai.TruncationNote(cut) }

// noReadingsNote is what an engine that cannot annotate says about a request
// for readings. It is openai.AnnotationNote's own words for the same
// situation — an answer that came back without pieces — because the user
// should not be able to tell which engine told them.
const noReadingsNote = "翻译服务没有逐词分段，这次没有读音与外来语标注。"

// prepared is what one request sends: the text cut to the ceiling, the source
// language decided from the text itself when the caller did not know it, and
// whether the text had to be cut.
//
// Both engines need the same three answers before they can build a request, and
// the note about a shortened text is the same note in both.
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

// annotatedResult turns an OpenAI-compatible answer into a result: the
// annotation when it was asked for, the paragraph otherwise, and the note that
// says what the answer did not have.
//
// A model that answers a plain translation when pieces were asked for is not a
// failure: the window draws the paragraph and says there are no readings,
// which is openai.AnnotationNote's own semantics — the same thing the paid
// backends already do. It is only the B 站 engine that can reach this path,
// because it is the only free engine that is asked for readings.
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

// plainResult turns a sentence-level answer into a result.
//
// An engine that answers one sentence per text — the Edge endpoint does — has
// nothing to say about the words in it. A request that asked for readings
// therefore gets the translation and the note that says there are none, which
// is the same note a model's unsegmented answer produces.
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

// ensure the interface is satisfied where it is defined, not at the call site.
var _ domain.Translator = (*Microsoft)(nil)
var _ domain.Translator = (*Bilibili)(nil)
