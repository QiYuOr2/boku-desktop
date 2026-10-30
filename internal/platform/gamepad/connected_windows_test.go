//go:build windows

package gamepad

import (
	"runtime"
	"testing"
)

func TestDeviceIDDropsCollection(t *testing.T) {
	a := deviceID(`\\?\hid#vid_054c&pid_0ce6&mi_03#7&abc&0&0000#{guid}`)
	b := deviceID(`\\?\HID#VID_054C&PID_0CE6&MI_03#7&abc&0&0001#{other}`)
	if a == "" || a != b {
		t.Fatalf("collections were not one device: %q %q", a, b)
	}
}

func TestConnectedDualSenseReportsBattery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip()
	}
	var ds Pad
	var wired bool
	var found bool
	for _, item := range listPads() {
		if item.pad.Kind != KindDualSense {
			continue
		}
		ds = item.pad
		wired = item.wired
		found = true
		break
	}
	if !found {
		t.Skip("no DualSense is connected")
	}
	if wired {
		if ds.Battery.Known {
			t.Fatalf("wired DualSense still shows battery: %+v", ds.Battery)
		}
		return
	}
	if !ds.Battery.Known || ds.Battery.Percent < 0 || ds.Battery.Percent > 100 {
		t.Fatalf("Bluetooth DualSense battery = %+v", ds.Battery)
	}
}

func TestConnectedSwitchProReportsBattery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip()
	}
	var pro Pad
	var wired bool
	var found bool
	for _, item := range listPads() {
		if item.pad.Kind != KindSwitchPro {
			continue
		}
		pro = item.pad
		wired = item.wired
		found = true
		break
	}
	if !found {
		t.Skip("no Switch Pro is connected")
	}
	if wired {
		if pro.Battery.Known {
			t.Fatalf("wired Switch Pro still shows battery: %+v", pro.Battery)
		}
		return
	}
	if !pro.Battery.Known || pro.Battery.Percent%25 != 0 || pro.Battery.Percent < 0 || pro.Battery.Percent > 100 {
		t.Fatalf("Bluetooth Switch Pro battery = %+v", pro.Battery)
	}
}

func TestConnectedXInputBatteryMatchesReport(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip()
	}
	slots := xinputSlots()
	var xbox []Pad
	for _, pad := range Connected() {
		if pad.Kind == KindXbox {
			xbox = append(xbox, pad)
		}
	}
	if len(slots) == 0 || len(xbox) == 0 {
		t.Skip("no Xbox-protocol pad is connected")
	}
	if len(slots) == 1 && len(xbox) == 1 && xbox[0].Battery != slots[0] {
		t.Fatalf("pad battery %+v, xinput %+v", xbox[0].Battery, slots[0])
	}
}
