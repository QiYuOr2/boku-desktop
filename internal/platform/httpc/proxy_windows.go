//go:build windows

package httpc

import (
	"errors"
	"syscall"
	"unsafe"
)

// Windows keeps the proxy its own network dialog sets in the registry, in the
// same key the dialog writes. It is read here through advapi32 directly: there
// is no cgo in this build, and reading four values of one key does not need a
// library.
const (
	internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	hkeyCurrentUser     = 0x80000001
	keyRead             = 0x20019

	regSZ       = 1
	regExpandSZ = 2
	regDWORD    = 4
)

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	regOpenKeyExW      = advapi32.NewProc("RegOpenKeyExW")
	regQueryValueExW   = advapi32.NewProc("RegQueryValueExW")
	regCloseKeyProc    = advapi32.NewProc("RegCloseKey")
	errProxyUnreadable = errors.New("httpx: the system proxy setting could not be read")
)

// platformProxy reads the proxy Windows is configured with. A key or a value
// that is not there is not an error worth failing a request over: the answer is
// then simply that there is no proxy to use.
func platformProxy() (proxySettings, error) {
	name, err := syscall.UTF16PtrFromString(internetSettingsKey)
	if err != nil {
		return proxySettings{}, err
	}
	var key syscall.Handle
	if rc, _, _ := regOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(name)), 0, keyRead, uintptr(unsafe.Pointer(&key))); rc != 0 {
		return proxySettings{}, nil
	}
	defer regCloseKeyProc.Call(uintptr(key))

	enabled, err := readDWORD(key, "ProxyEnable")
	if err != nil {
		return proxySettings{}, nil
	}
	server, err := readString(key, "ProxyServer")
	if err != nil {
		return proxySettings{}, nil
	}
	bypass, _ := readString(key, "ProxyOverride")
	return proxySettings{
		Enabled: enabled != 0,
		Server:  server,
		Bypass:  splitBypass(bypass),
	}, nil
}

// readDWORD reads one 32-bit value of a key.
func readDWORD(key syscall.Handle, name string) (uint32, error) {
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var (
		kind uint32
		size uint32 = 4
		data uint32
	)
	if rc, _, _ := regQueryValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return 0, errProxyUnreadable
	}
	if kind != regDWORD {
		return 0, errProxyUnreadable
	}
	return data, nil
}

// readString reads one string value of a key. The buffer is sized from the
// value itself, because a bypass list is as long as the user made it.
func readString(key syscall.Handle, name string) (string, error) {
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var (
		kind uint32
		size uint32
	)
	if rc, _, _ := regQueryValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", errProxyUnreadable
	}
	if kind != regSZ && kind != regExpandSZ {
		return "", errProxyUnreadable
	}
	if size == 0 {
		return "", nil
	}
	// size counts bytes, and the value ends with a NUL: one more unit than
	// the value needs is room for both.
	buffer := make([]uint16, size/2+1)
	if rc, _, _ := regQueryValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", errProxyUnreadable
	}
	return syscall.UTF16ToString(buffer), nil
}
