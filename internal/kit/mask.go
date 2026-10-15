package kit

import "strings"

// MaskSecret shows enough of a credential to recognise it and not enough to
// use it: only its last four characters survive ("…a1b2"). A value with
// nothing to hide is masked whole, so a short secret never appears.
func MaskSecret(s string) string {
	text := strings.TrimSpace(s)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= 4 {
		return strings.Repeat("•", len(runes))
	}
	return "…" + string(runes[len(runes)-4:])
}
