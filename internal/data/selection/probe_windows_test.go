//go:build windows

package selection

// This file is a throwaway by-hand probe, not a test of the package: it does
// not assert anything about the selection. It builds its own window with a
// known selection in it, then asks UI Automation what has the keyboard and
// what is selected, twice — once while the text control has the keyboard, and
// once after a second, empty window has taken it.
//
// What it is for: the reading of the selection is a call into another
// application's UI Automation provider, and what that provider answers when
// the keyboard has moved out from under the selection is the thing that
// decides whether a translated card can be shown without the selection being
// lost. That cannot be answered by an assertion — it is answered by the
// platform, so the probe prints what the platform said, verbatim, HRESULTs
// included.
//
// Run it with:
//
//	$env:BOKU_SELECTION_PROBE=1
//	go test ./internal/data/selection -run TestSelectionProbeByHand -v
func TestSelectionProbeByHand(t *testing.T) {
	if os.Getenv(probeEnv) != "1" {
		t.Skip("set BOKU_SELECTION_PROBE=1 to run the by-hand UI Automation probe")
	}

	// The probe creates windows and pumps their messages, which COM and the
	// window manager both tie to one thread. The body therefore runs on a
	// thread of its own and is watched: a call into another application's UI
	// Automation provider is a call that can hang.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	done := make(chan error, 1)
	go func() { done <- probeRun() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("probe could not run: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("watchdog: the probe did not return within 20s (a UI Automation or window call is stuck)")
	}
}

// --- the probe itself -------------------------------------------------------

// probeResult is what one state of the probe saw.
type probeResult struct {
	class string
	hrGet uintptr
	hrCls uintptr
	text  string
	err   error
}

// probeRun builds the two windows, asks UI Automation about each in turn, and
// prints everything it saw.
func probeRun() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if _, ok := comInit(); !ok {
		return errProbe("CoInitializeEx(COINIT_APARTMENTTHREADED) failed")
	}
	automation := createAutomation()
	if automation == nil {
		return errProbe("CoCreateInstance(CLSID_CUIAutomation) failed")
	}
	defer release(automation)
	fmt.Printf("IUIAutomation = %p\n", automation)

	class, err := probeLoadRichEdit()
	if err != nil {
		return err
	}
	fmt.Printf("rich edit window class = %s\n", class)

	instance, _, _ := probeGetModuleHandleW.Call(0)
	className := probeU16(fmt.Sprintf("BokuSelectionProbeClass%d", os.Getpid()))
	wc := probeWndClassExW{
		cbSize:        uint32(unsafe.Sizeof(probeWndClassExW{})),
		lpfnWndProc:   probeWindowProc,
		hInstance:     instance,
		hbrBackground: 6, // COLOR_WINDOW + 1
		lpszClassName: className,
	}
	atom, _, callErr := probeRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return errProbe("RegisterClassExW failed: " + probeErrText(callErr))
	}
	fmt.Printf("RegisterClassExW atom = 0x%X\n", atom)

	// --- the window the selection lives in ----------------------------------
	title := probeU16("boku-selection-probe")
	parent, _, callErr := probeCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		probeWSOverlappedWindow|probeWSVisible,
		100, 100, 680, 260,
		0, 0, instance, 0,
	)
	if parent == 0 {
		return errProbe("CreateWindowExW(parent) failed: " + probeErrText(callErr))
	}
	empty := probeU16("")
	editClass := probeU16(class)
	edit, _, callErr := probeCreateWindowExW.Call(
		probeWSExClientEdge,
		uintptr(unsafe.Pointer(editClass)),
		uintptr(unsafe.Pointer(empty)),
		probeWSChild|probeWSVisible|probeWSVScroll|probeESMultiline|probeESNoHideSel,
		10, 10, 640, 180,
		parent, 0, instance, 0,
	)
	if edit == 0 {
		return errProbe("CreateWindowExW(rich edit) failed: " + probeErrText(callErr))
	}
	fmt.Printf("parent hwnd = 0x%X  rich edit hwnd = 0x%X\n", parent, edit)

	const sentence = "The quick brown fox jumps over the lazy dog"
	body := probeU16(sentence)
	ok, _, _ := probeSetWindowTextW.Call(edit, uintptr(unsafe.Pointer(body)))
	fmt.Printf("SetWindowTextW(%q) = %d\n", sentence, ok)

	fmt.Println("=== STATE A: the rich edit holds the selection ===")
	probeShowWindow.Call(parent, probeSWShow)
	probeShowWindow.Call(edit, probeSWShow)
	probeActivate(parent, edit)
	probePump(400*time.Millisecond, "A")
	probePrintKeyboard()
	sent, _, _ := probeSendMessageW.Call(edit, probeEMSetSel, 10, 19)
	fmt.Printf("SendMessageW(edit, EM_SETSEL, 10, 19) = %d  (chars 10..19 = %q)\n",
		sent, sentence[10:19])
	probePump(150*time.Millisecond, "A-after-select")

	a := probeReadState(automation, "A")

	// --- the window that takes the keyboard away ----------------------------
	fmt.Println("=== STATE B: a plain window takes the keyboard ===")
	cardTitle := probeU16("boku-card-mimic")
	card, _, callErr := probeCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(cardTitle)),
		probeWSOverlappedWindow|probeWSVisible,
		140, 140, 380, 200,
		0, 0, instance, 0,
	)
	if card == 0 {
		return errProbe("CreateWindowExW(card) failed: " + probeErrText(callErr))
	}
	probeShowWindow.Call(card, probeSWShow)
	probeActivate(card, 0)
	probePump(400*time.Millisecond, "B")
	probePrintKeyboard()
	b := probeReadState(automation, "B")

	// B2: the same, with the keyboard put on the card by hand. A card window
	// that steals the keyboard does this; the probe reports both so the two
	// are not confused.
	fmt.Println("=== STATE B2: the card is also given focus explicitly ===")
	probeActivate(card, card)
	probePump(400*time.Millisecond, "B2")
	probePrintKeyboard()
	b2 := probeReadState(automation, "B2")

	fmt.Println("=== SUMMARY ===")
	fmt.Printf("STATE A (richedit focused) = %s\n", probeDescribe(a))
	fmt.Printf("STATE B (plain window focused) = %s\n", probeDescribe(b))
	fmt.Printf("STATE B2 (plain window focused + SetFocus) = %s\n", probeDescribe(b2))

	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	runtime.KeepAlive(empty)
	runtime.KeepAlive(editClass)
	runtime.KeepAlive(body)
	runtime.KeepAlive(cardTitle)
	return nil
}

