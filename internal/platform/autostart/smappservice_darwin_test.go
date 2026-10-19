//go:build darwin

package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// fakeLoginItem is the system side of SMAppService the tests decide: its
// status, what each write answers, and how often it was asked.
type fakeLoginItem struct {
	status        loginItemStatus
	statusErr     error
	registerErr   error
	unregisterErr error
	// afterRegister is the status the system reports once a register
	// succeeds; nil means enabled.
	afterRegister *loginItemStatus
	registers     int
	unregisters   int
}

func (f *fakeLoginItem) Status() (loginItemStatus, error) {
	if f.statusErr != nil {
		return loginItemNotRegistered, f.statusErr
	}
	return f.status, nil
}

func (f *fakeLoginItem) Register() error {
	f.registers++
	if f.registerErr != nil {
		return f.registerErr
	}
	if f.afterRegister != nil {
		f.status = *f.afterRegister
	} else {
		f.status = loginItemEnabled
	}
	return nil
}

func (f *fakeLoginItem) Unregister() error {
	f.unregisters++
	if f.unregisterErr != nil {
		return f.unregisterErr
	}
	f.status = loginItemNotRegistered
	return nil
}

// testManager is the real manager with the system service and the legacy
// folder replaced: no test registers the test binary as a login item.
func testManager(t *testing.T, service loginItemService) *SMAppService {
	t.Helper()
	manager := NewSMAppService()
	manager.service = service
	manager.legacyDir = t.TempDir()
	manager.installed = func() error { return nil }
	return manager
}

// Read maps the four statuses to the switch: enabled is on, a record that is
// not there is off, and a record the user turned off is a third answer the
// page has to explain.
func TestSMAppServiceRead(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    loginItemStatus
		wantOn    bool
		wantErr   error
		wantOther bool
	}{
		{name: "enabled", status: loginItemEnabled, wantOn: true},
		{name: "not registered", status: loginItemNotRegistered},
		{name: "not found", status: loginItemNotFound},
		{name: "requires approval", status: loginItemRequiresApproval, wantErr: ErrNeedsApproval},
		{name: "unknown", status: loginItemStatus(42), wantOther: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := testManager(t, &fakeLoginItem{status: test.status})
			on, err := manager.Read(Launcher{Label: "com.boku.app"})
			if on != test.wantOn {
				t.Errorf("Read = %v, want %v", on, test.wantOn)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Errorf("Read error = %v, want %v", err, test.wantErr)
			}
			if test.wantOther && err == nil {
				t.Error("Read accepted an unknown status")
			}
		})
	}
}

// A status the system could not answer is not "off": it is returned as the
// error it is, so the page can say it does not know.
func TestSMAppServiceReadReportsStatusFailure(t *testing.T) {
	manager := testManager(t, &fakeLoginItem{statusErr: errors.New("the system did not answer")})
	if _, err := manager.Read(Launcher{}); err == nil {
		t.Error("Read swallowed a status failure")
	}
}

