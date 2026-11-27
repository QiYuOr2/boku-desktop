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

// fakeLoginItem 是测试可控的 SMAppService 系统侧：状态、每次写入的返回、被调用次数。
type fakeLoginItem struct {
	status        loginItemStatus
	statusErr     error
	registerErr   error
	unregisterErr error
	// afterRegister 是注册成功后系统报告的状态；nil 表示已启用。
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

// testManager 返回替换掉系统服务与旧目录的真实 manager：不让任何测试把测试二进制注册成登录项。
func testManager(t *testing.T, service loginItemService) *SMAppService {
	t.Helper()
	manager := NewSMAppService()
	manager.service = service
	manager.legacyDir = t.TempDir()
	manager.installed = func() error { return nil }
	return manager
}

// 四种状态到开关的映射：已启用为开；没有记录为关；被用户关掉的记录是需要页面解释的第三种答案。
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

// 系统无法回答的状态不是「关」：按错误返回，让页面能说它不知道。
func TestSMAppServiceReadReportsStatusFailure(t *testing.T) {
	manager := testManager(t, &fakeLoginItem{statusErr: errors.New("the system did not answer")})
	if _, err := manager.Read(Launcher{}); err == nil {
		t.Error("Read swallowed a status failure")
	}
}

// 开关写入系统登录项：Start 注册、Stop 移除，之后页面读到的是系统状态。
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

// 从未注册过的登录项、移除它不请求任何操作，也不算失败。
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

// 注册成功但仍等待用户授权的，按需授权报告：记录已存在，页面说明去哪里允许。
func TestSMAppServiceStartNeedsApproval(t *testing.T) {
	approval := loginItemRequiresApproval
	manager := testManager(t, &fakeLoginItem{afterRegister: &approval})
	if err := manager.Start(Launcher{}); !errors.Is(err, ErrNeedsApproval) {
		t.Errorf("Start = %v, want ErrNeedsApproval", err)
	}
}

// 不在 Applications 文件夹里的构建永远不碰系统：否则登录项会指向下次登录时已不存在的 bundle。
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

// 开关下方的说明指向系统列出该登录项的页面，而不是本包写入的某个文件。
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

// 旧机制的 launch agent 在首次读取时迁移：先注册登录项、再删除文件，
// 升级时既不会丢失启动，也不会留下两份。
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

// 路径辅助函数决定能否写入登录项：开发构建、磁盘镜像、同级目录都算在外部。
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

// nsString 把系统的 NSError 文本桥接成 Go error。这里用测试自造的 NSString 来测，
// 这样对注册失败的叙述就不依赖于在真实机器上制造一次失败。
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
