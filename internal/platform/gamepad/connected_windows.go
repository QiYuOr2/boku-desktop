//go:build windows

package gamepad

import (
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	genericRead        = 0x80000000
	genericWrite       = 0x40000000
	fileShareRead      = 0x1
	fileShareWrite     = 0x2
	openExisting       = 3
	fileFlagOverlapped = 0x40000000

	digcfPresent         = 0x2
	digcfDeviceInterface = 0x10

	usagePageDesktop = 0x01
	usageJoystick    = 0x04
	usageGamepad     = 0x05

	hidpStatusSuccess = 0x00110000
	errorIOPending    = 997
	waitObject0       = 0
)

var (
	setupapi             = syscall.NewLazyDLL("setupapi.dll")
	hid                  = syscall.NewLazyDLL("hid.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	cfgmgr               = syscall.NewLazyDLL("cfgmgr32.dll")
	hidGetGuid           = hid.NewProc("HidD_GetHidGuid")
	hidGetAttributes     = hid.NewProc("HidD_GetAttributes")
	hidGetPreparsed      = hid.NewProc("HidD_GetPreparsedData")
	hidFreePreparsed     = hid.NewProc("HidD_FreePreparsedData")
	hidGetCaps           = hid.NewProc("HidP_GetCaps")
	setupGetClassDevs    = setupapi.NewProc("SetupDiGetClassDevsW")
	setupEnumInterfaces  = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	setupInterfaceDetail = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	setupDestroy         = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	createFile           = kernel32.NewProc("CreateFileW")
	closeHandle          = kernel32.NewProc("CloseHandle")
	readFile             = kernel32.NewProc("ReadFile")
	createEvent          = kernel32.NewProc("CreateEventW")
	waitForSingle        = kernel32.NewProc("WaitForSingleObject")
	getOverlapped        = kernel32.NewProc("GetOverlappedResult")
	cancelIoEx           = kernel32.NewProc("CancelIoEx")
	xinput               = syscall.NewLazyDLL("xinput1_4.dll")
	xinputBattery        = xinput.NewProc("XInputGetBatteryInformation")
	cmParent             = cfgmgr.NewProc("CM_Get_Parent")
	cmDeviceID           = cfgmgr.NewProc("CM_Get_Device_IDW")
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type spDeviceInterfaceData struct {
	cbSize    uint32
	classGUID guid
	flags     uint32
	reserved  uintptr
}

type hidAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

type hidCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

type nativeOverlapped struct {
	internal     uintptr
	internalHigh uintptr
	offset       uint32
	offsetHigh   uint32
	hEvent       uintptr
}

type listed struct {
	pad    Pad
	usage  uint16
	id     string
	xinput bool
	wired  bool
}

// Connected lists HID gamepads and joysticks. A DualSense or a Switch Pro
// contributes its charge byte only over Bluetooth. An Xbox-protocol pad
// contributes an XInput charge only when the report is not the wired one.
func Connected() []Pad {
	found := listPads()
	if found == nil {
		note(nil)
		return nil
	}
	sort.Slice(found, func(i, j int) bool {
		if kindOrder(found[i].pad.Kind) != kindOrder(found[j].pad.Kind) {
			return kindOrder(found[i].pad.Kind) < kindOrder(found[j].pad.Kind)
		}
		return found[i].id < found[j].id
	})
	assignXInput(found)
	out := make([]Pad, len(found))
	for i := range found {
		out[i] = found[i].pad
	}
	note(out)
	return out
}

func listPads() []listed {
	var class guid
	hidGetGuid.Call(uintptr(unsafe.Pointer(&class)))
	devs, _, _ := setupGetClassDevs.Call(
		uintptr(unsafe.Pointer(&class)),
		0, 0,
		digcfPresent|digcfDeviceInterface,
	)
	if devs == 0 || devs == ^uintptr(0) {
		return nil
	}
	defer setupDestroy.Call(devs)

	var found []listed
	for i := uint32(0); ; i++ {
		data := spDeviceInterfaceData{cbSize: uint32(unsafe.Sizeof(spDeviceInterfaceData{}))}
		ok, _, _ := setupEnumInterfaces.Call(
			devs,
			0,
			uintptr(unsafe.Pointer(&class)),
			uintptr(i),
			uintptr(unsafe.Pointer(&data)),
		)
		if ok == 0 {
			break
		}
		path, devInst := interfacePath(devs, &data)
		if path == "" {
			continue
		}
		item, okPad := inspect(path, wiredConnection(instanceChain(devInst)))
		if !okPad {
			continue
		}
		found = mergePad(found, item)
	}
	if found == nil {
		return []listed{}
	}
	return found
}

func mergePad(found []listed, item listed) []listed {
	for i := range found {
		if found[i].id != item.id {
			continue
		}
		if item.usage == usageGamepad && found[i].usage != usageGamepad {
			found[i] = item
		} else if item.pad.Battery.Known && !found[i].pad.Battery.Known {
			found[i].pad.Battery = item.pad.Battery
		}
		return found
	}
	return append(found, item)
}

type spDevInfoData struct {
	cbSize    uint32
	classGUID guid
	devInst   uint32
	reserved  uintptr
}

func interfacePath(devs uintptr, data *spDeviceInterfaceData) (string, uint32) {
	var need uint32
	setupInterfaceDetail.Call(devs, uintptr(unsafe.Pointer(data)), 0, 0, uintptr(unsafe.Pointer(&need)), 0)
	if need < 8 || need > 4096 {
		return "", 0
	}
	buf := make([]byte, need)
	// On 64-bit Windows cbSize is 8, but the path begins at byte 4.
	cbSize := uint32(8)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		cbSize = 6
	}
	*(*uint32)(unsafe.Pointer(&buf[0])) = cbSize
	info := spDevInfoData{cbSize: uint32(unsafe.Sizeof(spDevInfoData{}))}
	ok, _, _ := setupInterfaceDetail.Call(
		devs,
		uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(need),
		uintptr(unsafe.Pointer(&need)),
		uintptr(unsafe.Pointer(&info)),
	)
	if ok == 0 {
		return "", 0
	}
	return utf16From(buf, 4), info.devInst
}

func instanceChain(devInst uint32) []string {
	var ids []string
	for i := 0; i < 8 && devInst != 0; i++ {
		buf := make([]uint16, 200)
		r, _, _ := cmDeviceID.Call(uintptr(devInst), uintptr(unsafe.Pointer(&buf[0])), 200, 0)
		if r == 0 {
			ids = append(ids, syscall.UTF16ToString(buf))
		}
		var parent uint32
		r, _, _ = cmParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(devInst), 0)
		if r != 0 {
			break
		}
		devInst = parent
	}
	return ids
}

