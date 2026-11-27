package codex

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

// now 是测试所处的时刻：过期计算写死，不读系统时钟。
var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// jwt 构建一个自我声明过期时间的 token。只读 payload：问的是「还值不值得发送」，
// 真伪由端点判断。
func jwt(expiresAt time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(expiresAt.Unix(), 10) + `}`))
	return "header." + payload + ".signature"
}

// loginFile 把一份 CLI 登录写入它自己的主目录，位置是 CLI 默认所用的地方，并返回
// 该主目录。
func loginFile(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("make the CLI's directory: %v", err)
	}
	path := filepath.Join(home, ".codex", authFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write the login: %v", err)
	}
	return home
}

// codexHomeWith 把登录写到 CODEX_HOME 指向的位置，该处是文件本身而不是 CLI 自己的
// 目录。
func codexHomeWith(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, authFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("write the login: %v", err)
	}
	return dir
}

func authJSON(access, refresh, account string) string {
	return `{"OPENAI_API_KEY":null,"tokens":{"access_token":"` + access + `","refresh_token":"` +
		refresh + `","id_token":"id-token","account_id":"` + account + `"},"last_refresh":"2026-10-14T11:00:00Z"}`
}

// tokenServer 用新 token 回应刷新，并统计被询问的次数。
func tokenServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func newLogin(t *testing.T, home, tokenEndpoint string) *Login {
	t.Helper()
	return NewLogin(LoginOptions{
		Client:        httpc.New(httpc.Options{}),
		Home:          home,
		TokenEndpoint: tokenEndpoint,
		Now:           func() time.Time { return now },
	})
}

// CLI 已经建立的登录按原样交给 provider：token 及其所属账户。
func TestCredentialsUseTheLoginsToken(t *testing.T) {
	access := jwt(now.Add(2 * time.Hour))
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", authFileName)
	if err := os.WriteFile(path, []byte(authJSON(access, "refresh-token", "account-9")), 0o600); err != nil {
		t.Fatal(err)
	}
	// 仍然有效的 token 不能再被换取。
	server, calls := tokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a token that had not expired was refreshed")
	})

	creds, err := newLogin(t, home, server.URL).Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials failed: %v", err)
	}
	if creds.ProviderID != ProviderID {
		t.Errorf("ProviderID = %q", creds.ProviderID)
	}
	if got := creds.Secrets[AccessTokenCredential]; got != access {
		t.Errorf("accessToken = %q, want the login's own", got)
	}
	if got := creds.Setting(AccountIDSetting); got != "account-9" {
		t.Errorf("accountId = %q, want the login's own", got)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("the token endpoint was asked %d time(s)", got)
	}
}

// 没有 Codex 登录的机器就是用户登录前的状态：面板照此提示，而不是显示数字。
func TestCredentialsRefuseAMachineWithoutALogin(t *testing.T) {
	_, err := newLogin(t, t.TempDir(), "").Credentials(context.Background())
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if !strings.Contains(err.Error(), LoginCommand) {
		t.Errorf("the error does not say what to run: %v", err)
	}
}

// CLI 还有另一种登录模式，套餐配额并不在其中：说明用户处于哪种模式比报失败更有用。
func TestCredentialsRefuseAnAPIKeyLogin(t *testing.T) {
	home := loginFile(t, `{"OPENAI_API_KEY":"sk-not-a-plan","tokens":{}}`)
	_, err := newLogin(t, home, "").Credentials(context.Background())
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("the error does not say which login it found: %v", err)
	}
}

// 当 CLI 设置了自己的 home 时，登录就在那里查找。
func TestCodexHomeIsHonoured(t *testing.T) {
	codexHome := codexHomeWith(t, authJSON(jwt(now.Add(time.Hour)), "refresh-token", ""))
	// 用户自己的主目录里没有登录，CODEX_HOME 是唯一有登录的地方，因此它胜出。
	login := NewLogin(LoginOptions{
		Client:    httpc.New(httpc.Options{}),
		Home:      t.TempDir(),
		CodexHome: codexHome,
		Now:       func() time.Time { return now },
	})
	creds, err := login.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials failed: %v", err)
	}
	if creds.Secrets[AccessTokenCredential] == "" {
		t.Error("the login under CODEX_HOME was not read")
	}
}

