//go:build windows

package httpc

import (
	"errors"
	"syscall"
	"unsafe"
)

// Windows 把系统网络对话框设置的代理存在注册表的同一键下。此处经 advapi32 直接读取：
// 本构建不能用 cgo，而读一个键的四个值也不需要额外库。
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

// platformProxy 读取 Windows 配置的代理。键或值不存在不值得让请求失败：此时只是没有可用的代理。
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

// readDWORD 读取键的一个 32 位值。
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

// readString 读取键的一个字符串值。缓冲区大小取自值本身，因为绕过列表可能很长。
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
	// size 以字节计，且值以 NUL 结尾：多留一个单位同时容纳两者。
	buffer := make([]uint16, size/2+1)
	if rc, _, _ := regQueryValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", errProxyUnreadable
	}
	return syscall.UTF16ToString(buffer), nil
}
