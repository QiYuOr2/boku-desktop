//go:build windows && amd64

package proc

import "testing"

func TestReadClocksComesFromKernelBase(t *testing.T) {
	clocks, err := ReadClocks()
	if err != nil {
		t.Fatal(err)
	}
	if clocks.Awake <= 0 || clocks.Biased < clocks.Awake {
		t.Fatalf("clocks = %+v", clocks)
	}
}

func TestExitCallbackKeepsOnlyTheBoolean(t *testing.T) {
	signaled := &exitWatch{}
	onProcessExit(newExitToken(signaled), 0x25922696d00)
	if signaled.exited.Load() == 0 {
		t.Fatal("a signaled exit was discarded because the high bits were dirty")
	}
	timedOut := &exitWatch{}
	onProcessExit(newExitToken(timedOut), 0x10001)
	if timedOut.exited.Load() != 0 {
		t.Fatal("a timeout was recorded")
	}
}
