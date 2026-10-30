// Package openai holds what the OpenAI-compatible translators have in common:
// one chat-completions request, one answer to read out of it, and one way of
// reading the piece-by-piece annotation that the Japanese settings ask for.
//
// It is a package of its own because the two translators differ only in
// their address, their model and the words they put in the prompt: the
// protocol is one thing, and it is written down once.
//
// The field names of the wire format stop here. The rest of the application
// hands over an address, a model, a key and a prompt, and gets back text and
// segments.
package openai

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/httpc"
)

// DefaultMaxChars bounds what one request carries. The caller truncates before
// this, so a request that reaches here is already inside it; the constant is
// the ceiling for callers that are not the use case, such as a test — and the
// number the note about a shortened text names.
const DefaultMaxChars = 4000

// Options is one call: where to ask, what to call the model, what to say,
// and what to translate.
type Options struct {
	Client *httpc.Client
	// Endpoint is the full address, without a trailing slash, and without
	// the path: the path is added here.
	BaseURL string
	Model   string
	// APIKey is sent as a bearer token. Empty means no Authorization header
	// at all: a local service that needs none should not be told about an
	// empty key.
	APIKey string
	// System is the instruction that decides the shape of the answer.
	System string
	// Source and Target are the languages, for the prompt's own use.
	Source domain.Language
	Target domain.Language
	// Text is what to translate.
	Text string
}

// endpointPath is the OpenAI-compatible path every service of this kind
// answers on.
const endpointPath = "/chat/completions"

// CustomService is where a user's own OpenAI-compatible service is, and what
// to call it.
//
// It lives here, beside the protocol it names, because the two things that
// need it are on either side of a boundary: the custom backend asks a service
// at this address, and the settings that hold it are the translation feature's.
// The backend cannot import the feature — the feature imports the backend — so
// the type they share belongs with the protocol instead.
type CustomService struct {
	// BaseURL is the service's address, such as "https://api.example.com/v1".
	// Empty means the service is not configured, and the window says so.
	BaseURL string
	// DisplayName is what the user calls the service. Empty means the
	// interface uses its own word for it.
	DisplayName string
	// Model is the OpenAI-compatible model name to ask.
	Model string
	// SupportsImage is a verified capability stored by the translation feature.
	SupportsImage bool
}

// NormalizeBaseURL turns what a user typed into an address the application
// will ask, or into nothing.
//
// It accepts what people write — "api.example.com/v1", or a full URL — and
// refuses anything that is not plain HTTP: a credential is about to be sent
// there, and a scheme the transport does not speak is a mistake worth dropping
// rather than passing on. A protocol-relative address ("//host/v1") is dropped
// too: it is how a web page means "the same scheme as this page", which a
// desktop application has no answer for.
//
// It sits here rather than in the feature that stores the address because the
// backend that asks it needs the same answer: what is stored and what is asked
// must be the same string, and two cleanings is two chances for them to differ.
func NormalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "//") {
		return ""
	}
	lower := strings.ToLower(value)
	scheme := ""
	if index := strings.Index(lower, "://"); index >= 0 {
		scheme, value = lower[:index], value[index+3:]
	} else if index := strings.IndexAny(lower, ":/?#"); index >= 0 {
		// Something before the first slash that is not the host is a scheme,
		// however it is punctuated: "javascript:alert(1)" names a scheme and
		// a payload, not a host and a path. Two things are hosts rather than
		// schemes and are let through: a port ("localhost:8080/v1") and an
		// IPv6 literal in brackets ("[::1]:8080/v1"), whose colons are part of
		// the address itself.
		head := lower[:index]
		bracketed := strings.Contains(head, "[")
		if lower[index] == ':' && !bracketed && !strings.ContainsAny(head, "./") && !isPort(head, lower[index+1:]) {
			return ""
		}
	}
	if scheme != "" && scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.TrimLeft(value, "/")
	if host == "" {
		return ""
	}
	if scheme == "" {
		scheme = defaultScheme(host)
	}
	return scheme + "://" + strings.TrimRight(host, "/")
}

// defaultScheme is the scheme a typed address without one is given.
//
// It is https, which is what a hosted service is, with one exception: an
// address on the machine itself is http. A service the user runs — Ollama, LM
// Studio, LibreTranslate, the mock in tools/ — listens on plain http, and a
// bare "127.0.0.1:11434/v1" turned into "https://127.0.0.1:11434/v1" is a
// request to a port that speaks no TLS. The user typed the address they copied
// out of that service's own documentation, and the application should not make
// it wrong.
//
// "localhost" counts as the machine; a hostname that merely contains the word
// does not.
func defaultScheme(host string) string {
	name := host
	if index := strings.IndexAny(name, "/?#"); index >= 0 {
		name = name[:index]
	}
	// An IPv6 literal is bracketed, and its colons are part of the address:
	// the port, if there is one, is the colon after the closing bracket.
	if strings.HasPrefix(name, "[") {
		if end := strings.IndexByte(name, ']'); end >= 0 {
			name = name[1:end]
		}
	} else if index := strings.IndexByte(name, ':'); index >= 0 {
		name = name[:index]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return "http"
	}
	return "https"
}

