package gamepad

import "sync"

// The playtime recorder reads this instead of enumerating HID again.
// Connected already walks the devices for the title mark.
var (
	snapMu sync.Mutex
	snap   []Pad
)

func note(pads []Pad) {
	snapMu.Lock()
	snap = append([]Pad(nil), pads...)
	snapMu.Unlock()
}

// Snapshot is the last Connected result. It is empty until that first look
// finishes, and it does not start a look of its own.
func Snapshot() []Pad {
	snapMu.Lock()
	defer snapMu.Unlock()
	return append([]Pad(nil), snap...)
}
