//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Windows keeps the login start as a value in the Run key of the user, and
// quietly turns it off in a second one.
//
// Task Manager's Startup page does not delete the command when the user
// disables an entry: it sets a flag in StartupApproved\Run, and the start
// stops happening while the Run value is still there. So "is it on" is two
// questions — the value exists and it names this program, and it has not been
// turned off — and "turn it on" has to clear that flag, or a user who disabled
// it once could never enable it again from Boku.
//
// The registry is reached through advapi32 directly, as the proxy reader of
// platform/httpc does: this build has no cgo, and a handful of values in the
// user's own key does not need a library. Everything is read and written in
// HKCU, the user's own hive: a login start is the user's, and the application
// never writes the machine's.
const (
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	keyWrite        = 0x20006

	regSZ     = 1
	regBinary = 3

	// errnoFileNotFound is ERROR_FILE_NOT_FOUND, which RegOpenKeyEx gives for
	// a key that does not exist.
	errnoFileNotFound = syscall.Errno(2)
	// errnoMoreData is ERROR_MORE_DATA, which a fixed buffer returns when the
	// value is longer than it: a command line is read in two steps.
	errnoMoreData = syscall.Errno(234)
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	regOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	regCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	regQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	regSetValueExW  = advapi32.NewProc("RegSetValueExW")
	regDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	regCloseKey     = advapi32.NewProc("RegCloseKey")
)

// errKeyNotFound and errValueNotFound are the two ordinary "there is nothing
// here" answers, told apart from a real failure so that a login start that
// was never written is simply off.
var (
	errKeyNotFound   = errors.New("autostart: the registry key is not there")
	errValueNotFound = errors.New("autostart: the registry value is not there")
)

// Windows is the Manager over the registry of the current user. It touches
// nothing when it is built: the registry is read when a question is asked,
// which is after the application is running.
type Windows struct{}

// NewWindows returns the Windows manager.
func NewWindows() *Windows { return &Windows{} }