// 过期的 token 会被换取成新的，且其来源文件保持原样：它属于 CLI。
func TestCredentialsRefreshAnExpiredTokenAndLeaveTheFileAlone(t *testing.T) {
	access := jwt(now.Add(-time.Minute))
	home := loginFile(t, authJSON(access, "refresh-token", "account-9"))
	path := filepath.Join(home, ".codex", authFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var form string
	server, calls := tokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		form = r.PostForm.Encode()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + jwt(now.Add(time.Hour)) + `","refresh_token":"new-refresh","expires_in":3600}`))
	})
	login := newLogin(t, home, server.URL)

	creds, err := login.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials failed: %v", err)
	}
	if got := creds.Secrets[AccessTokenCredential]; got == access || got == "" {
		t.Errorf("accessToken = %q, want a token this process exchanged for the expired one", got)
	}
	if !strings.Contains(form, "refresh_token=refresh-token") {
		t.Errorf("the exchange did not carry the login's refresh token: %q", form)
	}
	if !strings.Contains(form, "client_id="+url.QueryEscape(ClientID)) {
		t.Errorf("the exchange did not carry Codex's client id: %q", form)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the CLI's login file was rewritten:\nbefore: %s\nafter:  %s", before, after)
	}

	// 换来的 token 会被记住：同一文件不能再被换取第二次。
	if _, err := login.Credentials(context.Background()); err != nil {
		t.Fatalf("Credentials failed on the second call: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("the token endpoint was asked %d time(s), want one exchange for one expired token", got)
	}
}

// 被端点拒绝的登录是用户唯一能采取行动的事：重新登录。信息会说明这一点。
func TestCredentialsRefuseARefusedRefresh(t *testing.T) {
	home := loginFile(t, authJSON(jwt(now.Add(-time.Hour)), "stale-refresh", ""))
	server, _ := tokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	})
	login := newLogin(t, home, server.URL)

	_, err := login.Credentials(context.Background())
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if !strings.Contains(err.Error(), LoginCommand) {
		t.Errorf("the error does not say what to run: %v", err)
	}
}

// 因网络断开而失败的刷新不是登录过期：面板应提示「服务不可用」，而不是「重新登录」。
func TestCredentialsPropagateANetworkFailure(t *testing.T) {
	home := loginFile(t, authJSON(jwt(now.Add(-time.Hour)), "refresh-token", ""))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // 此时已无监听
	login := newLogin(t, home, server.URL)

	_, err := login.Credentials(context.Background())
	if err == nil {
		t.Fatal("an unreachable token endpoint was accepted")
	}
	if errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("a network failure was reported as a login that expired: %v", err)
	}
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Errorf("error = %v, want ErrProviderUnavailable", err)
	}
}

// 不符合 CLI 写出结构的登录文件，与完全没有登录是同一种答案：没有可发送的东西。
func TestCredentialsRefuseAnUnreadableLogin(t *testing.T) {
	home := loginFile(t, `{"tokens":`)
	if _, err := newLogin(t, home, "").Credentials(context.Background()); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("error = %v, want ErrCredentialMissing", err)
	}
}

// 未安装 CLI 时启动登录会明确告知，而不是打开空无一物。
func TestStartLoginWithoutTheCommand(t *testing.T) {
	previous := lookupLoginCommand
	lookupLoginCommand = func(string) (string, error) {
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookupLoginCommand = previous })

	err := newLogin(t, t.TempDir(), "").StartLogin()
	if !errors.Is(err, domain.ErrLoginCommandMissing) {
		t.Errorf("error = %v, want ErrLoginCommandMissing", err)
	}
}
