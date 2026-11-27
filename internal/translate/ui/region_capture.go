package ui

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"math"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/platform/screenoverlay"
	"boku/internal/platform/screenrecording"
)

type nativeRegionScreenshot struct {
	state   *State
	capture RegionCapture
}

func (d *nativeRegionScreenshot) Capture(ctx context.Context) ([]byte, error) {
	if d == nil || d.state == nil || d.capture == nil {
		return nil, fmt.Errorf("%w: region capture is unavailable", domain.ErrImageTranslateUnavailable)
	}
	// macOS 未获屏幕录制权限时会返回黑像素，须在打开选区前请求授权。
	if !screenrecording.Granted() {
		granted := false
		mygo.RunOnMain(func() { granted = screenrecording.Request() })
		if !granted {
			return nil, fmt.Errorf("%w: macOS has not allowed Boku to record the screen", domain.ErrScreenRecordingDenied)
		}
	}
	slog.Info("screenshot region selector opened", "event", "screenshot_region_selector_opened")
	// Windows 原生选区与 GDI 均使用物理像素；混合 DPI 显示器不可共用主显示器坐标。
	if selector, ok := d.capture.(interface {
		Select(context.Context) ([]byte, error)
	}); ok {
		return selector.Select(ctx)
	}
	answer := make(chan regionCaptureAnswer, 1)
	d.state.onMain(func() { d.state.openRegionCapture(ctx, d.capture, answer) })
	select {
	case result := <-answer:
		return result.png, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type regionCaptureAnswer struct {
	png []byte
	err error
}

type regionCaptureSession struct {
	state          *State
	ctx            context.Context
	capture        RegionCapture
	answer         chan<- regionCaptureAnswer
	display        mygo.Display
	windows        []*mygo.Window
	start, current mygo.Point
	dragging       bool
	capturing      bool
	selected       image.Rectangle
	once           sync.Once
}

const regionCaptureMinSide = 4

// macOS 仅列出主动请求过屏幕录制权限的应用。
func requestScreenRecording() {
	if runtime.GOOS != "darwin" {
		return
	}
	granted := screenrecording.Request()
	slog.Info("screen recording access", "granted", granted)
}

// 屏幕录制设置入口仅适用于 macOS。
func (s *State) openScreenRecordingSettings() {
	url := screenrecording.SettingsURL()
	if url == "" {
		return
	}
	if err := mygo.Shell.OpenExternal(url); err != nil {
		slog.Warn("cannot open the screen recording settings", "event", "screen_recording_settings_open_failed", "error", err)
	}
}

var screenshotMask = ui.RGBA(0, 0, 0, 0.62)

func (s *State) openRegionCapture(ctx context.Context, capture RegionCapture, answer chan<- regionCaptureAnswer) {
	// 同时只能存在一个选区会话，后发请求取消前一会话。
	if s.regionSession != nil {
		s.regionSession.cancel()
	}
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		answer <- regionCaptureAnswer{err: fmt.Errorf("%w: no display", domain.ErrImageTranslateUnavailable)}
		return
	}
	session := &regionCaptureSession{state: s, ctx: ctx, capture: capture, answer: answer}
	s.regionSession = session
	for _, display := range displays {
		session.openOverlay(display)
	}
	go func() {
		<-ctx.Done()
		s.onMain(session.cancel)
	}()
}

func (session *regionCaptureSession) openOverlay(display mygo.Display) {
	if display.Bounds.Width <= 0 || display.Bounds.Height <= 0 {
		return
	}
	x, y := initialWindowPoint(display)
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: appName + " 截图翻译", X: x, Y: y, Width: 1, Height: 1,
		Hidden: true, UseContentSize: true, Frameless: true, Transparent: true,
		AlwaysOnTop: true, SkipTaskbar: true, DisableResize: true,
		DisableShadow: true,
		Panel:         runtime.GOOS == "darwin", Content: ui.View(func(c *ui.Context) { session.view(c, display) }),
	})
	if err := screenoverlay.Cover(window.NativeHandle()); err != nil {
		slog.Warn("the screenshot overlay cannot cover the whole display", "event", "screenshot_overlay_cover_failed", "error", err)
	}
	window.SetContentBounds(display.Bounds)
	window.OnClosed(func() {
		if session.capturing {
			// 截图时由 captureSelected 自己关闭浮层，并在关闭后再读像素。
			return
		}
		session.clearRegionSession()
		session.finish(nil, context.Canceled)
	})
	session.windows = append(session.windows, window)
	window.Show()
}

