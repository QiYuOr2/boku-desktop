//go:build windows

package gamepad

import "testing"

func TestListenHIDRegisters(t *testing.T) {
	stop, ok := listenHID(func() {})
	if !ok {
		t.Fatal("HID notifications did not register")
	}
	stop()
}
