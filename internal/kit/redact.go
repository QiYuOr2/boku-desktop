// 本文件从将被人读取的文本中移除凭据：错误信息、响应片段、日志记录。
//
// 它放在这里而不是最初需要它的 HTTP 客户端旁，因为日志器需要完全相同的规则，
// 而去网络包里取用会让日志依赖网络。凭据长什么样是同一处的一条规则。
package kit

import (
	"net/url"
	"regexp"
	"strings"
)

// sensitiveName 判断一个名字是否承载凭据：查询参数、JSON 字段、日志属性。
//
// 这份名单就是识别的全部范围：以无法猜测的名字出现的凭据不会被这里发现，
// 因此调用方本就不应把凭据放在那种名字下。
func sensitiveName(name string) bool {
	switch strings.ToLower(name) {
	case "key", "api_key", "apikey", "api-key", "access_token", "token",
		"secret", "password", "passwd", "auth", "authorization",
		"sig", "signature", "client_secret":
		return true
	}
	return false
}

// Redact 从 URL 中移除绝不应进入日志或错误信息的部分：用户信息，以及任何看似凭据的参数值。
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

// secretField 以 JSON 或查询字段形式匹配同名项：服务返回自身错误时常会把凭据回显在
// body 里，响应片段不得将其带入日志或窗口。
var secretField = regexp.MustCompile(`(?i)("?(?:key|api_key|apikey|api-key|access_token|token|secret|password|passwd|auth|authorization|sig|signature|client_secret)"?\s*:\s*)("[^"]*"|[^,}\s]+)`)

// RedactText 从任意文本中移除凭据值，例如引用了 URL 的传输错误信息，或错误携带的响应片段。
func RedactText(text string) string {
	redacted := secretAssignment.ReplaceAllString(text, "$1=redacted")
	return secretField.ReplaceAllString(redacted, "$1\"redacted\"")
}

// RedactField 移除以某名字写入的值中的凭据，对应日志记录中属性的打印方式（`apiKey=…`）：
// 单独的值可能就是一个裸凭据、本身无可供识别的特征，是它被记录时所用的名字说明了它是什么。
//
// 其它名字下的值仍会按文本清洗，因为值自身也可能带有 `key=…` 这样的键值对。
func RedactField(name, value string) string {
	if value == "" {
		return value
	}
	if sensitiveName(strings.TrimSpace(name)) {
		return "redacted"
	}
	return RedactText(value)
}
