//go:build !darwin && !windows

package loopback

import "context"

// 本构建平台没有音频采集（Linux 不在支持范围内）。
func Start(ctx context.Context, source string) (*Stream, error) {
	return nil, ErrUnsupported
}
