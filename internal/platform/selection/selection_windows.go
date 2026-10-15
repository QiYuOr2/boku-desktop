//go:build windows

package selection

import (
	"context"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"boku/internal/domain"
)

// The selection is read through UI Automation, the interface a screen reader
// reads a control with. It is called through its vtable directly: there is no
// cgo in this build, and the client-side API is COM.
//
// The vtable slots below are from uiautomationclient.h. A COM vtable is
// ordered rather than named, so a slot number is the only way to reach a
// method; they are written down here with the interface they belong to, and
// the layout is checked by what a slot answers rather than trusted — a slot
// that is off by one answers a method with the wrong arguments, which is why
// get_CurrentClassName (slot 30) returning "Edit" for an edit control is what
// the reading of the header was checked against.
const (
	slotGetFocusedElement = 8  // IUIAutomation::GetFocusedElement
	slotGetCurrentPattern = 16 // IUIAutomationElement::GetCurrentPattern
	slotGetSelection      = 5  // IUIAutomationTextPattern::GetSelection
	slotRangeArrayLength  = 3  // IUIAutomationTextRangeArray::get_Length
	slotRangeArrayElement = 4  // IUIAutomationTextRangeArray::GetElement
	slotRangeText         = 12 // IUIAutomationTextRange::GetText
	slotRelease           = 2  // IUnknown::Release
)

// textPatternID is UIA_TextPatternId: the control pattern that exposes a
// control's text and what is selected in it. A control that does not publish
// its text — a canvas, a game, the bridge a plain edit control is exposed
// through — has none, which is the ordinary "the clipboard will have to
// answer" case.
const textPatternID = 10014

// comPtr is a COM interface pointer. It is an unsafe.Pointer rather than a
// uintptr so that the vtable and the string a BSTR points at can be read
// without converting an integer back into a pointer, which is the conversion
// that is not allowed.
type comPtr = unsafe.Pointer

// New returns the device: UI Automation reads what publishes its text, and a
// synthetic copy is what asks the applications that publish nothing.
func New(opts Options) domain.SelectionDevice {
	return Device{clipboard: opts.Clipboard, beforeCopy: opts.BeforeCopy}
}

// Device reads the selection through Windows' UI Automation, and asks the
// application to copy it when UI Automation has nothing to say.
type Device struct {
	// clipboard is what the copy fallback reads and puts back, and is nil in
	// a build that was given none.
	clipboard domain.ClipboardDevice
	// beforeCopy is called just before the synthetic copy, so that the
	// clipboard tool can keep the copied value out of the history.
	beforeCopy func()
}

// SelectedText asks the system what is selected in the application the user
// is working in.
//
// It is not one call. The applications being read turn their accessibility on
// when they are first asked — a browser builds its tree a moment after the
// first query arrives — so a single reading of a browser answers "nothing is
// selected" when a selection is right there on screen. The reading is therefore
// attempted a few times (DefaultAttempts, DefaultRetryGap), and each attempt
// runs on a thread of its own and is waited for with a bound: a call into
// another application's UI Automation provider is a call that can hang, and the
// tool has an older answer — the clipboard — it can fall back on rather than
// keep the user waiting. A reading left behind finishes by itself and gives its
// thread back.
//
// When UI Automation has nothing to say, the application is asked to copy
// (see readWithACopy): that is what reaches the applications that do not
// publish their text — a chat window, the Windows 11 Notepad — and it is the
// one path here that the user can notice, because it borrows the clipboard and
// gives it back.
func (d Device) SelectedText(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(DefaultTimeout)
	if text := readUntilAnswered(ctx, readWithin, DefaultAttempts, DefaultRetryGap, deadline); text != "" {
		return text, nil
	}
	if d.clipboard != nil && safeToCopy() {
		if text := readWithACopy(ctx, d.clipboard, d.beforeCopy, clipboardSequence, pressCopy); text != "" {
			return text, nil
		}
	}
	// Every attempt said the same thing, which is the ordinary answer for an
	// application that publishes no text and for nothing being selected at
	// all. It is not a failure: the caller asks the clipboard.
	return "", domain.ErrSelectionUnavailable
}

// readWithin is one reading of the selection — an attempt, as readUntilAnswered
// asks for it: the reading runs on a thread of its own and this waits for it
// with a bound, because the call it makes belongs to another application.
func readWithin(ctx context.Context, within time.Duration) string {
	done := make(chan string, 1)
	go func() {
		text, _ := readSelection()
		done <- text
	}()
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case text := <-done:
		return text
	case <-ctx.Done():
		return ""
	case <-timer.C:
		return ""
	}
}

