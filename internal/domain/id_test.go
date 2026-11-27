package domain

import (
	"strings"
	"testing"
	"time"
)

// Id 是 UUIDv7：按生成时间可排序。两次复制常在同一秒内，历史有时据此排序，
// 因此同一毫秒内的顺序必须是它们生成的顺序。

func TestNewIDLooksLikeAUUID(t *testing.T) {
	at := time.Date(2026, 2, 3, 9, 0, 0, 123000000, time.UTC)
	id, err := NewIDAt(at)
	if err != nil {
		t.Fatalf("NewIDAt failed: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("id = %q, want the 8-4-4-4-12 form", id)
	}
	want := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != want[i] {
			t.Fatalf("id = %q, want the 8-4-4-4-12 form", id)
		}
		for _, r := range part {
			if !strings.ContainsRune("0123456789abcdef", r) {
				t.Fatalf("id = %q is not hexadecimal", id)
			}
		}
	}
	if parts[2][0] != '7' {
		t.Errorf("id %q does not carry version 7", id)
	}
	switch parts[3][0] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("id %q does not carry the RFC 4122 variant", id)
	}
}

// 时间在前 48 位：更晚的时间总是排在后面，无论 id 其余部分是什么。
func TestNewIDsSortByTime(t *testing.T) {
	forgetMintedIDs()
	previous := ""
	for _, at := range []time.Time{
		time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 3, 9, 0, 0, 1_000_000, time.UTC),
		time.Date(2026, 2, 3, 9, 0, 1, 0, time.UTC),
		time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		id, err := NewIDAt(at)
		if err != nil {
			t.Fatalf("NewIDAt failed: %v", err)
		}
		if previous != "" && id <= previous {
			t.Errorf("an id minted at %s (%q) does not sort after %q", at, id, previous)
		}
		previous = id
	}
}

// 同一毫秒内出现多个 id 对剪贴板很常见，它们的顺序正是复制历史排序的依据。
func TestNewIDsOfOneMillisecondCountUp(t *testing.T) {
	forgetMintedIDs()
	at := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	previous := ""
	for i := 0; i < 50; i++ {
		id, err := NewIDAt(at)
		if err != nil {
			t.Fatalf("NewIDAt failed: %v", err)
		}
		if id == previous {
			t.Fatalf("NewIDAt handed out the same id twice: %q", id)
		}
		if previous != "" && id <= previous {
			t.Fatalf("id %d (%q) does not sort after %q", i, id, previous)
		}
		previous = id
	}
}

// 同一毫秒的两个 id 不能是同一个 id：以 id 为键的存储会丢掉其中一个。
func TestNewIDsAreUnique(t *testing.T) {
	forgetMintedIDs()
	at := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		id, err := NewIDAt(at)
		if err != nil {
			t.Fatalf("NewIDAt failed: %v", err)
		}
		if seen[id] {
			t.Fatalf("id %q was minted twice", id)
		}
		seen[id] = true
	}
}

// 时钟倒退时不能发出已存在于外部的重复 id。
func TestNewIDSurvivesAClockGoingBackwards(t *testing.T) {
	forgetMintedIDs()
	later := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	first, err := NewIDAt(later)
	if err != nil {
		t.Fatalf("NewIDAt failed: %v", err)
	}
	second, err := NewIDAt(later.Add(-time.Hour))
	if err != nil {
		t.Fatalf("NewIDAt failed: %v", err)
	}
	if first == second {
		t.Fatal("two ids are the same after the clock went backwards")
	}
	if second <= first {
		t.Errorf("id %q does not sort after %q", second, first)
	}
}

// 前 48 位是 id 生成的时间，这正是 id 列表能按发生时间排序的原因。
func TestNewIDCarriesTheMintTime(t *testing.T) {
	cases := []time.Time{
		time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 3, 9, 0, 0, 123000000, time.UTC),
		time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, at := range cases {
		forgetMintedIDs()
		id, err := NewIDAt(at)
		if err != nil {
			t.Fatalf("NewIDAt failed: %v", err)
		}
		// 前六个字节是纪元毫秒数，版本半字节位于最后一对的中间。
		if got := id[:8] + id[9:13]; got != millisHex(at) {
			t.Errorf("NewIDAt(%s) = %q, whose time bits are %s, want %s", at, id, got, millisHex(at))
		}
	}
}

// NewID 生成的 id 是调用时刻：它排在一小时前与一小时后生成的 id 之间。
func TestNewIDUsesTheCurrentTime(t *testing.T) {
	forgetMintedIDs()
	now := time.Now().UTC()
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID failed: %v", err)
	}
	// 两次比较用同一时刻，使测试运行期间时钟移动也不会令它们不一致。
	if got := id[:8] + id[9:13]; got < millisHex(now.Add(-time.Minute)) || got > millisHex(now.Add(time.Minute)) {
		t.Errorf("NewID = %q, whose time is %s, and the call happened at %s", id, got, millisHex(now))
	}
}
