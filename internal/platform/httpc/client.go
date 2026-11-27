// Package httpc 是应用唯一的出网通道：统一为每个第三方请求加上超时与取消、
// 响应体上限，以及可供调用方分类的错误而非裸状态码。
//
// 它也决定请求走哪里：优先用环境变量里的代理，环境未指定时用系统本身的
// 代理（proxy.go）。只查环境变量的程序，在代理设在 Windows 系统设置里的机器
// 上会完全连不出去。
//
// 凭据脱敏规则由 internal/kit 拥有，日志落盘时也应用同一规则。
package httpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

const (
	// DefaultTimeout 是单个请求端到端的默认上限，任何第三方请求都不等得更久。
	DefaultTimeout = 10 * time.Second
	// DefaultMaxBytes 是默认读取的响应体上限，超过这个量的响应按异常处理。
	DefaultMaxBytes = 1 << 20
)

// Options 配置 Client；零值可用，即标准 client、User-Agent "Boku"、
// DefaultTimeout 与 DefaultMaxBytes。
type Options struct {
	// HTTPClient 是使用的传输层，它自身的 Timeout 不会被动：超时靠请求的
	// context 携带，因此同一个 client 可服务不同期限的请求。
	HTTPClient *http.Client
	UserAgent  string
	MaxBytes   int64
	Timeout    time.Duration
}

// RequestOptions 只收窄单个请求的配置，不影响其它 provider 共享的 client。
// Timeout 非正数用 client 默认期限，MaxBytes 非正数用其默认上限。
type RequestOptions struct {
	Timeout  time.Duration
	MaxBytes int64
}

// Client 发起请求。可安全地被多个 goroutine 并发使用，调用之间不保留状态。
type Client struct {
	http      *http.Client
	userAgent string
	maxBytes  int64
	timeout   time.Duration
}

// New 应用选项并填充默认值后返回 client。
//
// 自带 *http.Client 的调用方也自带传输层，从而自带代理决策；此处构建的
// client 使用标准传输层，并叠加系统代理决策（proxy.go）。
func New(opts Options) *Client {
	c := &Client{
		http:      opts.HTTPClient,
		userAgent: strings.TrimSpace(opts.UserAgent),
		maxBytes:  opts.MaxBytes,
		timeout:   opts.Timeout,
	}
	if c.http == nil {
		c.http = &http.Client{Transport: ProxyTransport()}
	}
	if c.userAgent == "" {
		c.userAgent = "Boku"
	}
	if c.maxBytes <= 0 {
		c.maxBytes = DefaultMaxBytes
	}
	if c.timeout <= 0 {
		c.timeout = DefaultTimeout
	}
	return c
}

// ProxyTransport 是所有 Boku HTTP client 使用的传输层：标准传输层，代理决策
// 先取环境再取系统本身。
//
// 用克隆而非替换，以保留平台默认的拨号超时、连接复用与 TLS 设置。
func ProxyTransport() *http.Transport {
	standard, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{Proxy: ProxyFunc()}
	}
	transport := standard.Clone()
	transport.Proxy = ProxyFunc()
	return transport
}

// GetJSON 请求一个 JSON 文档。
func (c *Client) GetJSON(ctx context.Context, endpoint string, headers map[string]string) (domain.FetchResult, error) {
	return c.do(ctx, http.MethodGet, endpoint, headers, nil, RequestOptions{})
}

// GetJSONWithOptions 以请求专属的期限或上限请求 JSON，不影响其它 provider 共享的 client 默认值。
func (c *Client) GetJSONWithOptions(ctx context.Context, endpoint string, headers map[string]string, opts RequestOptions) (domain.FetchResult, error) {
	return c.do(ctx, http.MethodGet, endpoint, headers, nil, opts)
}

// GetBytes 请求一个受限长度的二进制响应。它与 JSON 请求共享代理、超时、
// 状态翻译和凭据脱敏，但刻意不校验响应是否为 JSON（例如厂商封面图）。
func (c *Client) GetBytes(ctx context.Context, endpoint string, headers map[string]string) (domain.FetchResult, error) {
	return c.doBytes(ctx, http.MethodGet, endpoint, headers, nil, RequestOptions{})
}

