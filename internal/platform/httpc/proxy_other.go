//go:build !windows && !darwin

package httpc

// platformProxy 返回空：本构建不读取自身的代理设置。这些平台上的程序预期
// 查看环境变量，ProxyFunc 在任何平台都先问环境。
//
// Linux 有自身的设置 gsettings，尚未读取。macOS 在 proxy_darwin.go 里读 scutil。
func platformProxy() (proxySettings, error) { return proxySettings{}, nil }
