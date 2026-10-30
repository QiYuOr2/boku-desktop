// Package deepseek translates text with DeepSeek's chat completions, using
// the same API key the balance panel uses: the user configured one account,
// and asking them for the same key twice would be a second thing to get
// wrong.
//
// It does HTTP and error translation, and it knows nothing about windows.
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
	// ProviderID is the translator's id, and the balance provider's: they
	// are the same service and the same key, so they are the same name.
	ProviderID = contract.ProviderID
	// defaultBaseURL is DeepSeek's own endpoint. Nothing guesses a
	// service's address.
	defaultBaseURL = "https://api.deepseek.com"
	// defaultModel is the text model the tool asks. A model name is a service's
	// own detail and it moves; it is written here so that a change is one
	// line in one file.
	defaultModel = "deepseek-chat"
	// imageModel is DeepSeek's current multimodal OpenAI-compatible model.
	// deepseek-chat accepts text only, so image requests must name this model.
	imageModel = "deepseek-flash"
)

// Translator asks DeepSeek to translate.
type Translator struct {
	client     *httpc.Client
	baseURL    string
	model      string
	imageModel string
	now        func() time.Time
}

// New returns the translator, talking to DeepSeek itself.
func New(client *httpc.Client) *Translator {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL returns the translator talking to another address. Tests
// use it to point at an httptest server.
func NewWithBaseURL(client *httpc.Client, baseURL string) *Translator {
	return &Translator{
		client:     client,
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      defaultModel,
		imageModel: imageModel,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// ID returns the translator's id.
func (t *Translator) ID() string { return ProviderID }

// Descriptor says what the translator is called and what it needs. The
// endpoint is the address as the settings stand now, so the interface can
// show where the text is going before it goes.
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

// TranslateImage asks DeepSeek's multimodal model to recognize and translate
// visible text in one request. The image stays in memory and is never passed
// through local OCR or persisted.
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

// Translate asks for the translation.
//
// Without a key it does not ask at all: an unauthenticated request would
// only teach the endpoint that the key is wrong, and the window needs to say
// "configure a key" rather than "the service refused".
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

// translate is one request and one answer.
func (t *Translator) translate(ctx context.Context, key string, req domain.TranslationRequest) (domain.TranslationResult, error) {
	// What was selected may be longer than one request should carry. The cut
	// is reported rather than made silently: half a translated paragraph
	// that says nothing about its missing half is worse than a short one
	// that says so.
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

// ensure the interface is satisfied where it is defined, not at the call
// site.
var _ domain.Translator = (*Translator)(nil)
var _ domain.ImageTranslator = (*Translator)(nil)
