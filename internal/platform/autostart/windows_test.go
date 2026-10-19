//go:build windows

package autostart

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// The Windows login start: a Run value in the user's own key, and the flag
// Task Manager keeps beside it.
//
// The tests never write the value Boku would write. A Run value named Boku is
// a start at the next login, and a test must not be able to cause one. The
// document — the value's name and the command line — is a pure function and is
// tested in full; the round trip through the registry uses a value of the
// test's own, and removes it again when the test ends.

// testRunValue is the name the round-trip test writes: the application's name
// plus a suffix, so that it is recognisably a test's and never the real one.
const testRunValue = "Boku autostart test"

// launcher is one login start, as the installer's shortcut would make it: a
// program under a path with a space in it, which is most Windows installs.
func launcher() Launcher {
	return Launcher{
		Label: "com.boku.app",
		Name:  "Boku",
		Exe:   `C:\Users\test\AppData\Local\Programs\Boku\Boku.exe`,
		Arg:   OpenedAtLoginArg,
	}
}

// The value is named after the application, and the command line is the
// program quoted with the argument after it: unquoted, a path with a space in
// it would start "C:" and nothing else.
func TestWindowsDocument(t *testing.T) {
	doc, err := NewWindows().Document(launcher())
	if err != nil {
		t.Fatalf("Document failed: %v", err)
	}
	if want := runKeyPath + `\Boku`; doc.Key != want {
		t.Errorf("the Run value is %q, want %q", doc.Key, want)
	}
	want := `"C:\Users\test\AppData\Local\Programs\Boku\Boku.exe" --mygo-opened-at-login`
	if string(doc.Contents) != want {
		t.Errorf("the command line is %q, want %q", doc.Contents, want)
	}
}

// A start with no name has no value to be recorded under.
func TestWindowsRefusesAStartWithNoName(t *testing.T) {
	if _, err := NewWindows().Document(Launcher{Exe: `C:\Boku.exe`}); err == nil {
		t.Error("Document accepted a start with no name")
	}
}

// Nothing has been written for this name, so the start is off — which is an
// answer, not an error.
func TestWindowsReadsAnAbsentValueAsOff(t *testing.T) {
	on, err := NewWindows().Read(launcher())
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if on {
		t.Error("Read said Boku starts at login, but nothing wrote that value")
	}
	if !Available(NewWindows(), launcher()) {
		t.Error("Available said Windows has no login start")
	}
}

// The round trip through the registry, on a value of the test's own: write
// it, read it back, take it away. A value that names another program reads as
// off, because it would start something that is not this application.
func TestWindowsStartStopRoundTrip(t *testing.T) {
	l := launcher()
	// The value's name is the one thing the test changes: everything else
	// about the start is the real one.
	l.Name = testRunValue
	manager := NewWindows()
	t.Cleanup(func() { _ = manager.Stop(l) })

	if err := manager.Start(l); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || !on {
		t.Fatalf("Read after Start = %v, %v; want true, nil", on, err)
	}
	other := l
	other.Exe = `D:\Elsewhere\Boku.exe`
	if on, err := manager.Read(other); err != nil || on {
		t.Fatalf("Read of another program = %v, %v; want false, nil", on, err)
	}
	if err := manager.Stop(l); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || on {
		t.Fatalf("Read after Stop = %v, %v; want false, nil", on, err)
	}
	// Stopping again is not a failure: the switch is off either way.
	if err := manager.Stop(l); err != nil {
		t.Errorf("Stop without a start = %v, want nil", err)
	}
}

// Task Manager can turn the entry off without removing it. That is a start
// that does not happen, so it reads as off — and writing the start again is
// how the user turns it back on, which is why Start clears the flag.
func TestWindowsTaskManagerFlagReadsAsOff(t *testing.T) {
	l := launcher()
	l.Name = testRunValue
	manager := NewWindows()
	t.Cleanup(func() { _ = manager.Stop(l) })
	if err := manager.Start(l); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	// The flag Task Manager writes: a binary value whose first byte is odd.
	if err := writeTestFlag(testRunValue, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatalf("writing the flag failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || on {
		t.Fatalf("Read with Task Manager's flag = %v, %v; want false, nil", on, err)
	}
	// Pressing the switch again is the answer to that, and it takes the flag
	// away.
	if err := manager.Start(l); err != nil {
		t.Fatalf("Start over the flag failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || !on {
		t.Fatalf("Read after Start again = %v, %v; want true, nil", on, err)
	}
}

// The command line is what Windows runs, so it has to keep the program whole:
// quoted exactly once, with the argument outside the quotes.
func TestWindowsCommandLineKeepsThePathWhole(t *testing.T) {
	l := launcher()
	if count := strings.Count(l.CommandLine(), `"`); count != 2 {
		t.Errorf("the command line has %d quotes, want the program quoted once: %s", count, l.CommandLine())
	}
	if !strings.HasSuffix(l.CommandLine(), " "+OpenedAtLoginArg) {
		t.Errorf("the command line has no argument after the program: %s", l.CommandLine())
	}
}

// The program a login start names has to be a real file, so that the switch
// is not writing a start of something that is not there.
func TestWindowsBinaryPath(t *testing.T) {
	path, err := BinaryPath()
	if err != nil {
		t.Fatalf("BinaryPath failed: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		t.Errorf("BinaryPath = %q, which is not a program: %v", path, err)
	}
}

// A shortcut's target is what the shell stored, which is often a command line
// rather than a path: quoted when the path has a space in it, and with an
// argument after it either way.
func TestCleanShortcutTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Programs\Boku\Boku.exe`, `C:\Programs\Boku\Boku.exe`},
		{`"C:\Program Files\Boku\Boku.exe"`, `C:\Program Files\Boku\Boku.exe`},
		{`"C:\Program Files\Boku\Boku.exe" --background`, `C:\Program Files\Boku\Boku.exe`},
		{`C:\Boku\Boku.exe --background`, `C:\Boku\Boku.exe`},
		{``, ``},
	}
	for _, c := range cases {
		if got := CleanShortcutTarget(c.in); got != c.want {
			t.Errorf("CleanShortcutTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// writeTestFlag writes Task Manager's "turned off" marker for one value.
//
// Only a test writes this marker: the application clears it and never sets
// it, and the key it goes in may not exist on a machine where Task Manager
// has never been used, so it is created when it is missing.
func writeTestFlag(name string, data []byte) error {
	path, err := syscall.UTF16PtrFromString(approvedKeyPath)
	if err != nil {
		return err
	}
	var key syscall.Handle
	if rc, _, _ := regCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, 0, 0,
		keyWrite|keyRead, 0, uintptr(unsafe.Pointer(&key)), 0); rc != 0 {
		return syscall.Errno(rc)
	}
	defer regCloseKey.Call(uintptr(key))
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if rc, _, _ := regSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0, regBinary,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data))); rc != 0 {
		return syscall.Errno(rc)
	}
	return nil
}
