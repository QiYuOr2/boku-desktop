// Package openai 汇集 OpenAI 兼容翻译器的公共部分：一次 chat-completions 请求、
// 从响应中读出答案，以及读取日文设置所需逐词标注的唯一方式。
//
// 它独立成包，是因为两个翻译器只在地址、模型和提示词措辞上不同：协议是同一件事，
// 只写一遍。
//
// 线格式的字段名止步于此。应用其余部分交出地址、模型、密钥和提示词，拿回文本与片段。
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

// DefaultMaxChars 限制单次请求携带的文本量。调用方在此之前已截断，故到达此处的请求本
// 已在上限内；该常量是给非用例调用方（如测试）的上限，也是「文本被截短」提示里引用的数字。
const DefaultMaxChars = 4000

// Options 是一次调用：向何处请求、用哪个模型、说什么、翻译什么。
type Options struct {
	Client *httpc.Client
	// BaseURL 是完整地址，不带尾部斜杠也不带路径：路径在此处附加。
	BaseURL string
	Model   string
	// APIKey 作为 bearer token 发送。为空则完全不带 Authorization 头：不需要密钥的本地
	// 服务不应被告知一个空密钥。
	APIKey string
	// System 是决定回答形状的指令。
	System string
	// Source 与 Target 是语种，供提示词使用。
	Source domain.Language
	Target domain.Language
	// Text 是待翻译内容。
	Text string
}

// endpointPath 是这类服务通用的 OpenAI 兼容路径。
const endpointPath = "/chat/completions"

// CustomService 是用户自建 OpenAI 兼容服务的位置与称呼。
//
// 它与它所命名的协议同处一包，是因为需要它的两边分处边界两侧：自定义后端向此地址请求，
// 而保存它的设置属于翻译功能。后端不能 import 该功能（功能 import 后端），于是共享类型
// 随协议而居。
type CustomService struct {
	// BaseURL 是服务地址，如 "https://api.example.com/v1"。为空表示未配置，窗口会说明。
	BaseURL string
	// DisplayName 是用户对服务的称呼。为空时界面用自带的叫法。
	DisplayName string
	// Model 是要请求的 OpenAI 兼容模型名。
	Model string
	// SupportsImage 是翻译功能保存的、已核实的图片能力。
	SupportsImage bool
}

// NormalizeBaseURL 把用户输入的地址整理成应用将请求的地址，或整理为空。
//
// 它接受人们习惯的写法（"api.example.com/v1" 或完整 URL），拒绝任何非纯 HTTP 的输入：
// 即将把凭据发往此处，传输层不支持的 scheme 值得丢弃而非放行。协议相对地址（"//host/v1"）
// 也丢弃：那是网页表示「与本页同 scheme」的写法，桌面应用无从应对。
//
// 它放在这里而非保存地址的功能里，是因为发起请求的后端需要同一答案：保存的与请求的必须
// 是同一字符串，两处各自清洗就有两次出现分歧的机会。
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
		// 首个斜杠之前的东西若不是主机，而是某种标了 scheme 的写法，就是 scheme：
		// "javascript:alert(1)" 是 scheme 加载荷，不是主机加路径。有两种是主机而非
		// scheme、予以放行：端口（"localhost:8080/v1"）与方括号中的 IPv6 字面量
		// （"[::1]:8080/v1"），它们的冒号本就属于地址。
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

// defaultScheme 是缺 scheme 的输入地址所补的 scheme。
//
// 它是 https，因为托管服务如此，只有一个例外：本机地址用 http。用户自跑的服务
// （Ollama、LM Studio、LibreTranslate、tools/ 里的 mock）监听明文 http，把裸地址
// "127.0.0.1:11434/v1" 变成 "https://127.0.0.1:11434/v1" 就是向不讲 TLS 的端口发请求。
// 用户键入的是从该服务文档里复制的地址，应用不该把它弄错。
//
// "localhost" 算作本机；仅包含该词的其它主机名不算。
func defaultScheme(host string) string {
	name := host
	if index := strings.IndexAny(name, "/?#"); index >= 0 {
		name = name[:index]
	}
	// IPv6 字面量带方括号，其冒号属于地址：端口（若有）是右括号之后的冒号。
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

// isPort 报告冒号之后是否为端口号而非 scheme 载荷："8080/v1" 是，"alert(1)" 不是。
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
	// 端口是数字，且它所属的是主机：没有哪个 scheme 前面以数字写出。
	return head != ""
}