// readSelection takes the answer on a thread it has initialized for COM: COM
// belongs to the thread that called CoInitializeEx, and a goroutine of the
// translation may be any thread at all.
func readSelection() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninitialize, ok := comInit()
	if !ok {
		return "", domain.ErrSelectionUnavailable
	}
	defer uninitialize()

	automation := createAutomation()
	if automation == nil {
		return "", domain.ErrSelectionUnavailable
	}
	defer release(automation)

	// The element that has the keyboard is the one the user is working in,
	// and the one whose selection they mean.
	focused, _ := out(automation, slotGetFocusedElement)
	return selectionOf(focused)
}

// selectionOf reads the selected text of one element: the control pattern
// that publishes text, the ranges it says are selected, and the text of the
// first of them.
//
// Nothing here is an error the caller has to tell apart. An element that is
// nil, a control that does not publish its text, a selection of nothing and a
// call that failed are all "the platform cannot say", which is answered with
// ErrSelectionUnavailable so the caller can fall back to the clipboard.
func selectionOf(element comPtr) (string, error) {
	if element == nil {
		// Nothing has the keyboard: no application is being worked in, so
		// there is nothing selected anywhere.
		return "", domain.ErrSelectionUnavailable
	}
	defer release(element)

	pattern, _ := out(element, slotGetCurrentPattern, textPatternID)
	if pattern == nil {
		// The control in front does not publish its text. This is the case
		// the clipboard has to answer.
		return "", domain.ErrSelectionUnavailable
	}
	defer release(pattern)

	ranges, _ := out(pattern, slotGetSelection)
	if ranges == nil {
		return "", domain.ErrSelectionUnavailable
	}
	defer release(ranges)

	var count int32
	if hr := call(ranges, slotRangeArrayLength, uintptr(unsafe.Pointer(&count))); failed(hr) || count <= 0 {
		// A selection of nothing: the user has a caret and no range, which is
		// what an application answers when the shortcut is pressed with
		// nothing selected.
		return "", domain.ErrSelectionUnavailable
	}
	span, _ := out(ranges, slotRangeArrayElement, 0)
	if span == nil {
		return "", domain.ErrSelectionUnavailable
	}
	defer release(span)

	// -1 asks for the whole range rather than its first N characters: what a
	// selection is worth translating is the whole of it.
	var text comPtr
	if hr := call(span, slotRangeText, ^uintptr(0), uintptr(unsafe.Pointer(&text))); failed(hr) {
		return "", domain.ErrSelectionUnavailable
	}
	selected := strings.TrimSpace(bstr(text))
	if selected == "" {
		return "", domain.ErrSelectionUnavailable
	}
	return selected, nil
}

// --- COM and UI Automation, as plain calls ---------------------------------

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")

	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
	sysFreeString    = oleaut32.NewProc("SysFreeString")
)

// COINIT_APARTMENTTHREADED and CLSCTX_INPROC_SERVER, from objbase.h.
const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
)

// CLSID_CUIAutomation and IID_IUIAutomation, from uiautomationclient.h.
var (
	clsidCUIAutomation = guid{0xFF48DBA4, 0x60EF, 0x4201, [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation   = guid{0x30CBE57D, 0xD9D0, 0x452A, [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
)

// guid is a COM globally unique identifier, laid out as the platform lays it
// out: three fields, then the eight bytes written in order.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// comInit initializes COM on the calling thread and returns what puts it
// back. A thread that was already initialized — by the application itself, or
// by another apartment — is not initialized again, and is left as it was:
// this reading of the selection does not own the thread's apartment.
func comInit() (func(), bool) {
	hr, _, _ := coInitializeEx.Call(0, coinitApartmentThreaded)
	switch uint32(hr) {
	case 0: // S_OK: this call initialized the apartment.
		return func() { coUninitialize.Call() }, true
	case 1: // S_FALSE: already initialized here.
		return func() {}, true
	case rpcEChangedMode:
		// The thread is in another apartment already, which UI Automation
		// works from just as well.
		return func() {}, true
	default:
		return func() {}, false
	}
}

// createAutomation builds the UI Automation client, or answers nil.
func createAutomation() comPtr {
	var automation comPtr
	hr, _, _ := coCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&automation)),
	)
	if failed(hr) {
		return nil
	}
	return automation
}

// call invokes the method in a vtable slot of an interface pointer.
func call(iface comPtr, slot int, args ...uintptr) uintptr {
	vtable := *(*comPtr)(iface)
	method := *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	result, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(iface)}, args...)...)
	return result
}

