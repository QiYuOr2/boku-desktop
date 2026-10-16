package free

import (
	"context"
	"strings"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

// Bilibili is B 站's Index-Translate engine: an open translation model served
// on a public OpenAI-compatible endpoint.
//
// Note that the model is Bilibili's, not Boku's: the name is written here
// because it is the service's own name, which is what the user is shown and
// what the request has to say.
//
// It is the engine that can annotate. Measured (§3.2 of the free-translate research):
// no key, reachable without a proxy, 0.16–0.6 s per sentence, and — the part
// that matters — it answers the tool's own annotation prompt with one JSON
// object per piece, so the Japanese readings work without a key. That is what
// makes it the engine to pick for Japanese, and the settings page says so.
//
// The endpoint is OpenAI-compatible, so the request is openai.Translate's, the
// one the paid backends already use. That is deliberate: there is one
// chat-completions request in this application, and a second copy of it would
// be a second thing to keep right.
type Bilibili struct {
	client  *httpc.Client
	baseURL string
	model   string
	now     func() time.Time
}

const (
	// bilibiliEndpoint is the address the open endpoint answers on. The
	// OpenAI-compatible path is added by openai.Translate.
	bilibiliEndpoint = "https://index-translate.bilibili.com/v1"
	// bilibiliModel is the model the public endpoint serves. It is written here
	// so that a model change is one line in one file.
	bilibiliModel = "Index-Translate-35B-A3B"
)

// NewBilibili returns the engine over the HTTP client.
func NewBilibili(client *httpc.Client) *Bilibili {
	return newBilibili(client, bilibiliEndpoint, bilibiliModel)
}

// newBilibili returns the engine asking another base URL and model. Tests use
// it to point at an httptest server.
func newBilibili(client *httpc.Client, baseURL, model string) *Bilibili {
	return &Bilibili{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// ID returns the translator's id, which is what the settings file stores.
func (b *Bilibili) ID() string { return contract.Bilibili }

// Descriptor says what the engine is called, that it needs no key, and where
// the text goes.
func (b *Bilibili) Descriptor() domain.TranslatorDescriptor {
	return domain.TranslatorDescriptor{
		ID:                   contract.Bilibili,
		DisplayName:          "B站 Index-Translate",
		CredentialKeys:       []string{contract.APIKeyCredential},
		DocsURL:              contract.DocsURL,
		Endpoint:             "index-translate.bilibili.com/v1/chat/completions",
		AllowEmptyCredential: true,
	}
}

// Translate asks the model.
//
// No credential is read, and none is required: the endpoint is public, and the
// header is left off rather than sent empty.
func (b *Bilibili) Translate(ctx context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	text := prepare(req)
	options := openai.AnnotationOptions{
		Furigana:      req.Annotation.Furigana,
		LoanwordGloss: req.Annotation.LoanwordGloss,
	}
	content, err := openai.Translate(ctx, openai.Options{
		Client:  b.client,
		BaseURL: b.baseURL,
		Model:   b.model,
		System:  openai.SystemPrompt(text.source, req.TargetLang, options),
		Source:  text.source,
		Target:  req.TargetLang,
		Text:    text.text,
	})
	if err != nil {
		return domain.TranslationResult{}, err
	}
	result := annotatedResult(content, req, text.source, text.text, b.now())
	result.Note = firstNote(truncationNote(text.cut), result.Note)
	if err := result.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return result, nil
}
