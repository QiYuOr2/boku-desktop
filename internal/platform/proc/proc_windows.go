//go:build windows && amd64

package proc

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	processQueryLimited           = 0x1000
	processQueryInformation       = 0x0400
	processVMRead                 = 0x0010
	processSynchronize            = 0x00100000
	classSystemProcessInformation = 5
	classProcessBasicInformation  = 0
	processWow64Information       = 26
	waitExecuteOnlyOnce           = 0x00000008
)

var (
	ntdll                       = syscall.NewLazyDLL("ntdll.dll")
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	kernelbase                  = syscall.NewLazyDLL("kernelbase.dll")
	advapi32                    = syscall.NewLazyDLL("advapi32.dll")
	ntQuerySystemInformation    = ntdll.NewProc("NtQuerySystemInformation")
	ntQueryInformationProcess   = ntdll.NewProc("NtQueryInformationProcess")
	queryInterruptTime          = kernelbase.NewProc("QueryInterruptTime")
	queryUnbiasedInterruptTime  = kernel32.NewProc("QueryUnbiasedInterruptTime")
	openProcessProc             = kernel32.NewProc("OpenProcess")
	closeHandleProc             = kernel32.NewProc("CloseHandle")
	queryFullProcessImageNameW  = kernel32.NewProc("QueryFullProcessImageNameW")
	readProcessMemoryProc       = kernel32.NewProc("ReadProcessMemory")
	registerWaitForSingleObject = kernel32.NewProc("RegisterWaitForSingleObject")
	regOpenKeyExW               = advapi32.NewProc("RegOpenKeyExW")
	regQueryValueExW            = advapi32.NewProc("RegQueryValueExW")
	regCloseKey                 = advapi32.NewProc("RegCloseKey")
)

// The fields the recorder reads sit at the front of SYSTEM_PROCESS_INFORMATION.
// Later fields differ between Windows versions and are not declared: the next
// record is found from NextEntryOffset, not from the size of this struct.
type processInfo struct {
	NextEntryOffset              uint32
	NumberOfThreads              uint32
	WorkingSetPrivateSize        int64
	HardFaultCount               uint32
	NumberOfThreadsHighWatermark uint32
	CycleTime                    uint64
	CreateTime                   int64
	UserTime                     int64
	KernelTime                   int64
	ImageName                    ntUnicodeString
	BasePriority                 int32
	UniqueProcessID              uintptr
	InheritedFromUniqueProcessID uintptr
}

type ntUnicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type basicProcessInfo struct {
	ExitStatus                   uint32
	_                            uint32
	PebBaseAddress               uintptr
	AffinityMask                 uintptr
	BasePriority                 int32
	_                            uint32
	UniqueProcessID              uintptr
	InheritedFromUniqueProcessID uintptr
}

// Snapshot lists processes without opening them. Creation time comes from
// the same list the task manager uses, so a game noticed a poll late still
// has its real start.
func Snapshot() ([]Process, error) {
	buf, err := processBuffer()
	if err != nil {
		return nil, err
	}
	var out []Process
	for offset := 0; offset+int(unsafe.Sizeof(processInfo{})) <= len(buf); {
		info := (*processInfo)(unsafe.Pointer(&buf[offset]))
		pid := int(info.UniqueProcessID)
		if pid > 0 {
			out = append(out, Process{
				PID:       pid,
				PPID:      int(info.InheritedFromUniqueProcessID),
				Name:      info.ImageName.string(),
				CreatedAt: filetime(info.CreateTime),
			})
		}
		if info.NextEntryOffset == 0 {
			break
		}
		next := offset + int(info.NextEntryOffset)
		if next <= offset || next > len(buf) {
			break
		}
		offset = next
	}
	return out, nil
}

func processBuffer() ([]byte, error) {
	size := uint32(1 << 20)
	for {
		buf := make([]byte, size)
		var needed uint32
		status, _, _ := ntQuerySystemInformation.Call(
			uintptr(classSystemProcessInformation),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&needed)),
		)
		if status == 0 {
			return buf, nil
		}
		// STATUS_INFO_LENGTH_MISMATCH: the list grew while it was being read.
		if status == 0xC0000004 {
			if needed <= size {
				needed = size * 2
			}
			size = needed + 65536
			continue
		}
		return nil, fmt.Errorf("proc: list processes: status %x", status)
	}
}

// ReadClocks reads the two boot clocks. Their difference is time spent
// asleep, which wall time cannot separate from a clock change.
func ReadClocks() (Clocks, error) {
	awake, err := ticks(queryUnbiasedInterruptTime)
	if err != nil {
		return Clocks{}, err
	}
	biased, err := ticks(queryInterruptTime)
	if err != nil {
		return Clocks{}, err
	}
	return Clocks{Awake: time.Duration(awake) * 100, Biased: time.Duration(biased) * 100}, nil
}

func ticks(proc *syscall.LazyProc) (uint64, error) {
	if err := proc.Find(); err != nil {
		return 0, fmt.Errorf("proc: read the boot clock: %w", err)
	}
	var value uint64
	r, _, err := proc.Call(uintptr(unsafe.Pointer(&value)))
	if r == 0 {
		if err == nil {
			err = syscall.EINVAL
		}
		return 0, fmt.Errorf("proc: read the boot clock: %w", err)
	}
	return value, nil
}