// probeReadState asks UI Automation what has the keyboard and what is
// selected in it, and prints every call it made.
func probeReadState(automation comPtr, label string) probeResult {
	result := probeResult{}
	foreground := probeCall0(probeGetForegroundWindow)
	focus := probeCall0(probeGetFocus)
	fmt.Printf("[%s] GetForegroundWindow = 0x%X class=%q\n", label,
		foreground, probeClassName(foreground))
	fmt.Printf("[%s] GetFocus            = 0x%X class=%q\n", label,
		focus, probeClassName(focus))

	element, hr := out(automation, slotGetFocusedElement)
	result.hrGet = hr
	fmt.Printf("[%s] IUIAutomation::GetFocusedElement hr=0x%08X failed=%v element=%p\n",
		label, uint32(hr), failed(hr), element)
	if element == nil {
		result.class = "<no focused element>"
		result.text, result.err = readSelection()
		fmt.Printf("[%s] readSelection() = %q, err = %v\n", label, result.text, result.err)
		return result
	}
	defer release(element)

	class, hrCls := out(element, slotClassName)
	result.hrCls = hrCls
	result.class = bstr(class)
	fmt.Printf("[%s] get_CurrentClassName (slot %d) hr=0x%08X failed=%v -> %q\n",
		label, slotClassName, uint32(hrCls), failed(hrCls), result.class)

	name, hrName := out(element, slotName)
	fmt.Printf("[%s] get_CurrentName (slot %d) hr=0x%08X failed=%v -> %q\n",
		label, slotName, uint32(hrName), failed(hrName), bstr(name))

	var controlType int32
	hrType := call(element, slotControlType, uintptr(unsafe.Pointer(&controlType)))
	fmt.Printf("[%s] get_CurrentControlType (slot %d) hr=0x%08X failed=%v -> %d\n",
		label, slotControlType, uint32(hrType), failed(hrType), controlType)

	result.text, result.err = readSelection()
	fmt.Printf("[%s] readSelection() = %q, err = %v\n", label, result.text, result.err)
	return result
}

