//go:build darwin

package httpc

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"
)

// macOS keeps its proxy setting in SystemConfiguration, where the Network
// pane writes it. A GUI app is not started from a shell, so HTTPS_PROXY and
// HTTP_PROXY are usually empty even when the user has a system proxy on —
// which is the normal way Clash, Surge and the corporate proxies are set up.
//
// scutil prints that setting as a dictionary. Its keys are the same ones the
// Network pane writes: HTTPEnable/HTTPProxy/HTTPPort, HTTPS..., SOCKS..., and
// ExceptionsList. The environment is still asked first by proxy.go; this is
// only the fallback.
//
// PAC (ProxyAutoConfigEnable) is not evaluated: a configuration that only has
// a PAC URL is left to the environment, and a direct request is made. PAC is
// a JavaScript engine and belongs to a proxy library, not to this file.
func platformProxy() (proxySettings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// The absolute path is deliberate: a GUI process does not always have
	// /usr/sbin on PATH, and scutil is part of the system.
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return proxySettings{}, err
	}
	return parseSCUtilProxy(string(out))
}

// parseSCUtilProxy reads the dictionary scutil prints.
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
		// Array and dictionary openers are not values.
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
