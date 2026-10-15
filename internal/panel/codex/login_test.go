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

// now is the moment the tests live at: expiry arithmetic is written down
// rather than read from the wall clock.
var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// jwt builds a token that states when it expires. Only the payload is read:
// the question is "is this worth sending", and the endpoint decides whether
// it is genuine.
func jwt(expiresAt time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(expiresAt.Unix(), 10) + `}`))
	return "header." + payload + ".signature"
}

// loginFile writes a CLI login into a home directory of its own, in the
// place the CLI uses by default, and returns that home.
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

// codexHomeWith writes a login where CODEX_HOME points, which is the file
// itself rather than a directory of the CLI's own.
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

// tokenServer answers a refresh with a new token, and counts what it was
// asked.
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

// A login the CLI has already made is handed to the provider as it lies: the
// token, and the account it belongs to.
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
	// A token that is still good must not be exchanged again.
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

// A machine with no Codex login is the state before the user logs in: the
// panel says so rather than showing a number.
func TestCredentialsRefuseAMachineWithoutALogin(t *testing.T) {
	_, err := newLogin(t, t.TempDir(), "").Credentials(context.Background())
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if !strings.Contains(err.Error(), LoginCommand) {
		t.Errorf("the error does not say what to run: %v", err)
	}
}

// The CLI has a second login mode, and a plan's quota is not behind it:
// saying which mode the user is in is more useful than a failure.
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

// The CLI's own home, when it sets one, is where the login is looked for.
func TestCodexHomeIsHonoured(t *testing.T) {
	codexHome := codexHomeWith(t, authJSON(jwt(now.Add(time.Hour)), "refresh-token", ""))
	// The user's own home has nothing; CODEX_HOME is the only place with a
	// login, and it wins.
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

// An expired token is exchanged for a new one, and the file it came from is
// left exactly as it was: it belongs to the CLI.
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

	// The exchanged token is remembered: the same file must not be
	// exchanged for it twice.
	if _, err := login.Credentials(context.Background()); err != nil {
		t.Fatalf("Credentials failed on the second call: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("the token endpoint was asked %d time(s), want one exchange for one expired token", got)
	}
}

// A login the endpoint refuses is the one thing the user can act on: log in
// again. The message says so.
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

// A refresh that failed because the network is down is not a login that
// expired: the panel must say "the service is unavailable", not "log in
// again".
func TestCredentialsPropagateANetworkFailure(t *testing.T) {
	home := loginFile(t, authJSON(jwt(now.Add(-time.Hour)), "refresh-token", ""))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // nothing is listening any more
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

// A login file that is not the shape the CLI writes is the same answer as no
// login at all: there is nothing to send.
func TestCredentialsRefuseAnUnreadableLogin(t *testing.T) {
	home := loginFile(t, `{"tokens":`)
	if _, err := newLogin(t, home, "").Credentials(context.Background()); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("error = %v, want ErrCredentialMissing", err)
	}
}

// Starting the login without the CLI installed says so instead of opening
// nothing.
func TestStartLoginWithoutTheCommand(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := newLogin(t, t.TempDir(), "").StartLogin()
	if !errors.Is(err, domain.ErrLoginCommandMissing) {
		t.Errorf("error = %v, want ErrLoginCommandMissing", err)
	}
}
