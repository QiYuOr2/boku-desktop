// Package chat holds what the OpenAI-compatible translators have in common:
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
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"boku/internal/data/httpx"
	"boku/internal/domain"
)

// DefaultMaxChars bounds what one request carries. The use case truncates
// before this, so a request that reaches here is already inside it; the
// constant is the translator's own ceiling for callers that are not the use
// case, such as a test.
const DefaultMaxChars = 4000

// Options is one call: where to ask, what to call the model, what to say,
// and what to translate.
type Options struct {
	Client *httpx.Client
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
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	snippet := func() string { return httpx.RedactText(domain.Snippet(result.Payload)) }
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
