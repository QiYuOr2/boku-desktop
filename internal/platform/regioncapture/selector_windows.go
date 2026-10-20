//go:build windows

package regioncapture

import (
	"context"
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// The selector is a native Win32 popup instead of a MyGo window. MyGo's
// display bounds are DIPs whose origin is scaled per monitor; a virtual
// desktop containing mixed-DPI displays has no one DIP coordinate space.
// Win32 mouse coordinates, window regions, and GDI capture rectangles are all
// physical virtual-desktop pixels, which is the coordinate system a screenshot
// selector needs.
const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmPaint        = 0x000F
	wmKeyDown      = 0x0100
	wmLButtonDown  = 0x0201
	wmLButtonUp    = 0x0202
	wmMouseMove    = 0x0200
	wscPopup       = 0x80000000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	wsExLayered    = 0x00080000
	swHide         = 0
	swShow         = 5
	swpShowWindow  = 0x0040
	hwndTopmost    = ^uintptr(0)
	lwaAlpha       = 0x00000002
	rgnDiff        = 4
	keyEscape      = 0x1B
	blackBrush     = 4
	idcArrow       = 32512
	smXVirtual     = 76
	smYVirtual     = 77
	smCXVirtual    = 78
	smCYVirtual    = 79
)

var (
	selectorUser32 = syscall.NewLazyDLL("user32.dll")
	selectorGDI32  = syscall.NewLazyDLL("gdi32.dll")
	selectorKernel = syscall.NewLazyDLL("kernel32.dll")
	registerClass  = selectorUser32.NewProc("RegisterClassExW")
	createWindow   = selectorUser32.NewProc("CreateWindowExW")
	defWindowProc  = selectorUser32.NewProc("DefWindowProcW")
	destroyWindow  = selectorUser32.NewProc("DestroyWindow")
	showWindow     = selectorUser32.NewProc("ShowWindow")
	setWindowPos   = selectorUser32.NewProc("SetWindowPos")
	setLayered     = selectorUser32.NewProc("SetLayeredWindowAttributes")
	getMessage     = selectorUser32.NewProc("GetMessageW")
	translateMsg   = selectorUser32.NewProc("TranslateMessage")
	dispatchMsg    = selectorUser32.NewProc("DispatchMessageW")
	postQuit       = selectorUser32.NewProc("PostQuitMessage")
	postMessage    = selectorUser32.NewProc("PostMessageW")
	getCursor      = selectorUser32.NewProc("GetCursorPos")
	loadCursor     = selectorUser32.NewProc("LoadCursorW")
	setCapture     = selectorUser32.NewProc("SetCapture")
	releaseCapture = selectorUser32.NewProc("ReleaseCapture")
	getMetric      = selectorUser32.NewProc("GetSystemMetrics")
	setWindowRgn   = selectorUser32.NewProc("SetWindowRgn")
	beginPaint     = selectorUser32.NewProc("BeginPaint")
	endPaint       = selectorUser32.NewProc("EndPaint")
	fillRect       = selectorUser32.NewProc("FillRect")
	stockObject    = selectorGDI32.NewProc("GetStockObject")
	createRectRgn  = selectorGDI32.NewProc("CreateRectRgn")
	combineRgn     = selectorGDI32.NewProc("CombineRgn")
	deleteObjectR  = selectorGDI32.NewProc("DeleteObject")
	getModule      = selectorKernel.NewProc("GetModuleHandleW")

	selectorClassOnce sync.Once
	selectorClassErr  error
	selectorWndProc   = syscall.NewCallback(selectorWindowProc)
	selectorMu        sync.Mutex
	selectorCurrent   *selectorSession
)

const selectorClassName = "BokuNativeRegionSelector"

type selectorPoint struct{ X, Y int32 }

type selectorMessage struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      selectorPoint
	Private uint32
}

// selectorPaint is PAINTSTRUCT. Its explicit layout matters here because a
// native overlay has no framework renderer to paint its background for us.
type selectorPaint struct {
	DC        uintptr
	Erase     int32
	Rect      selectorRect
	Restore   int32
	Increment int32
	Reserved  [32]byte
}

type selectorRect struct{ Left, Top, Right, Bottom int32 }

type selectorWindowClass struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

type selectorSession struct {
	device *Device
	hwnd   uintptr
	full   image.Rectangle
	start  image.Point
	drag   bool
	chosen image.Rectangle
	cancel bool
	err    error
}

// Select provides the Snipaste-style interaction: one dimmed virtual desktop,
// a live rectangular hole during the drag, and a GDI capture only after the
// window is hidden. It never reads or writes the clipboard.
func (d *Device) Select(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureSelectorClass(); err != nil {
		return nil, err
	}
	result := make(chan struct{})
	var session *selectorSession
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		full := virtualDesktopBounds()
		if full.Empty() {
			close(result)
			return
		}
		session = &selectorSession{device: d, full: full, cancel: true}
		selectorMu.Lock()
		selectorCurrent = session
		selectorMu.Unlock()
		defer func() {
			selectorMu.Lock()
			if selectorCurrent == session {
				selectorCurrent = nil
			}
			selectorMu.Unlock()
			close(result)
		}()
		instance, _, _ := getModule.Call(0)
		hwnd, _, err := createWindow.Call(
			wsExTopmost|wsExToolWindow|wsExLayered,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(selectorClassName))),
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Boku 截图翻译"))),
			wscPopup, uintptr(full.Min.X), uintptr(full.Min.Y), uintptr(full.Dx()), uintptr(full.Dy()),
			0, 0, instance, 0,
		)
		if hwnd == 0 {
			session.err = fmt.Errorf("region selector: create window: %w", err)
			return
		}
		session.hwnd = hwnd
		setLayered.Call(hwnd, 0, 158, lwaAlpha)
		setSelectorRegion(session, image.Rectangle{})
		setWindowPos.Call(hwnd, hwndTopmost, uintptr(full.Min.X), uintptr(full.Min.Y), uintptr(full.Dx()), uintptr(full.Dy()), swpShowWindow)
		showWindow.Call(hwnd, swShow)
		var msg selectorMessage
		for {
			status, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(status) <= 0 {
				return
			}
			translateMsg.Call(uintptr(unsafe.Pointer(&msg)))
			dispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()

	select {
	case <-result:
		if session == nil || session.cancel || session.chosen.Dx() < 4 || session.chosen.Dy() < 4 {
			if session != nil && session.err != nil {
				return nil, session.err
			}
			return nil, context.Canceled
		}
		return d.Capture(ctx, session.chosen)
	case <-ctx.Done():
		selectorMu.Lock()
		hwnd := uintptr(0)
		if selectorCurrent != nil {
			hwnd = selectorCurrent.hwnd
		}
		selectorMu.Unlock()
		if hwnd != 0 {
			postMessage.Call(hwnd, wmClose, 0, 0)
		}
		return nil, ctx.Err()
	}
}

