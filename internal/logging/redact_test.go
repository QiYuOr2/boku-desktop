package logging

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// testCredential 是测试中泄露密钥的样子：形状像真的，但不是真的。
const testCredential = "sk-live-9f8e7d6c5b4a"

// 凭据无论以何种形态出现都不得进入文件：消息里的键值对、以凭据为名的裸值、
// 挂在日志器上的同名属性、group 成员，或被厂商错误消息引用的 URL。
func TestCredentialsDoNotReachTheFile(t *testing.T) {
	dir := t.TempDir()
	clock := &testClock{at: localDay(2026, time.October, 15, 9)}
	l := newTestLog(t, Options{Dir: dir, Now: clock.now})
	logger := l.Logger()

	logger.Error("the request failed with key=" + testCredential)
	logger.Warn("a bare value under the name of a credential", "apiKey", testCredential)
	logger.With("token", testCredential).Warn("attached to the logger")
	logger.Warn("inside a group", slog.Group("auth", slog.String("secret", testCredential)))
	logger.Warn("quoted by the provider",
		"error", fmt.Errorf("POST https://api.example.com/v1/chat?key=%s: refused", testCredential))

	text := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	mustNotContain(t, text, testCredential)
	mustNotContain(t, text, "9f8e7d6c5b4a")
	mustContain(t, text, "redacted")
}

// 脱敏不能吃掉整条记录：普通一行原样输出，隐藏诊断信息的日志比没有日志更糟。
func TestRedactionKeepsAnOrdinaryRecord(t *testing.T) {
	dir := t.TempDir()
	clock := &testClock{at: localDay(2026, time.October, 15, 9)}
	l := newTestLog(t, Options{Dir: dir, Now: clock.now})
	l.Logger().Warn("the panel could not be refreshed", "panel", "deepseek", "status", "error")

	text := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	mustContain(t, text, `msg="the panel could not be refreshed"`)
	mustContain(t, text, "panel=deepseek")
	mustContain(t, text, "status=error")
	mustNotContain(t, text, "redacted")
}
