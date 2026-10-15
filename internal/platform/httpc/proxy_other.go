//go:build !windows

package httpc

// platformProxy answers with nothing: this build reads no proxy setting of its
// own. The environment is what a program on these platforms is expected to look
// at, and ProxyFunc asks it first on every platform.
//
// macOS and Linux have settings of their own that could be read here — scutil,
// gsettings — and neither is read yet: nobody has run this application behind
// one.
func platformProxy() (proxySettings, error) { return proxySettings{}, nil }
