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
	var ds *Pad
	for _, pad := range Connected() {
		if pad.Kind == KindDualSense {
			ds = &pad
			break
		}
	}
	if ds == nil {
		t.Skip("no DualSense is connected")
	}
	if !ds.Battery.Known || ds.Battery.Percent < 0 || ds.Battery.Percent > 100 {
		t.Fatalf("DualSense battery = %+v", ds.Battery)
	}
}