// isPort reports whether what follows a colon is a port number rather than the
// payload of a scheme: "8080/v1" is, "alert(1)" is not.
func isPort(head, rest string) bool {
	digits := rest
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		digits = rest[:index]
	}
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	// A port is a number, and the host it belongs to is a host: no scheme is
	// spelled with digits before it.
	return head != ""
}

// EndpointOf is the address a base URL is asked at, or "" when there is no
// base URL. The settings window shows it, so that a user can see where their
// text and their key are going before either is sent.
func EndpointOf(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + endpointPath
}

// chatRequest is the body of one call. Only the fields every
// OpenAI-compatible service understands are sent: a service built on the
// same protocol is not always built on the same extras.
type chatRequest struct {
	Model              string         `json:"model"`
	Messages           []chatMessage  `json:"messages"`
	Temperature        float64        `json:"temperature"`
	Stream             bool           `json:"stream"`
	MaxTokens          int            `json:"max_tokens,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// ImageTimeout bounds one vision request. Recognizing and translating a
// screenshot routinely takes longer than an ordinary API call; the 10-second
// default would cut the stream in the middle of the model's answer, and the
// app would report its own deadline as a network timeout. The screenshot task
// gives the whole operation two minutes, and this request may use all of it.
const ImageTimeout = 2 * time.Minute

// ImageMaxBytes is the vision-stream ceiling. Thinking-mode SSE frames every
// token, so the body is much larger than the final JSON. The ordinary 1 MiB
// ceiling tears the last event, and that tear is reported as an unreadable
// response. Apple allows the same 8 MiB.
const ImageMaxBytes = 8 << 20

// ImageOptions is an OpenAI-compatible vision request. Image is PNG bytes;
// callers never log it or include it in errors.
type ImageOptions struct {
	Client                         *httpc.Client
	BaseURL, Model, APIKey, System string
	Target                         domain.Language
	Image                          []byte
	// Headers, MaxTokens and DisableThinking are only supplied when a
	// particular OpenAI-compatible service documents them. They remain empty
	// for generic endpoints so we do not send vendor extensions by default.
	Headers         map[string]string
	MaxTokens       int
	DisableThinking bool
	// Timeout overrides ImageTimeout for this request; zero uses it.
	Timeout time.Duration
}

// ImageResponse is the complete, structured answer requested from a vision
// model. A response is either a translation or a closed-set failure, never
// both and never free-form error prose.
type ImageResponse struct {
	Status         string                `json:"status"`
	SourceLang     domain.Language       `json:"sourceLang"`
	SourceText     string                `json:"sourceText"`
	TargetLang     domain.Language       `json:"targetLang"`
	TranslatedText string                `json:"translatedText"`
	Failure        *imageResponseFailure `json:"failure"`
}

type imageResponseFailure struct {
	Code domain.ImageTranslationFailureCode `json:"code"`
}

// ParseImageResponse enforces the image-model response contract. A declared
// model failure is useful data, while a response outside this envelope remains
// a provider bad response.
func ParseImageResponse(content string) (ImageResponse, error) {
	var answer ImageResponse
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		return ImageResponse{}, fmt.Errorf("%w: image translator did not return structured JSON", domain.ErrProviderBadResponse)
	}
	switch answer.Status {
	case "success":
		if answer.Failure != nil {
			return ImageResponse{}, fmt.Errorf("%w: successful image response includes a failure", domain.ErrProviderBadResponse)
		}
		return answer, nil
	case "failure":
		if answer.Failure == nil || !answer.Failure.Code.Valid() {
			return ImageResponse{}, fmt.Errorf("%w: image translator returned an unknown failure code", domain.ErrProviderBadResponse)
		}
		return ImageResponse{}, &domain.ImageTranslationError{Code: answer.Failure.Code}
	default:
		return ImageResponse{}, fmt.Errorf("%w: image translator omitted a valid status", domain.ErrProviderBadResponse)
	}
}

// chatResponse is as much of the answer as is read: the first choice's
// message. Everything else a service may add is left alone.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// TranslateImage sends one image as a data URL in the standard multimodal chat
// shape and reads its SSE response as it arrives. The final model output still
// has to be the required JSON envelope; streaming only reduces time-to-first
// output and never turns partial model text into a translation.
func TranslateImage(ctx context.Context, opts ImageOptions) (string, error) {
	if opts.Client == nil || len(opts.Image) == 0 || strings.TrimSpace(opts.Model) == "" || EndpointOf(opts.BaseURL) == "" {
		return "", fmt.Errorf("%w: incomplete image translation request", domain.ErrInvalidArgument)
	}
	request := chatRequest{Model: strings.TrimSpace(opts.Model), Messages: []chatMessage{
		{Role: "system", Content: opts.System},
		{Role: "user", Content: []map[string]any{
			{"type": "text", "text": "Read and translate this image according to the system instruction."},
			{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(opts.Image)}},
		}},
	}, Temperature: 0, Stream: true, MaxTokens: opts.MaxTokens}
	if opts.DisableThinking {
		request.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("chat: encode image request: %w", err)
	}
	headers := make(map[string]string, len(opts.Headers)+1)
	for name, value := range opts.Headers {
		headers[name] = value
	}
	if key := strings.TrimSpace(opts.APIKey); key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = ImageTimeout
	}
	var content string
	result, err := opts.Client.PostJSONStream(ctx, EndpointOf(opts.BaseURL), headers, body, httpc.RequestOptions{Timeout: timeout, MaxBytes: ImageMaxBytes}, func(stream io.Reader) error {
		var readErr error
		content, readErr = readSSE(stream)
		return readErr
	})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(chatResponse{Choices: []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}{{Message: struct {
		Content string `json:"content"`
	}{Content: content}}}})
	if err != nil {
		return "", fmt.Errorf("chat: encode stream response: %w", err)
	}
	result.Payload = payload
	return readAnswer(result, opts.Target)
}

// readSSE accumulates OpenAI-compatible chat-completion deltas. A completed
// stream ends with [DONE]; accepting an EOF before that would make a network
// truncation look like a finished translation. Thinking-mode chunks set
// content to null while they stream reasoning; that null is not text and is
// not an invalid event. SSE lines may end in CR, which the scanner leaves on.
func readSSE(stream io.Reader) (string, error) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 8*1024), httpc.DefaultMaxBytes)
	var answer strings.Builder
	done := false
	var dataLines []string
	consume := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if data == "[DONE]" {
			done = true
			return nil
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content *string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("%w: image stream contains an invalid event", domain.ErrProviderBadResponse)
		}
		for _, choice := range event.Choices {
			if choice.Delta.Content != nil {
				answer.WriteString(*choice.Delta.Content)
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := consume(); err != nil {
				return "", err
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("%w: image stream stopped before completion", domain.ErrProviderTimeout)
	}
	if err := consume(); err != nil {
		return "", err
	}
	if !done {
		return "", fmt.Errorf("%w: image stream ended without completion", domain.ErrProviderBadResponse)
	}
	return answer.String(), nil
}

// Translate makes the request and reads the answer out of it.
//
// Everything that can go wrong is a domain error: an unreachable service, a
// rejected key, a response that is not the documented shape. A response that
// IS the shape but carries nothing is ErrProviderBadResponse, not an empty
// translation: the caller must never draw a blank half as if it were an
// answer.
func Translate(ctx context.Context, opts Options) (content string, err error) {
	if opts.Client == nil {
		return "", fmt.Errorf("%w: the translator has no HTTP client", domain.ErrInvalidArgument)
	}
	text := strings.TrimSpace(opts.Text)
	if text == "" {
		return "", fmt.Errorf("%w: there is no text to translate", domain.ErrInvalidArgument)
	}
	endpoint := EndpointOf(opts.BaseURL)
	if endpoint == "" {
		return "", fmt.Errorf("%w: the translator has no address", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(opts.Model) == "" {
		return "", fmt.Errorf("%w: the translator has no model", domain.ErrInvalidArgument)
	}

	body, err := json.Marshal(chatRequest{
		Model: strings.TrimSpace(opts.Model),
		Messages: []chatMessage{
			{Role: "system", Content: opts.System},
			{Role: "user", Content: text},
		},
		Temperature: 0,
		Stream:      false,
	})
	if err != nil {
		return "", fmt.Errorf("chat: encode the request: %w", err)
	}
	headers := map[string]string{}
	if key := strings.TrimSpace(opts.APIKey); key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	result, err := opts.Client.PostJSON(ctx, endpoint, headers, body)
	if err != nil {
		return "", err
	}
	return readAnswer(result, opts.Target)
}

// readAnswer pulls the text out of a successful response, or says why the
// response is not one.
//
// A fragment of the response goes into the error, so that a service which
// renamed a field can be diagnosed without a debugger. It is redacted first:
// a service that echoes a credential back in its answer must not put it in
// front of the user.
func readAnswer(result domain.FetchResult, target domain.Language) (string, error) {
	snippet := func() string { return kit.RedactText(domain.Snippet(result.Payload)) }
	var parsed chatResponse
	if err := json.Unmarshal(result.Payload, &parsed); err != nil {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer is not an OpenAI-compatible response",
			Snippet:    snippet(),
		}
	}
	if len(parsed.Choices) == 0 {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer carries no choices",
			Snippet:    snippet(),
		}
	}
	if !hasContent(result.Payload) {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer carries no message content",
			Snippet:    snippet(),
		}
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("%w: the service answered with an empty translation into %s",
			domain.ErrProviderBadResponse, target.Name())
	}
	return content, nil
}

// hasContent reports whether the answer has a first choice whose message has
// a content field at all. It is what tells "the service answered with
// nothing" apart from "the service answered a different shape": both carry
// an empty string once decoded, and only one of them is worth retrying.
func hasContent(payload []byte) bool {
	var probe struct {
		Choices []struct {
			Message map[string]json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return false
	}
	if len(probe.Choices) == 0 || probe.Choices[0].Message == nil {
		return false
	}
	_, ok := probe.Choices[0].Message["content"]
	return ok
}
