// 本文件用 xAI 的公开设备码登录，并把 token 存进 Boku 自己的机密存储；
// 不读写 Grok CLI 的文件。
package grok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const refreshSkew = 5 * time.Minute

// SecretStore 是本次登录所需的凭据服务子集；credential 包实现它，本包不 import 它。
type SecretStore interface {
	Get(ctx context.Context, providerID, credentialKey string) (string, error)
	Set(ctx context.Context, providerID, credentialKey, value string) error
	Delete(ctx context.Context, providerID, credentialKey string) error
}

// LoginOptions 配置 Login；零值指向 xAI 正式地址。
type LoginOptions struct {
	Client         *httpc.Client
	Secrets        SecretStore
	DeviceEndpoint string
	TokenEndpoint  string
	Now            func() time.Time
	Pause          func(context.Context, time.Duration) error
}

// Login 是 Grok 面板的凭据来源，可被多个 goroutine 安全使用。
type Login struct {
	client         *httpc.Client
	secrets        SecretStore
	deviceEndpoint string
	tokenEndpoint  string
	now            func() time.Time
	pause          func(context.Context, time.Duration) error

	mu       sync.Mutex
	loggedIn bool
	pending  *deviceAuthorization
	cancel   context.CancelFunc
	pollCtx  context.Context
}

type deviceAuthorization struct {
	deviceCode string
	userCode   string
	url        string
	interval   time.Duration
	deadline   time.Time
}

