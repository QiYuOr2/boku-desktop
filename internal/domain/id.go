package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 本进程最近一次生成的 id、其生成毫秒、以及该毫秒首个 id 使用的随机字节。
//
// UUIDv7 前 48 位是时间，其余是随机位，而随机位无序。同一毫秒内出现两个 id
// 很常见（剪贴板按定时器读取，历史记录若把较早的记录排在前面就是错的），
// 因此每毫秒只随机抽取一次尾部字节，同毫秒后续 id 都在该抽取值上加计数器：
// 同一毫秒的 id 按生成顺序排列，比较两个 id 即比较其生成时间。
var idCounter struct {
	sync.Mutex
	millis uint64
	seq    uint64
	base   [10]byte
}

// NewID 返回 UUIDv7：前 48 位是当前时间的标识符，可按时序排序，两台设备
// 无需通信即可各自生成。它是本应用唯一生成的标识符，实体的 id 生成后不变。
func NewID() (string, error) { return NewIDAt(time.Now()) }

// NewIDAt 是用调用方时钟的 NewID，测试可据此固定时间位。
//
// 时钟不晚于上一个 id 时，不回拨时间位：新 id 取得的是上一个 id 之后的毫秒，
// 而不是一个新的随机抽取。若 id 排到已发出的 id 之前，会破坏所有读取这些
// id 的顺序。同一毫秒内，计数器加到该毫秒抽取的字节上，所以同毫秒的两个 id
// 按生成顺序排序——这正是同一秒内复制记录的历史所用的排序依据。
func NewIDAt(at time.Time) (string, error) {
	millis := uint64(at.UTC().UnixMilli())

	idCounter.Lock()
	var seq uint64
	switch {
	case millis == idCounter.millis:
		// 同一毫秒的另一个 id：在最后一个上递增。
		idCounter.seq++
		seq = idCounter.seq
	case millis > idCounter.millis:
		idCounter.millis, idCounter.seq = millis, 0
		if _, err := rand.Read(idCounter.base[:]); err != nil {
			idCounter.Unlock()
			return "", fmt.Errorf("generate an id: %w", err)
		}
	default:
		// 时钟落后于上一个 id：继续排在它之前，并改用新的毫秒，而不复用已
		// 用过的毫秒。
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
	// 前六个字节是自 Unix 纪元起的毫秒数，大端序。
	for i := 0; i < 6; i++ {
		id[5-i] = byte(stamp >> (8 * i))
	}
	copy(id[6:], tail[:])
	addToTail(id[6:], seq)
	id[6] = (id[6] & 0x0f) | 0x70 // 版本 7
	id[8] = (id[8] & 0x3f) | 0x80 // RFC 4122 变体
	return formatUUID(id), nil
}

// addToTail 把 n 当作一个大端数加到十个字节上，向前进位。最前面溢出的进位
// 被丢弃：它会改变时间位，而同一毫秒内计数远达不到末尾。
func addToTail(tail []byte, n uint64) {
	for i := len(tail) - 1; i >= 0 && n > 0; i-- {
		sum := uint64(tail[i]) + (n & 0xff)
		tail[i] = byte(sum)
		n = (n >> 8) + (sum >> 8)
	}
}

// formatUUID 把这 16 字节写成规范的 8-4-4-4-12 形式。
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

// millisHex 是某个时间对应 id 前 48 位的读法，不含版本半字节处的短横。
// 测试用它比对 id 的时间。
func millisHex(at time.Time) string {
	millis := uint64(at.UTC().UnixMilli())
	var six [6]byte
	for i := 0; i < 6; i++ {
		six[5-i] = byte(millis >> (8 * i))
	}
	text := hex.EncodeToString(six[:])
	return text[:8] + text[8:]
}

// forgetMintedIDs 清空计数器记住的状态，好让测试为任意时间生成 id。
// 生产代码永不调用：遗忘会让时钟把同一毫秒发出去两次。
func forgetMintedIDs() {
	idCounter.Lock()
	idCounter.millis, idCounter.seq, idCounter.base = 0, 0, [10]byte{}
	idCounter.Unlock()
}
