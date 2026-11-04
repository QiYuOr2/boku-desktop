package httpc

import "net/http"

// DirectTransport is the standard transport with no proxy at all.
//
// ProxyFunc asks the environment first. A loopback address makes that answer
// "no proxy", which is the same answer as "the environment named nothing", so
// the machine's own proxy is used next. Its bypass list does not include
// 127.0.0.1. The local llama-server would then be sent to the proxy. This
// transport is only for that one port.
func DirectTransport() *http.Transport {
	standard, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	transport := standard.Clone()
	transport.Proxy = nil
	return transport
}

// NewDirect is a client that never uses a proxy. Callers that talk to another
// machine keep using New.
func NewDirect(opts Options) *Client {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Transport: DirectTransport()}
	}
	return New(opts)
}
