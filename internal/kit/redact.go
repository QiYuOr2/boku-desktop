// This file removes credentials from text on its way somewhere a person can
// read it: an error message, a response fragment, a log record.
//
// It lives here, and not with the HTTP client that first needed it, because
// the logger needs exactly the same rule and reaching into a network package
// to get it would make the log depend on the network. What a credential looks
// like is one rule in one place; AGENTS.md's Secrets Rules and
// docs/security.md §5 both point at this file.
package kit

import (
	"net/url"
	"regexp"
	"strings"
)

// sensitiveName reports whether a name carries a credential: a query
// parameter, a JSON field, a log attribute.
//
// The list is the whole of the recognition: a credential under a name nobody
// would guess is not found here, which is why the caller must not put one
// there in the first place.
func sensitiveName(name string) bool {
	switch strings.ToLower(name) {
	case "key", "api_key", "apikey", "api-key", "access_token", "token",
		"secret", "password", "passwd", "auth", "authorization",
		"sig", "signature", "client_secret":
		return true
	}
	return false
}

// Redact removes what must never reach a log or an error message from a
// URL: the user info, and the value of any parameter that looks like a
// credential.
func Redact(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" {
		return RedactText(endpoint)
	}
	if parsed.User != nil {
		parsed.User = url.User("redacted")
	}
	query := parsed.Query()
	changed := false
	for name := range query {
		if sensitiveName(name) {
			query.Set(name, "redacted")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

var secretAssignment = regexp.MustCompile(`(?i)\b(key|api_key|apikey|api-key|access_token|token|secret|password|passwd|auth|authorization|sig|signature|client_secret)=([^&\s"']+)`)

// secretField is the same names as a JSON or query field: a service that
// answers with its own error often echoes the credential back inside the
// body, and a response fragment must not carry one into a log or a window.
var secretField = regexp.MustCompile(`(?i)("?(?:key|api_key|apikey|api-key|access_token|token|secret|password|passwd|auth|authorization|sig|signature|client_secret)"?\s*:\s*)("[^"]*"|[^,}\s]+)`)

// RedactText removes credential values from arbitrary text, such as a
// transport error message that quotes a URL, or a response fragment an error
// carries.
func RedactText(text string) string {
	redacted := secretAssignment.ReplaceAllString(text, "$1=redacted")
	return secretField.ReplaceAllString(redacted, "$1\"redacted\"")
}

// RedactField removes a credential from a value written under a name, which
// is how a log record prints an attribute (`apiKey=…`): the value on its own
// may be a bare credential with nothing in it to recognize, and the name it
// is logged under is what says what it is.
//
// A value under any other name is still scrubbed as text, because a value can
// carry a `key=…` pair of its own.
func RedactField(name, value string) string {
	if value == "" {
		return value
	}
	if sensitiveName(strings.TrimSpace(name)) {
		return "redacted"
	}
	return RedactText(value)
}
