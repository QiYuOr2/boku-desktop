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

// mixinKeyEncTab 是 B 站 WBI 签名的固定置换表，属于线上协议而非某个账号：它把票据
// 接口返回的 img_key/sub_key 对重排成每个请求签名所用的 32 字符密钥。
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28,
	14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54,
	21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

// wbiKeys 是票据接口下发的两个实时 token。它们约每天轮换，所以由 service 缓存，
// 不在本函数缓存。
type wbiKeys struct {
	imgKey string
	subKey string
}

// valid 报告两半是否都已读到。无法解析的票据应答会让它们为空，下一个请求会重新抓取。
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

// signWBI 为 params 追加 wts 和 w_rid，这是让查询被风控层接受的关键。没有它，
// space/dynamic 接口无论请求其余部分多规范都会回 -352 或 -412。
//
// 参数完全按 B 站的方式排序、过滤并百分号编码后才哈希；改变其中任何一步都会让签名
// 出错，而服务只会把它报成笼统的拒绝。
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

// escapeWBI 是 B 站对单个签名分量的编码：先丢弃 !'()* 这些字符，再对其余部分做
// 百分号编码。
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
