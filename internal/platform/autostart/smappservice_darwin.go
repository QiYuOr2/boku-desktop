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

// loginItemLocation 是 macOS 列出应用登录项的页面。它是系统设置里的一页而非文件：
// 记录由系统保管。
const loginItemLocation = "系统设置 > 通用 > 登录项与扩展"

// loginItemStatus 是 SMAppServiceStatus，数值取自框架。
type loginItemStatus int

const (
	loginItemNotRegistered    loginItemStatus = 0
	loginItemEnabled          loginItemStatus = 1
	loginItemRequiresApproval loginItemStatus = 2
	loginItemNotFound         loginItemStatus = 3
)

// loginItemService 是本包用到的 SMAppService 部分。用接口是为了让状态机可测，
// 而不必把测试二进制注册成登录项。
type loginItemService interface {
	Status() (loginItemStatus, error)
	Register() error
	Unregister() error
}

// SMAppService 是 macOS 的 Manager：应用自身的登录项，即系统设置 > 通用 >
// 登录项与扩展里列出、用户可关闭的那一项。
//
// macOS 另有 launch agent（本包此前用的机制）：它是后台项，不出现在登录项列表中，
// 用户在系统为「登录时打开」指示的位置找不到它。主应用的登录项才是与该开关对应的机制。
type SMAppService struct {
	service   loginItemService
	legacyDir string
	installed func() error
	migrate   sync.Once
}

// NewSMAppService 返回管理应用登录项的 manager。
func NewSMAppService() *SMAppService {
	// 家目录读不出时，旧目录保持空串而不是拼接到空值上：filepath.Join("", "Library", ...)
	// 是相对路径，会按应用碰巧的工作目录解析，迁移步骤可能找到并删掉别人的文件。
	// 空值正是迁移步骤已经检查的情况。
	return &SMAppService{
		service:   objcLoginItem{},
		legacyDir: legacyLaunchAgentsDir(),
		installed: requireInstalledApp,
	}
}

// legacyLaunchAgentsDir 是 SMAppService 之前的登录 agent 所在目录，家目录读不出时返回 ""。
func legacyLaunchAgentsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("the home directory could not be read; the old login item is left alone", "event", "autostart_home_unavailable", "error", err)
		return ""
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents")
}

// Read 报告系统是否为该应用启用了登录项。用户在系统设置里关掉的记录不算「关」，
// 而是返回 ErrNeedsApproval，由设置页面解释。
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

// Location 是列出登录项的系统设置页面。记录由系统保管，没有文件路径可展示。
func (s *SMAppService) Location(Launcher) (string, error) {
	return loginItemLocation, nil
}

// Start 把应用注册为登录项。重新注册就是把用户在系统设置里关掉的记录重新打开：
// 系统会替换该记录，而不是新建一条。
func (s *SMAppService) Start(l Launcher) error {
	s.migrateLegacy(l)
	if err := s.installed(); err != nil {
		return err
	}
	if err := s.service.Register(); err != nil {
		return err
	}
	// registerAndReturnError: 可能成功但该项仍在等待用户授权。页面应说明去哪里允许，
	// 而不是声称开关什么也没做。
	if status, err := s.service.Status(); err == nil && status == loginItemRequiresApproval {
		return ErrNeedsApproval
	}
	return nil
}

// Stop 移除登录项。记录本就不存在正是用户想要的状态，不算失败。
func (s *SMAppService) Stop(l Launcher) error {
	s.migrateLegacy(l)
	status, err := s.service.Status()
	if err == nil && (status == loginItemNotRegistered || status == loginItemNotFound) {
		return nil
	}
	return s.service.Unregister()
}

// migrateLegacy 移除旧机制写入的 launch agent。若 agent 存在，同时注册登录项，
// 使已有的「开」状态在切换中保留；不读取 agent 自身的 RunAtLoad，与旧开关显示一致。
//
// 这是一次性迁移：等每份已安装副本都跑过认识登录项的版本后即自行失效。
// 删除是安全的：label 属于本应用，且此后不再读取该文件。
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
				// 不在 Applications 里的构建不得改动已安装应用的登录启动。
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

// requireInstalledApp 拒绝为下次登录时不在同一位置的程序写登录项：开发构建、挂载的磁盘镜像、
// 即将被替换的副本。系统登录项指向 bundle，因此 bundle 必须位于 Applications 文件夹。
func requireInstalledApp() error {
	exe, err := BinaryPath()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotInstalled, err)
	}
	bundle := appBundle(exe)
	if bundle == "" {
		return ErrNotInstalled
	}
	// 家目录读不出时只查系统 Applications：拼接到空家目录会变成相对 "Applications"，
	// 那不是本检查想查的位置。
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	} else if err != nil {
		slog.Warn("the home directory could not be read; only the system Applications folder is checked", "event", "autostart_home_unavailable", "error", err)
	}
	for _, root := range roots {
		if inside(root, bundle) {
			return nil
		}
	}
	return ErrNotInstalled
}

// appBundle 从可执行文件向上查找包含它的 bundle。
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

// inside 报告 path 是否为 root 或其下，且不把名字以 root 开头的同级目录误判为在内。
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// objcLoginItem 通过 Objective-C 运行时访问 SMAppService.mainApp。本包与其余应用一样
// 在不启用 cgo 下构建，因此经 purego 调用框架。
type objcLoginItem struct{}

// Status 报告 SMAppService.mainApp.status。
func (objcLoginItem) Status() (loginItemStatus, error) {
	service, err := mainApp()
	if err != nil {
		return loginItemNotRegistered, err
	}
	return loginItemStatus(objc.Send[int64](service, statusSel)), nil
}

// Register 调用 -[SMAppService registerAndReturnError:]。
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

// Unregister 调用 -[SMAppService unregisterAndReturnError:]。
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

// bridge 加载 ServiceManagement 并一次性解析类与 selector。
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
	// Objective-C 的类属性名为 mainAppService；Swift 简写为 SMAppService.mainApp。
	mainAppSel = objc.RegisterName("mainAppService")
	statusSel = objc.RegisterName("status")
	registerSel = objc.RegisterName("registerAndReturnError:")
	unregisterSel = objc.RegisterName("unregisterAndReturnError:")
	localizedDescriptionSel = objc.RegisterName("localizedDescription")
	smBridgeOK = true
}

// mainApp 解析 SMAppService.mainApp。
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

// loginItemError 把框架返回的 NSError 转成 Go error。描述是系统自己的句子，
// 正是设置页要展示的；本包读不出的指针不应成为丢失失败信息本身的理由。
func loginItemError(action string, nsErr objc.ID) error {
	if nsErr != 0 {
		if description := nsString(nsErr, localizedDescriptionSel); description != "" {
			return fmt.Errorf("autostart: could not %s: %s", action, description)
		}
	}
	return fmt.Errorf("autostart: could not %s", action)
}

// nsString 复制系统返回的 NSString。有上界：描述只是一句句子，坏指针不得无限遍历。
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
	// 字符串由系统在本次调用期间持有；4096 字节远超一条本地化描述，且 NUL 会更早终止。
	buf := unsafe.Slice(ptr, 4096)
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