func ensureSelectorClass() error {
	selectorClassOnce.Do(func() {
		instance, _, _ := getModule.Call(0)
		class := selectorWindowClass{
			Size:      uint32(unsafe.Sizeof(selectorWindowClass{})),
			WndProc:   selectorWndProc,
			Instance:  instance,
			Cursor:    selectorArrowCursor(),
			ClassName: syscall.StringToUTF16Ptr(selectorClassName),
		}
		if atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			selectorClassErr = fmt.Errorf("region selector: register window class: %w", err)
		}
	})
	return selectorClassErr
}

// selectorArrowCursor keeps selection mode recognisable. A class without an
// explicit cursor inherits whatever the foreground application last chose,
// which can be the system waiting cursor while screenshot translation starts.
func selectorArrowCursor() uintptr {
	cursor, _, _ := loadCursor.Call(0, idcArrow)
	return cursor
}

func virtualDesktopBounds() image.Rectangle {
	x, _, _ := getMetric.Call(smXVirtual)
	y, _, _ := getMetric.Call(smYVirtual)
	w, _, _ := getMetric.Call(smCXVirtual)
	h, _, _ := getMetric.Call(smCYVirtual)
	return image.Rect(int(int32(x)), int(int32(y)), int(int32(x))+int(int32(w)), int(int32(y))+int(int32(h)))
}

func selectorWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	selectorMu.Lock()
	session := selectorCurrent
	selectorMu.Unlock()
	if session == nil || session.hwnd != hwnd {
		result, _, _ := defWindowProc.Call(hwnd, uintptr(message), wparam, lparam)
		return result
	}
	switch message {
	case wmPaint:
		var paint selectorPaint
		dc, _, _ := beginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		if dc != 0 {
			// A layered popup has no automatic background. Fill the visible
			// window region black; SetLayeredWindowAttributes supplies the
			// 62%% opacity and SetWindowRgn excludes the live selection hole.
			fillRect.Call(dc, uintptr(unsafe.Pointer(&paint.Rect)), stockBlackBrush())
			endPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		}
		return 0
	case wmLButtonDown:
		session.start = selectorCursor()
		session.drag = true
		setCapture.Call(hwnd)
		setSelectorRegion(session, image.Rect(session.start.X, session.start.Y, session.start.X, session.start.Y))
		return 0
	case wmMouseMove:
		if session.drag {
			setSelectorRegion(session, normalizedSelectorRect(session.start, selectorCursor()))
		}
		return 0
	case wmLButtonUp:
		if session.drag {
			session.drag = false
			releaseCapture.Call()
			session.chosen = normalizedSelectorRect(session.start, selectorCursor())
			session.cancel = session.chosen.Dx() < 4 || session.chosen.Dy() < 4
			showWindow.Call(hwnd, swHide)
			destroyWindow.Call(hwnd)
		}
		return 0
	case wmKeyDown:
		if wparam == keyEscape {
			session.cancel = true
			destroyWindow.Call(hwnd)
		}
		return 0
	case wmClose:
		session.cancel = true
		destroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		postQuit.Call(0)
		return 0
	}
	result, _, _ := defWindowProc.Call(hwnd, uintptr(message), wparam, lparam)
	return result
}

func stockBlackBrush() uintptr {
	brush, _, _ := stockObject.Call(blackBrush)
	return brush
}

func selectorCursor() image.Point {
	var point selectorPoint
	getCursor.Call(uintptr(unsafe.Pointer(&point)))
	return image.Pt(int(point.X), int(point.Y))
}

func normalizedSelectorRect(a, b image.Point) image.Rectangle {
	return image.Rect(min(a.X, b.X), min(a.Y, b.Y), max(a.X, b.X), max(a.Y, b.Y))
}

func setSelectorRegion(session *selectorSession, chosen image.Rectangle) {
	full := session.full
	region, _, _ := createRectRgn.Call(0, 0, uintptr(full.Dx()), uintptr(full.Dy()))
	if region == 0 {
		return
	}
	if !chosen.Empty() {
		local := chosen.Sub(full.Min)
		hole, _, _ := createRectRgn.Call(uintptr(local.Min.X), uintptr(local.Min.Y), uintptr(local.Max.X), uintptr(local.Max.Y))
		if hole != 0 {
			combineRgn.Call(region, region, hole, rgnDiff)
			deleteObjectR.Call(hole)
		}
	}
	// SetWindowRgn takes ownership of region on success.
	if ok, _, _ := setWindowRgn.Call(session.hwnd, region, 1); ok == 0 {
		deleteObjectR.Call(region)
	}
}
