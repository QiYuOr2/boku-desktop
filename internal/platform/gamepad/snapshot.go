package gamepad

import "sync"

var (
	snapMu sync.Mutex
	snap   []Pad
)

func note(pads []Pad) {
	snapMu.Lock()
	snap = append([]Pad(nil), pads...)
	snapMu.Unlock()
}

// Snapshot 是上一次 Connected 的结果。在首次探测完成前为空，且不会自行
// 发起一次探测。
func Snapshot() []Pad {
	snapMu.Lock()
	defer snapMu.Unlock()
	return append([]Pad(nil), snap...)
}
