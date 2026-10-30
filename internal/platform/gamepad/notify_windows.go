//go:build windows

package gamepad

import (
	"sync"
	"syscall"
	"unsafe"
)

const (
	cmNotifyFilterTypeDeviceInterface = 0
	cmNotifyActionInterfaceArrival    = 0
	cmNotifyActionInterfaceRemoval    = 1
	maxDeviceIDLen                    = 200
)

var (
	cfgmgr32       = syscall.NewLazyDLL("cfgmgr32.dll")
	cmRegister     = cfgmgr32.NewProc("CM_Register_Notification")
	cmUnregister   = cfgmgr32.NewProc("CM_Unregister_Notification")
	hidNotifyThunk = syscall.NewCallback(hidNotify)

	notifyMu     sync.Mutex
	notifyLive   *notifyBox
	notifyHandle uintptr
)

// The union is wide enough for a device-instance id, which is what Windows
// measures cbSize against.
type cmNotifyFilter struct {
	cbSize     uint32
	flags      uint32
	filterType uint32
	reserved   uint32
	data       [maxDeviceIDLen]uint16
}

func listenHID(wake func()) (stop func(), ok bool) {
	if wake == nil {
		return nil, false
	}
	var class guid
	hidGetGuid.Call(uintptr(unsafe.Pointer(&class)))
	filter := cmNotifyFilter{
		filterType: cmNotifyFilterTypeDeviceInterface,
	}
	filter.cbSize = uint32(unsafe.Sizeof(filter))
	*(*guid)(unsafe.Pointer(&filter.data[0])) = class
	box := &notifyBox{wake: wake}
	notifyMu.Lock()
	notifyLive = box
	notifyMu.Unlock()
	var handle uintptr
	ret, _, _ := cmRegister.Call(
		uintptr(unsafe.Pointer(&filter)),
		0,
		hidNotifyThunk,
		uintptr(unsafe.Pointer(&handle)),
	)
	if ret != 0 || handle == 0 {
		notifyMu.Lock()
		notifyLive = nil
		notifyMu.Unlock()
		return nil, false
	}
	notifyMu.Lock()
	notifyHandle = handle
	notifyMu.Unlock()
	return func() {
		cmUnregister.Call(handle)
		notifyMu.Lock()
		if notifyHandle == handle {
			notifyLive = nil
			notifyHandle = 0
		}
		notifyMu.Unlock()
	}, true
}

type notifyBox struct {
	wake func()
}

func hidNotify(handle, _, action, _, _ uintptr) uintptr {
	if action != cmNotifyActionInterfaceArrival && action != cmNotifyActionInterfaceRemoval {
		return 0
	}
	notifyMu.Lock()
	box := notifyLive
	live := notifyHandle
	notifyMu.Unlock()
	if box == nil || box.wake == nil || (live != 0 && handle != live) {
		return 0
	}
	box.wake()
	return 0
}