func (session *regionCaptureSession) view(c *ui.Context, display mygo.Display) {
	root := c.Root()
	root.Background(ui.Transparent)
	// 遮罩由输入区自身绘制，而不是子元素。MyGo 会先提交流内子元素、再提交绝对定位子元素，
	// 且把后者放在命中链最前，所以盖在区域上的遮罩盒会成为指针下的元素并抢走按下事件。
	// 在这里绘制就能让同一个元素同时拥有遮罩和指针。
	full := image.Rect(0, 0, display.Bounds.Width, display.Bounds.Height)
	area := ui.Box(c).Fill().Focusable().FocusRing(false).Background(ui.Transparent)
	area.Draw(func(p *ui.Painter, _ ui.Rect) {
		for _, part := range maskParts(full, session.selectionOn(display)) {
			p.Fill(ui.Rect{
				X: float32(part.Min.X), Y: float32(part.Min.Y),
				W: float32(part.Dx()), H: float32(part.Dy()),
			}, screenshotMask, 0)
		}
	})
	area.HandleInput(func(ev ui.InputEvent) bool {
		return session.input(ev, display)
	})
	// 这是浮层快捷键：与聚焦元素快捷键不同，即使另一块显示器的浮层持有键盘焦点，
	// 它也能收到 Esc。
	if area.OverlayShortcut(0, ui.KeyEscape) {
		session.cancel()
		return
	}
}

func (session *regionCaptureSession) input(ev ui.InputEvent, display mygo.Display) bool {
	// 已排队排在 mouse-up 之后的指针事件，不能在 captureSelected 关掉浮层后又重建选区。
	if session.capturing {
		return true
	}
	switch ev.Kind {
	case ui.InputKeyDown:
		if ev.Key == ui.KeyEscape {
			session.cancel()
			return true
		}
	case ui.InputPointerDown:
		if ev.Button == 0 {
			session.display = display
			session.start = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			session.current = session.start
			session.dragging = true
			return true
		}
	case ui.InputPointerMove:
		if session.dragging {
			session.current = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			return true
		}
	case ui.InputPointerUp:
		if session.dragging && ev.Button == 0 {
			session.current = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			r := normalizedDIPRect(session.start, session.current)
			// 选区只属于它开始的那块显示器。指针拖入相邻显示器仍会把移动事件发到这里，
			// 而像素读取本就止于本显示器边缘。
			r = r.Intersect(image.Rect(0, 0, display.Bounds.Width, display.Bounds.Height))
			if r.Dx() < regionCaptureMinSide || r.Dy() < regionCaptureMinSide {
				session.cancel()
				return true
			}
			session.captureSelected(r)
			return true
		}
	}
	return false
}

func (session *regionCaptureSession) captureSelected(dip image.Rectangle) {
	// 先销毁浮层：取像素必须看到下方桌面，而非 Boku 自己的遮罩或选区。
	session.capturing = true
	// 窗口消失前保持拖拽状态，这样落在松开与关闭之间的一帧仍把选区当空洞而非遮黑。
	// 截取矩形就是这个空洞。
	// 关闭回调不能发起读取：Window.Close 会在原生窗口销毁前运行它们，在那里读会截到
	// Boku 自己的遮罩。本线程关闭所有窗口后再读桌面。浮层没有关闭动画
	// （screenoverlay.Cover），所以 Close 返回时窗口已不在屏幕上。
	session.selected = nativeRegionRect(session.display, dip)
	slog.Info("screenshot region selected", "event", "screenshot_region_selected", "width", session.selected.Dx(), "height", session.selected.Dy())
	for _, window := range session.windows {
		window.Close()
	}
	session.clearRegionSession()
	session.readSelected()
}