func inspect(path string, wired bool) (listed, bool) {
	handle := openHID(path)
	if handle == 0 || handle == ^uintptr(0) {
		return listed{}, false
	}
	defer closeHandle.Call(handle)

	attr := hidAttributes{Size: uint32(unsafe.Sizeof(hidAttributes{}))}
	ok, _, _ := hidGetAttributes.Call(handle, uintptr(unsafe.Pointer(&attr)))
	if ok == 0 {
		return listed{}, false
	}
	var preparsed uintptr
	ok, _, _ = hidGetPreparsed.Call(handle, uintptr(unsafe.Pointer(&preparsed)))
	if ok == 0 || preparsed == 0 {
		return listed{}, false
	}
	defer hidFreePreparsed.Call(preparsed)
	var caps hidCaps
	status, _, _ := hidGetCaps.Call(preparsed, uintptr(unsafe.Pointer(&caps)))
	if status != hidpStatusSuccess {
		return listed{}, false
	}
	if caps.UsagePage != usagePageDesktop || (caps.Usage != usageGamepad && caps.Usage != usageJoystick) {
		return listed{}, false
	}
	item := listed{
		pad:    Pad{Kind: kindOf(attr.VendorID, attr.ProductID)},
		usage:  caps.Usage,
		id:     deviceID(path),
		xinput: strings.Contains(strings.ToLower(path), "&ig_"),
		wired:  wired,
	}
	var battery Battery
	var known bool
	if !wired {
		switch {
		case readsDualSenseBattery(attr.VendorID, attr.ProductID):
			battery, known = batteryFromReport(readReport(handle, caps.InputReportByteLength))
		case readsSwitchBattery(attr.VendorID, attr.ProductID):
			battery, known = batteryFromSwitchReport(readReport(handle, caps.InputReportByteLength))
		}
	}
	if known {
		item.pad.Battery = battery
	}
	return item, true
}

