package bilibili

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// mixinKeyEncTab is Bilibili's fixed permutation table for WBI signing. It is
// part of the wire protocol, not of any one account: the table reshuffles the
// img_key/sub_key pair the ticket endpoint returns into the 32-character key
// every request is signed with.
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28,
	14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54,
	21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

// wbiKeys are the two live tokens the ticket endpoint hands out. They rotate
// roughly daily, so they are cached by the service, not by this function.
type wbiKeys struct {
	imgKey string
	subKey string
}

// valid reports whether both halves were read. A ticket answer that could not
// be parsed leaves them empty and the next request fetches them again.
func (k wbiKeys) valid() bool { return k.imgKey != "" && k.subKey != "" }

func mixinKey(orig string) string {
	var b strings.Builder
	for _, n := range mixinKeyEncTab {
		if n >= 0 && n < len(orig) {
			b.WriteByte(orig[n])
		}
	}
	key := b.String()
	if len(key) > 32 {
		key = key[:32]
	}
	return key
}

// signWBI appends wts and w_rid to params, which is what makes a query
// acceptable to the risk-control layer. Without it the space/dynamic endpoint
// answers -352 or -412 however well-formed the rest of the request is.
//
// The parameters are sorted, filtered and percent-encoded exactly as Bilibili
// does before hashing; changing any of those steps makes the signature wrong in
// a way the service only reports as a generic rejection.
func signWBI(params map[string]string, keys wbiKeys, now time.Time) string {
	values := make(map[string]string, len(params)+1)
	for k, v := range params {
		values[k] = v
	}
	values["wts"] = strconv.FormatInt(now.Unix(), 10)

	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Strings(names)

	var query strings.Builder
	for i, name := range names {
		if i > 0 {
			query.WriteByte('&')
		}
		query.WriteString(escapeWBI(name))
		query.WriteByte('=')
		query.WriteString(escapeWBI(values[name]))
	}
	sum := md5.Sum([]byte(query.String() + mixinKey(keys.imgKey+keys.subKey)))
	query.WriteString("&w_rid=")
	query.WriteString(hex.EncodeToString(sum[:]))
	return query.String()
}

// escapeWBI is Bilibili's encoding of one signing component: the characters
// !'()* are dropped, then the rest is percent-encoded.
func escapeWBI(v string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '!', '\'', '(', ')', '*':
			return -1
		}
		return r
	}, v)
	return url.QueryEscape(clean)
}
