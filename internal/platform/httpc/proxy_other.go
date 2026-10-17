//go:build !windows && !darwin

package httpc

// platformProxy answers with nothing: this build reads no proxy setting of its
// own. The environment is what a program on these platforms is expected to look
// at, and ProxyFunc asks it first on every platform.
//
// Linux has a setting of its own — gsettings — that could be read here, and it
// is not read yet. macOS reads scutil in proxy_darwin.go.
func platformProxy() (proxySettings, error) { return proxySettings{}, nil }
