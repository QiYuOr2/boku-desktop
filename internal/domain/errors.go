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

	// Credentials: the store keeps values in a plain-text file, so the only
	// two things that can go wrong are a name that is not there and a name
	// that already is. There is no locked state to report — §5 of the
	// security design says why — and the errors an encrypted store would
	// need were removed with the encryption rather than left for a caller to
	// branch on something that never happens.
	ErrSecretNotFound = errors.New("secret not found")
	ErrSecretExists   = errors.New("secret exists")

	// Tools.
	ErrInvalidJSON                = errors.New("invalid json")
	ErrInvalidMagnetURI           = errors.New("invalid magnet uri")
	ErrUnsupportedClipboardFormat = errors.New("unsupported clipboard format")
	ErrTranslateUnavailable       = errors.New("translation unavailable")
	ErrImageTranslateUnavailable  = errors.New("image translation unavailable")
	// ErrImageTranslationFailed means the image model understood the request
	// but could not translate this particular image. Unlike a bad response, its
	// structured code gives the interface an actionable explanation.
	ErrImageTranslationFailed = errors.New("image translation failed")
	// ErrScreenRecordingDenied is macOS refusing to read the screen because
	// the user has not allowed Boku to record it. It is not a translation
	// failure and not a missing translator: the window sends the user to the
	// System Settings switch. The capture is checked before pixels are read,
	// because an unauthorized CoreGraphics read returns black rather than
	// nothing.
	ErrScreenRecordingDenied = errors.New("screen recording denied")
	// ErrTranslateDisabled is the tool switched off, which is not the same
	// thing as the tool having nowhere to send a text. They are separate
	// errors because they are separate sentences to the user: "you turned this
	// off, here is how to turn it back on" and "there is no service to ask".
	// One error for both made the card tell a user whose tool was off that
	// every free service was down.
	ErrTranslateDisabled = errors.New("translation disabled")

	// Credentials that belong to another tool: the login Boku reads is the
	// tool's, so the tool is what has to be logged in, and it may not be
	// there at all.
	ErrLoginCommandMissing = errors.New("login command missing")
	ErrLoginUnsupported    = errors.New("login cannot be started here")
	// ErrDeviceLoginExpired is a device code the user did not finish in time.
	ErrDeviceLoginExpired = errors.New("device login expired")
)

// ImageTranslationFailureCode is the closed set a vision model may use when
// it cannot produce a translation. Keeping this small and machine-readable
// avoids presenting a model's untrusted prose as an application error.
type ImageTranslationFailureCode string

const (
	ImageFailureNoReadableText ImageTranslationFailureCode = "no_readable_text"
	ImageFailureUnsupported    ImageTranslationFailureCode = "unsupported_image"
	ImageFailureRefused        ImageTranslationFailureCode = "refused"
)

func (c ImageTranslationFailureCode) Valid() bool {
	switch c {
	case ImageFailureNoReadableText, ImageFailureUnsupported, ImageFailureRefused:
		return true
	default:
		return false
	}
}

// ImageTranslationError carries the model's structured failure code only.
// It deliberately contains neither the image nor model-generated text.
type ImageTranslationError struct {
	Code ImageTranslationFailureCode
}

func (e *ImageTranslationError) Error() string {
	return "image translation failed: " + string(e.Code)
}

func (e *ImageTranslationError) Unwrap() error { return ErrImageTranslationFailed }

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
