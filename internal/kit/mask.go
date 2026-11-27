package kit

import "strings"

// MaskSecret 只保留凭据最后四个字符（"…a1b2"），足以辨认而不能使用。
// 没有可隐藏内容的短值会被整段打码，短密钥永不外露。
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
