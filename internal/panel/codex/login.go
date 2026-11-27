// 本文件读取 Codex CLI 已经建立的 ChatGPT 登录，并在该登录过期时用它的 refresh
// token 换取新的 access token。
//
// 它是应用里唯一打开另一个工具凭据文件的地方：不会复制进 Boku 自己的存储，也不会写回
// CLI 的文件，token 只在发往 ChatGPT 时离开本进程（见安全设计 §9）。它实现
// domain.CredentialSource，面板由此拿到一份 Boku 并不保存的凭据。
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	// authFileName 是 Codex CLI 写入其登录的文件名。
	authFileName = "auth.json"
	// codexHomeEnv 是 CLI 自己用来覆盖该文件位置的变量。
	codexHomeEnv = "CODEX_HOME"
	// staleLogin 是文件自身「上次刷新」可容许的最大时长；超过后即使 token 看似有效
	// 也会刷新。它与 CLI 采用的八天一致。
	staleLogin = 8 * 24 * time.Hour
	// refreshSkew 是提前多久替换 token：迟到的回答不能用一个已经死在路上的 token 去回。
	refreshSkew = 5 * time.Minute
)

// LoginCommand 是用户自己会敲的命令，放在这里使界面提示与文档说同一件事。
const LoginCommand = "codex login"

// LoginOptions 配置 Login。零值可用：真实主目录、真实环境与 Codex 自己的 token 端点。
type LoginOptions struct {
	// Client 执行刷新所需的那一次请求。零值是采用应用默认值的 client。
	Client *httpc.Client
	// Home 覆盖用户主目录：测试指向自己的目录，其它地方不读它。
	Home string
	// CodexHome 覆盖 CODEX_HOME，CLI 自身会遵守它。
	CodexHome string
	// TokenEndpoint 覆盖刷新请求发往的地址。
	TokenEndpoint string
	// Now 覆盖时钟。
	Now func() time.Time
}

// Login 是 Codex 面板的凭据来源。可被多个 goroutine 同时使用：面板在主线程之外
// 向它查询。
type Login struct {
	client        *httpc.Client
	home          string
	codexHome     string
	tokenEndpoint string
	now           func() time.Time

	mu sync.Mutex
	// remembered 是本进程为文件里那个 token 换取来的新 token。没有它，每一轮刷新都会
	// 重复使用同一个 refresh token，因为来源文件属于 CLI，Boku 从不更新它。
	remembered remembered
}

type remembered struct {
	// from 是换取该 token 时文件持有的 access token：用户重新登录后 token 会变，
	// 那个新的应当优先。
	from  string
	token Token
}

// NewLogin 返回登录来源。
func NewLogin(opts LoginOptions) *Login {
	client := opts.Client
	if client == nil {
		client = httpc.New(httpc.Options{})
	}
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		if found, err := os.UserHomeDir(); err == nil {
			home = found
		}
	}
	codexHome := strings.TrimSpace(opts.CodexHome)
	if codexHome == "" {
		codexHome = strings.TrimSpace(os.Getenv(codexHomeEnv))
	}
	endpoint := strings.TrimSpace(opts.TokenEndpoint)
	if endpoint == "" {
		endpoint = TokenEndpoint
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Login{
		client:        client,
		home:          home,
		codexHome:     codexHome,
		tokenEndpoint: endpoint,
		now:           now,
	}
}

// ProviderID 是本来源提供的 provider。
func (l *Login) ProviderID() string { return ProviderID }

