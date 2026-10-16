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
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
