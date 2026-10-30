package gamepad

import "testing"

func TestKindOf(t *testing.T) {
	tests := []struct {
		vid, pid uint16
		want     Kind
	}{
		{vid: 0x054C, pid: 0x0CE6, want: KindDualSense},
		{vid: 0x054C, pid: 0x0DF2, want: KindDualSense},
		{vid: 0x054C, pid: 0x05C4, want: KindDualSense},
		{vid: 0x057E, pid: 0x2009, want: KindSwitchPro},
		{vid: 0x057E, pid: 0x2007, want: KindXbox},
		{vid: 0x045E, pid: 0x0B13, want: KindXbox},
		{vid: 0x0000, pid: 0x0000, want: KindXbox},
	}
	for _, test := range tests {
		if got := kindOf(test.vid, test.pid); got != test.want {
			t.Errorf("kindOf(%04X, %04X) = %d, want %d", test.vid, test.pid, got, test.want)
		}
	}
}

func TestBatteryFromReport(t *testing.T) {
	usb := make([]byte, 64)
	usb[0] = 0x01
	usb[53] = 0x28
	full, ok := batteryFromReport(usb)
	if !ok || !full.Known || full.Percent != 100 || full.Charging {
		t.Fatalf("full USB report: %+v ok=%v", full, ok)
	}

	usb[53] = 0x15
	charging, ok := batteryFromReport(usb)
	if !ok || charging.Percent != 50 || !charging.Charging {
		t.Fatalf("charging USB report: %+v ok=%v", charging, ok)
	}

	usb[53] = 0x08
	empty, ok := batteryFromReport(usb)
	if !ok || empty.Percent != 80 || empty.Charging {
		t.Fatalf("discharging USB report: %+v ok=%v", empty, ok)
	}

	bt := make([]byte, 78)
	bt[0] = 0x31
	bt[54] = 0x0A
	got, ok := batteryFromReport(bt)
	if !ok || got.Percent != 100 || got.Charging {
		t.Fatalf("bluetooth full nibble: %+v ok=%v", got, ok)
	}

	usb[53] = 0xB8
	if _, ok := batteryFromReport(usb); ok {
		t.Fatal("an error status was accepted")
	}
	if _, ok := batteryFromReport([]byte{0x01, 0x28}); ok {
		t.Fatal("a short report was accepted")
	}
	if _, ok := batteryFromReport(nil); ok {
		t.Fatal("an empty report was accepted")
	}
}

func TestPadsEqual(t *testing.T) {
	a := []Pad{{Kind: KindDualSense, Battery: Battery{Known: true, Percent: 80}}}
	b := []Pad{{Kind: KindDualSense, Battery: Battery{Known: true, Percent: 80}}}
	if !padsEqual(a, b) {
		t.Fatal("equal pads differed")
	}
	b[0].Battery.Percent = 70
	if padsEqual(a, b) {
		t.Fatal("a battery change compared equal")
	}
	if padsEqual(a, nil) {
		t.Fatal("different lengths compared equal")
	}
	if !padsEqual(nil, nil) {
		t.Fatal("two empty lists differed")
	}
}

func TestNeedsBatteryOnlyForAChargeReading(t *testing.T) {
	if needsBattery(nil) || needsBattery([]Pad{{Kind: KindXbox}}) {
		t.Fatal("a pad without a charge byte asked for a refresh")
	}
	if !needsBattery([]Pad{{Kind: KindDualSense}}) || !needsBattery([]Pad{{Kind: KindSwitchPro}}) {
		t.Fatal("a pad with a charge byte did not ask for a refresh")
	}
	if !needsBattery([]Pad{{Kind: KindXbox, Battery: Battery{Known: true, Percent: 66}}}) {
		t.Fatal("a wireless Xbox charge did not ask for a refresh")
	}
}

func TestWiredConnection(t *testing.T) {
	usb := []string{`HID\VID_054C&PID_0CE6&MI_03\7&1`, `USB\VID_054C&PID_0CE6&MI_03\6&2`}
	if !wiredConnection(usb) {
		t.Fatal("a USB cable was treated as wireless")
	}
	bt := []string{`HID\VID_057E&PID_2009\8&1`, `BTHENUM\{00001124-0000-1000-8000-00805f9b34fb}_VID&0002057E_PID&2009\1`}
	if wiredConnection(bt) {
		t.Fatal("Bluetooth was treated as a cable")
	}
	if wiredConnection([]string{`HID\VID_054C&PID_0CE6\1`, `BTHLE\DEV_AABBCCDDEEFF`}) {
		t.Fatal("Bluetooth LE was treated as a cable")
	}
	if wiredConnection(nil) || wiredConnection([]string{`HID\VID_054C&PID_0CE6\1`}) {
		t.Fatal("an unknown bus was treated as a cable")
	}
}

func TestBatteryFromXInput(t *testing.T) {
	full, ok := batteryFromXInput(0x02, 3)
	if !ok || full.Percent != 100 || full.Charging {
		t.Fatalf("alkaline full: %+v ok=%v", full, ok)
	}
	low, ok := batteryFromXInput(0x03, 1)
	if !ok || low.Percent != 33 {
		t.Fatalf("nimh low: %+v ok=%v", low, ok)
	}
	mid, ok := batteryFromXInput(0xFF, 2)
	if !ok || mid.Percent != 66 {
		t.Fatalf("unknown medium: %+v ok=%v", mid, ok)
	}
	empty, ok := batteryFromXInput(0x02, 0)
	if !ok || empty.Percent != 0 || !empty.Known {
		t.Fatalf("alkaline empty: %+v ok=%v", empty, ok)
	}
	if _, ok := batteryFromXInput(0x01, 3); ok {
		t.Fatal("a wired full report was accepted")
	}
	if _, ok := batteryFromXInput(0x00, 0); ok {
		t.Fatal("a disconnected report was accepted")
	}
	if _, ok := batteryFromXInput(0x02, 4); ok {
		t.Fatal("an undefined level was accepted")
	}
}

func TestBatteryFromSwitchReport(t *testing.T) {
	full, ok := batteryFromSwitchReport([]byte{0x30, 0x28, 0x91})
	if !ok || full.Percent != 100 || !full.Charging {
		t.Fatalf("full charging report: %+v ok=%v", full, ok)
	}
	discharging, ok := batteryFromSwitchReport([]byte{0x30, 0x01, 0x80, 0x00})
	if !ok || discharging.Percent != 100 || discharging.Charging {
		t.Fatalf("full report: %+v ok=%v", discharging, ok)
	}
	low, ok := batteryFromSwitchReport([]byte{0x21, 0x02, 0x40})
	if !ok || low.Percent != 50 || low.Charging {
		t.Fatalf("low report: %+v ok=%v", low, ok)
	}
	if _, ok := batteryFromSwitchReport([]byte{0x30, 0x00, 0xB0}); ok {
		t.Fatal("an undefined charge nibble was accepted")
	}
	if _, ok := batteryFromSwitchReport([]byte{0x3F, 0x00, 0x80}); ok {
		t.Fatal("a simple report was accepted")
	}
	if _, ok := batteryFromSwitchReport([]byte{0x30}); ok {
		t.Fatal("a short report was accepted")
	}
}
