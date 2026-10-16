package free

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/httpc"
)

// Microsoft is the Microsoft Edge engine: the endpoint Edge's own translate
// feature asks.
//
// Measured (§3.1 of the free-translate research): no key, no token, reachable without a
// proxy from inside China, and the widest throughput of the candidates — 30
// requests in a row without a single 429.
//
// It answers one sentence per text, so it is the engine for a plain
// translation and not for the Japanese readings. A request that asks for
// readings is answered with the sentence and a note saying there are no
// readings, rather than with a failure: the user gets a translation and is told
// exactly what it does not have.
//
// Two things about its protocol are worth writing down here rather than in the
// design document, because they are the code: the body is a bare JSON array of
// strings rather than an object, and the answer is an array aligned with the
// request. The languages are not the tool's two-letter codes either —
// "zh-Hans" rather than "zh" — which is why the codes are translated below.
type Microsoft struct {
	client *httpc.Client
	now    func() time.Time
	// endpoint is a format string whose one verb is the target language, so
	// that a test can point it at an httptest server.
	endpoint string
}

// microsoftEndpoint is where the engine asks. The query string is the one Edge
// itself sends; the empty `from` is what makes the endpoint detect the source
// language for us.
const microsoftEndpoint = "https://edge.microsoft.com/translate/translatetext?from=&to=%s&isEnterpriseClient=false"

// NewMicrosoft returns the engine over the HTTP client.
func NewMicrosoft(client *httpc.Client) *Microsoft {
	return newMicrosoft(client, microsoftEndpoint)
}

// newMicrosoft returns the engine asking another address.
func newMicrosoft(client *httpc.Client, endpoint string) *Microsoft {
	return &Microsoft{
		client:   client,
		now:      func() time.Time { return time.Now().UTC() },
		endpoint: endpoint,
	}
}

// ID returns the translator's id, which is what the settings file stores.
func (m *Microsoft) ID() string { return contract.MicrosoftEdge }

// Descriptor says what the engine is called and that it needs no key. The
// endpoint is the address the text goes to, which the settings window shows
// because §12 of the product design requires the user to be able to see it.
func (m *Microsoft) Descriptor() domain.TranslatorDescriptor {
	return domain.TranslatorDescriptor{
		ID:                   contract.MicrosoftEdge,
		DisplayName:          "微软 Edge 翻译",
		CredentialKeys:       []string{contract.APIKeyCredential},
		DocsURL:              contract.DocsURL,
		Endpoint:             "edge.microsoft.com/translate/translatetext",
		AllowEmptyCredential: true,
	}
}

// Translate asks the endpoint.
//
// No credential is read, and none is required: a free engine that asked the
// user for a key would be the "先填 Key" wall this whole tier exists to remove.
func (m *Microsoft) Translate(ctx context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	text := prepare(req)
	if strings.TrimSpace(text.text) == "" {
		return domain.TranslationResult{}, fmt.Errorf("%w: there is no text to translate", domain.ErrInvalidArgument)
	}
	body, err := json.Marshal([]string{text.text})
	if err != nil {
		return domain.TranslationResult{}, fmt.Errorf("free: encode the request: %w", err)
	}
	endpoint := fmt.Sprintf(m.endpoint, microsoftLanguage(req.TargetLang))
	result, err := m.client.PostJSON(ctx, endpoint, map[string]string{
		"Content-Type": "application/json",
	}, body)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	translated, err := readMicrosoftAnswer(result)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	answer := plainResult(translated, req, text.source, text.text, m.now())
	answer.Note = firstNote(truncationNote(text.cut), answer.Note)
	if err := answer.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return answer, nil
}

// readMicrosoftAnswer pulls the translation out of the endpoint's answer.
//
// The shape is an array aligned with the request — one object per string that
// was sent — and each object carries the detected language and a translations
// array. A response that is not that shape is a bad response rather than an
// empty translation: the caller must never draw a blank as if it were an
// answer.
func readMicrosoftAnswer(result domain.FetchResult) (string, error) {
	snippet := func() string { return kit.RedactText(domain.Snippet(result.Payload)) }
	var parsed []struct {
		DetectedLanguage struct {
			Language string  `json:"language"`
			Score    float64 `json:"score"`
		} `json:"detectedLanguage"`
		Translations []struct {
			Text string `json:"text"`
			To   string `json:"to"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(result.Payload, &parsed); err != nil {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer is not the Edge translation shape",
			Snippet:    snippet(),
		}
	}
	if len(parsed) == 0 || len(parsed[0].Translations) == 0 {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer carries no translation",
			Snippet:    snippet(),
		}
	}
	translated := strings.TrimSpace(parsed[0].Translations[0].Text)
	if translated == "" {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer carries an empty translation",
			Snippet:    snippet(),
		}
	}
	return translated, nil
}

// microsoftLanguage is the tool's language code as this endpoint spells it.
//
// Chinese is the one that matters: the tool says "zh", Edge says "zh-Hans" for
// the simplified script and "zh-Hant" for the traditional one, and asking for
// "zh" is not one of its codes. Everything else passes through, which is what
// §3.1 measured for "en", "ja", "ko" and "ru".
func microsoftLanguage(language domain.Language) string {
	switch language {
	case domain.LangChinese:
		return "zh-Hans"
	default:
		return string(language)
	}
}

// firstNote is the first of the notes with anything in it: a result carries one
// line about what it is missing, and a shortened text is the more important
// thing to say about it.
func firstNote(notes ...string) string {
	for _, note := range notes {
		if strings.TrimSpace(note) != "" {
			return note
		}
	}
	return ""
}