// PostJSON 发送一个 JSON 文档并请求一个 JSON 返回。
func (c *Client) PostJSON(ctx context.Context, endpoint string, headers map[string]string, body []byte) (domain.FetchResult, error) {
	return c.do(ctx, http.MethodPost, endpoint, headers, body, RequestOptions{})
}

// PostJSONWithOptions 以请求专属的期限发送 JSON。用于图片推理这类文档写明
// 延迟不同于普通 API 请求的操作，不得用来悄悄放宽所有 provider 的默认值。
func (c *Client) PostJSONWithOptions(ctx context.Context, endpoint string, headers map[string]string, body []byte, opts RequestOptions) (domain.FetchResult, error) {
	return c.do(ctx, http.MethodPost, endpoint, headers, body, opts)
}

// PostJSONStream 发送 JSON，并把成功的响应体交给 consume 处理。它保留与普通
// 请求相同的代理、超时、状态翻译和响应上限，同时允许 server-sent events 这类
// 协议由其协议包增量解码。
func (c *Client) PostJSONStream(ctx context.Context, endpoint string, headers map[string]string, body []byte, opts RequestOptions, consume func(io.Reader) error) (domain.FetchResult, error) {
	safe := kit.Redact(endpoint)
	if consume == nil {
		return domain.FetchResult{Endpoint: safe}, fmt.Errorf("%w: stream consumer is required", domain.ErrInvalidArgument)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return domain.FetchResult{Endpoint: safe}, fmt.Errorf("%w: %s: %v", domain.ErrInvalidArgument, safe, err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return domain.FetchResult{Endpoint: safe}, transportError(err, safe)
	}
	defer response.Body.Close()
	result := domain.FetchResult{Endpoint: safe, FetchedAt: time.Now().UTC(), HTTPStatus: response.StatusCode}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		payload, readErr := readLimited(response.Body, c.maxBytes)
		if readErr != nil {
			return result, readErr
		}
		return result, statusError(response.StatusCode, safe, payload)
	}
	limit := c.maxBytes
	if opts.MaxBytes > 0 {
		limit = opts.MaxBytes
	}
	// 多读一个字节，用来区分「刚好填满上限」和「越过了上限」的响应。仅达到
	// 上限时读取以普通 EOF 结束，否则流解码器会把被截断的最后一个事件误报为
	// 坏响应。
	limited := &io.LimitedReader{R: response.Body, N: limit + 1}
	tooLarge := &domain.BadResponseError{Endpoint: safe, HTTPStatus: response.StatusCode, Reason: fmt.Sprintf("the response is larger than %d bytes", limit)}
	if err := consume(limited); err != nil {
		if limited.N <= 0 {
			return result, tooLarge
		}
		return result, err
	}
	if limited.N <= 0 {
		return result, tooLarge
	}
	return result, nil
}

// PostForm 发送 application/x-www-form-urlencoded 请求体并请求 JSON 返回。
// 用于应用唯一的 OAuth token 交换（Codex 登录），它是唯一不接受 JSON 的端点。
func (c *Client) PostForm(ctx context.Context, endpoint string, headers map[string]string, form url.Values) (domain.FetchResult, error) {
	merged := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	for name, value := range headers {
		merged[name] = value
	}
	return c.do(ctx, http.MethodPost, endpoint, merged, []byte(form.Encode()), RequestOptions{})
}