// out invokes a method that answers through a pointer, and returns both what
// it answered with and its HRESULT.
func out(iface comPtr, slot int, args ...uintptr) (comPtr, uintptr) {
	var result comPtr
	callArgs := append(append([]uintptr{}, args...), uintptr(unsafe.Pointer(&result)))
	return result, call(iface, slot, callArgs...)
}

// release drops a reference to an interface, which is what says a COM object
// is no longer wanted. UI Automation hands out a reference per answer, and a
// reading that kept them would keep a provider in another process alive.
func release(iface comPtr) {
	if iface != nil {
		call(iface, slotRelease)
	}
}

// failed reports whether an HRESULT is a failure.
func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

// bstr reads a BSTR — the string type COM answers with — as a Go string, and
// frees it. A nil pointer is an empty string, and freeing one is allowed:
// SysFreeString does nothing with it.
func bstr(value comPtr) string {
	defer sysFreeString.Call(uintptr(value))
	return decodeBSTR(value)
}

// decodeBSTR reads the layout of a BSTR: its length in bytes is the four
// bytes before the pointer, and its characters are UTF-16.
//
// It is separate from bstr so that it can be tested without COM: a test lays
// those bytes out itself and reads them back.
func decodeBSTR(value comPtr) string {
	if value == nil {
		return ""
	}
	bytes := *(*int32)(unsafe.Add(value, -4))
	if bytes <= 0 {
		return ""
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(value), bytes/2)))
}

// --- asking the application to copy ----------------------------------------

// The key codes and record flags of a synthetic copy, from winuser.h.
const (
	vkShift        = 0x10
	vkControl      = 0x11
	vkMenu         = 0x12
	vkLWin         = 0x5B
	vkRWin         = 0x5C
	vkC            = 0x43
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
)

// inputRecord is one INPUT of SendInput, laid out the way the platform lays it
// out. It is a record rather than a union of mouse, keyboard and hardware input
// because only the keyboard half is used here: the union is as large as its
// largest member — 32 bytes on a 64-bit build — and a record SendInput is told
// is a different size is refused.
type inputRecord struct {
	kind  uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
	_     [8]byte
}

// user32 is the window manager: the foreground window, the clipboard's write
// counter and the synthetic key press all come from it.
var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procSendInput                  = user32.NewProc("SendInput")
)

// clipboardSequence is the clipboard's own write counter: the system raises it
// every time the clipboard is written to, whoever wrote it. It is what says
// whether an application answered a copy at all — an application with nothing
// selected answers with nothing, and the counter does not move.
func clipboardSequence() uint32 {
	value, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(value)
}

// safeToCopy reports whether the application in front may be asked to copy.
//
// Two windows may not. This application's own: asking it to copy would be a
// copy of nothing, and it is not the window the user selected in. And a
// console: there the copy combination is a signal that interrupts whatever is
// running, not a copy.
func safeToCopy() bool {
	foreground, _, _ := procGetForegroundWindow.Call()
	if foreground == 0 {
		return false
	}
	var owner uint32
	procGetWindowThreadProcessID.Call(foreground, uintptr(unsafe.Pointer(&owner)))
	if owner == uint32(os.Getpid()) {
		return false
	}
	switch windowClass(foreground) {
	case "ConsoleWindowClass", "CASCADIA_HOSTING_WINDOW_CLASS", "mintty":
		return false
	}
	return true
}

// windowClass is the class name of a window: what a console is told apart from
// an editor by.
func windowClass(hwnd uintptr) string {
	buffer := make([]uint16, 256)
	length, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer[:length])
}

// pressCopy presses Ctrl+C, having first let go of whatever the user is still
// holding: the combination that got here is one of those, and a copy made while
// Ctrl and Shift are down is a copy the application may read as something else.
func pressCopy() {
	releaseHeldModifiers()
	sendKey(vkControl, false)
	sendKey(vkC, false)
	sendKey(vkC, true)
	sendKey(vkControl, true)
}

// releaseHeldModifiers sends a key-up for every modifier, which is what the
// other desktop translation tools do before a synthetic copy: a modifier the
// user is still holding would otherwise stay down through it.
func releaseHeldModifiers() {
	for _, key := range []uint16{vkShift, vkControl, vkMenu, vkLWin, vkRWin} {
		sendKey(key, true)
	}
	time.Sleep(10 * time.Millisecond)
}

// sendKey presses or releases one key. The size is what SendInput checks before
// it presses anything.
func sendKey(vk uint16, up bool) {
	record := inputRecord{kind: inputKeyboard, vk: vk}
	if up {
		record.flags = keyeventfKeyUp
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&record)), unsafe.Sizeof(record))
}
