// Package deepseek translates text with DeepSeek's chat completions, using
// the same API key the balance panel uses: the user configured one account,
// and asking them for the same key twice would be a second thing to get
// wrong.
//
// It does HTTP and error translation, and it knows nothing about windows.
package deepseek

import (
	"context"
	"strconv"
	"strings"
	"time"

	"boku/internal/data/httpx"
	"boku/internal/data/providers/translate/chat"
	"boku/internal/domain"
	deepseekdomain "boku/internal/domain/deepseek"
	"boku/internal/tools"
)

const (
	// ProviderID is the translator's id, and the balance provider's: they
	// are the same service and the same key, so they are the same name.
	ProviderID = deepseekdomain.ProviderID
	// defaultBaseURL is DeepSeek's own endpoint. Nothing guesses a
	// service's address.
	defaultBaseURL = "https://api.deepseek.com"
	// defaultModel is the model the tool asks. A model name is a service's
	// own detail and it moves; it is written here so that a change is one
	// line in one file.
	defaultModel = "deepseek-chat"
	// docsURL is where the user creates the key.
	docsURL = "https://platform.deepseek.com/api_keys"
)

// Translator asks DeepSeek to translate.
type Translator struct {
	client  *httpx.Client
	baseURL string
	model   string
	now     func() time.Time
}

// New returns the translator, talking to DeepSeek itself.
func New(client *httpx.Client) *Translator {
	return NewWithBaseURL(client, defaultBaseURL)
}

// NewWithBaseURL returns the translator talking to another address. Tests
// use it to point at an httptest server.
func NewWithBaseURL(client *httpx.Client, baseURL string) *Translator {
	return &Translator{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   defaultModel,
		now:     func() time.Time { return time.Now().UTC() },
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
		CredentialKeys: []string{deepseekdomain.APIKeyCredential},
		DocsURL:        docsURL,
		Endpoint:       chat.EndpointOf(t.baseURL),
	}
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
	key, err := creds.Secret(deepseekdomain.APIKeyCredential)
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
	text, cut := chat.Truncate(req.Text, domain.MaxTranslationChars)
	source := req.SourceLang
	if source == domain.LangUnknown {
		source = domain.Language(tools.DetectLanguage(text))
	}
	options := chat.AnnotationOptions{Furigana: req.Annotation.Furigana, LoanwordGloss: req.Annotation.LoanwordGloss}
	content, err := chat.Translate(ctx, chat.Options{
		Client:  t.client,
		BaseURL: t.baseURL,
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
	// What came back is read as an annotation only when one was asked for:
	// a model that wraps a plain translation in a JSON object should not
	// turn a paragraph into a column of one word.
	if options.Wants() {
		annotation := chat.ParseAnnotation(content)
		if annotation.Segmented {
			// The pieces are the annotation and nothing else. The source the
			// window draws stays the text that was sent, with the line breaks
			// the user copied: joining the pieces would put their sentence on
			// one line, which is the format they did not ask to lose.
			result.Segments = annotation.Segments
		}
		result.TranslatedText = annotation.TranslatedText
		// What could not be read is said out loud: an answer that came back
		// without the pieces that were asked for is drawn as a plain
		// translation, and the window should say why it has no readings
		// rather than let it look like the answer simply had none.
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