// EndpointOf 是 base URL 的请求地址，无 base URL 时为 ""。设置窗口会显示它，让用户在
// 发出文本与密钥之前知道它们会去哪里。
func EndpointOf(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + endpointPath
}

// chatRequest 是一次调用的请求体。只发送每个 OpenAI 兼容服务都理解的字段：基于同一协议
// 的服务未必支持同一套扩展。
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

// ImageTimeout 限制单次视觉请求。识别并翻译一张截图通常比普通 API 调用久；10 秒默认会
// 在模型回答中途切断流，应用便把自己设的期限报成网络超时。截图任务整体给两分钟，本请求
// 可用满它。
const ImageTimeout = 2 * time.Minute

// ImageMaxBytes 是视觉流上限。思考模式的 SSE 逐 token 发送，体积远大于最终 JSON。普通
// 1 MiB 上限会截断最后一个事件，并被报成无法读取的响应。Apple 端同样用 8 MiB。
const ImageMaxBytes = 8 << 20

// ImageOptions 是一次 OpenAI 兼容视觉请求。Image 为 PNG 字节；调用方绝不记录它或把它
// 放进错误。
type ImageOptions struct {
	Client                         *httpc.Client
	BaseURL, Model, APIKey, System string
	Target                         domain.Language
	Image                          []byte
	// Headers、MaxTokens 与 DisableThinking 仅在特定 OpenAI 兼容服务有文档说明时才提供；
	// 通用端点保持为空，以免默认发送厂商扩展。
	Headers         map[string]string
	MaxTokens       int
	DisableThinking bool
	// Timeout 覆盖本次请求的 ImageTimeout；为零则使用它。
	Timeout time.Duration
}

// ImageResponse 是向视觉模型请求的完整结构化回答。响应要么是译文，要么是封闭集合里的
// 失败，绝不同时、绝不是自由文本的错误描述。
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

// ParseImageResponse 强制执行图片模型的响应契约。模型声明的失败是有用的数据，而超出此
// 信封的响应仍是服务商无效响应。
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

// chatResponse 只读回答的一部分：首个 choice 的 message。服务可能附加的其余内容一概不管。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// TranslateImage 以数据 URL 形式在标准多模态 chat 结构里发送一张图片，并边到达边读取
// 其 SSE 响应。最终模型输出仍必须是要求的 JSON 信封；流式只降低首字延迟，绝不会把模型的
// 片段文本当成译文。
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

// readSSE 累积 OpenAI 兼容 chat-completion 的增量。完整流以 [DONE] 结束；在此之前接受
// EOF 会把网络截断误当成已完成的译文。思考模式块在流式推理时把 content 设为 null；该 null
// 不是文本，也不是非法事件。SSE 行可能以 CR 结尾，scanner 会保留它。
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

// Translate 发起请求并从中读出答案。
//
// 一切出错都是域错误：服务不可达、密钥被拒、响应不是文档规定的形状。若响应形状正确却
// 内容为空，那是 ErrProviderBadResponse 而非空译文：调用方绝不能把空白的一半当作答案画出来。
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

// readAnswer 从成功响应里取出文本，或说明它为何不是成功响应。
//
// 错误里会带一段响应片段，便于诊断服务改名的情况而无需调试器。片段先脱敏：在回答里回显
// 凭据的服务不能把它摆到用户面前。
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

// hasContent 报告回答是否有首个 choice、且其 message 带 content 字段。它区分「服务答了空」
// 与「服务答了别的形状」：解码后两者都是空串，但只有一种值得重试。
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
