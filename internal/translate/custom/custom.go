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
	"fmt"
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

const (
	// ProviderID is the first of the translator ids a user's own services are
	// registered under: the bare "custom", which is what a file written before
	// there could be several services holds. The ones after it are numbered
	// ("custom-2", "custom-3" …), and which id a service has is the
	// translation feature's decision, not this package's.
	ProviderID = "custom"
	// DefaultDisplayName is what a service the user has not named is called.
	// It is this feature's own word, and it is only used until the user gives
	// the service a name of their own.
	DefaultDisplayName = "自定义"
	// DefaultModel preserves existing configuration files written before a
	// custom service stored its model. New services must name their model in
	// the settings window.
	DefaultModel = "gpt-4o-mini"
	// APIKeyCredential is the name the key is stored under, without the
	// provider prefix.
	APIKeyCredential = "apiKey"
	// docsURL is where a user of a hosted gateway gets a key. A local
	// service needs none, and the window says so.
	docsURL = "https://platform.openai.com/api-keys"
)

// Translator asks one of the user's own services to translate.
//
// It is built per service rather than once for the feature: the user may keep
// several — a model on their own machine, a gateway somewhere else — and each
// one has its own id, its own address and its own name. What they share is this
// type and the protocol behind it.
type Translator struct {
	client *httpc.Client
	// id is the translator id this one is registered under: "custom",
	// "custom-2", … It is also the key its settings are stored under, so the
	// address and the name it reads are its own.
	id string
	// settings is what the application was configured with. The settings
	// window reads it to show the address and the name; a translation uses
	// whatever is stored at the moment the request is made.
	settings func() openai.CustomService
	now      func() time.Time
}

// New returns the translator for one of the user's own services. Settings is a
// function rather than a value because the user may change the address while
// the application runs, and the next translation should use the new one.
func New(client *httpc.Client, id string, settings func() openai.CustomService) *Translator {
	return &Translator{
		client:   client,
		id:       id,
		settings: settings,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// NewWithBaseURL returns a translator with an address of its own, whatever
// the settings say. Tests use it to point at an httptest server.
func NewWithBaseURL(client *httpc.Client, baseURL string) *Translator {
	return New(client, ProviderID, func() openai.CustomService {
		return openai.CustomService{BaseURL: baseURL}
	})
}

// ID returns the translator's id: the one it was built for, not a constant.
func (t *Translator) ID() string { return t.id }

// Descriptor says what the service is called and what it needs.
//
// The name is the user's own when they gave one, and this feature's word for it
// when they did not: the window draws it wherever the service is named, so a
// service the user called "本地 Ollama" is called that and not "自定义".
//
// An address nobody typed yet leaves the endpoint empty: the interface shows
// that there is nowhere to send anything, rather than an address it made up.
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

// displayNameOf is what a service is called: the name the user gave it, or this
// feature's own word for a service they have not named.
func displayNameOf(service openai.CustomService) string {
	if name := strings.TrimSpace(service.DisplayName); name != "" {
		return name
	}
	return DefaultDisplayName
}

// current is what the user configured, as the settings hold it now.
//
// The address is cleaned the way this feature cleans it, so that what is asked
// is what will be stored; the backend does not judge it further.
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
	// Existing configuration files predate the model field. Keeping their
	// original default avoids silently breaking a working text service.
	return DefaultModel
}

// TranslateImage uses the same model and endpoint as the text translator. It
// is intentionally available for the capability probe before Descriptor says
// the service supports images; normal image translation remains gated by that
// verified descriptor bit.
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
		System: `You translate text visible in an image. Return exactly one JSON object and no Markdown. For a translation return {"status":"success","sourceLang":"ja","sourceText":"original visible text","targetLang":"zh","translatedText":"translation","failure":null}. Use only zh,en,ja,ko,ru,ar,th for language codes and preserve line breaks. If the image has no readable text, is unsupported, or must be refused, return {"status":"failure","sourceLang":"","sourceText":"","targetLang":"","translatedText":"","failure":{"code":"no_readable_text"}}; code must be exactly one of no_readable_text, unsupported_image, refused.`,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	answer, err := openai.ParseImageResponse(content)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	sourceText := strings.TrimSpace(answer.SourceText)
	result := domain.TranslationResult{
		SourceText:     sourceText,
		TranslatedText: strings.TrimSpace(answer.TranslatedText),
		SourceLang:     kit.ImageSourceLanguage(answer.SourceLang, sourceText),
		TargetLang:     target,
		ProviderID:     t.id,
		CreatedAt:      t.now(),
	}
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
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
			ProviderID: t.id,
			Reason:     "这个自定义翻译服务还没有填写地址",
		}
	}
	// The key is optional, and absent means absent: no Authorization header
	// is sent at all, rather than one carrying an empty key. A service that
	// needs a key answers 401, which the window translates into "check the
	// key".
	key := strings.TrimSpace(creds.Secrets[APIKeyCredential])
	text, cut := openai.Truncate(req.Text, openai.DefaultMaxChars)
	source := req.SourceLang
	if source == domain.LangUnknown {
		source = domain.Language(kit.DetectLanguage(text))
	}
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
	result := domain.TranslationResult{
		SourceText: text,
		SourceLang: source,
		TargetLang: req.TargetLang,
		ProviderID: t.id,
		CreatedAt:  t.now(),
	}
	if options.Wants() {
		annotation := openai.ParseAnnotation(content)
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
		result.Note = openai.AnnotationNote(annotation, options)
	} else {
		result.TranslatedText = content
	}
	if cut {
		result.Note = "原文过长，只翻译了前 " + strconv.Itoa(openai.DefaultMaxChars) + " 个字。"
	}
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
}

// ensure the interface is satisfied where it is defined, not at the call
// site.
var _ domain.Translator = (*Translator)(nil)
var _ domain.ImageTranslator = (*Translator)(nil)
