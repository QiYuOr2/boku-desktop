package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// The last id this process minted, the millisecond it was minted in, and the
// random bytes that millisecond's first id ended with.
//
// UUIDv7 is the time in its first 48 bits plus random bits, and the random
// bits are not ordered. Two copies within one millisecond are ordinary — the
// clipboard is read on a timer, and a history that lists an older copy first
// is wrong — so the rest of the id is drawn once per millisecond and every
// later id of that millisecond is that draw plus the counter: the ids of one
// millisecond are in the order they were minted in, and comparing two ids
// compares the times they were made at.
var idCounter struct {
	sync.Mutex
	millis uint64
	seq    uint64
	base   [10]byte
}

// NewID returns a UUIDv7: an identifier whose first 48 bits are the current
// time, so identifiers sort by creation and two devices can mint them
// without talking to each other. It is the only identifier the application
// mints, and an entity's id never changes after this.
func NewID() (string, error) { return NewIDAt(time.Now()) }

// NewIDAt is NewID with the caller's clock, so a test can pin the time bits.
//
// A clock that is no further along than the last id minted does not move the
// time bits backwards: the new id is given the millisecond after the last
// one instead. An id that sorted before one already handed out would break
// every order that reads these ids.
//
// Within one millisecond the counter is added to the bytes that millisecond
// was drawn with, so two ids of the same millisecond sort in the order they
// were minted in — which is what a history of copies timestamped to the same
// second is ordered by.
func NewIDAt(at time.Time) (string, error) {
	millis := uint64(at.UTC().UnixMilli())

	idCounter.Lock()
	var seq uint64
	switch {
	case millis == idCounter.millis:
		// Another id in the same millisecond: count up from the last.
		idCounter.seq++
		seq = idCounter.seq
	case millis > idCounter.millis:
		idCounter.millis, idCounter.seq = millis, 0
		if _, err := rand.Read(idCounter.base[:]); err != nil {
			idCounter.Unlock()
			return "", fmt.Errorf("generate an id: %w", err)
		}
	default:
		// The clock is behind the last id: stay in front of it, with a
		// fresh draw rather than a millisecond already used.
		idCounter.millis++
		idCounter.seq = 0
		if _, err := rand.Read(idCounter.base[:]); err != nil {
			idCounter.Unlock()
			return "", fmt.Errorf("generate an id: %w", err)
		}
	}
	stamp := idCounter.millis
	tail := idCounter.base
	idCounter.Unlock()

	var id [16]byte
	// The first six bytes are the number of milliseconds since the Unix
	// epoch, big endian.
	for i := 0; i < 6; i++ {
		id[5-i] = byte(stamp >> (8 * i))
	}
	copy(id[6:], tail[:])
	addToTail(id[6:], seq)
	id[6] = (id[6] & 0x0f) | 0x70 // version 7
	id[8] = (id[8] & 0x3f) | 0x80 // RFC 4122 variant
	return formatUUID(id), nil
}

// addToTail adds n to the ten bytes as one big-endian number, carrying
// towards the front. A carry out of the front is dropped: it would change
// the time bits, and a counter of ids in one millisecond is nowhere near its
// end.
func addToTail(tail []byte, n uint64) {
	for i := len(tail) - 1; i >= 0 && n > 0; i-- {
		sum := uint64(tail[i]) + (n & 0xff)
		tail[i] = byte(sum)
		n = (n >> 8) + (sum >> 8)
	}
}

// formatUUID writes the 16 bytes in the canonical 8-4-4-4-12 form.
func formatUUID(id [16]byte) string {
	var b strings.Builder
	b.Grow(36)
	b.WriteString(hex.EncodeToString(id[0:4]))
	b.WriteByte('-')
	b.WriteString(hex.EncodeToString(id[4:6]))
	b.WriteByte('-')
	b.WriteString(hex.EncodeToString(id[6:8]))
	b.WriteByte('-')
	b.WriteString(hex.EncodeToString(id[8:10]))
	b.WriteByte('-')
	b.WriteString(hex.EncodeToString(id[10:16]))
	return b.String()
}

// millisHex is how the first 48 bits of an id read for a time, without the
// version nibble's dash. It is what a test compares an id's time against.
func millisHex(at time.Time) string {
	millis := uint64(at.UTC().UnixMilli())
	var six [6]byte
	for i := 0; i < 6; i++ {
		six[5-i] = byte(millis >> (8 * i))
	}
	text := hex.EncodeToString(six[:])
	return text[:8] + text[8:]
}

// forgetMintedIDs drops what the counter remembers, so that a test can mint
// ids for times it chooses. Production never calls it: forgetting would let
// the clock hand out a millisecond twice.
func forgetMintedIDs() {
	idCounter.Lock()
	idCounter.millis, idCounter.seq, idCounter.base = 0, 0, [10]byte{}
	idCounter.Unlock()
}
