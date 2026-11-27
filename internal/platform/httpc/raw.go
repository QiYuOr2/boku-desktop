package httpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// RawOptions 配置单次原始 HTTP 请求。
type RawOptions struct {
	Timeout      time.Duration
	MaxBytes     int64
	MaxRedirects int
	// ExactHeaders 为 true 时不注入默认 User-Agent，供带签名头列表的对象存储直传使用。
	ExactHeaders bool
}

// RawResponse 保留状态码、小写响应头、最终地址与原始响应体，供需要按协议
// 自行解释非 2xx 报文或 HTML 响应的调用方使用。
type RawResponse struct {
	Status    int
	Headers   map[string]string
	Body      []byte
	FinalURL  string
	FetchedAt time.Time
}

// Raw 发起一次通用 HTTP 请求并原样返回状态与响应体，沿用本客户端的代理、
// 超时、响应体积上限与错误脱敏规则。
func (c *Client) Raw(ctx context.Context, method, endpoint string, headers map[string]string, body []byte, opts RawOptions) (RawResponse, error) {
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
		return RawResponse{}, fmt.Errorf("%w: %s: %v", domain.ErrInvalidArgument, safe, err)
	}
	if !opts.ExactHeaders {
		request.Header.Set("User-Agent", c.userAgent)
	}
	for name, value := range headers {
		// Content-Length 由 net/http 根据 body 长度设置；这里保留签名头里其余键。
		if strings.EqualFold(name, "Content-Length") {
			continue
		}
		request.Header.Set(name, value)
	}
	if body != nil {
		request.ContentLength = int64(len(body))
	}

	client := c.http
	if opts.MaxRedirects > 0 {
		copied := *c.http
		limit := opts.MaxRedirects
		copied.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) > limit {
				return fmt.Errorf("stopped after %d redirects", limit)
			}
			return nil
		}
		client = &copied
	}

	response, err := client.Do(request)
	if err != nil {
		return RawResponse{}, transportError(err, safe)
	}
	defer response.Body.Close()

	limit := c.maxBytes
	if opts.MaxBytes > 0 {
		limit = opts.MaxBytes
	}
	data, err := readLimited(response.Body, limit)
	if err != nil {
		return RawResponse{}, err
	}

	flatHeaders := make(map[string]string, len(response.Header))
	for k, vals := range response.Header {
		if len(vals) > 0 {
			flatHeaders[strings.ToLower(k)] = vals[0]
		}
	}
	finalURL := endpoint
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	return RawResponse{
		Status:    response.StatusCode,
		Headers:   flatHeaders,
		Body:      data,
		FinalURL:  finalURL,
		FetchedAt: time.Now().UTC(),
	}, nil
}