// NewLogin 返回登录来源。
func NewLogin(opts LoginOptions) *Login {
	client := opts.Client
	if client == nil {
		client = httpc.New(httpc.Options{})
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	pause := opts.Pause
	if pause == nil {
		pause = func(ctx context.Context, wait time.Duration) error {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	deviceEndpoint := strings.TrimSpace(opts.DeviceEndpoint)
	if deviceEndpoint == "" {
		deviceEndpoint = DeviceEndpoint
	}
	tokenEndpoint := strings.TrimSpace(opts.TokenEndpoint)
	if tokenEndpoint == "" {
		tokenEndpoint = TokenEndpoint
	}
	return &Login{
		client:         client,
		secrets:        opts.Secrets,
		deviceEndpoint: deviceEndpoint,
		tokenEndpoint:  tokenEndpoint,
		now:            now,
		pause:          pause,
	}
}

func (l *Login) ProviderID() string { return ProviderID }

// Remember 读取是否已保存登录。窗口启动时只读一次，之后信任内存标志：
// 存有 token 对即为已登录，缺失即普通的未登录状态。
func (l *Login) Remember(ctx context.Context) error {
	_, _, err := l.tokens(ctx)
	if err != nil {
		return ignoreMissing(err)
	}
	l.setLoggedIn(true)
	return nil
}

// LoggedIn 返回内存标志（由 Remember、完成的登录或登出设置），不读磁盘。
func (l *Login) LoggedIn() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loggedIn
}

// Credentials 返回可用的 access token；其自身过期时间落在偏差窗口内时先刷新。
func (l *Login) Credentials(ctx context.Context) (domain.Credentials, error) {
	access, refresh, err := l.tokens(ctx)
	if err != nil {
		return domain.Credentials{}, err
	}
	if l.expiring(access) {
		return l.refresh(ctx, refresh)
	}
	l.setLoggedIn(true)
	return l.creds(access), nil
}

// RefreshCredentials 即使 access token 看起来仍有效也用 refresh token 换取新 token；
// 面板在账单端点拒绝该 access token 后调用它。
func (l *Login) RefreshCredentials(ctx context.Context) (domain.Credentials, error) {
	_, refresh, err := l.tokens(ctx)
	if err != nil {
		return domain.Credentials{}, err
	}
	return l.refresh(ctx, refresh)
}

// BeginDeviceLogin 向 xAI 申请一个供用户输入的设备码；设备码本身留在本地。
func (l *Login) BeginDeviceLogin(ctx context.Context) (domain.DevicePrompt, error) {
	auth, err := l.requestDeviceCode(ctx)
	if err != nil {
		return domain.DevicePrompt{}, err
	}
	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.pollCtx = pollCtx
	l.pending = &auth
	l.mu.Unlock()
	return domain.DevicePrompt{UserCode: auth.userCode, VerificationURL: auth.url}, nil
}

// FinishDeviceLogin 等待用户完成、设备码过期或登录被取消。
func (l *Login) FinishDeviceLogin(ctx context.Context) error {
	l.mu.Lock()
	pending := l.pending
	pollCtx := l.pollCtx
	l.mu.Unlock()
	if pending == nil || pollCtx == nil {
		return fmt.Errorf("%w: no Grok login is waiting", domain.ErrInvalidArgument)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-pollCtx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	interval := pending.interval
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch result, err := l.poll(ctx, pending.deviceCode); {
		case err != nil:
			return err
		case result.accessToken != "":
			refresh := result.refreshToken
			if refresh == "" {
				return &domain.BadResponseError{ProviderID: ProviderID, Endpoint: l.tokenEndpoint, Reason: "the login answered no refresh token"}
			}
			if err := l.save(ctx, result.accessToken, refresh); err != nil {
				return err
			}
			l.clearPending()
			return nil
		case result.slowDown:
			interval = clampInterval(interval + 5*time.Second)
		}
		if !l.now().Before(pending.deadline) {
			l.clearPending()
			return domain.ErrDeviceLoginExpired
		}
		if err := l.pause(ctx, interval); err != nil {
			return err
		}
	}
}

// CancelDeviceLogin 停止仍在等待的登录。
func (l *Login) CancelDeviceLogin() {
	l.mu.Lock()
	cancel := l.cancel
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	l.clearPending()
}

// Logout 删除已保存的 token。
func (l *Login) Logout(ctx context.Context) error {
	l.CancelDeviceLogin()
	var err error
	for _, key := range []string{AccessTokenCredential, RefreshTokenCredential} {
		if deleteErr := l.secrets.Delete(ctx, ProviderID, key); deleteErr != nil && !errors.Is(deleteErr, domain.ErrSecretNotFound) {
			err = deleteErr
		}
	}
	l.setLoggedIn(false)
	return err
}

func (l *Login) tokens(ctx context.Context) (string, string, error) {
	if l.secrets == nil {
		l.setLoggedIn(false)
		return "", "", fmt.Errorf("%w: no secret store", domain.ErrCredentialMissing)
	}
	access, err := l.one(ctx, AccessTokenCredential)
	if err != nil {
		l.setLoggedIn(false)
		return "", "", err
	}
	refresh, err := l.one(ctx, RefreshTokenCredential)
	if err != nil {
		l.setLoggedIn(false)
		return "", "", err
	}
	return access, refresh, nil
}

func (l *Login) one(ctx context.Context, key string) (string, error) {
	value, err := l.secrets.Get(ctx, ProviderID, key)
	if errors.Is(err, domain.ErrSecretNotFound) || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s", domain.ErrCredentialMissing, domain.CredentialKey(ProviderID, key))
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (l *Login) expiring(access string) bool {
	expiry := expiresAt(access)
	return !expiry.IsZero() && expiry.Sub(l.now()) < refreshSkew
}

func (l *Login) refresh(ctx context.Context, refresh string) (domain.Credentials, error) {
	fresh, err := l.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {refresh},
	})
	if err != nil {
		if errors.Is(err, domain.ErrProviderUnauthorized) || errors.Is(err, domain.ErrProviderBadResponse) {
			l.setLoggedIn(false)
			return domain.Credentials{}, fmt.Errorf("%w: the Grok login was refused", domain.ErrCredentialMissing)
		}
		return domain.Credentials{}, err
	}
	if fresh.refreshToken == "" {
		fresh.refreshToken = refresh
	}
	if err := l.save(ctx, fresh.accessToken, fresh.refreshToken); err != nil {
		return domain.Credentials{}, err
	}
	return l.creds(fresh.accessToken), nil
}

func (l *Login) save(ctx context.Context, access, refresh string) error {
	if err := l.secrets.Set(ctx, ProviderID, AccessTokenCredential, access); err != nil {
		return err
	}
	if err := l.secrets.Set(ctx, ProviderID, RefreshTokenCredential, refresh); err != nil {
		return err
	}
	l.setLoggedIn(true)
	return nil
}

func (l *Login) creds(access string) domain.Credentials {
	return domain.Credentials{ProviderID: ProviderID, Secrets: map[string]string{AccessTokenCredential: access}, Settings: map[string]string{}}
}

func (l *Login) setLoggedIn(on bool) {
	l.mu.Lock()
	l.loggedIn = on
	l.mu.Unlock()
}

func (l *Login) clearPending() {
	l.mu.Lock()
	l.pending = nil
	l.pollCtx = nil
	l.mu.Unlock()
}

func ignoreMissing(err error) error {
	if errors.Is(err, domain.ErrCredentialMissing) {
		return nil
	}
	return err
}

type tokenResult struct {
	accessToken  string
	refreshToken string
	slowDown     bool
}

func (l *Login) requestDeviceCode(ctx context.Context) (deviceAuthorization, error) {
	result, err := l.client.PostForm(ctx, l.deviceEndpoint, nil, url.Values{
		"client_id": {ClientID},
		"scope":     {scopes},
		"referrer":  {"boku"},
	})
	if err != nil {
		return deviceAuthorization{}, err
	}
	var payload struct {
		DeviceCode string   `json:"device_code"`
		UserCode   string   `json:"user_code"`
		Complete   string   `json:"verification_uri_complete"`
		URI        string   `json:"verification_uri"`
		URL        string   `json:"verification_url"`
		ExpiresIn  flexible `json:"expires_in"`
		Interval   flexible `json:"interval"`
	}
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return deviceAuthorization{}, badForm(l.deviceEndpoint, result, "the login did not answer with a device code")
	}
	deviceCode := strings.TrimSpace(payload.DeviceCode)
	userCode := strings.TrimSpace(payload.UserCode)
	page := firstHTTPS(payload.Complete, payload.URI, payload.URL)
	if deviceCode == "" || userCode == "" || page == "" {
		return deviceAuthorization{}, badForm(l.deviceEndpoint, result, "the login did not answer with a device code")
	}
	expires := time.Duration(int64(payload.ExpiresIn.seconds(900))) * time.Second
	return deviceAuthorization{
		deviceCode: deviceCode,
		userCode:   userCode,
		url:        page,
		interval:   clampInterval(time.Duration(int64(payload.Interval.seconds(5))) * time.Second),
		deadline:   l.now().Add(expires),
	}, nil
}

