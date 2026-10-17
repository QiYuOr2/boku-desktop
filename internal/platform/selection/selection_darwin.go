//go:build darwin

package selection

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"boku/internal/domain"
)

// The selection is read through the Accessibility API first: the focused
// element publishes its selected text, and reading it disturbs neither the
// application nor the clipboard. That interface needs the user's permission
// in System Settings > Privacy & Security > Accessibility; without it the
// copy fallback below is the answer, which is why the two live together.
//
// The copy fallback is the same logic Windows uses (selection.go): press the
// platform's copy combination, watch the clipboard's write counter to learn
// whether anything was selected, read the text, and put the clipboard back.
// The only platform-specific parts are the key press and the counter.
//
// A system with neither — permission refused and an application that does not
// answer the copy — is the ordinary "the platform cannot say" answer, and the
// caller falls back to the clipboard it already has.
func New(opts Options) domain.SelectionDevice {
	return Device{
		clipboard:  opts.Clipboard,
		beforeCopy: opts.BeforeCopy,
		sequence:   opts.Sequence,
	}
}

// Device reads the selection through macOS' Accessibility API, and asks the
// application to copy it when that answers nothing.
type Device struct {
	// clipboard is what the copy fallback reads and puts back.
	clipboard domain.ClipboardDevice
	// beforeCopy is called just before the synthetic copy, so that the
	// clipboard tool can keep the borrowed value out of the history.
	beforeCopy func()
	// sequence is the clipboard's write counter. A device without one can
	// read what Accessibility publishes, but cannot tell a copy that selected
	// nothing from one that copied the same text again.
	sequence func() uint32
}

// SelectedText asks macOS what is selected in the application the user is
// working in.
//
// Accessibility is tried a few times, for the same reason Windows does it: a
// browser turns its accessibility tree on when it is first asked, and the
// first answer can be empty while the selection is right there on screen. A
// call that hangs is left behind after the reading's bound; the copy fallback
// is the older answer and is always at hand.
func (d Device) SelectedText(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(DefaultTimeout)
	if text := readUntilAnswered(ctx, readAXWithin, DefaultAttempts, DefaultRetryGap, deadline); text != "" {
		return text, nil
	}
	if d.clipboard != nil && d.sequence != nil && canPostEvents() && !secureInput() {
		if text := readWithACopy(ctx, d.clipboard, d.beforeCopy, d.sequence, pressCopy); text != "" {
			return text, nil
		}
	}
	return "", domain.ErrSelectionUnavailable
}

