//go:build darwin

package autostart

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// loginItemLocation is where macOS lists the application's login item. It is
// a page in System Settings, not a file: the system keeps the record.
const loginItemLocation = "系统设置 > 通用 > 登录项与扩展"

// loginItemStatus is SMAppServiceStatus. The numbers are the framework's.
type loginItemStatus int

const (
	loginItemNotRegistered    loginItemStatus = 0
	loginItemEnabled          loginItemStatus = 1
	loginItemRequiresApproval loginItemStatus = 2
	loginItemNotFound         loginItemStatus = 3
)

// loginItemService is the part of SMAppService this package uses. It is an
// interface so the state machine can be tested without registering the test
// binary as a login item.
type loginItemService interface {
	Status() (loginItemStatus, error)
	Register() error
	Unregister() error
}

// SMAppService is the macOS Manager: the application's own login item, the
// one System Settings > General > Login Items & Extensions lists and the user
// can switch off there.
//
// macOS also has launch agents, and the package used one before. An agent is
// a background item: it does not appear in the login items list, and a user
// who looks in the place the system names for "open at login" cannot find it.
// The main application's login item is the mechanism that matches the switch.
type SMAppService struct {
	service   loginItemService
	legacyDir string
	installed func() error
	migrate   sync.Once
}

// NewSMAppService returns the manager over the application's login item.
func NewSMAppService() *SMAppService {
	home, _ := os.UserHomeDir()
	return &SMAppService{
		service:   objcLoginItem{},
		legacyDir: filepath.Join(home, "Library", "LaunchAgents"),
		installed: requireInstalledApp,
	}
}

// Read reports whether the system has an enabled login item for this
// application. A record the user turned off in System Settings is not "off":
// it is ErrNeedsApproval, which the settings page can explain.
func (s *SMAppService) Read(l Launcher) (bool, error) {
	s.migrateLegacy(l)
	status, err := s.service.Status()
	if err != nil {
		return false, err
	}
	switch status {
	case loginItemEnabled:
		return true, nil
	case loginItemNotRegistered, loginItemNotFound:
		return false, nil
	case loginItemRequiresApproval:
		return false, ErrNeedsApproval
	default:
		return false, fmt.Errorf("autostart: the login item reported an unknown status %d", status)
	}
}

// Location is the System Settings page that lists the login item. The system
// owns the record, so there is no file path to show.
func (s *SMAppService) Location(Launcher) (string, error) {
	return loginItemLocation, nil
}

// Start registers the application as a login item. Registering again is how a
// record the user switched off in System Settings is switched back on: the
// system replaces the record rather than making a second one.
func (s *SMAppService) Start(l Launcher) error {
	s.migrateLegacy(l)
	if err := s.installed(); err != nil {
		return err
	}
	if err := s.service.Register(); err != nil {
		return err
	}
	// registerAndReturnError: can succeed while the item still waits for the
	// user's approval. The page should say where to allow it rather than
	// claim the switch did nothing.
	if status, err := s.service.Status(); err == nil && status == loginItemRequiresApproval {
		return ErrNeedsApproval
	}
	return nil
}

// Stop removes the login item. A record that is not there is the state the
// user asked for, not a failure.
func (s *SMAppService) Stop(l Launcher) error {
	s.migrateLegacy(l)
	status, err := s.service.Status()
	if err == nil && (status == loginItemNotRegistered || status == loginItemNotFound) {
		return nil
	}
	return s.service.Unregister()
}

// migrateLegacy removes the launch agent the previous mechanism wrote. It
// also registers the login item when the agent was there, so an existing
// "on" survives the change; the agent's own RunAtLoad is not consulted, which
// matches what the old switch showed.
//
// This is a one-time migration. It retires itself once every installed copy
// has run a build that knows the login item; removal is safe because the
// label belongs to this application and the file is not read after this.
func (s *SMAppService) migrateLegacy(l Launcher) {
	s.migrate.Do(func() {
		if strings.TrimSpace(l.Label) == "" || s.legacyDir == "" {
			return
		}
		path := filepath.Join(s.legacyDir, l.Label+".plist")
		if _, err := os.Stat(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("the old launch agent could not be inspected", "path", path, "error", err)
			}
			return
		}
		if s.installed != nil {
			if err := s.installed(); err != nil {
				// A build outside Applications must not move the installed
				// application's login start.
				return
			}
		}
		status, err := s.service.Status()
		if err != nil {
			slog.Warn("the old launch agent was kept: the login item could not be read", "error", err)
			return
		}
		if status != loginItemEnabled {
			if err := s.service.Register(); err != nil {
				slog.Warn("the old launch agent was kept: the login item could not be registered", "error", err)
				return
			}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("the old launch agent could not be removed", "path", path, "error", err)
		}
	})
}

