// Package gamepad 列出操作系统暴露的游戏控制器，并为无线 DualSense、
// Switch Pro 或 Xbox 协议手柄读取电量。
package gamepad

import "strings"

// Kind 是游玩时长标题为已连接控制器绘制的标记。
type Kind int

const (
	// KindXbox 表示既非 DualSense 也非 Switch Pro 的所有控制器。
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

// Battery 是一次控制器电量读数。Known 为 false 表示该类型协议未被读取；
// 缺失读数不等于零电量。
type Battery struct {
	Known    bool
	Percent  int
	Charging bool
}

type Pad struct {
	Kind    Kind
	Battery Battery
}

// kindOf 选择标记。Sony 的手柄共用 DualSense 图形；只有 DualSense 与
// Switch Pro 的报告布局带电量读数。
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

// Name 是存储在游玩会话上的 token。它按类型命名而非某个物理手柄：
// 两个 Xbox 手柄是同一个 token。
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

func readsSwitchBattery(vid, pid uint16) bool {
	return vid == vidNintendo && pid == pidSwitchPro
}

// wiredConnection 判断设备的实例链是否为 USB 线缆。第一个决定性节点生效：
// USB 视为有线，蓝牙视为无线。Xbox 接收器也是 USB，但其电量来自 XInput，
// 不来自此实例链。
func wiredConnection(ids []string) bool {
	for _, id := range ids {
		up := strings.ToUpper(id)
		switch {
		case strings.HasPrefix(up, `USB\`):
			return true
		case strings.HasPrefix(up, `BTHENUM\`), strings.HasPrefix(up, `BTHLE\`):
			return false
		}
	}
	return false
}

// batteryFromReport 读取 DualSense 的 USB（0x01）或蓝牙（0x31）输入报告。
// 电量 nibble 为 0–10；状态为 2 表示电池已满。
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

// batteryFromSwitchReport 读取 Switch Pro 标准输入报告（0x21 或 0x30–0x33）。
// 高 nibble 是五档电量，其最低位表示是否充电。0x3F 简单报告没有电量字节，
// 不能当作零电量。
func batteryFromSwitchReport(report []byte) (Battery, bool) {
	if len(report) < 3 {
		return Battery{}, false
	}
	switch report[0] {
	case 0x21, 0x30, 0x31, 0x32, 0x33:
	default:
		return Battery{}, false
	}
	nibble := report[2] >> 4
	level := int(nibble >> 1)
	if level > 4 {
		return Battery{}, false
	}
	return Battery{Known: true, Percent: level * 25, Charging: nibble&1 == 1}, true
}

// batteryFromXInput 读取 Xbox 协议电量。碱性电池、NiMH 与未知是四种真实
// 电量档。有线报告的档位总是满，并非真实电量，因此保持未知。
func batteryFromXInput(kind, level byte) (Battery, bool) {
	switch kind {
	case 0x02, 0x03, 0xFF:
	default:
		return Battery{}, false
	}
	var percent int
	switch level {
	case 0:
		percent = 0
	case 1:
		percent = 33
	case 2:
		percent = 66
	case 3:
		percent = 100
	default:
		return Battery{}, false
	}
	return Battery{Known: true, Percent: percent}, true
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