// authFile 是 CLI 自己的登录文件在磁盘上的样子。
type authFile struct {
	// APIKey 是 CLI 的另一种登录方式。读它只是为了说明用户处于哪种模式：API key
	// 无法读取 ChatGPT 套餐配额。
	APIKey *string `json:"OPENAI_API_KEY"`
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// Credentials 返回 Codex provider 所需的内容：CLI 登录所用的 token 及其所属账户。
func (l *Login) Credentials(ctx context.Context) (domain.Credentials, error) {
	file, _, err := l.read()
	if err != nil {
		return domain.Credentials{}, err
	}
	if strings.TrimSpace(file.Tokens.AccessToken) == "" {
		if file.APIKey != nil && strings.TrimSpace(*file.APIKey) != "" {
			return domain.Credentials{}, fmt.Errorf("%w: the Codex CLI is logged in with an API key, and the plan's quota is only there for a ChatGPT login", domain.ErrCredentialMissing)
		}
		return domain.Credentials{}, fmt.Errorf("%w: the Codex login has no access token; run %s", domain.ErrCredentialMissing, LoginCommand)
	}
	token, err := l.token(ctx, file)
	if err != nil {
		return domain.Credentials{}, err
	}
	return domain.Credentials{
		ProviderID: ProviderID,
		Secrets:    map[string]string{AccessTokenCredential: token.AccessToken},
		Settings:   map[string]string{AccountIDSetting: strings.TrimSpace(file.Tokens.AccountID)},
	}, nil
}

// StartLogin 在终端窗口里启动 CLI 自己的登录流程。这个流程属于 CLI 而非 Boku：
// 这里只是省去用户自己找终端，下一次读取就会拿到该登录。
func (l *Login) StartLogin() error {
	command, err := lookupLoginCommand("codex")
	if err != nil {
		return fmt.Errorf("%w: %s is not installed: %v", domain.ErrLoginCommandMissing, LoginCommand, err)
	}
	return startLogin(command)
}

// lookupLoginCommand 在打开终端前找到 CLI。查找是平台相关的：从 GUI 启动的 macOS
// 应用没有终端的 PATH，因此除了 PATH 还会询问用户的登录 shell 与常见安装位置。它是
// 变量只是为了测试能替身机器的真实工具。
var lookupLoginCommand = findLoginCommand

// 在端口接口定义处满足它们，而不是在调用点。
var (
	_ domain.CredentialSource = (*Login)(nil)
	_ domain.LoginStarter     = (*Login)(nil)
)

// paths 返回 CLI 登录可能所在的路径，按查找顺序：CLI 自己的覆盖优先，然后是它默认
// 使用的两个位置。
func (l *Login) paths() []string {
	if l.codexHome != "" {
		return []string{filepath.Join(l.codexHome, authFileName)}
	}
	if l.home == "" {
		return nil
	}
	return []string{
		filepath.Join(l.home, ".config", "codex", authFileName),
		filepath.Join(l.home, ".codex", authFileName),
	}
}

// read 返回它找到的第一个登录及其位置。机器上完全没有 Codex 登录属于
// ErrCredentialMissing：这是用户登录前的状态，不是服务故障。
func (l *Login) read() (authFile, string, error) {
	paths := l.paths()
	if len(paths) == 0 {
		return authFile{}, "", fmt.Errorf("%w: this machine has no home directory to look for a Codex login in", domain.ErrCredentialMissing)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return authFile{}, path, fmt.Errorf("read the Codex login at %s: %w", path, err)
		}
		var file authFile
		if err := json.Unmarshal(data, &file); err != nil {
			return authFile{}, path, fmt.Errorf("%w: the Codex login at %s is not readable: %v", domain.ErrCredentialMissing, path, err)
		}
		return file, path, nil
	}
	return authFile{}, "", fmt.Errorf("%w: no Codex login at %s (run %s)",
		domain.ErrCredentialMissing, strings.Join(paths, ", "), LoginCommand)
}

// token 返回一个值得发送的 token：文件自身的（仍有效时）、本进程已换来的，或全新的。
//
// 绝不写回文件：它属于 CLI，Boku 若改写它只会让 CLI 自己的状态变错。
func (l *Login) token(ctx context.Context, file authFile) (Token, error) {
	access := strings.TrimSpace(file.Tokens.AccessToken)
	refresh := strings.TrimSpace(file.Tokens.RefreshToken)
	expiry := ExpiresAt(access)
	// CLI 写入的时间戳若无法解析，按「未写入」处理：token 照原样发送，而不是按
	// 无人担保的计划去刷新。仍值得记一行日志——文件不符预期正是旧 token 一直被发送
	// 的原因，没有别处会说明这点。
	lastRefresh, lastRefreshErr := time.Parse(time.RFC3339, strings.TrimSpace(file.LastRefresh))
	if lastRefreshErr != nil && strings.TrimSpace(file.LastRefresh) != "" {
		slog.Warn("the Codex login's last refresh time could not be read", "event", "codex_login_timestamp_unreadable", "error", lastRefreshErr)
	}
	now := l.now()

	l.mu.Lock()
	cached := l.remembered
	l.mu.Unlock()
	if cached.from == access && cached.from != "" && !cached.token.ExpiresAt.IsZero() &&
		now.Before(cached.token.ExpiresAt.Add(-refreshSkew)) {
		return cached.token, nil
	}

	needsRefresh := false
	switch {
	case !expiry.IsZero():
		needsRefresh = !now.Before(expiry.Add(-refreshSkew))
	case !lastRefresh.IsZero():
		// 不声明过期时间的 token 按 CLI 自己的规则替换：距上次刷新超过八天。
		needsRefresh = now.Sub(lastRefresh) > staleLogin
	}
	if !needsRefresh {
		return Token{
			AccessToken:  access,
			RefreshToken: refresh,
			IDToken:      strings.TrimSpace(file.Tokens.IDToken),
			ExpiresAt:    expiry,
		}, nil
	}

	fresh, err := RefreshAccessToken(ctx, l.client, l.tokenEndpoint, refresh)
	if err != nil {
		return Token{}, loginRefused(err)
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = refresh
	}
	if fresh.IDToken == "" {
		fresh.IDToken = strings.TrimSpace(file.Tokens.IDToken)
	}
	l.mu.Lock()
	l.remembered = remembered{from: access, token: fresh}
	l.mu.Unlock()
	return fresh, nil
}

// loginRefused 把 token 端点的话转成用户唯一能采取的行动：登录已失效，请重新登录。
// 网络故障不属于此列，原样传出。
func loginRefused(err error) error {
	var bad *domain.BadResponseError
	switch {
	case errors.Is(err, domain.ErrProviderUnauthorized), errors.As(err, &bad):
		return fmt.Errorf("%w: the Codex login was refused (%v); run %s", domain.ErrCredentialMissing, err, LoginCommand)
	default:
		return err
	}
}
