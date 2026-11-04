package live

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"boku/internal/platform/httpc"
)

func TestDownloadNoticeNamesTheCause(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "timeout", err: &fileError{name: "a.gguf", err: errors.New("dial tcp 1.2.3.4:443: i/o timeout")}, want: "a.gguf：下载超时"},
		{name: "reset", err: errors.New("read: connection reset by peer"), want: "连接中断"},
		{name: "eof", err: io.ErrUnexpectedEOF, want: "连接中断"},
		{name: "status", err: &fileError{name: "a.gguf", err: &httpc.StatusError{Code: 503}}, want: "a.gguf：服务器返回 503"},
		{name: "checksum", err: &fileError{name: "a.gguf", err: errChecksum}, want: "a.gguf：文件校验失败"},
		{name: "proxy", err: errors.New(`Get "https://example.test/a": proxyconnect tcp: dial tcp 127.0.0.1:7890: i/o timeout`), want: "代理连接失败"},
		{name: "permission", err: fmt.Errorf("open %s: %w", "/Users/a/model.part", syscall.EACCES), want: "没有写入权限"},
		{name: "zip", err: errors.New("zip: not a valid zip file"), want: "zip: not a valid zip file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := downloadNotice(tc.err)
			if got != tc.want {
				t.Fatalf("got %q", got)
			}
			if strings.Contains(got, "example.test") || strings.Contains(got, "/Users/") {
				t.Fatal(got)
			}
		})
	}
}