// Document is the Run value: its full name, and the command line in it.
func (w *Windows) Document(l Launcher) (Document, error) {
	if strings.TrimSpace(l.Name) == "" {
		return Document{}, errors.New("autostart: the login start needs a name")
	}
	return Document{
		Key:      runKeyPath + `\` + l.Name,
		Contents: []byte(l.CommandLine()),
	}, nil
}

// Location is the Run value's full name, which is what the settings page
// shows under the switch.
func (w *Windows) Location(l Launcher) (string, error) {
	doc, err := w.Document(l)
	if err != nil {
		return "", err
	}
	return doc.Key, nil
}

// Read reports whether the Run value names this program and Task Manager has
// not turned it off.
//
// A value that names another copy of the application reads as off: the start
// would launch a program that is not the one the login start was made for.
func (w *Windows) Read(l Launcher) (bool, error) {
	doc, err := w.Document(l)
	if err != nil {
		return false, err
	}
	command, kind, ok, err := readRegistryString(runKeyPath, l.Name)
	if err != nil {
		return false, err
	}
	if !ok || kind != regSZ || !strings.EqualFold(command, string(doc.Contents)) {
		return false, nil
	}
	state, kind, ok, err := readRegistryBytes(approvedKeyPath, l.Name)
	if err != nil {
		return false, err
	}
	if !ok || kind != regBinary || len(state) == 0 {
		return true, nil
	}
	// The first byte is odd when Task Manager has the entry off.
	return state[0]&1 == 0, nil
}

// Start writes the Run value and clears what Task Manager recorded about it.
//
// The flag belongs to a user who turned the entry off there once. The
// application asking for the login start again is the user's answer to that,
// so the flag goes with it. The approval key does not exist on every machine,
// and its absence is not a failure: there is nothing to turn back on.
func (w *Windows) Start(l Launcher) error {
	doc, err := w.Document(l)
	if err != nil {
		return err
	}
	if err := writeRegistryString(runKeyPath, l.Name, string(doc.Contents)); err != nil {
		return err
	}
	if err := deleteRegistryValue(approvedKeyPath, l.Name); err != nil && !errors.Is(err, errValueNotFound) {
		return err
	}
	return nil
}

// Stop takes both values away. A Run value that is gone with a flag left
// behind is a start that does nothing — and a later Boku that wrote the value
// again would find itself off, with nothing on screen explaining why.
func (w *Windows) Stop(l Launcher) error {
	if err := deleteRegistryValue(runKeyPath, l.Name); err != nil && !errors.Is(err, errValueNotFound) {
		return err
	}
	if err := deleteRegistryValue(approvedKeyPath, l.Name); err != nil && !errors.Is(err, errValueNotFound) {
		return err
	}
	return nil
}

// regError tells the two "there is nothing here" answers from a real failure.
func regError(errno syscall.Errno) error {
	if errno == errnoFileNotFound {
		return errKeyNotFound
	}
	return errno
}

// missingValue reads RegQueryValueEx's "nothing there" as a value rather than
// an error: both a missing key and a missing value mean the start is off.
func missingValue(errno syscall.Errno) error {
	if errno == errnoFileNotFound || errno == errnoMoreData {
		return nil
	}
	return errno
}

// openKey opens a key of the current user, reporting a key that is not there
// as errKeyNotFound: a login start that was never written is a key that was
// never created.
func openKey(path string) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	if rc, _, _ := regOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(name)), 0,
		keyRead|keyWrite, uintptr(unsafe.Pointer(&key))); rc != 0 {
		return 0, regError(syscall.Errno(rc))
	}
	return key, nil
}

// readRegistryString reads a value of a key. ok is false when the key or the
// value is not there, which is not an error: the start is then simply off.
func readRegistryString(path, name string) (string, uint32, bool, error) {
	key, err := openKey(path)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	defer regCloseKey.Call(uintptr(key))

	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", 0, false, err
	}
	var kind, size uint32
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", 0, false, missingValue(syscall.Errno(rc))
	}
	if size == 0 {
		return "", kind, true, nil
	}
	// size counts bytes, and the value ends with a NUL: one more unit than
	// the value needs is room for both.
	buffer := make([]uint16, size/2+1)
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", 0, false, missingValue(syscall.Errno(rc))
	}
	return syscall.UTF16ToString(buffer), kind, true, nil
}

// readRegistryBytes reads a value that is not a string, which is how Task
// Manager's flag is stored.
func readRegistryBytes(path, name string) ([]byte, uint32, bool, error) {
	key, err := openKey(path)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	defer regCloseKey.Call(uintptr(key))

	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, 0, false, err
	}
	var kind, size uint32
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size))); rc != 0 {
		return nil, 0, false, missingValue(syscall.Errno(rc))
	}
	if size == 0 {
		return []byte{}, kind, true, nil
	}
	buffer := make([]byte, size)
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return nil, 0, false, missingValue(syscall.Errno(rc))
	}
	return buffer[:size], kind, true, nil
}

// writeRegistryString writes one string value, creating the key when it is
// not there. The key's own permissions are left to Windows.
func writeRegistryString(path, name, text string) error {
	keyPath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var key syscall.Handle
	var disposition uint32
	if rc, _, _ := regCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(keyPath)), 0, 0, 0,
		keyWrite|keyRead, 0, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition))); rc != 0 {
		return syscall.Errno(rc)
	}
	defer regCloseKey.Call(uintptr(key))

	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	data := syscall.StringToUTF16(text)
	if rc, _, _ := regSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0, regSZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2)); rc != 0 {
		return syscall.Errno(rc)
	}
	return nil
}

// deleteRegistryValue removes one value, reporting a key or value that is not
// there as errValueNotFound so the caller can say it had nothing to do.
func deleteRegistryValue(path, name string) error {
	key, err := openKey(path)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return errValueNotFound
		}
		return err
	}
	defer regCloseKey.Call(uintptr(key))
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if rc, _, _ := regDeleteValueW.Call(uintptr(key), uintptr(unsafe.Pointer(value))); rc != 0 {
		if syscall.Errno(rc) == errnoFileNotFound {
			return errValueNotFound
		}
		return syscall.Errno(rc)
	}
	return nil
}

// BinaryPath is the program a login start runs.
//
// It is not os.Executable. The installer's shortcut is the program's home: an
// update replaces the files the shortcut points at, and the next start runs
// them. A login start pointing anywhere else would be a second copy. The
// shortcut's target therefore answers first, and the running program answers
// when there is no shortcut to read — a development build, or an application
// unpacked by hand.
func BinaryPath() (string, error) {
	if link := applicationShortcutPath(); link != "" {
		if target := CleanShortcutTarget(ShortcutTarget(link)); target != "" {
			return target, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the executable: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}