func (l *Login) poll(ctx context.Context, deviceCode string) (tokenResult, error) {
	result, err := l.client.PostForm(ctx, l.tokenEndpoint, nil, url.Values{
		"grant_type":  {deviceGrant},
		"client_id":   {ClientID},
		"device_code": {deviceCode},
	})
	return l.tokenAnswer(result, err, true)
}

func (l *Login) exchange(ctx context.Context, form url.Values) (tokenResult, error) {
	result, err := l.client.PostForm(ctx, l.tokenEndpoint, nil, form)
	return l.tokenAnswer(result, err, false)
}

func (l *Login) tokenAnswer(result domain.FetchResult, err error, allowPending bool) (tokenResult, error) {
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if len(result.Payload) > 0 {
		_ = json.Unmarshal(result.Payload, &payload)
	}
	switch payload.Error {
	case "":
	case "authorization_pending":
		if allowPending {
			return tokenResult{}, nil
		}
	case "slow_down":
		if allowPending {
			return tokenResult{slowDown: true}, nil
		}
	case "access_denied", "expired_token", "invalid_grant":
		return tokenResult{}, domain.ErrProviderUnauthorized
	default:
		return tokenResult{}, badForm(l.tokenEndpoint, result, "the login refused the request")
	}
	if err != nil {
		return tokenResult{}, err
	}
	access := strings.TrimSpace(payload.AccessToken)
	if access == "" {
		return tokenResult{}, badForm(l.tokenEndpoint, result, "the login answered no access token")
	}
	return tokenResult{accessToken: access, refreshToken: strings.TrimSpace(payload.RefreshToken)}, nil
}

func firstHTTPS(values ...string) string {
	for _, value := range values {
		parsed, err := url.Parse(strings.TrimSpace(value))
		if err == nil && parsed.Scheme == "https" && parsed.Host != "" {
			return parsed.String()
		}
	}
	return ""
}

func clampInterval(interval time.Duration) time.Duration {
	if interval < time.Second {
		return time.Second
	}
	if interval > 30*time.Second {
		return 30 * time.Second
	}
	return interval
}

func badForm(endpoint string, result domain.FetchResult, reason string) error {
	return &domain.BadResponseError{
		ProviderID: ProviderID,
		Endpoint:   endpoint,
		HTTPStatus: result.HTTPStatus,
		Reason:     reason,
		Snippet:    domain.Snippet(result.Payload),
	}
}

// flexible 读取 JSON 数字或数字字符串；设备码响应两种都出现过。
type flexible struct {
	value float64
	set   bool
}

func (f *flexible) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	if text[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		text = strings.TrimSpace(raw)
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	f.value, f.set = parsed, true
	return nil
}

func (f flexible) seconds(fallback float64) float64 {
	if !f.set || f.value <= 0 {
		return fallback
	}
	return f.value
}

var (
	_ domain.CredentialSource    = (*Login)(nil)
	_ domain.CredentialRefresher = (*Login)(nil)
	_ domain.DeviceLogin         = (*Login)(nil)
)