// readAXWithin is one reading of the selection on a thread of its own, waited
// for with a bound: the call belongs to another application's accessibility
// implementation, and may not return.
func readAXWithin(ctx context.Context, within time.Duration) string {
	done := make(chan string, 1)
	go func() { done <- readSelectionAX() }()
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

// readSelectionAX reads the selected text of the focused element, or answers
// nothing. A missing permission, an element that publishes no text, a
// selection of nothing and a failed call are all the same answer here.
func readSelectionAX() string {
	if !loadAX() || !axIsProcessTrusted() {
		return ""
	}
	system := axCreateSystemWide()
	if system == 0 {
		return ""
	}
	defer cfRelease(system)

	focused := axCopyAttribute(system, axFocusedElement)
	if focused == 0 {
		return ""
	}
	defer cfRelease(focused)

	selected := axCopyAttribute(focused, axSelectedText)
	if selected == 0 {
		return ""
	}
	defer cfRelease(selected)
	return strings.TrimSpace(cfString(selected))
}

// --- Accessibility and Core Foundation, as plain calls --------------------

const kCFStringEncodingUTF8 = 0x08000100

var (
	axOnce sync.Once
	axOK   bool

	cfRelease                         func(ref uintptr)
	cfDictionaryCreate                func(allocator uintptr, keys, values *uintptr, numValues int64, keyCallBacks, valueCallBacks uintptr) uintptr
	cfGetTypeID                       func(ref uintptr) uintptr
	cfStringGetTypeID                 func() uintptr
	cfStringCreateWithCString         func(allocator uintptr, cStr *byte, encoding uint32) uintptr
	cfStringGetLength                 func(str uintptr) int64
	cfStringGetMaximumSizeForEncoding func(length int64, encoding uint32) int64
	cfStringGetCString                func(str uintptr, buffer *byte, size int64, encoding uint32) uint8
	axCreateSystemWide                func() uintptr
	axCopyAttributeValue              func(element uintptr, attribute uintptr, value *uintptr) int32
	axIsProcessTrustedFunc            func() uint8
	axIsProcessTrustedWithOptionsFunc func(options uintptr) uint8

	axFocusedElement uintptr
	axSelectedText   uintptr

	axTrustedCheckOptionPrompt uintptr
	cfBooleanTrue              uintptr
	cfDictionaryKeyCallBacks   uintptr
	cfDictionaryValueCallBacks uintptr
)

func loadAX() bool {
	axOnce.Do(func() {
		coreFoundation, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		applicationServices, err := purego.Dlopen(
			"/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&cfDictionaryCreate, coreFoundation, "CFDictionaryCreate")
		purego.RegisterLibFunc(&cfGetTypeID, coreFoundation, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, coreFoundation, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfStringCreateWithCString, coreFoundation, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfStringGetLength, coreFoundation, "CFStringGetLength")
		purego.RegisterLibFunc(&cfStringGetMaximumSizeForEncoding, coreFoundation, "CFStringGetMaximumSizeForEncoding")
		purego.RegisterLibFunc(&cfStringGetCString, coreFoundation, "CFStringGetCString")
		purego.RegisterLibFunc(&axCreateSystemWide, applicationServices, "AXUIElementCreateSystemWide")
		purego.RegisterLibFunc(&axCopyAttributeValue, applicationServices, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axIsProcessTrustedFunc, applicationServices, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptionsFunc, applicationServices, "AXIsProcessTrustedWithOptions")

		axTrustedCheckOptionPrompt = dataReference(applicationServices, "kAXTrustedCheckOptionPrompt")
		cfBooleanTrue = dataReference(coreFoundation, "kCFBooleanTrue")
		cfDictionaryKeyCallBacks = dataAddress(coreFoundation, "kCFTypeDictionaryKeyCallBacks")
		cfDictionaryValueCallBacks = dataAddress(coreFoundation, "kCFTypeDictionaryValueCallBacks")

		axFocusedElement = newCFString("AXFocusedUIElement")
		axSelectedText = newCFString("AXSelectedText")
		axOK = axFocusedElement != 0 && axSelectedText != 0
	})
	return axOK
}

func axIsProcessTrusted() bool {
	return axIsProcessTrustedFunc != nil && axIsProcessTrustedFunc() != 0
}

// RequestSelectionAccess asks the system to show its Accessibility prompt. The
// app cannot grant itself the permission, and macOS does not even list it in
// System Settings until it asks: that is what this call does. The answer is
// whether the permission is granted now; false after the prompt means the user
// still has to turn the switch on.
func (d Device) RequestSelectionAccess() bool {
	if !loadAX() {
		return false
	}
	if axIsProcessTrusted() {
		return true
	}
	return requestAccessibilityPrompt()
}

// SelectionAccessGranted reports the current permission without asking for it.
func (d Device) SelectionAccessGranted() bool {
	return loadAX() && axIsProcessTrusted()
}

// requestAccessibilityPrompt is the prompt half, shared by the settings button
// and the first copy fallback. It returns the current permission, not the
// user's answer: the system does not tell an application what the user chose.
func requestAccessibilityPrompt() bool {
	if !loadAX() || axIsProcessTrustedWithOptionsFunc == nil {
		return false
	}
	if axTrustedCheckOptionPrompt == 0 || cfBooleanTrue == 0 ||
		cfDictionaryCreate == nil || cfDictionaryKeyCallBacks == 0 || cfDictionaryValueCallBacks == 0 {
		return false
	}
	keys := []uintptr{axTrustedCheckOptionPrompt}
	values := []uintptr{cfBooleanTrue}
	options := cfDictionaryCreate(0, &keys[0], &values[0], 1, cfDictionaryKeyCallBacks, cfDictionaryValueCallBacks)
	if options == 0 {
		return false
	}
	defer cfRelease(options)
	return axIsProcessTrustedWithOptionsFunc(options) != 0
}

// dataReference reads a Core Foundation object held in a global variable, such
// as kAXTrustedCheckOptionPrompt or kCFBooleanTrue.
func dataReference(handle uintptr, name string) uintptr {
	p, err := purego.Dlsym(handle, name)
	if err != nil || p == 0 {
		return 0
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

// dataAddress is the address of a global struct, such as the dictionary key
// callbacks; the C API wants the address, not the bytes it holds.
func dataAddress(handle uintptr, name string) uintptr {
	address, err := purego.Dlsym(handle, name)
	if err != nil {
		return 0
	}
	return address
}

func axCopyAttribute(element, attribute uintptr) uintptr {
	var value uintptr
	if axCopyAttributeValue(element, attribute, &value) != 0 {
		return 0
	}
	return value
}

// newCFString makes a Core Foundation string, with the NUL that the C
// constructor reads to. The two attribute names above are kept for the life of
// the process because they never change.
func newCFString(value string) uintptr {
	bytes := append([]byte(value), 0)
	ref := cfStringCreateWithCString(0, &bytes[0], kCFStringEncodingUTF8)
	runtime.KeepAlive(bytes)
	return ref
}

// cfString reads a Core Foundation string as a Go string.
func cfString(value uintptr) string {
	if value == 0 || cfStringGetLength == nil || cfGetTypeID == nil || cfStringGetTypeID == nil {
		return ""
	}
	if cfGetTypeID(value) != cfStringGetTypeID() {
		// An accessibility attribute can be something other than text; a
		// CFString call on it would be a crash rather than a wrong answer.
		return ""
	}
	length := cfStringGetLength(value)
	if length <= 0 {
		return ""
	}
	size := cfStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1
	if size <= 0 || size > 1<<20 {
		return ""
	}
	buffer := make([]byte, size)
	if cfStringGetCString(value, &buffer[0], size, kCFStringEncodingUTF8) == 0 {
		return ""
	}
	if index := bytes.IndexByte(buffer, 0); index >= 0 {
		buffer = buffer[:index]
	}
	return string(buffer)
}

// --- asking the application to copy ----------------------------------------

// The virtual key codes and the flags of a synthetic Command+C, from
// Carbon's Events.h and CoreGraphics' CGEventTypes.h.
const (
	keyCodeCommand = 0x37
	keyCodeShift   = 0x38
	keyCodeOption  = 0x3A
	keyCodeControl = 0x3B
	keyCodeC       = 0x08

	kCGEventFlagMaskCommand                 = 1 << 20
	kCGHIDEventTap                          = 0
	kCGEventSourceStateCombinedSessionState = 0
)

var (
	cgOnce sync.Once
	cgOK   bool

	cgEventCreateKeyboardEvent func(source uintptr, key uint16, down uint8) uintptr
	cgEventSetFlags            func(event uintptr, flags uint64)
	cgEventPost                func(tap uint32, event uintptr)
	cgEventSourceKeyState      func(state uint32, key uint16) uint8
	cgPreflightPostEventAccess func() uint8
)

func loadCoreGraphics() bool {
	cgOnce.Do(func() {
		coreGraphics, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		coreFoundation, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, coreGraphics, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventSetFlags, coreGraphics, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventPost, coreGraphics, "CGEventPost")
		purego.RegisterLibFunc(&cgEventSourceKeyState, coreGraphics, "CGEventSourceKeyState")
		cgPreflightPostEventAccess = dlsymFunc[func() uint8](coreGraphics, "CGPreflightPostEventAccess")
		cgOK = true
	})
	return cgOK
}

// dlsymFunc returns a callable symbol, or nil when this macOS version does not
// have it. It is used for the two access-check calls, which arrived in 10.15.
func dlsymFunc[T any](handle uintptr, name string) T {
	var zero T
	address, err := purego.Dlsym(handle, name)
	if err != nil {
		return zero
	}
	var fn T
	purego.RegisterFunc(&fn, address)
	return fn
}

// canPostEvents reports whether this process may post keyboard events.
//
// It is a check, not a request. Asking from the shortcut path happens while
// Boku is an accessory app (no window has been opened), and macOS does not
// show the Accessibility prompt for such a call: it records the app with the
// permission off instead. The one place that asks is the settings button,
// where the settings window makes the app active.
func canPostEvents() bool {
	if !loadCoreGraphics() {
		return false
	}
	return cgPreflightPostEventAccess == nil || cgPreflightPostEventAccess() != 0
}

// secureInput reports whether a secure input field has the keyboard. Copying
// a password into the clipboard to translate it is not what the user asked
// for, and the clipboard would keep it after the tool put the old value back
// only by accident.
func secureInput() bool {
	if secureEventInputEnabled == nil {
		return false
	}
	return secureEventInputEnabled() != 0
}

var secureEventInputEnabled func() uint8

func init() {
	carbon, err := purego.Dlopen(
		"/System/Library/Frameworks/Carbon.framework/Carbon",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		return
	}
	secureEventInputEnabled = dlsymFunc[func() uint8](carbon, "IsSecureEventInputEnabled")
}

// pressCopy presses Command+C, having first let go of whatever modifier the
// user is still holding: the shortcut that got here is one of those, and a
// copy made while Command and Shift are down is a copy the application may
// read as something else.
func pressCopy() {
	if !loadCoreGraphics() {
		return
	}
	releaseHeldModifiers()
	postKey(keyCodeCommand, true, kCGEventFlagMaskCommand)
	postKey(keyCodeC, true, kCGEventFlagMaskCommand)
	postKey(keyCodeC, false, kCGEventFlagMaskCommand)
	postKey(keyCodeCommand, false, 0)
}

// releaseHeldModifiers sends a key-up for every modifier that is still down.
func releaseHeldModifiers() {
	for _, key := range []uint16{keyCodeShift, keyCodeControl, keyCodeOption, keyCodeCommand} {
		if cgEventSourceKeyState(kCGEventSourceStateCombinedSessionState, key) != 0 {
			postKey(key, false, 0)
		}
	}
	time.Sleep(10 * time.Millisecond)
}

// postKey posts one keyboard event. The event is created autoreleased
// equivalent (+1), so it is released after posting.
func postKey(key uint16, down bool, flags uint64) {
	value := uint8(0)
	if down {
		value = 1
	}
	event := cgEventCreateKeyboardEvent(0, key, value)
	if event == 0 {
		return
	}
	if flags != 0 {
		cgEventSetFlags(event, flags)
	}
	cgEventPost(kCGHIDEventTap, event)
	cfRelease(event)
}
