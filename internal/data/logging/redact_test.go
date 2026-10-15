package logging

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// testCredential is what a leaked key looks like in a test: shaped like a real
// one, and not one.
const testCredential = "sk-live-9f8e7d6c5b4a"

// A credential must not reach the file whatever shape it arrives in: a pair
// inside a message, a bare value under a name that means one, the same name
// attached to the logger, a member of a group, or a URL quoted by a
// provider's own error message.
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

// Redaction must not eat the record: an ordinary line comes out as it went in,
// because a log that hides the diagnostic is worse than no log.
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
