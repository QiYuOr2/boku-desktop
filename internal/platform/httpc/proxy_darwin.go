//go:build darwin

package httpc

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"
)

// macOS 把代理设置存在 SystemConfiguration 里（网络面板写入的位置）。GUI 应用
// 不是从 shell 启动的，所以即使系统代理开着（Clash、Surge 及企业代理的常见
// 配置），HTTPS_PROXY/HTTP_PROXY 通常也是空的。
//
// scutil 把该设置打印成 dictionary，键与网络面板写入的相同：HTTPEnable/
// HTTPProxy/HTTPPort、HTTPS…、SOCKS… 及 ExceptionsList。proxy.go 仍会先问
// 环境，这里只是兜底。
//
// 不解析 PAC（ProxyAutoConfigEnable）：仅有 PAC URL 的配置交给环境处理，走
// 直连。PAC 是 JavaScript 引擎，属于代理库，不属于本文件。
func platformProxy() (proxySettings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 刻意用绝对路径：GUI 进程的 PATH 未必含 /usr/sbin，而 scutil 是系统组件。
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return proxySettings{}, err
	}
	return parseSCUtilProxy(string(out))
}

// parseSCUtilProxy 解析 scutil 打印的 dictionary。
func parseSCUtilProxy(output string) (proxySettings, error) {
	values := make(map[string]string)
	var bypass []string
	inExceptions := false
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if inExceptions {
			if line == "}" {
				inExceptions = false
				continue
			}
			if _, value, ok := strings.Cut(line, ":"); ok {
				if value = strings.TrimSpace(value); value != "" {
					bypass = append(bypass, value)
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if key == "ExceptionsList" {
			inExceptions = true
			continue
		}
		// 数组和字典的开括号不是值。
		if strings.HasPrefix(value, "<") {
			continue
		}
		values[key] = value
	}

	settings := proxySettings{Bypass: bypass}
	add := func(enableKey, hostKey, portKey, kind string) {
		if values[enableKey] != "1" {
			return
		}
		host := strings.TrimSpace(values[hostKey])
		port := strings.TrimSpace(values[portKey])
		if host == "" || port == "" {
			return
		}
		settings.Enabled = true
		if settings.Server != "" {
			settings.Server += ";"
		}
		settings.Server += kind + "=" + net.JoinHostPort(host, port)
	}
	add("HTTPEnable", "HTTPProxy", "HTTPPort", "http")
	add("HTTPSEnable", "HTTPSProxy", "HTTPSPort", "https")
	add("SOCKSEnable", "SOCKSProxy", "SOCKSPort", "socks")

	return settings, nil
}