func (session *regionCaptureSession) readSelected() {
	go func() {
		png, err := session.capture.Capture(session.ctx, session.selected)
		session.finish(png, err)
	}()
}

func (session *regionCaptureSession) cancel() {
	if session.capturing {
		return
	}
	slog.Info("screenshot region selection cancelled", "event", "screenshot_region_selection_cancelled")
	for _, window := range session.windows {
		window.Close()
	}
	session.finish(nil, context.Canceled)
}

func (session *regionCaptureSession) clearRegionSession() {
	if session.state != nil && session.state.regionSession == session {
		session.state.regionSession = nil
	}
}

func (session *regionCaptureSession) finish(png []byte, err error) {
	session.once.Do(func() { session.answer <- regionCaptureAnswer{png: png, err: err} })
}

// selectionOn 返回本显示器 DIP 坐标下的当前选区；若拖拽属于另一块显示器或尚未开始，返回空矩形。
func (session *regionCaptureSession) selectionOn(display mygo.Display) image.Rectangle {
	if !session.dragging || session.display.ID != display.ID {
		return image.Rectangle{}
	}
	return normalizedDIPRect(session.start, session.current)
}

func normalizedDIPRect(a, b mygo.Point) image.Rectangle {
	return image.Rect(min(a.X, b.X), min(a.Y, b.Y), max(a.X, b.X), max(a.Y, b.Y))
}

// maskParts 把 full 拆成 hole 四周仍然保留的遮罩条带，使选区成为同一窗口里的透明空洞，
// 而不是一个盖在指针之上的独立窗口。hole 会裁到 full 内；hole 为空或落在外面时整块都遮罩。
func maskParts(full, hole image.Rectangle) []image.Rectangle {
	hole = hole.Intersect(full)
	if hole.Empty() {
		return []image.Rectangle{full}
	}
	parts := []image.Rectangle{
		image.Rect(full.Min.X, full.Min.Y, full.Max.X, hole.Min.Y),
		image.Rect(full.Min.X, hole.Min.Y, hole.Min.X, hole.Max.Y),
		image.Rect(hole.Max.X, hole.Min.Y, full.Max.X, hole.Max.Y),
		image.Rect(full.Min.X, hole.Max.Y, full.Max.X, full.Max.Y),
	}
	kept := parts[:0]
	for _, part := range parts {
		if !part.Empty() {
			kept = append(kept, part)
		}
	}
	return kept
}

func initialWindowPoint(display mygo.Display) (int, int) {
	// WindowOptions 的 X、Y 和 Bounds 一样是 DIP。零/零会把窗口居中到主显示器，
	// 而不是建在本显示器上。
	x, y := display.Bounds.X, display.Bounds.Y
	if x == 0 && y == 0 {
		return 1, 1
	}
	return x, y
}

// desktopRect 把显示器本地的 DIP 选区映射到平台取像素器所用的坐标空间。
// 显示器原点与选区分别缩放：把全局矩形整体缩放会破坏起始原点不同的混合 DPI 布局。
// 取像素器按屏幕点工作（CoreGraphics）时 scale 为 1，按物理像素工作（GDI）时用显示器缩放因子。
func desktopRect(display mygo.Display, local image.Rectangle, scale float64) image.Rectangle {
	if scale <= 0 {
		scale = 1
	}
	x := int(math.Round(float64(display.Bounds.X) * scale))
	y := int(math.Round(float64(display.Bounds.Y) * scale))
	return image.Rect(
		x+int(math.Round(float64(local.Min.X)*scale)),
		y+int(math.Round(float64(local.Min.Y)*scale)),
		x+int(math.Round(float64(local.Max.X)*scale)),
		y+int(math.Round(float64(local.Max.Y)*scale)),
	)
}