// ImagePath is the full executable path. The process list only carries the
// file name, which is not enough to tell two games apart.
func ImagePath(pid int) (string, error) {
	handle, err := openProcess(processQueryLimited, pid)
	if err != nil {
		return "", err
	}
	defer closeHandle(handle)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	r, _, err := queryFullProcessImageNameW.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
	)
	if r == 0 {
		if err == nil {
			err = syscall.EINVAL
		}
		return "", err
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

// SteamAppID reads SteamAppId or SteamGameId from a process's own
// environment. The rest of the environment is discarded here: it is not a
// value the recorder keeps.
func SteamAppID(pid int) (string, error) {
	handle, err := openProcess(processQueryInformation|processVMRead, pid)
	if err != nil {
		return "", err
	}
	defer closeHandle(handle)
	block, err := environment(handle)
	if err != nil {
		return "", err
	}
	for _, entry := range strings.Split(block, "\x00") {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || (key != "SteamAppId" && key != "SteamGameId") {
			continue
		}
		id := leadingDigits(strings.TrimSpace(value))
		if id != "" && id != "0" {
			return id, nil
		}
	}
	return "", nil
}

// SteamRoot is the Steam installation recorded for the current user.
func SteamRoot() (string, error) {
	if path, err := regString(syscall.HKEY_CURRENT_USER, `Software\Valve\Steam`, "SteamPath"); err == nil && path != "" {
		return path, nil
	}
	if path, err := regString(syscall.HKEY_LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"); err == nil && path != "" {
		return path, nil
	}
	return "", fmt.Errorf("proc: steam is not installed")
}

type exitWatch struct {
	handle  syscall.Handle
	created time.Time
	token   uintptr
	exited  atomic.Int64
}

var (
	watchMu      sync.Mutex
	watches      = map[string]*exitWatch{}
	tokens       = map[uintptr]*exitWatch{}
	nextToken    uintptr
	callbackOnce sync.Once
	exitCallback uintptr
)

// WatchExit asks to be told when pid exits. The notification carries the
// wall time of the exit, so the session does not wait for the next poll to
// end. A process that cannot be opened is simply polled instead.
func WatchExit(pid int, created time.Time) error {
	callbackOnce.Do(func() {
		exitCallback = syscall.NewCallback(onProcessExit)
	})
	handle, err := openProcess(processSynchronize|processQueryLimited, pid)
	if err != nil {
		return err
	}
	watch := &exitWatch{handle: handle, created: created.UTC()}
	key := watchKey(pid, created)
	token := newExitToken(watch)
	watchMu.Lock()
	watches[key] = watch
	watchMu.Unlock()
	var registration uintptr
	// The token is an integer so the callback does not take a Go pointer.
	// The map keeps the watch alive until the exit is collected.
	r, _, err := registerWaitForSingleObject.Call(
		uintptr(unsafe.Pointer(&registration)),
		uintptr(handle),
		exitCallback,
		token,
		0xFFFFFFFF,
		waitExecuteOnlyOnce,
	)
	if r == 0 {
		watchMu.Lock()
		delete(watches, key)
		delete(tokens, token)
		watchMu.Unlock()
		closeHandle(handle)
		if err == nil {
			err = syscall.EINVAL
		}
		return err
	}
	return nil
}

// TakeExit reports the exit time recorded for the process, once.
func TakeExit(pid int, created time.Time) (time.Time, bool) {
	key := watchKey(pid, created)
	watchMu.Lock()
	watch := watches[key]
	if watch != nil && watch.exited.Load() != 0 {
		delete(watches, key)
		delete(tokens, watch.token)
	} else {
		watch = nil
	}
	watchMu.Unlock()
	if watch == nil {
		return time.Time{}, false
	}
	closeHandle(watch.handle)
	return time.Unix(0, watch.exited.Load()).UTC(), true
}

func onProcessExit(param, timedOut uintptr) uintptr {
	// BOOLEAN is one byte. On x64 the rest of the register is undefined, so a
	// signaled exit still arrives with a non-zero uintptr.
	if param == 0 || timedOut&0xFF != 0 {
		return 0
	}
	watchMu.Lock()
	watch := tokens[param]
	watchMu.Unlock()
	if watch != nil {
		watch.exited.Store(time.Now().UTC().UnixNano())
	}
	return 0
}

func newExitToken(watch *exitWatch) uintptr {
	watchMu.Lock()
	defer watchMu.Unlock()
	nextToken++
	if nextToken == 0 {
		nextToken++
	}
	watch.token = nextToken
	tokens[nextToken] = watch
	return nextToken
}

func watchKey(pid int, created time.Time) string {
	return fmt.Sprintf("%d:%d", pid, created.UTC().UnixNano())
}

func (s ntUnicodeString) string() string {
	if s.Buffer == nil || s.Length < 2 {
		return ""
	}
	n := int(s.Length) / 2
	raw := unsafe.Slice(s.Buffer, n)
	return string(utf16.Decode(raw))
}

func filetime(ticks int64) time.Time {
	if ticks <= 0 {
		return time.Time{}
	}
	const windowsEpoch = 116444736000000000
	ticks -= windowsEpoch
	return time.Unix(ticks/1e7, (ticks%1e7)*100).UTC()
}

func leadingDigits(value string) string {
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	return value[:i]
}

func openProcess(access uint32, pid int) (syscall.Handle, error) {
	handle, _, err := openProcessProc.Call(uintptr(access), 0, uintptr(pid))
	if handle == 0 {
		if err == nil {
			err = syscall.EINVAL
		}
		return 0, err
	}
	return syscall.Handle(handle), nil
}

func closeHandle(handle syscall.Handle) {
	_, _, _ = closeHandleProc.Call(uintptr(handle))
}

func environment(handle syscall.Handle) (string, error) {
	if peb, ok := wow64PEB(handle); ok {
		var params uint32
		if err := readInto(handle, peb+0x10, unsafe.Pointer(&params), 4); err != nil {
			return "", err
		}
		var env uint32
		if err := readInto(handle, uintptr(params)+0x48, unsafe.Pointer(&env), 4); err != nil {
			return "", err
		}
		return readEnvironment(handle, uintptr(env))
	}
	var basic basicProcessInfo
	if err := queryProcess(handle, classProcessBasicInformation, unsafe.Pointer(&basic), unsafe.Sizeof(basic)); err != nil {
		return "", err
	}
	var params uintptr
	if err := readInto(handle, basic.PebBaseAddress+0x20, unsafe.Pointer(&params), 8); err != nil {
		return "", err
	}
	var env uintptr
	if err := readInto(handle, params+0x80, unsafe.Pointer(&env), 8); err != nil {
		return "", err
	}
	return readEnvironment(handle, env)
}

func wow64PEB(handle syscall.Handle) (uintptr, bool) {
	var peb uintptr
	if err := queryProcess(handle, processWow64Information, unsafe.Pointer(&peb), unsafe.Sizeof(peb)); err != nil || peb == 0 {
		return 0, false
	}
	return peb, true
}

func queryProcess(handle syscall.Handle, class int, out unsafe.Pointer, size uintptr) error {
	status, _, _ := ntQueryInformationProcess.Call(uintptr(handle), uintptr(class), uintptr(out), size, 0)
	if status != 0 {
		return fmt.Errorf("proc: query process: status %x", status)
	}
	return nil
}

func readInto(handle syscall.Handle, addr uintptr, out unsafe.Pointer, size uintptr) error {
	var got uintptr
	r, _, err := readProcessMemoryProc.Call(uintptr(handle), addr, uintptr(out), size, uintptr(unsafe.Pointer(&got)))
	if r == 0 || got != size {
		if err == nil {
			err = syscall.EINVAL
		}
		return err
	}
	return nil
}

func readEnvironment(handle syscall.Handle, addr uintptr) (string, error) {
	if addr == 0 {
		return "", fmt.Errorf("proc: process has no environment")
	}
	var raw []byte
	for len(raw) < 256*1024 {
		page := make([]byte, 4096)
		var got uintptr
		r, _, err := readProcessMemoryProc.Call(
			uintptr(handle),
			addr+uintptr(len(raw)),
			uintptr(unsafe.Pointer(&page[0])),
			uintptr(len(page)),
			uintptr(unsafe.Pointer(&got)),
		)
		if r == 0 || got == 0 {
			if len(raw) == 0 {
				if err == nil {
					err = syscall.EINVAL
				}
				return "", err
			}
			break
		}
		raw = append(raw, page[:got]...)
		if doubleNUL(raw) {
			break
		}
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(units)), nil
}

func doubleNUL(raw []byte) bool {
	for i := 0; i+3 < len(raw); i += 2 {
		if raw[i] == 0 && raw[i+1] == 0 && raw[i+2] == 0 && raw[i+3] == 0 {
			return true
		}
	}
	return false
}

func regString(key syscall.Handle, path, name string) (string, error) {
	sub, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var opened syscall.Handle
	status, _, err := regOpenKeyExW.Call(uintptr(key), uintptr(unsafe.Pointer(sub)), 0, 0x20019, uintptr(unsafe.Pointer(&opened)))
	if status != 0 {
		if err == nil {
			err = syscall.Errno(status)
		}
		return "", err
	}
	defer regCloseKey.Call(uintptr(opened))
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var kind, size uint32
	status, _, err = regQueryValueExW.Call(uintptr(opened), uintptr(unsafe.Pointer(value)), 0, uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size)))
	if status != 0 || size == 0 {
		if err == nil {
			err = syscall.Errno(status)
		}
		return "", err
	}
	buf := make([]uint16, size/2+1)
	status, _, err = regQueryValueExW.Call(
		uintptr(opened),
		uintptr(unsafe.Pointer(value)),
		0,
		uintptr(unsafe.Pointer(&kind)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if status != 0 {
		if err == nil {
			err = syscall.Errno(status)
		}
		return "", err
	}
	return syscall.UTF16ToString(buf), nil
}
