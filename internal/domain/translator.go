package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Translation is the second kind of third-party work Boku does, next to
// fetching a panel: the user selects text somewhere, and an answer comes
// back. It is deliberately not a Provider: a provider hands over a raw
// payload for an adapter to interpret, while a translator answers in the
// domain's own words — there is nothing between the two that an adapter
// could decide.
//
// The vendor's field names live in internal/data/providers/translate/<id>,
// never here: this file knows text, languages and segments, and nothing
// about chat completions.

// Language is a language the tool can name, as a two-letter code: "zh",
// "ja", "en", "ko". An empty Language means "not known": a request without a
// source language asks the translator to detect one, and a result that
// names none has not been classified.
type Language string

const (
	LangUnknown  Language = ""
	LangChinese  Language = "zh"
	LangEnglish  Language = "en"
	LangJapanese Language = "ja"
	LangKorean   Language = "ko"
	LangRussian  Language = "ru"
	LangArabic   Language = "ar"
)

// translationLanguages is the set a stored or detected code may be, so that
// a value from a settings file or a model's answer can be rejected cheaply.
var translationLanguages = map[Language]string{
	LangChinese:  "中文",
	LangEnglish:  "English",
	LangJapanese: "日本語",
	LangKorean:   "한국어",
	LangRussian:  "Русский",
	LangArabic:   "العربية",
}

// Valid reports whether the code is one the application knows.
func (l Language) Valid() bool {
	_, ok := translationLanguages[l]
	return ok
}

// Name is what the interface calls the language, in the language's own
// words. A code nobody knows is named by itself, so a translator may answer
// with a language this build has no word for without the window going blank.
func (l Language) Name() string {
	if name, ok := translationLanguages[l]; ok {
		return name
	}
	return string(l)
}

// TranslationRequest is what to translate, from what, and into what.
type TranslationRequest struct {
	Text string
	// SourceLang is what the caller already knows, and is empty when the
	// caller wants the translator to detect it.
	SourceLang Language
	// TargetLang is required: a request that does not say what language it
	// wants is a request nobody can answer.
	TargetLang Language
	// Annotation is how much more than a translation to ask for. It is the
	// caller's choice rather than the translator's, because it is the
	// user's: the translator says what it can provide, and the settings say
	// whether to ask.
	Annotation AnnotationOptions
}

// AnnotationOptions is what to ask the translator for beyond a translation:
// the reading of each word, and the word a loanword came from.
//
// Both are about Japanese, which is why they live here rather than in a
// provider: a translator that cannot annotate ignores them and answers with
// the translation alone.
type AnnotationOptions struct {
	// Furigana asks for the reading of Japanese kanji, written in kana.
	Furigana bool
	// LoanwordGloss asks for the English word a katakana loanword came
	// from.
	LoanwordGloss bool
}

// Wants reports whether anything beyond a translation was asked for.
func (o AnnotationOptions) Wants() bool { return o.Furigana || o.LoanwordGloss }

// Validate reports what is missing from the request. It runs before any
// request is made: text that is not there, or a target that is not named, is
// a caller's mistake rather than a service's failure.
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

// Segment is one word or one run of the source text, with what the
// translator could say about it: how it is read, and — for a loanword
// written in katakana — which word it came from.
//
// The three are separate because the interface draws them as separate lines:
// a reading above the word, the word itself, and the loanword spelled out in
// the same place the reading would be. Everything is optional: a translator
// that can say nothing about a word still hands the word over, and the
// window draws it on one line.
type Segment struct {
	// Text is the piece of the source text, as it is written.
	Text string
	// Reading is how it is read, in kana for Japanese. Empty when the
	// translator has none.
	Reading string
	// Gloss is the word the piece came from, for a loanword written in
	// katakana: コンパイラ → compiler.
	Gloss string
	// Target is this piece's own translation, when the translator gives
	// one per piece. When no piece has one, the whole translated text is
	// used instead.
	Target string
}

// Validate reports whether the segment says anything at all.
func (s Segment) Validate() error {
	if strings.TrimSpace(s.Text) == "" {
		return fmt.Errorf("%w: a segment has no text", ErrInvalidArgument)
	}
	return nil
}

// TranslationResult is one answer, in the shape the window draws: the source
// text, the translation, the languages, and — when the translator could give
// them — the piece-by-piece readings.
type TranslationResult struct {
	SourceText     string
	TranslatedText string
	// SourceLang is what the translator decided the text is, which is the
	// answer when the request did not name one.
	SourceLang Language
	TargetLang Language
	ProviderID string
	// Segments is the piece-by-piece annotation, empty when the translator
	// gives none: the window then draws the source as one line and the
	// translation as one paragraph.
	Segments []Segment
	// Note is what the translator noticed about the answer that the user
	// should know: text that had to be shortened, pieces it could not
	// annotate. It is empty when the answer was complete.
	Note      string
	CreatedAt time.Time
}

// Validate reports whether the result is an answer at all. It is checked at
// the boundary, so that a translator which answers with nothing becomes a
// visible failure instead of an empty window.
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

// Annotated reports whether the result carries more than the text: whether
// there is a piece-by-piece reading to draw.
func (r TranslationResult) Annotated() bool {
	for _, segment := range r.Segments {
		if segment.Reading != "" || segment.Gloss != "" {
			return true
		}
	}
	return false
}

// TranslatorDescriptor says what a translator is called, what it needs, and
// where the user gets it.
type TranslatorDescriptor struct {
	ID          string
	DisplayName string
	// CredentialKeys are the secrets the translator needs, without the
	// provider prefix, such as "apiKey".
	CredentialKeys []string
	// DocsURL is where the user gets a credential, or the service's own
	// page when it needs none.
	DocsURL string
	// Endpoint is the address asked with the settings as they are now, so
	// that the interface can show where the text is going. It is empty for
	// a translator whose address is not configured yet.
	Endpoint string
	// BaseURLSetting names the setting that holds the address, for a
	// translator the user points at a service of their own. It is empty
	// for a translator with a fixed address.
	BaseURLSetting string
	// AllowEmptyCredential is set by a translator that works without one:
	// a local service the user runs themselves.
	AllowEmptyCredential bool
}

// Validate reports what is missing from the descriptor. The assembly checks
// it at startup, where a translator nobody can configure is a mistake worth
// failing on rather than a window that says nothing.
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
	// An address is either fixed or named as a setting: a translator with
	// neither has nowhere to send the text.
	if strings.TrimSpace(d.Endpoint) == "" && strings.TrimSpace(d.BaseURLSetting) == "" {
		return fmt.Errorf("%w: translator %s does not say where it asks", ErrInvalidArgument, d.ID)
	}
	return nil
}

// NeedsCredential reports whether the translator can be used at all without
// the named credential.
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

// Translator turns text into another language. It does HTTP and error
// translation, and it knows nothing about windows or panels.
type Translator interface {
	ID() string
	Descriptor() TranslatorDescriptor
	Translate(ctx context.Context, creds Credentials, req TranslationRequest) (TranslationResult, error)
}

// TranslatorRegistry is a use case's view of the registered translators: it
// looks one up by id and never knows which ones exist. The registry that
// implements it lives in internal/data; a use case depends on this
// interface, not on that package.
type TranslatorRegistry interface {
	// Translator returns the translator registered under an id, or
	// ErrProviderNotFound.
	Translator(id string) (Translator, error)
	// TranslatorIDs returns the registered ids, sorted.
	TranslatorIDs() []string
}
