package httpc

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// 机器可能处在代理之后，但环境变量里并未体现。Windows 的代理设置存在注册表（系统网络
// 对话框写入处），net/http 并不读取它：http.ProxyFromEnvironment 只看 HTTPS_PROXY 与
// HTTP_PROXY。由托盘或安装器启动的窗口两者都没有，只查环境变量的程序在这类机器上啥也访问不到。
//
// 因此先查环境变量（开发者为此程序刻意设置），系统的答案作为兜底。

// proxySettings 是操作系统给出的代理设置：是否开启、代理是什么、哪些主机绕过。
type proxySettings struct {
	Enabled bool
	// Server 是系统写下的原始设置：要么一个地址适用于全部，要么按协议各一个，
	// 形如 "http=127.0.0.1:7897;https=…"。
	Server string
	// Bypass 是绕过代理的主机列表。
	Bypass []string
}

// readSystemProxy 读取操作系统自身的代理设置。做成变量是为了让测试能在包的其余部分
// 之前注入自己的设置；真实读取器按平台实现，不读取该设置的平台返回空。
var readSystemProxy = platformProxy

// ProxyFunc 是客户端决定请求去向的方式。
//
// 不做缓存：用户在 Boku 运行时关掉代理，下一个请求就不走代理，正是他刚要求的。
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(request *http.Request) (*url.URL, error) {
		return proxyFor(http.ProxyFromEnvironment, readSystemProxy, request)
	}
}

// proxyFor 是完整的决策，两半都由外部传入：测试可以同时放入环境变量与机器设置，
// 看谁优先，而不必改动运行测试的机器。
//
// 环境变量优先，因为它是有人为此程序刻意设置的，也因为这样能原样保留标准库自己的
// 规则（NO_PROXY、CGI 防护等）：这里不重新实现它们。
func proxyFor(envProxy func(*http.Request) (*url.URL, error), system func() (proxySettings, error), request *http.Request) (*url.URL, error) {
	if envProxy != nil {
		if proxy, err := envProxy(request); proxy != nil || err != nil {
			return proxy, err
		}
	}
	return systemProxy(request, system)
}

// systemProxy 返回请求对应的操作系统代理，没有可用代理时返回空。
//
// 读不出的设置不等于发不出的请求：这里返回无代理，让请求自行失败，原因归于网络而非注册表键。
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

// parseProxyServer 读取系统代理设置，返回某个 scheme 应使用的地址；设置表明该 scheme
// 不走代理时返回空。
//
// 设置要么是一个适用全部的地址——"127.0.0.1:7897"——要么按协议分别给出并在前面标协议，
// 如 "http=127.0.0.1:7897;https=127.0.0.1:7897"。列表未列出的协议直连，除非列表指定了
// SOCKS 代理，此时它会用于所有没有专属代理的协议。
//
// 裸地址即 HTTP 代理：这正是该设置的含义，也是向 https 地址开隧道所用的协议。已是 URL 的
// 条目则按原样使用。
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

// proxyURL 把单个设置值转成 transport 需要的地址。带 scheme 的值按原样使用；裸值按代理由
// 类型决定的 scheme 补全——默认 HTTP，除非系统将其标为 SOCKS。
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

// bypassesProxy 判断主机是否属于设置要绕过代理的范围。
//
// 列表是系统自己的格式：名字以 ";" 或 "," 分隔，每个可以是 "<local>"（不含点的主机名，
// 即局域网内的机器）、macOS 写本地网络所用的 CIDR 段（"192.168.0.0/16"）、前导点的域名
// （".corp.example"，代表自身及子域），或含 "*" 通配任何字符（如 "127.*"、"*.local"）的模式。
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
			// macOS 把本地网络写成 CIDR 段
			// （192.168.0.0/16、10.0.0.0/8、172.16.0.0/12），
			// 网关就是代理的网络不应再走代理。
			if _, network, err := net.ParseCIDR(entry); err == nil {
				if ip := net.ParseIP(host); ip != nil && network.Contains(ip) {
					return true
				}
			}
		case strings.HasPrefix(entry, "."):
			// 前导点表示该域名及其下所有子域。
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

// matchesPattern 用含 "*" 的模式匹配主机，"*" 代表任意长度（包括零）的字符：
// "127.*" 匹配 127.0.0.1，"*.local" 匹配 printer.local。
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

// splitBypass 把系统绕过列表拆成条目。名字间以 ";" 分隔，也接受 ","，因为有些工具这样写。
func splitBypass(list string) []string {
	return strings.FieldsFunc(list, func(r rune) bool { return r == ';' || r == ',' })
}
