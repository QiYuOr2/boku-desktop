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

func TestNeedsBatteryOnlyForADualSense(t *testing.T) {
	if needsBattery(nil) || needsBattery([]Pad{{Kind: KindXbox}, {Kind: KindSwitchPro}}) {
		t.Fatal("a pad without a charge byte asked for a refresh")
	}
	if !needsBattery([]Pad{{Kind: KindDualSense}}) {
		t.Fatal("a DualSense did not ask for a refresh")
	}
}