// requireInstalledApp refuses a login item for a program that will not be in
// the same place at the next login: a development build, a mounted disk image,
// a copy that is about to be replaced. The system login item names the bundle,
// so the bundle has to be in an Applications folder.
func requireInstalledApp() error {
	exe, err := BinaryPath()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotInstalled, err)
	}
	bundle := appBundle(exe)
	if bundle == "" {
		return ErrNotInstalled
	}
	home, _ := os.UserHomeDir()
	for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
		if inside(root, bundle) {
			return nil
		}
	}
	return ErrNotInstalled
}

// appBundle walks up from an executable to the bundle that contains it.
func appBundle(exe string) string {
	for dir := filepath.Dir(exe); ; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
	}
}

// inside reports whether path is root or below it, without treating a
// sibling whose name starts with root's as inside.
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// objcLoginItem is SMAppService.mainApp reached through the Objective-C
// runtime. The package builds without cgo, as the rest of the application
// does, so the framework is called through purego.
type objcLoginItem struct{}

// Status reports SMAppService.mainApp.status.
func (objcLoginItem) Status() (loginItemStatus, error) {
	service, err := mainApp()
	if err != nil {
		return loginItemNotRegistered, err
	}
	return loginItemStatus(objc.Send[int64](service, statusSel)), nil
}

// Register calls -[SMAppService registerAndReturnError:].
func (objcLoginItem) Register() error {
	service, err := mainApp()
	if err != nil {
		return err
	}
	var nsErr objc.ID
	if ok := objc.Send[bool](service, registerSel, &nsErr); !ok {
		return loginItemError("register the login item", nsErr)
	}
	return nil
}

// Unregister calls -[SMAppService unregisterAndReturnError:].
func (objcLoginItem) Unregister() error {
	service, err := mainApp()
	if err != nil {
		return err
	}
	var nsErr objc.ID
	if ok := objc.Send[bool](service, unregisterSel, &nsErr); !ok {
		return loginItemError("remove the login item", nsErr)
	}
	return nil
}

var (
	smBridgeOnce            sync.Once
	smBridgeOK              bool
	smClass                 objc.Class
	mainAppSel              objc.SEL
	statusSel               objc.SEL
	registerSel             objc.SEL
	unregisterSel           objc.SEL
	localizedDescriptionSel objc.SEL
)

// bridge loads ServiceManagement and resolves the class and selectors once.
func bridge() {
	if _, err := purego.Dlopen(
		"/System/Library/Frameworks/ServiceManagement.framework/ServiceManagement",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	); err != nil {
		return
	}
	class := objc.GetClass("SMAppService")
	if class == 0 {
		return
	}
	smClass = class
	// The Objective-C class property is mainAppService; Swift shortens it to
	// SMAppService.mainApp.
	mainAppSel = objc.RegisterName("mainAppService")
	statusSel = objc.RegisterName("status")
	registerSel = objc.RegisterName("registerAndReturnError:")
	unregisterSel = objc.RegisterName("unregisterAndReturnError:")
	localizedDescriptionSel = objc.RegisterName("localizedDescription")
	smBridgeOK = true
}

// mainApp resolves SMAppService.mainApp.
func mainApp() (objc.ID, error) {
	smBridgeOnce.Do(bridge)
	if !smBridgeOK {
		return 0, ErrUnsupported
	}
	service := objc.Send[objc.ID](objc.ID(smClass), mainAppSel)
	if service == 0 {
		return 0, errors.New("autostart: the system returned no login item")
	}
	return service, nil
}

// loginItemError turns the NSError the framework returned into a Go error.
// The description is the system's own sentence, which is what the settings
// page can show; a pointer this package cannot read is not a reason to lose
// the failure itself.
func loginItemError(action string, nsErr objc.ID) error {
	if nsErr != 0 {
		if description := nsString(nsErr, localizedDescriptionSel); description != "" {
			return fmt.Errorf("autostart: could not %s: %s", action, description)
		}
	}
	return fmt.Errorf("autostart: could not %s", action)
}

// nsString copies an NSString returned by the system. It is bounded: the
// description is a sentence, and a bad pointer must not walk forever.
func nsString(value objc.ID, sel objc.SEL) string {
	if value == 0 {
		return ""
	}
	str := objc.Send[objc.ID](value, sel)
	if str == 0 {
		return ""
	}
	ptr := objc.Send[*byte](str, objc.RegisterName("UTF8String"))
	if ptr == nil {
		return ""
	}
	// The system owns the string for the duration of the call; 4096 bytes is
	// far more than a localized description, and the NUL ends it earlier.
	buf := unsafe.Slice(ptr, 4096)
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
