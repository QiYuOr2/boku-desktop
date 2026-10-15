// Package httpc is the application's only way out to the network. It puts
// the same discipline around every third-party request: a timeout and a
// cancellation, a ceiling on the response, and an error the caller can
// classify instead of a raw status code.
//
// It also decides where a request goes: the environment's proxy setting first,
// and the machine's own if the environment names none (proxy.go). A program
// that asks only the environment reaches nothing on a machine whose proxy is
// set in Windows, which is how the Codex quota panel first failed.
//
// Keeping a credential out of a message is not this package's job either,
// though it is done here: internal/kit owns the rule, and the logger applies
// the same one on its way to disk.
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
	// DefaultTimeout bounds one request, end to end. A provider may ask for
	// less; nothing waits on a third party longer than this.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxBytes is how much of a response is read. A provider that
	// answers with more than this is answering with something else.
	DefaultMaxBytes = 1 << 20
)

// Options configures a Client. The zero value is usable: a standard client,
// "Boku" as the user agent, DefaultTimeout and DefaultMaxBytes.
type Options struct {
	// HTTPClient is the transport to use. Its own Timeout is left alone:
	// the request context carries the deadline, so one client can serve
	// requests with different ones.
	HTTPClient *http.Client
	UserAgent  string
	MaxBytes   int64
	Timeout    time.Duration
}

// Client makes the requests. It is safe to use from several goroutines, and
// it keeps no state between calls.
type Client struct {
	http      *http.Client
	userAgent string
	maxBytes  int64
	timeout   time.Duration
}

// New returns a client with the options applied, filling in the defaults.
//
// A caller that brings its own *http.Client brings its own transport, and with
// it its own decision about proxies; a client built here gets the standard
// transport with the proxy decision the machine's own setting adds (proxy.go).
func New(opts Options) *Client {
	c := &Client{
		http:      opts.HTTPClient,
		userAgent: strings.TrimSpace(opts.UserAgent),
		maxBytes:  opts.MaxBytes,
		timeout:   opts.Timeout,
	}
	if c.http == nil {
		c.http = &http.Client{Transport: proxyTransport()}
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

// proxyTransport is the transport a client built here uses: the standard one,
// which knows how to dial and how long to wait, with the proxy decision drawn
// from the environment and then from the machine itself.
//
// It is a clone rather than a replacement, so that everything else about it —
// the dial timeouts, the connection reuse, the TLS settings — stays what the
// platform's own defaults are.
func proxyTransport() *http.Transport {
	standard, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{Proxy: ProxyFunc()}
	}
	transport := standard.Clone()
	transport.Proxy = ProxyFunc()
	return transport
}

// GetJSON asks for a JSON document.
func (c *Client) GetJSON(ctx context.Context, endpoint string, headers map[string]string) (domain.FetchResult, error) {
	return c.do(ctx, http.MethodGet, endpoint, headers, nil)
}

// PostJSON sends a JSON document and asks for one back.
func (c *Client) PostJSON(ctx context.Context, endpoint string, headers map[string]string, body []byte) (domain.FetchResult, error) {
	return c.do(ctx, http.MethodPost, endpoint, headers, body)
}

// PostForm sends an application/x-www-form-urlencoded body and asks for a
// JSON answer. It is for the one OAuth token exchange the application makes
// (the Codex login), which is the one endpoint that does not take JSON.
func (c *Client) PostForm(ctx context.Context, endpoint string, headers map[string]string, form url.Values) (domain.FetchResult, error) {
	merged := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	for name, value := range headers {
		merged[name] = value
	}
	return c.do(ctx, http.MethodPost, endpoint, merged, []byte(form.Encode()))
}

// do performs one request and translates everything that can go wrong into
// a domain error:
//
//	401, 403         ErrProviderUnauthorized
//	429              ErrProviderRateLimited
//	5xx              ErrProviderUnavailable
//	any other status BadResponseError (ErrProviderBadResponse)
//	2xx but not JSON BadResponseError
//	timeout, cancel  ErrProviderTimeout
//	no answer        ErrProviderUnavailable
func (c *Client) do(ctx context.Context, method, endpoint string, headers map[string]string, body []byte) (domain.FetchResult, error) {
	safe := kit.Redact(endpoint)
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		// The address is the caller's to get right; it is configuration,
		// not a service that failed.
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

// readLimited reads at most max bytes and reports a longer answer rather
// than truncating it into something that parses by accident.
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
			Reason:     "the request was rejected",
			Snippet:    kit.RedactText(domain.Snippet(payload)),
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
	// The transport's own message can quote the URL, so it is redacted
	// before it reaches a log or a card.
	return fmt.Errorf("%w%s: %s", domain.ErrProviderUnavailable, where, kit.RedactText(err.Error()))
}
