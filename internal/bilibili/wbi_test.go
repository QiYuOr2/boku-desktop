package bilibili

import (
	"strings"
	"testing"
	"time"
)

func TestMixinKeyMatchesTheWireTable(t *testing.T) {
	// The table's first entries are 46,47,18,2…, so a 64-character key made of
	// the alphabet maps to the same permutation the protocol specifies.
	orig := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ+/"
	got := mixinKey(orig)
	if len(got) != 32 {
		t.Fatalf("mixin key is %d characters, want 32", len(got))
	}
	want := ""
	for _, n := range mixinKeyEncTab {
		want += string(orig[n])
	}
	if got != want[:32] {
		t.Fatalf("mixinKey = %q, want %q", got, want[:32])
	}
}

func TestSignWBIAddsTimestampAndSignature(t *testing.T) {
	keys := wbiKeys{imgKey: "7cd084941338484aae1ad9425b84077c", subKey: "4932caff0ff746eab6f01bf08b70ac45"}
	now := time.Unix(1700000000, 0)
	query := signWBI(map[string]string{"host_mid": "1"}, keys, now)
	if !strings.Contains(query, "wts=1700000000") {
		t.Errorf("query %q has no wts", query)
	}
	if !strings.HasPrefix(query, "host_mid=1&wts=1700000000&w_rid=") {
		t.Errorf("query = %q, want sorted parameters then w_rid", query)
	}
	if len(query)-strings.Index(query, "w_rid=")-len("w_rid=") != 32 {
		t.Errorf("signature in %q is not 32 hex characters", query)
	}
}

// The signature must depend on the parameters and the keys; a change to either
// has to change the digest, otherwise the test above would pass on a stub.
func TestSignWBIDependsOnParamsAndKeys(t *testing.T) {
	keys := wbiKeys{imgKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", subKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	now := time.Unix(1700000000, 0)
	a := signWBI(map[string]string{"host_mid": "1"}, keys, now)
	b := signWBI(map[string]string{"host_mid": "2"}, keys, now)
	c := signWBI(map[string]string{"host_mid": "1"}, wbiKeys{imgKey: "cccccccccccccccccccccccccccccccc", subKey: "dddddddddddddddddddddddddddddddd"}, now)
	if a == b {
		t.Error("the signature ignores host_mid")
	}
	if a == c {
		t.Error("the signature ignores the keys")
	}
}

func TestEscapeWBIDropsTheProtocolCharacters(t *testing.T) {
	if got := escapeWBI("a!b'c(d)e*f"); got != "abcdef" {
		t.Errorf("escapeWBI strips !'()* → %q, want %q", got, "abcdef")
	}
	if got := escapeWBI("a b"); got != "a+b" {
		t.Errorf("spaces are encoded as %q", got)
	}
}

func TestWBIKeysValidRequiresBothHalves(t *testing.T) {
	if (wbiKeys{}).valid() {
		t.Error("empty keys reported valid")
	}
	if !(wbiKeys{imgKey: "a", subKey: "b"}).valid() {
		t.Error("complete keys reported invalid")
	}
	if (wbiKeys{imgKey: "a"}).valid() {
		t.Error("a half key reported valid")
	}
}