// The switch writes the system login item: Start registers it, Stop removes
// it, and the state the page reads afterwards is the system's.
func TestSMAppServiceStartStop(t *testing.T) {
	service := &fakeLoginItem{}
	manager := testManager(t, service)
	if err := manager.Start(Launcher{Label: "com.boku.app"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if service.registers != 1 {
		t.Errorf("registers = %d, want 1", service.registers)
	}
	if on, err := manager.Read(Launcher{Label: "com.boku.app"}); err != nil || !on {
		t.Errorf("Read after Start = %v, %v; want true, nil", on, err)
	}
	if err := manager.Stop(Launcher{Label: "com.boku.app"}); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if service.unregisters != 1 {
		t.Errorf("unregisters = %d, want 1", service.unregisters)
	}
	if on, err := manager.Read(Launcher{Label: "com.boku.app"}); err != nil || on {
		t.Errorf("Read after Stop = %v, %v; want false, nil", on, err)
	}
}

// Stopping a login item that was never registered asks for nothing and is not
// a failure.
func TestSMAppServiceStopWithoutARecord(t *testing.T) {
	service := &fakeLoginItem{status: loginItemNotRegistered}
	manager := testManager(t, service)
	if err := manager.Stop(Launcher{}); err != nil {
		t.Errorf("Stop without a record = %v, want nil", err)
	}
	if service.unregisters != 0 {
		t.Errorf("unregisters = %d, want 0", service.unregisters)
	}
}

// A register that succeeds but still waits for the user's approval is
// reported as such: the record exists, and the page says where to allow it.
func TestSMAppServiceStartNeedsApproval(t *testing.T) {
	approval := loginItemRequiresApproval
	manager := testManager(t, &fakeLoginItem{afterRegister: &approval})
	if err := manager.Start(Launcher{}); !errors.Is(err, ErrNeedsApproval) {
		t.Errorf("Start = %v, want ErrNeedsApproval", err)
	}
}

// A build outside an Applications folder never reaches the system: the
// login item would name a bundle that is not there at the next login.
func TestSMAppServiceStartRefusesAnUninstalledBuild(t *testing.T) {
	service := &fakeLoginItem{}
	manager := testManager(t, service)
	manager.installed = func() error { return ErrNotInstalled }
	if err := manager.Start(Launcher{}); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Start = %v, want ErrNotInstalled", err)
	}
	if service.registers != 0 {
		t.Errorf("registers = %d, want 0", service.registers)
	}
}

// The line under the switch names the page the system lists the item on, not
// a file this package writes.
func TestSMAppServiceLocation(t *testing.T) {
	manager := testManager(t, &fakeLoginItem{})
	location, err := manager.Location(Launcher{})
	if err != nil {
		t.Fatalf("Location failed: %v", err)
	}
	if location != loginItemLocation {
		t.Errorf("Location = %q, want %q", location, loginItemLocation)
	}
}

// The previous mechanism's launch agent is migrated on the first read: the
// login item is registered first, and only then is the file removed, so an
// upgrade does not lose the start or leave two behind.
func TestSMAppServiceMigratesTheLaunchAgent(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        loginItemStatus
		registerErr   error
		installedErr  error
		wantRegisters int
		wantFile      bool
	}{
		{name: "off becomes on", status: loginItemNotRegistered, wantRegisters: 1},
		{name: "already on", status: loginItemEnabled},
		{name: "register fails", status: loginItemNotRegistered, registerErr: errors.New("no"), wantRegisters: 1, wantFile: true},
		{name: "dev build keeps it", status: loginItemNotRegistered, installedErr: ErrNotInstalled, wantFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "com.boku.app.plist")
			if err := os.WriteFile(path, []byte("old launch agent"), 0o644); err != nil {
				t.Fatalf("write the legacy file: %v", err)
			}
			service := &fakeLoginItem{status: test.status, registerErr: test.registerErr}
			manager := NewSMAppService()
			manager.service = service
			manager.legacyDir = dir
			manager.installed = func() error { return test.installedErr }
			if _, err := manager.Read(Launcher{Label: "com.boku.app"}); err != nil {
				t.Fatalf("Read failed: %v", err)
			}
			if service.registers != test.wantRegisters {
				t.Errorf("registers = %d, want %d", service.registers, test.wantRegisters)
			}
			_, err := os.Stat(path)
			if test.wantFile && err != nil {
				t.Errorf("the legacy file was removed: %v", err)
			}
			if !test.wantFile && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the legacy file is still there: %v", err)
			}
		})
	}
}

// The path helpers are what decide whether a login item may be written at
// all. A dev build, a disk image, and a sibling directory are all outside.
func TestSMAppServiceInstalledPaths(t *testing.T) {
	if got := appBundle("/Applications/Boku.app/Contents/MacOS/Boku"); got != "/Applications/Boku.app" {
		t.Errorf("appBundle = %q, want the bundle", got)
	}
	if got := appBundle("/tmp/build/Boku"); got != "" {
		t.Errorf("appBundle of an unpackaged program = %q, want empty", got)
	}
	for _, test := range []struct {
		root, path string
		want       bool
	}{
		{root: "/Applications", path: "/Applications/Boku.app", want: true},
		{root: "/Applications", path: "/Applications2/Boku.app", want: false},
		{root: "/Applications", path: "/tmp/Boku.app", want: false},
	} {
		if got := inside(test.root, test.path); got != test.want {
			t.Errorf("inside(%q, %q) = %v, want %v", test.root, test.path, got, test.want)
		}
	}
}

// nsString is the bridge from the system's NSError text to a Go error. It is
// tested with an NSString the test makes, so the account of a failed register
// does not depend on making one fail on a real machine.
func TestNSStringCopiesAnNSString(t *testing.T) {
	if _, err := purego.Dlopen(
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	); err != nil {
		t.Fatalf("load Foundation: %v", err)
	}
	class := objc.GetClass("NSString")
	if class == 0 {
		t.Fatal("NSString is not available")
	}
	alloc := objc.Send[objc.ID](objc.ID(class), objc.RegisterName("alloc"))
	text := append([]byte("hello"), 0)
	str := objc.Send[objc.ID](alloc, objc.RegisterName("initWithUTF8String:"), unsafe.Pointer(&text[0]))
	if str == 0 {
		t.Fatal("NSString initWithUTF8String: returned nothing")
	}
	defer objc.Send[objc.ID](str, objc.RegisterName("release"))
	if got := nsString(str, objc.RegisterName("description")); got != "hello" {
		t.Errorf("nsString = %q, want hello", got)
	}
}
