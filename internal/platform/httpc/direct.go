package httpc

import "net/http"

// DirectTransport 是完全不走代理的标准传输层。
//
// ProxyFunc 先问环境。环回地址会让它答「无代理」，与「环境未指定」相同，
// 于是接下来会用系统自身代理，而其绕过列表不含 127.0.0.1，本机 llama-server
// 就会被送去代理。本传输层仅用于那一个端口。
func DirectTransport() *http.Transport {
	standard, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	transport := standard.Clone()
	transport.Proxy = nil
	return transport
}

// NewDirect 返回从不使用代理的 client。访问其它机器的调用方仍用 New。
func NewDirect(opts Options) *Client {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Transport: DirectTransport()}
	}
	return New(opts)
}
