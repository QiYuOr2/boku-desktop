// Package custom translates text with a service of the user's own choosing,
// as long as it speaks the OpenAI chat-completions protocol — which is what
// most compatible services do, from a hosted gateway to a model the user
// runs on their own machine.
//
// The address is a setting rather than a constant, because there is no one
// address to write down: the translator's endpoint is whatever the user
// typed. The key is optional too: a service on localhost needs none.
//
// It does HTTP and error translation, and it knows nothing about windows.
package custom

import (
	"context"
	"strconv"
	"strings"
	"time"

	"boku/internal/data/httpx"
	"boku/internal/data/providers/translate/chat"
	"boku/internal/domain"
	"boku/internal/tools"
)

const (
	// ProviderID is the translator's id, and the name its settings are
	// stored under.
	ProviderID = "custom"
	// DefaultModel is the model asked for when the user named none. Every
	// compatible service has a different catalogue, so this is a guess that
	// the window shows: a user whose service does not know it changes it in
	// the settings, and the error names the model the service refused.
	DefaultModel = "gpt-4o-mini"
	// APIKeyCredential is the name the key is stored under, without the
	// provider prefix.
	APIKeyCredential = "apiKey"
	// docsURL is where a user of a hosted gateway gets a key. A local
	// service needs none, and the window says so.
	docsURL = "https://platform.openai.com/api-keys"
)

// Translator asks the user's own service to translate.
type Translator struct {
	client *httpx.Client
	// settings is what the application was configured with. The settings
	// window reads it to show the address and the name; a translation uses
	// whatever is stored at the moment the request is made.
	settings func() domain.CustomTranslatorSettings
	model    string
	now      func() time.Time
}

// New returns the translator. Settings is a function rather than a value
// because the user may change the address while the application runs, and
// the next translation should use the new one.
func New(client *httpx.Client, settings func() domain.CustomTranslatorSettings) *Translator {
	return &Translator{
		client:   client,
		settings: settings,
		model:    DefaultModel,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// NewWithBaseURL returns a translator with an address of its own, whatever
// the settings say. Tests use it to point at an httptest server.
func NewWithBaseURL(client *httpx.Client, baseURL string) *Translator {
	return New(client, func() domain.CustomTranslatorSettings {
		return domain.CustomTranslatorSettings{BaseURL: baseURL}
	})
}

// ID returns the translator's id.
func (t *Translator) ID() string { return ProviderID }

// Descriptor says what the translator is called and what it needs. An
// address nobody typed yet leaves the endpoint empty: the interface shows
// that there is nowhere to send anything, rather than an address it made up.
func (t *Translator) Descriptor() domain.TranslatorDescriptor {
	return domain.TranslatorDescriptor{
		ID:                   ProviderID,
		DisplayName:          "自定义",
		CredentialKeys:       []string{APIKeyCredential},
		DocsURL:              docsURL,
		Endpoint:             chat.EndpointOf(t.current().BaseURL),
		BaseURLSetting:       "custom.baseURL",
		AllowEmptyCredential: true,
	}
}

// current is what the user configured, as the settings hold it now.
func (t *Translator) current() domain.CustomTranslatorSettings {
	if t.settings == nil {
		return domain.CustomTranslatorSettings{}
	}
	return t.settings().Normalize()
}

// Translate asks for the translation.
//
// A missing address is refused before anything is sent: there is nowhere to
// send it, and that is a configuration problem rather than a service that
// failed. A missing key is not refused: a service the user runs themselves
// may need none, and one that does will say so with a 401, which the window
// translates into "check the key".
func (t *Translator) Translate(ctx context.Context, creds domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	settings := t.current()
	if settings.BaseURL == "" {
		return domain.TranslationResult{}, &domain.BadResponseError{
			ProviderID: ProviderID,
			Reason:     "自定义翻译服务的地址还没有填写",
		}
	}
	// The key is optional, and absent means absent: no Authorization header
	// is sent at all, rather than one carrying an empty key. A service that
	// needs a key answers 401, which the window translates into "check the
	// key".
	key := strings.TrimSpace(creds.Secrets[APIKeyCredential])

	text, cut := chat.Truncate(req.Text, domain.MaxTranslationChars)
	source := req.SourceLang
	if source == domain.LangUnknown {
		source = domain.Language(tools.DetectLanguage(text))
	}
	options := chat.AnnotationOptions{Furigana: req.Annotation.Furigana, LoanwordGloss: req.Annotation.LoanwordGloss}
	content, err := chat.Translate(ctx, chat.Options{
		Client:  t.client,
		BaseURL: settings.BaseURL,
		Model:   t.model,
		APIKey:  key,
		System:  chat.SystemPrompt(source, req.TargetLang, options),
		Source:  source,
		Target:  req.TargetLang,
		Text:    text,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	result := domain.TranslationResult{
		SourceText: text,
		SourceLang: source,
		TargetLang: req.TargetLang,
		ProviderID: ProviderID,
		CreatedAt:  t.now(),
	}
	if options.Wants() {
		annotation := chat.ParseAnnotation(content)
		if annotation.Segmented {
			// The pieces are the annotation only: the source the window
			// draws is the text that was sent, with the line breaks it came
			// with, as in the built-in translator.
			result.Segments = annotation.Segments
		}
		result.TranslatedText = annotation.TranslatedText
		// What could not be read is said out loud, as in the built-in
		// translator: a plain answer to a request for pieces has no
		// readings, and the window should say so.
		result.Note = chat.AnnotationNote(annotation, options)
	} else {
		result.TranslatedText = content
	}
	if cut {
		result.Note = "原文过长，只翻译了前 " + strconv.Itoa(domain.MaxTranslationChars) + " 个字。"
	}
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
}

// ensure the interface is satisfied where it is defined, not at the call
// site.
var _ domain.Translator = (*Translator)(nil)