// assignXInput pairs each XInput interface with a connected user, in index
// order. A wired report leaves that pad without a charge.
func assignXInput(found []listed) {
	need := false
	for i := range found {
		if found[i].xinput {
			need = true
			break
		}
	}
	if !need {
		return
	}
	slots := xinputSlots()
	n := 0
	for i := range found {
		if !found[i].xinput {
			continue
		}
		if n >= len(slots) {
			return
		}
		if slots[n].Known {
			found[i].pad.Battery = slots[n]
		}
		n++
	}
}

func xinputSlots() []Battery {
	var slots []Battery
	for user := uintptr(0); user < 4; user++ {
		var info [2]byte
		r, _, _ := xinputBattery.Call(user, 0, uintptr(unsafe.Pointer(&info[0])))
		if r != 0 {
			continue
		}
		battery, _ := batteryFromXInput(info[0], info[1])
		slots = append(slots, battery)
	}
	return slots
}

func openHID(path string) uintptr {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	handle, _, _ := createFile.Call(
		uintptr(unsafe.Pointer(ptr)),
		genericRead|genericWrite,
		fileShareRead|fileShareWrite,
		0,
		openExisting,
		fileFlagOverlapped,
		0,
	)
	return handle
}

func readReport(handle uintptr, length uint16) []byte {
	n := int(length)
	if n < 64 {
		n = 64
	}
	if n > 547 {
		n = 547
	}
	buf := make([]byte, n)
	event, _, _ := createEvent.Call(0, 1, 0, 0)
	if event == 0 {
		return nil
	}
	defer closeHandle.Call(event)
	var ov nativeOverlapped
	ov.hEvent = event
	var got uint32
	ok, _, err := readFile.Call(
		handle,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&got)),
		uintptr(unsafe.Pointer(&ov)),
	)
	if ok == 0 {
		errno, _ := err.(syscall.Errno)
		if errno != errorIOPending {
			return nil
		}
		wait, _, _ := waitForSingle.Call(event, 150)
		if wait != waitObject0 {
			cancelIoEx.Call(handle, uintptr(unsafe.Pointer(&ov)))
			waitForSingle.Call(event, 50)
			return nil
		}
		ok, _, _ = getOverlapped.Call(handle, uintptr(unsafe.Pointer(&ov)), uintptr(unsafe.Pointer(&got)), 0)
		if ok == 0 {
			return nil
		}
	}
	if int(got) > len(buf) {
		return nil
	}
	return buf[:got]
}

func deviceID(path string) string {
	path = strings.ToLower(path)
	if i := strings.Index(path, "#{"); i >= 0 {
		path = path[:i]
	}
	if i := strings.LastIndex(path, "&"); i >= 0 && digits(path[i+1:]) {
		path = path[:i]
	}
	return path
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func utf16From(buf []byte, off int) string {
	if off < 0 || off+2 > len(buf) {
		return ""
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[off])), (len(buf)-off)/2)
	n := 0
	for n < len(units) && units[n] != 0 {
		n++
	}
	return string(utf16.Decode(units[:n]))
}