// do 执行一次请求，并把所有可能的错误翻译为领域错误：
//
//	401, 403         令牌无效或无权，ErrProviderUnauthorized
//	429              被限流，ErrProviderRateLimited
//	5xx              服务不可用，ErrProviderUnavailable
//	其它状态码        BadResponseError（ErrProviderBadResponse）
//	2xx 但非 JSON     BadResponseError
//	超时、取消        ErrProviderTimeout
//	无应答            ErrProviderUnavailable
func (c *Client) do(ctx context.Context, method, endpoint string, headers map[string]string, body []byte, opts RequestOptions) (domain.FetchResult, error) {
	safe := kit.Redact(endpoint)
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		// 地址由调用方保证正确；这是配置错误，不是服务失败。
		return domain.FetchResult{Endpoint: safe}, fmt.Errorf("%w: %s: %v", domain.ErrInvalidArgument, safe, err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return domain.FetchResult{Endpoint: safe}, transportError(err, safe)
	}
	defer response.Body.Close()

	result := domain.FetchResult{
		Endpoint:   safe,
		FetchedAt:  time.Now().UTC(),
		HTTPStatus: response.StatusCode,
		Cookies:    responseCookies(response),
	}
	data, err := readLimited(response.Body, c.maxBytes)
	if err != nil {
		return result, err
	}
	result.Payload = data

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return result, statusError(response.StatusCode, safe, data)
	}
	if !json.Valid(data) {
		return result, &domain.BadResponseError{
			Endpoint:   safe,
			HTTPStatus: response.StatusCode,
			Reason:     "the response is not JSON",
			Snippet:    kit.RedactText(domain.Snippet(data)),
		}
	}
	return result, nil
}

func (c *Client) doBytes(ctx context.Context, method, endpoint string, headers map[string]string, body []byte, opts RequestOptions) (domain.FetchResult, error) {
	safe := kit.Redact(endpoint)
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return domain.FetchResult{Endpoint: safe}, fmt.Errorf("%w: %s: %v", domain.ErrInvalidArgument, safe, err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "image/*")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return domain.FetchResult{Endpoint: safe}, transportError(err, safe)
	}
	defer response.Body.Close()
	result := domain.FetchResult{Endpoint: safe, FetchedAt: time.Now().UTC(), HTTPStatus: response.StatusCode, Cookies: responseCookies(response)}
	data, err := readLimited(response.Body, c.maxBytes)
	if err != nil {
		return result, err
	}
	result.Payload = data
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return result, statusError(response.StatusCode, safe, data)
	}
	return result, nil
}

// responseCookies 保留每个 Set-Cookie 的 name=value，丢弃属性。只回传值，
// 且只有匿名身份 cookie 值得回传；会话 cookie 永不转发。
func responseCookies(response *http.Response) []string {
	raw := response.Header.Values("Set-Cookie")
	if len(raw) == 0 {
		return nil
	}
	cookies := make([]string, 0, len(raw))
	for _, line := range raw {
		pair, _, _ := strings.Cut(line, ";")
		if pair = strings.TrimSpace(pair); pair != "" {
			cookies = append(cookies, pair)
		}
	}
	return cookies
}

// readLimited 至多读取 max 字节，超长时直接报错，而不是截断成一段可能碰巧
// 能被解析的内容。
func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, transportError(err, "")
	}
	if int64(len(data)) > max {
		return nil, &domain.BadResponseError{
			Reason: fmt.Sprintf("the response is larger than %d bytes", max),
		}
	}
	return data, nil
}

func statusError(status int, endpoint string, payload []byte) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w: %s answered %d", domain.ErrProviderUnauthorized, endpoint, status)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s answered %d", domain.ErrProviderRateLimited, endpoint, status)
	case status >= 500:
		return fmt.Errorf("%w: %s answered %d", domain.ErrProviderUnavailable, endpoint, status)
	default:
		return &domain.BadResponseError{
			Endpoint:   endpoint,
			HTTPStatus: status,
			// 状态码保留在 reason 里，使卡片能区分「服务在拦截请求」
			// （Bilibili 对匿名调用返回 HTTP 412）和「响应毫无意义」。
			Reason:  fmt.Sprintf("the request was rejected (HTTP %d)", status),
			Snippet: kit.RedactText(domain.Snippet(payload)),
		}
	}
}

func transportError(err error, endpoint string) error {
	where := endpoint
	if where != "" {
		where = " at " + where
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w%s", domain.ErrProviderTimeout, where)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w%s", domain.ErrProviderTimeout, where)
	}
	// 传输层自身的错误信息可能引用 URL，因此在进入日志或卡片前先脱敏。
	return fmt.Errorf("%w%s: %s", domain.ErrProviderUnavailable, where, kit.RedactText(err.Error()))
}