func probeDescribe(r probeResult) string {
	if r.err != nil {
		return fmt.Sprintf("<error: %v>", r.err)
	}
	return fmt.Sprintf("%q", r.text)
}

// --- keeping the foreground and the focus where the probe wants them --------

// probeActivate puts hwnd in the foreground and, when focus is not zero, the
// keyboard on focus. SetForegroundWindow is refused unless the calling thread
// is allowed to take the foreground; attaching to the thread that has it now
// is what makes it allowed, and the probe reports whether it worked rather
// than pretending it did.
func probeActivate(hwnd, focus uintptr) {
	before, _, _ := probeGetForegroundWindow.Call()
	current, _, _ := probeGetCurrentThreadID.Call()
	owner := uintptr(0)
	if before != 0 {
		owner, _, _ = probeGetWindowThreadProcessID.Call(before, 0)
	}
	attached := false
	if owner != 0 && owner != current {
		ok, _, callErr := probeAttachThreadInput.Call(owner, current, 1)
		if ok != 0 {
			attached = true
		} else {
			fmt.Printf("  AttachThreadInput(0x%X, 0x%X, TRUE) = 0 (%s)\n",
				owner, current, probeErrText(callErr))
		}
	}
	ret, _, callErr := probeSetForegroundWindow.Call(hwnd)
	fmt.Printf("  SetForegroundWindow(0x%X) = %d (%s)\n", hwnd, ret, probeErrText(callErr))
	if ret == 0 {
		fmt.Printf("  FOREGROUND REFUSED: the window manager would not let this process take the foreground; UIA will report whatever really has it\n")
	}
	if focus != 0 {
		previous, _, callErr := probeSetFocus.Call(focus)
		fmt.Printf("  SetFocus(0x%X) = previous 0x%X (%s)\n", focus, previous, probeErrText(callErr))
	}
	if attached {
		probeAttachThreadInput.Call(owner, current, 0)
	}
}

func probePrintKeyboard() {
	fg, _, _ := probeGetForegroundWindow.Call()
	fcs, _, _ := probeGetFocus.Call()
	fmt.Printf("  foreground: hwnd=0x%X class=%q title=%q\n", fg, probeClassName(fg), probeWindowText(fg))
	fmt.Printf("  focus:      hwnd=0x%X class=%q title=%q\n", fcs, probeClassName(fcs), probeWindowText(fcs))
}

// probePump runs the message queue for a while so that the window settles and
// the accessibility layer, which is told about a window by messages, sees it.
func probePump(d time.Duration, label string) {
	deadline := time.Now().Add(d)
	message := probeMsg{}
	count := 0
	for time.Now().Before(deadline) {
		ret, _, _ := probePeekMessageW.Call(
			uintptr(unsafe.Pointer(&message)), 0, 0, 0, probePMRemove)
		if ret == 0 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		count++
		probeTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		probeDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
	fmt.Printf("  [%s] pumped %d messages over %s\n", label, count, d)
}

// --- the window class and the rich edit -------------------------------------

// probeLoadRichEdit loads the rich edit library, which is what registers its
// window class, and answers the class name it registered.
func probeLoadRichEdit() (string, error) {
	candidates := []struct{ dll, class string }{
		{"msftedit.dll", "RICHEDIT50W"},
		{"riched20.dll", "RichEdit20W"},
	}
	for _, candidate := range candidates {
		dll := probeU16(candidate.dll)
		handle, _, callErr := probeLoadLibraryW.Call(uintptr(unsafe.Pointer(dll)))
		fmt.Printf("LoadLibraryW(%q) = 0x%X (%s)\n", candidate.dll, handle, probeErrText(callErr))
		if handle != 0 {
			return candidate.class, nil
		}
	}
	return "", errProbe("neither msftedit.dll nor riched20.dll could be loaded")
}

// probeWndProc is the window procedure: the probe has nothing to do with the
// messages, so the default handling answers all of them.
func probeWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	result, _, _ := probeDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return result
}

