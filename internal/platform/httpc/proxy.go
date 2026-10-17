package httpc

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// A machine can be behind a proxy without saying so in the environment. On
// Windows the setting lives in the registry, where the system's own network
// dialog writes it, and nothing in net/http reads it: http.ProxyFromEnvironment
// looks at HTTPS_PROXY and HTTP_PROXY alone. A window started from the tray or
// by an installer has neither, so a program that only asks the environment
// reaches nothing on such a machine — which is what happened to the Codex
// quota panel on the first machine it ran on.
//
// So the environment is asked first, because it is the setting a developer set
// on purpose for this program, and the system's own answer is the fallback.

// proxySettings is what an operating system says about its proxy: whether it is
// on, what it is, and which hosts go around it.
type proxySettings struct {
	Enabled bool
	// Server is the setting as the system writes it: either one address for
	// everything, or one per protocol, as "http=127.0.0.1:7897;https=…".
	Server string
	// Bypass is the list of hosts that go around the proxy.
	Bypass []string
}

// readSystemProxy reads the operating system's own proxy setting. It is a
// variable rather than a call so that a test can put a setting of its own in
// front of the rest of the package; the real reader is per platform, and a
// platform whose setting this does not read answers with nothing.
var readSystemProxy = platformProxy

// ProxyFunc is how the client decides where a request goes.
//
// Nothing is cached: a user who turns their proxy off while Boku is running
// gets the next request without one, which is what they just asked for.
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(request *http.Request) (*url.URL, error) {
		return proxyFor(http.ProxyFromEnvironment, readSystemProxy, request)
	}
}

// proxyFor is the whole of the decision, with both halves passed in: a test can
// then put an environment and a machine in front of it and see which one wins
// without setting one on the machine running the test.
//
// The environment comes first, because it is the setting somebody made on
// purpose for this program — and because that keeps the standard library's own
// rules, NO_PROXY and the CGI guard among them, exactly as they are: they are
// not reimplemented here.
func proxyFor(envProxy func(*http.Request) (*url.URL, error), system func() (proxySettings, error), request *http.Request) (*url.URL, error) {
	if envProxy != nil {
		if proxy, err := envProxy(request); proxy != nil || err != nil {
			return proxy, err
		}
	}
	return systemProxy(request, system)
}

// systemProxy answers with the operating system's proxy for a request, or with
// nothing when there is none to use.
//
// A setting that cannot be read is not a request that cannot be made: this
// answers with no proxy and lets the request fail on its own, where the reason
// is about the network rather than about a registry key.
func systemProxy(request *http.Request, read func() (proxySettings, error)) (*url.URL, error) {
	if request == nil || request.URL == nil || read == nil {
		return nil, nil
	}
	settings, err := read()
	if err != nil || !settings.Enabled {
		return nil, nil
	}
	if bypassesProxy(request.URL.Hostname(), settings.Bypass) {
		return nil, nil
	}
	return parseProxyServer(settings.Server, request.URL.Scheme), nil
}

// parseProxyServer reads a system proxy setting and answers with the address to
// use for one scheme, or with nothing when the setting says this scheme does
// not go through one.
//
// The setting is either one address for everything — "127.0.0.1:7897" — or one
// per protocol with the protocol in front of it, as in
// "http=127.0.0.1:7897;https=127.0.0.1:7897". A protocol the list leaves out is
// a protocol the system sends straight out, unless the list names a SOCKS
// proxy, which is the one it uses for everything it has no proxy of its own
// for.
//
// A bare address is an HTTP proxy: that is what the setting means, and it is
// what a tunnel to an https address is opened on. An entry that is already a
// URL is taken as it is written.
func parseProxyServer(server, scheme string) *url.URL {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil
	}
	if !strings.Contains(server, "=") {
		return proxyURL(server, "http")
	}
	address, kind := "", ""
	socks := ""
	for _, part := range strings.Split(server, ";") {
		name, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch {
		case strings.EqualFold(name, scheme):
			address, kind = value, strings.ToLower(name)
		case strings.EqualFold(name, "socks"):
			socks = value
		}
	}
	if address == "" {
		address, kind = socks, "socks"
	}
	if address == "" {
		return nil
	}
	return proxyURL(address, kind)
}

// proxyURL turns one setting value into the address a transport wants. A value
// with a scheme is taken as it is written; a bare one takes the scheme of the
// kind of proxy it is, which decides the protocol spoken to it — an HTTP proxy
// unless the system named it as a SOCKS one.
func proxyURL(address, kind string) *url.URL {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	if !strings.Contains(address, "://") {
		if kind == "socks" || kind == "socks5" {
			kind = "socks5"
		} else {
			kind = "http"
		}
		address = kind + "://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return nil
	}
	return parsed
}

// bypassesProxy reports whether a host is one the setting sends around the
// proxy.
//
// The list is the system's own: names separated by ";" or ",", each either
// "<local>" — a name with no dot in it, which is a machine on this network
// rather than one out on the internet — a CIDR block as macOS writes local
// networks ("192.168.0.0/16"), a domain with a leading dot for itself and its
// subdomains (".corp.example"), or a pattern in which "*" stands for any run
// of characters, as in "127.*" or "*.local".
func bypassesProxy(host string, bypass []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, entry := range bypass {
		entry = strings.ToLower(strings.TrimSpace(entry))
		switch {
		case entry == "":
		case entry == "<local>":
			if !strings.Contains(host, ".") {
				return true
			}
		case strings.Contains(entry, "/"):
			// macOS writes local networks as CIDR blocks
			// (192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12), and a
			// network whose gateway is the proxy should not be sent
			// through it.
			if _, network, err := net.ParseCIDR(entry); err == nil {
				if ip := net.ParseIP(host); ip != nil && network.Contains(ip) {
					return true
				}
			}
		case strings.HasPrefix(entry, "."):
			// A leading dot means the domain and everything under it.
			if host == strings.TrimPrefix(entry, ".") || strings.HasSuffix(host, entry) {
				return true
			}
		case strings.Contains(entry, "*"):
			if matchesPattern(host, entry) {
				return true
			}
		default:
			if host == entry {
				return true
			}
		}
	}
	return false
}

// matchesPattern matches a host against a pattern in which "*" stands for any
// run of characters, including none: "127.*" matches 127.0.0.1, and "*.local"
// matches printer.local.
func matchesPattern(host, pattern string) bool {
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(host, parts[0]) {
		return false
	}
	rest := host[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		index := strings.Index(rest, part)
		if index < 0 {
			return false
		}
		rest = rest[index+len(part):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}

// splitBypass reads a system bypass list into its entries. The list is written
// with ";" between names, and "," is accepted because some tools write it.
func splitBypass(list string) []string {
	return strings.FieldsFunc(list, func(r rune) bool { return r == ';' || r == ',' })
}
