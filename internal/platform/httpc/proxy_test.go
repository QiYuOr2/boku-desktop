package httpc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// 请求先问环境再问系统：环境是有人专门为这个程序设置的，系统的是同一用户为
// 其它一切设置的。
func TestTheEnvironmentComesFirstAndTheMachineIsTheFallback(t *testing.T) {
	machine := proxySettings{Enabled: true, Server: "127.0.0.1:7897"}
	fromMachine := func() (proxySettings, error) { return machine, nil }
	request := mustRequest(t, "https://chatgpt.com/backend-api/wham/usage")

	fromEnvironment := func(*http.Request) (*url.URL, error) {
		return url.Parse("http://10.0.0.1:3128")
	}
	proxy, err := proxyFor(fromEnvironment, fromMachine, request)
	if err != nil {
		t.Fatalf("proxyFor failed: %v", err)
	}
	if proxy == nil || proxy.String() != "http://10.0.0.1:3128" {
		t.Errorf("proxy = %v, want the environment's", proxy)
	}

	// 环境没有设置：由系统作答，且是为所请求的协议作答。
	none := func(*http.Request) (*url.URL, error) { return nil, nil }
	proxy, err = proxyFor(none, fromMachine, request)
	if err != nil {
		t.Fatalf("proxyFor failed: %v", err)
	}
	if proxy == nil || proxy.String() != "http://127.0.0.1:7897" {
		t.Errorf("proxy = %v, want the machine's own", proxy)
	}

	// 环境拒绝请求也是一种答案：不会被系统设置覆盖。
	refused := errors.New("no proxy for this one")
	if _, err := proxyFor(func(*http.Request) (*url.URL, error) { return nil, refused }, fromMachine, request); !errors.Is(err, refused) {
		t.Errorf("proxyFor error = %v, want the environment's refusal", err)
	}
}

// 系统设置通过一个函数读取；读不到或已关闭的设置都视为无代理，而不是请求失败。
func TestAMachineSettingThatIsOffOrUnreadableIsNoProxy(t *testing.T) {
	request := mustRequest(t, "https://chatgpt.com/backend-api/wham/usage")
	cases := map[string]func() (proxySettings, error){
		"switched off": func() (proxySettings, error) {
			return proxySettings{Enabled: false, Server: "127.0.0.1:7897"}, nil
		},
		"unreadable": func() (proxySettings, error) {
			return proxySettings{}, errors.New("no such key")
		},
		"no setting at all": func() (proxySettings, error) { return proxySettings{}, nil },
	}
	for name, read := range cases {
		t.Run(name, func(t *testing.T) {
			proxy, err := proxyFor(nil, read, request)
			if err != nil {
				t.Fatalf("proxyFor failed: %v", err)
			}
			if proxy != nil {
				t.Errorf("proxy = %v, want none", proxy)
			}
		})
	}
}

// 系统设置有两种写法，都可能缺少某个协议。
func TestTheSystemProxySettingInBothShapes(t *testing.T) {
	cases := []struct {
		name   string
		server string
		scheme string
		want   string
	}{
		{"one address for everything", "127.0.0.1:7897", "https", "http://127.0.0.1:7897"},
		{"one address, http", "127.0.0.1:7897", "http", "http://127.0.0.1:7897"},
		{"per protocol, https", "http=127.0.0.1:7897;https=127.0.0.1:7898", "https", "http://127.0.0.1:7898"},
		{"per protocol, the scheme is not listed", "http=127.0.0.1:7897", "https", ""},
		{"per protocol, socks", "socks=127.0.0.1:7899", "http", "socks5://127.0.0.1:7899"},
		{"per protocol, socks covers what is not listed", "http=127.0.0.1:7897;socks=127.0.0.1:7899", "https", "socks5://127.0.0.1:7899"},
		{"per protocol, the scheme wins over socks", "https=127.0.0.1:7898;socks=127.0.0.1:7899", "https", "http://127.0.0.1:7898"},
		{"already a URL", "http://127.0.0.1:7897", "https", "http://127.0.0.1:7897"},
		{"with spaces", " http=127.0.0.1:7897 ", "http", "http://127.0.0.1:7897"},
		{"empty", "", "https", ""},
		{"nonsense", "=;=;", "https", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			proxy := parseProxyServer(test.server, test.scheme)
			got := ""
			if proxy != nil {
				got = proxy.String()
			}
			if got != test.want {
				t.Errorf("parseProxyServer(%q, %q) = %q, want %q", test.server, test.scheme, got, test.want)
			}
		})
	}
}

