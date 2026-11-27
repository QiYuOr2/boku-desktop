package domain

import (
	"errors"
	"fmt"
	"strings"
)

// 领域错误。调用方用 errors.Is 判断：携带更多信息的错误（如 BadResponseError）
// 会包裹其中之一。
var (
	// 通用。
	ErrNotFound           = errors.New("not found")
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrInvalidMetricValue = errors.New("invalid metric value")
	ErrAlreadyExists      = errors.New("already exists")

	// Provider 与网络。
	ErrProviderNotFound     = errors.New("provider not found")
	ErrProviderUnauthorized = errors.New("provider unauthorized")
	ErrProviderRateLimited  = errors.New("provider rate limited")
	ErrProviderUnavailable  = errors.New("provider unavailable")
	ErrProviderTimeout      = errors.New("provider timeout")
	ErrProviderBadResponse  = errors.New("provider bad response")
	ErrCredentialMissing    = errors.New("credential missing")

	// 凭据：存储把值放在明文文件里，因此只会出现两种问题——名称不存在、
	// 名称已存在。
	ErrSecretNotFound = errors.New("secret not found")
	ErrSecretExists   = errors.New("secret exists")

	// 工具。
	ErrInvalidJSON                = errors.New("invalid json")
	ErrInvalidMagnetURI           = errors.New("invalid magnet uri")
	ErrUnsupportedClipboardFormat = errors.New("unsupported clipboard format")
	ErrTranslateUnavailable       = errors.New("translation unavailable")
	ErrImageTranslateUnavailable  = errors.New("image translation unavailable")
	// ErrImageTranslationFailed 表示图像模型理解了请求但无法翻译这张图片。
	// 不同于坏响应，它的结构化代码能给界面可操作的说明。
	ErrImageTranslationFailed = errors.New("image translation failed")
	// ErrScreenRecordingDenied 是 macOS 因用户未允许 Boku 录屏而拒绝读取屏幕。
	// 它不是翻译失败，也不是缺少翻译器：窗口会引导用户去系统设置开关。
	// 读取像素前先检查授权，因为未授权的 CoreGraphics 读取返回黑色而非没有。
	ErrScreenRecordingDenied = errors.New("screen recording denied")
	// ErrTranslateDisabled 是工具被关闭，与工具无处发送文本不是一回事。
	// 它们是给用户的两句不同的话，因此是两个独立错误。
	ErrTranslateDisabled = errors.New("translation disabled")

	// 属于其它工具的凭据：Boku 读取的登录属于该工具，因此需要登录的是该工具，
	// 它也可能根本不存在。
	ErrLoginCommandMissing = errors.New("login command missing")
	ErrLoginUnsupported    = errors.New("login cannot be started here")
	// ErrDeviceLoginExpired 是用户未及时完成的设备码。
	ErrDeviceLoginExpired = errors.New("device login expired")
)

// ImageTranslationFailureCode 是视觉模型无法产出翻译时可用的一组封闭取值。
// 保持其小而机器可读，避免把模型不可信的散文当作应用错误展示。
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

// ImageTranslationError 只携带模型的结构化失败代码。
// 它有意不含图片，也不含模型生成的文本。
type ImageTranslationError struct {
	Code ImageTranslationFailureCode
}

func (e *ImageTranslationError) Error() string {
	return "image translation failed: " + string(e.Code)
}

func (e *ImageTranslationError) Unwrap() error { return ErrImageTranslationFailed }

// MaxSnippetBytes 是错误携带的响应片段量：足以看出被改名的字段，
// 又短到不会让整份负载填满日志。
const MaxSnippetBytes = 512

// BadResponseError 报告调用方无法使用的响应。它是 ErrProviderBadResponse
// 所包裹的对象，且从不携带凭据。
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

// Unwrap 使 errors.Is(err, ErrProviderBadResponse) 成立。
func (e *BadResponseError) Unwrap() error { return ErrProviderBadResponse }

// Snippet 返回响应的前 MaxSnippetBytes 字节，作为错误消息中的文本。
func Snippet(payload []byte) string {
	if len(payload) > MaxSnippetBytes {
		payload = payload[:MaxSnippetBytes]
	}
	return strings.ToValidUTF8(strings.TrimSpace(string(payload)), "\uFFFD")
}
