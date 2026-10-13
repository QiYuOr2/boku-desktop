package domain

import (
	"strings"
	"testing"
	"time"
)

// Ids are UUIDv7: sortable by the time they were minted. A history sorts on
// them when two copies share a second, which is ordinary, so their order
// within one millisecond has to be the order they were made in.

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

// The time is the first 48 bits: a later time always sorts later, whatever
// the rest of the id holds.
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

// Several ids in one millisecond are ordinary for the clipboard, and their
// order is what a history of copies is sorted by.
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

// Two ids of the same millisecond are not the same id twice: a store keyed
// on id would lose one of them.
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

// A clock that goes backwards must not hand out ids that collide with what
// is already out there.
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

// The first 48 bits are the time the id was minted at, which is what makes
// a list of ids sortable by when they happened.
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
		// The first six bytes are the millisecond of the epoch, with the
		// version nibble sitting in the middle of the last pair.
		if got := id[:8] + id[9:13]; got != millisHex(at) {
			t.Errorf("NewIDAt(%s) = %q, whose time bits are %s, want %s", at, id, got, millisHex(at))
		}
	}
}

// The id NewID mints is the time of the call: it sorts between an id of an
// hour ago and one of an hour's time.
func TestNewIDUsesTheCurrentTime(t *testing.T) {
	forgetMintedIDs()
	now := time.Now().UTC()
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID failed: %v", err)
	}
	// Both comparisons are made against the same instant, so that a clock
	// that moves while the test runs cannot make them disagree.
	if got := id[:8] + id[9:13]; got < millisHex(now.Add(-time.Minute)) || got > millisHex(now.Add(time.Minute)) {
		t.Errorf("NewID = %q, whose time is %s, and the call happened at %s", id, got, millisHex(now))
	}
}