// --- small helpers ----------------------------------------------------------

func probeU16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return p
}

// probeCall0 invokes a procedure that takes no arguments.
func probeCall0(proc *syscall.LazyProc) uintptr {
	result, _, _ := proc.Call()
	return result
}

func probeClassName(hwnd uintptr) string {
	if hwnd == 0 {
		return "<null hwnd>"
	}
	buffer := make([]uint16, 256)
	n, _, _ := probeGetClassNameW.Call(
		hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 {
		return "<no class name>"
	}
	return syscall.UTF16ToString(buffer[:n])
}

func probeWindowText(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buffer := make([]uint16, 256)
	n, _, _ := probeGetWindowTextW.Call(
		hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:n])
}

func probeErrText(err error) string {
	if err == nil {
		return "<no error>"
	}
	if errno, ok := err.(syscall.Errno); ok {
		if errno == 0 {
			return "last error 0"
		}
		return fmt.Sprintf("last error %d: %v", uintptr(errno), errno)
	}
	return err.Error()
}

type probeError string

func (e probeError) Error() string { return string(e) }

func errProbe(s string) error { return probeError(s) }

// --- the platform's own layouts ---------------------------------------------

const (
	probeEnv = "BOKU_SELECTION_PROBE"

	probeWSOverlappedWindow = 0x00CF0000
	probeWSVisible          = 0x10000000
	probeWSChild            = 0x40000000
	probeWSVScroll          = 0x00200000
	probeWSExClientEdge     = 0x00000200
	probeESMultiline        = 0x0004
	probeESNoHideSel        = 0x0100
	probeEMSetSel           = 0x00B1
	probeSWShow             = 5
	probePMRemove           = 0x0001

	// Slots of IUIAutomationElement, from uiautomationclient.h.
	slotClassName   = 30 // get_CurrentClassName
	slotName        = 23 // get_CurrentName
	slotControlType = 21 // get_CurrentControlType
)

var (
	probeUser32   = syscall.NewLazyDLL("user32.dll")
	probeKernel32 = syscall.NewLazyDLL("kernel32.dll")

	probeRegisterClassExW         = probeUser32.NewProc("RegisterClassExW")
	probeCreateWindowExW          = probeUser32.NewProc("CreateWindowExW")
	probeDefWindowProcW           = probeUser32.NewProc("DefWindowProcW")
	probeShowWindow               = probeUser32.NewProc("ShowWindow")
	probeSetForegroundWindow      = probeUser32.NewProc("SetForegroundWindow")
	probeSetFocus                 = probeUser32.NewProc("SetFocus")
	probeGetFocus                 = probeUser32.NewProc("GetFocus")
	probeGetForegroundWindow      = probeUser32.NewProc("GetForegroundWindow")
	probeGetClassNameW            = probeUser32.NewProc("GetClassNameW")
	probeGetWindowTextW           = probeUser32.NewProc("GetWindowTextW")
	probeSetWindowTextW           = probeUser32.NewProc("SetWindowTextW")
	probeSendMessageW             = probeUser32.NewProc("SendMessageW")
	probePeekMessageW             = probeUser32.NewProc("PeekMessageW")
	probeTranslateMessage         = probeUser32.NewProc("TranslateMessage")
	probeDispatchMessageW         = probeUser32.NewProc("DispatchMessageW")
	probeAttachThreadInput        = probeUser32.NewProc("AttachThreadInput")
	probeGetWindowThreadProcessID = probeUser32.NewProc("GetWindowThreadProcessId")

	probeGetModuleHandleW   = probeKernel32.NewProc("GetModuleHandleW")
	probeLoadLibraryW       = probeKernel32.NewProc("LoadLibraryW")
	probeGetCurrentThreadID = probeKernel32.NewProc("GetCurrentThreadId")
)

// probeWindowProc is made once, at package load, and kept: the address Go
// hands to the window manager must stay valid for as long as a window of this
// process exists.
var probeWindowProc = syscall.NewCallback(probeWndProc)

type probeWndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type probeMsg struct {
	hwnd    uintptr
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	_       uint32
	ptX     int32
	ptY     int32
}