// 绕过列表用的是系统自己的：本网络内的一台机器、一个地址段、一个域名，或一个主机。
func TestTheSystemBypassList(t *testing.T) {
	cases := []struct {
		host    string
		bypass  []string
		bypassd bool
	}{
		{"chatgpt.com", []string{"localhost", "127.*", "*.local"}, false},
		{"127.0.0.1", []string{"127.*"}, true},
		{"printer.local", []string{"*.local"}, true},
		{"intranet", []string{"<local>"}, true},
		{"chatgpt.com", []string{"<local>"}, false},
		{"api.deepseek.com", []string{"*.deepseek.com"}, true},
		{"api.deepseek.com", []string{"deepseek.com"}, false},
		{"deepseek.com", []string{"deepseek.com"}, true},
		{"chatgpt.com", []string{""}, false},
		{"CHATGPT.COM", []string{"chatgpt.com"}, true},
		{"chatgpt.com", nil, false},
		// macOS 的 SystemConfiguration 把本地网络写成 CIDR 段，域名带前导点。
		{"192.168.1.20", []string{"192.168.0.0/16"}, true},
		{"10.2.3.4", []string{"10.0.0.0/8"}, true},
		{"172.20.0.1", []string{"172.16.0.0/12"}, true},
		{"8.8.8.8", []string{"192.168.0.0/16"}, false},
		{"printer.corp.example", []string{".corp.example"}, true},
		{"corp.example", []string{".corp.example"}, true},
		{"notcorp.example", []string{".corp.example"}, false},
	}
	for _, test := range cases {
		if got := bypassesProxy(test.host, test.bypass); got != test.bypassd {
			t.Errorf("bypassesProxy(%q, %v) = %v, want %v", test.host, test.bypass, got, test.bypassd)
		}
	}
}

// 在绕过列表上的主机直连出去，不在列表上的走设置指定的代理。
func TestABypassedHostDoesNotUseTheProxy(t *testing.T) {
	read := func() (proxySettings, error) {
		return proxySettings{
			Enabled: true,
			Server:  "127.0.0.1:7897",
			Bypass:  []string{"*.internal"},
		}, nil
	}
	bypass, err := proxyFor(nil, read, mustRequest(t, "http://wiki.internal/page"))
	if err != nil {
		t.Fatalf("proxyFor failed: %v", err)
	}
	if bypass != nil {
		t.Errorf("proxy = %v, want none for a host on the bypass list", bypass)
	}
	through, err := proxyFor(nil, read, mustRequest(t, "https://chatgpt.com/backend-api/wham/usage"))
	if err != nil {
		t.Fatalf("proxyFor failed: %v", err)
	}
	if through == nil {
		t.Error("no proxy for a host that is not on the bypass list")
	}
}

// 完整路径，代理是一个真实 HTTP 服务器：client 把请求交给系统代理，且代理
// 收到的是绝对地址而非路径。
func TestTheClientGoesThroughTheMachineProxy(t *testing.T) {
	if os.Getenv("HTTP_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("ALL_PROXY") != "" {
		t.Skip("this machine has a proxy in its environment, which correctly comes first")
	}
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus"}`))
	}))
	defer proxy.Close()

	previous := readSystemProxy
	readSystemProxy = func() (proxySettings, error) {
		return proxySettings{Enabled: true, Server: strings.TrimPrefix(proxy.URL, "http://")}, nil
	}
	t.Cleanup(func() { readSystemProxy = previous })

	client := New(Options{})
	result, err := client.GetJSON(context.Background(), "http://panel.example.com/usage", nil)
	if err != nil {
		t.Fatalf("GetJSON through the machine's proxy failed: %v", err)
	}
	if result.HTTPStatus != http.StatusOK {
		t.Errorf("status = %d, want 200", result.HTTPStatus)
	}
	if len(asked) != 1 || asked[0] != "http://panel.example.com/usage" {
		t.Errorf("the proxy was asked for %v, want the absolute address once", asked)
	}
}

func mustRequest(t *testing.T, endpoint string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("NewRequest(%q) failed: %v", endpoint, err)
	}
	return request
}
