package domain

import (
	"errors"
	"fmt"
	"strings"
)

// The domain's errors. Callers test them with errors.Is: an error that
// describes more, such as BadResponseError, wraps one of these.
var (
	// General.
	ErrNotFound           = errors.New("not found")
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrInvalidMetricValue = errors.New("invalid metric value")
	ErrAlreadyExists      = errors.New("already exists")

	// Providers and the network.
	ErrProviderNotFound     = errors.New("provider not found")
	ErrProviderUnauthorized = errors.New("provider unauthorized")
	ErrProviderRateLimited  = errors.New("provider rate limited")
	ErrProviderUnavailable  = errors.New("provider unavailable")
	ErrProviderTimeout      = errors.New("provider timeout")
	ErrProviderBadResponse  = errors.New("provider bad response")
	ErrCredentialMissing    = errors.New("credential missing")

	// The secret store.
	ErrVaultLocked    = errors.New("vault locked")
	ErrVaultNotFound  = errors.New("vault not found")
	ErrPassphrase     = errors.New("passphrase rejected")
	ErrSecretNotFound = errors.New("secret not found")
	ErrSecretExists   = errors.New("secret exists")

	// Tools.
	ErrInvalidJSON                = errors.New("invalid json")
	ErrInvalidMagnetURI           = errors.New("invalid magnet uri")
	ErrUnsupportedClipboardFormat = errors.New("unsupported clipboard format")
	ErrTranslateUnavailable       = errors.New("translation unavailable")

	// Credentials that belong to another tool: the login Boku reads is the
	// tool's, so the tool is what has to be logged in, and it may not be
	// there at all.
	ErrLoginCommandMissing = errors.New("login command missing")
	ErrLoginUnsupported    = errors.New("login cannot be started here")
)

// MaxSnippetBytes is how much of a response an error carries: enough to see
// a renamed field, short enough not to fill a log with a whole payload.
const MaxSnippetBytes = 512

// BadResponseError reports a response the caller could not use. It is what
// ErrProviderBadResponse wraps, and it never carries credentials.
type BadResponseError struct {
	ProviderID string
	Endpoint   string
	HTTPStatus int
	Reason     string
	Snippet    string
}

func (e *BadResponseError) Error() string {
	var b strings.Builder
	b.WriteString("bad response")
	if e.ProviderID != "" {
		fmt.Fprintf(&b, " from provider %s", e.ProviderID)
	}
	if e.Endpoint != "" {
		fmt.Fprintf(&b, " at %s", e.Endpoint)
	}
	if e.HTTPStatus != 0 {
		fmt.Fprintf(&b, " (status %d)", e.HTTPStatus)
	}
	if e.Reason != "" {
		fmt.Fprintf(&b, ": %s", e.Reason)
	}
	if e.Snippet != "" {
		fmt.Fprintf(&b, "; response: %s", e.Snippet)
	}
	return b.String()
}

// Unwrap makes errors.Is(err, ErrProviderBadResponse) true.
func (e *BadResponseError) Unwrap() error { return ErrProviderBadResponse }

// Snippet returns the first MaxSnippetBytes of a response, as text, for an
// error message.
func Snippet(payload []byte) string {
	if len(payload) > MaxSnippetBytes {
		payload = payload[:MaxSnippetBytes]
	}
	return strings.ToValidUTF8(strings.TrimSpace(string(payload)), "\uFFFD")
}
