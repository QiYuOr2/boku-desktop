// Package gamepad lists game controllers the operating system is exposing
// and, for a DualSense, the battery in its input report.
package gamepad

// Kind is which mark the playtime title draws for a connected controller.
type Kind int

const (
	// KindXbox is every controller that is neither a DualSense nor a Switch Pro.
	KindXbox Kind = iota
	KindDualSense
	KindSwitchPro
)

const (
	vidSony     = 0x054C
	vidNintendo = 0x057E

	pidDualShock4    = 0x05C4
	pidDualShock4v2  = 0x09CC
	pidDualSense     = 0x0CE6
	pidDualSenseEdge = 0x0DF2
	pidSwitchPro     = 0x2009
)

// Battery is a controller charge reading. Known is false when this kind's
// protocol was not read; a missing reading is not zero percent.
type Battery struct {
	Known    bool
	Percent  int
	Charging bool
}

// Pad is one connected game controller.
type Pad struct {
	Kind    Kind
	Battery Battery
}

// kindOf picks the mark. Sony's pads share the DualSense drawing; only the
// DualSense report layout has a battery reading.
func kindOf(vid, pid uint16) Kind {
	switch {
	case vid == vidSony && (pid == pidDualShock4 || pid == pidDualShock4v2 || pid == pidDualSense || pid == pidDualSenseEdge):
		return KindDualSense
	case vid == vidNintendo && pid == pidSwitchPro:
		return KindSwitchPro
	default:
		return KindXbox
	}
}

// Name is the token stored on a play session. It names the kind, not one
// physical pad: two Xbox pads are one token.
func (k Kind) Name() string {
	switch k {
	case KindDualSense:
		return "dualsense"
	case KindSwitchPro:
		return "switch-pro"
	default:
		return "xbox"
	}
}

func kindOrder(kind Kind) int {
	switch kind {
	case KindDualSense:
		return 0
	case KindSwitchPro:
		return 1
	default:
		return 2
	}
}

func readsDualSenseBattery(vid, pid uint16) bool {
	return vid == vidSony && (pid == pidDualSense || pid == pidDualSenseEdge)
}

// batteryFromReport reads a DualSense USB (0x01) or Bluetooth (0x31) input
// report. The charge nibble is 0–10; a status of 2 means the pack is full.
func batteryFromReport(report []byte) (Battery, bool) {
	if len(report) == 0 {
		return Battery{}, false
	}
	var raw byte
	switch report[0] {
	case 0x01:
		if len(report) < 54 {
			return Battery{}, false
		}
		raw = report[53]
	case 0x31:
		if len(report) < 55 {
			return Battery{}, false
		}
		raw = report[54]
	default:
		return Battery{}, false
	}
	level := int(raw & 0x0F)
	status := raw >> 4
	if level > 10 || (status != 0 && status != 1 && status != 2) {
		return Battery{}, false
	}
	percent := level * 10
	if status == 2 {
		percent = 100
	}
	return Battery{Known: true, Percent: percent, Charging: status == 1}, true
}

func padsEqual(a, b []Pad) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
