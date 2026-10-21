package ui

import (
	"testing"
	"time"
)

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if got := RelativeTime(now.Add(-5*time.Minute), now); got != "5 分钟前" {
		t.Fatalf("RelativeTime = %q", got)
	}
}
