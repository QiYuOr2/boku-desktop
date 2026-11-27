package ui

import (
	"boku/internal/presentation"
	"context"
	"errors"
	"log/slog"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/translate"
)

// 翻译窗口创建一次并复用：每次翻译只重新定位，比每次按键新建窗口更省，
// 也能保持窗口形状稳定——答案替换同一张卡片里的占位内容，而不是再冒出一张卡。
const (
	// translateWidth 是卡片宽度：够放一行日文及其中文注音，下面是译文，不至于每三四个词就换行。
	translateWidth = 478
	// translateMinHeight 是卡片最小高度：一行提示、一条分隔线和脚部。
	translateMinHeight = 84
	// translateMaxHeight 是最大高度：超过后正文滚动，因为比屏幕还高的卡片就成了窗口而非卡片。
	translateMaxHeight = 620
	// translateHeight 是首帧量出内容前窗口的初始高度。
	translateHeight = 240
	// translateGap 是卡片与指针的距离：不压在指针下，又足够近。
	translateGap = 12
	// copiedMarkDuration 是复制操作反馈持续时长，与剪贴板历史一致，因为这是同一种手势。
	copiedMarkDuration = 700 * time.Millisecond
)

// translateStatus 是卡片当前显示的状态。用单个状态而非一组布尔，因为卡片只画其中之一。
type translateStatus int

const (
	// translateIdle 是尚未请求任何内容。
	translateIdle translateStatus = iota
	// translateLoading 是正在请求翻译器。
	translateLoading
	// translateReady 是已有译文。
	translateReady
	// translateQRReady 是本地识别出的二维码文本。它不是翻译：没有目标语言、服务和注音。
	translateQRReady
	// translateOff 是在设置里关闭了该工具。
	translateOff
	// translateUnconfigured 是没有可用的翻译器。
	translateUnconfigured
	// translateNoText 是剪贴板里没有可翻译的内容。
	translateNoText
	// translateNeedsScreenRecording 是 macOS 缺少截图读取所需权限。这不是翻译失败：
	// 修复办法是系统设置里的一个开关，卡片会说明在哪里。
	translateNeedsScreenRecording
	// translateFailed 是翻译器失败，保留了原因。
	translateFailed
)

// translationState 是窗口状态，与其他视图状态一样位于主线程。
type translationState struct {
	status translateStatus
	// image 表示卡片是关于截图的，脚部据此说明，失败时也据此提示重试方式。
	image bool
	// qr 表示卡片是关于二维码的：截图方式与截图工具相同，但答案是本机识别的文本而非翻译，
	// 不涉及服务和网络。
	qr      bool
	preview *ui.Bitmap
	// imageTaskID 仅截图任务非零，使完成的后台任务能更新已显示该任务的卡片，
	// 而不会重新打开用户已关闭的卡片。
	imageTaskID uint64
	source      string
	result      translate.Result
	// failure 是最近一次失败的原因，用窗口展示的措辞，仅在 translateFailed 时设置。
	failure string
	// pinned 是卡片是否在用户点击别处时保持不动，初值来自设置，由固定操作翻转。
	pinned bool
	// readings 是译文下的注音是否展开，初值关闭。
	readings bool
	// annotation 是第二次请求的结果：原文的词及其读法。展开时请求，切换卡片内容前保留。
	annotation annotationState
	// copied 是复制了哪一侧（"source" / "target"），空表示未复制；copiedAt 是时间，用于短暂反馈。
	copied   string
	copiedAt time.Time
	// dragging 表示指针正通过脚部拖动卡片；dragFrom 是按下时的窗口位置，dragPointer 是按下时的指针位置。
	// 按两次按下之间指针的位移移动卡片，使卡片停在抓取点之下而不是跳跃到指针位置。
	dragging    bool
	dragFrom    mygo.Rectangle
	dragPointer mygo.Point
}

// annotationStatus 是请求原文词读法的进度。
type annotationStatus uint8

const (
	// annotationIdle 是展开注音之前的状态：未请求，也未付费。
	annotationIdle annotationStatus = iota
	annotationLoading
	annotationReady
	annotationFailed
)

// annotationState 是该请求的答案：原文的词及每个词的读法。
type annotationState struct {
	status   annotationStatus
	segments []domain.Segment
	failure  string
}

// startTranslation 是快捷键和设置窗口测试都会调用的入口：显示卡片并向用例请求翻译。
//
// 它立即返回。请求在 goroutine 中运行，答案经 Window.Update 回来，因为调用方是主线程。
//
// 卡片在取得键盘之前先出现，其间读取待翻译文本：要读的是用户所在应用里选中的内容，
// 而刚失去键盘的应用无法再被询问选中了什么（工具设计 §2.2）。这段等待是读取（平台调用，
// 受选区设备限制）而非翻译；请求进行期间卡片持有键盘。
func (s *State) startTranslation(at mygo.Point) {
	if s.translate == nil {
		return
	}
	startedAt := time.Now()
	slog.Info("translation requested", "event", "text_translation_requested", "source", "shortcut")
	// 卡片打开时再按一次是新的翻译：用户复制的可能已变化，卡片应反映最新选中内容。
	settings, err := s.translate.Settings()
	if err != nil {
		// 卡片只能说「看不出来」；日志记录是哪次读取失败。
		slog.Warn("the translation settings could not be read", "error", err)
		s.showTranslation(at, translationState{status: translateFailed, failure: presentation.FriendlyError(err)})
		return
	}
	s.translation.pinned = settings.Pinned
	s.showTranslationInactive(at, translationState{status: translateLoading, pinned: settings.Pinned})
	window := s.translateWindow

	service := s.translate
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), translationTimeout)
		defer cancel()
		text, err := service.Source(ctx)
		// 读取完成，卡片可以取得键盘了。此时取得而非等答案回来时才取得，
		// 是为了等待期间用户能关闭卡片——Escape 和设置要求的关闭都依赖持有键盘。
		s.onMain(func() { s.focusTranslation(window) })
		var result translate.Result
		if err == nil {
			result, err = service.Translate(ctx, translate.Request{Text: text})
		}
		s.onMain(func() {
			switch {
			case err == nil:
				slog.Info("translation completed", "event", "text_translation_completed", "provider", result.ProviderID, "sourceChars", len([]rune(text)), "duration", time.Since(startedAt))
				// 原文取自分片（若有）：窗口显示翻译器标注的分片，以便读法对应到所属的词。
				s.translation = translationState{
					status:   translateReady,
					source:   result.SourceText,
					result:   result,
					pinned:   s.translation.pinned,
					copied:   s.translation.copied,
					copiedAt: s.translation.copiedAt,
				}
			default:
				// 卡片向用户显示简短原因；日志里的错误才说明具体失败方式。
				slog.Warn("the translation failed", "event", "text_translation_failed", "sourceChars", len([]rune(text)), "duration", time.Since(startedAt), "error", err)
				s.translation = translationState{
					status:  translationFailureStatus(err),
					result:  translate.Result{},
					failure: presentation.FriendlyError(err),
					pinned:  s.translation.pinned,
				}
			}
			// 卡片内容已不同于上一帧量到的，需要请求测量它的后续帧。
			s.cardChanged()
			s.fitTranslationWindow()
		})
	}()
}

// startScreenshotTranslation 在 UI 线程之外启动原生选区选择，然后把临时 PNG 发给选定的图片翻译器。
// 浮窗按钮和全局快捷键都从这里进入。
func (s *State) startScreenshotTranslation() {
	requestedAt := s.cursorPoint()
	if s.translate == nil || s.screenshot == nil {
		return
	}
	startedAt := time.Now()
	slog.Info("screenshot translation requested", "event", "screenshot_translation_requested")
	settings, err := s.translate.Settings()
	if err != nil {
		s.showTranslation(requestedAt, translationState{status: translateFailed, failure: presentation.FriendlyError(err)})
		return
	}
	service, capture := s.translate, s.screenshot
	go func() {
		captureCtx, cancelCapture := context.WithTimeout(context.Background(), screenshotCaptureTimeout)
		image, err := capture.Capture(captureCtx)
		cancelCapture()
		var preview *ui.Bitmap
		if err == nil {
			slog.Info("screenshot captured", "event", "screenshot_capture_completed", "imageBytes", len(image), "duration", time.Since(startedAt))
			preview, err = ui.DecodeBitmap(image)
		}
		if err == nil {
			// 用户选完区域，此时才显示卡片。锚点在选举结束后于主线程读取，
			// 而不是在按钮或快捷键启动选择器时读取。卡片关闭后任务仍继续运行。
			s.onMain(func() {
				anchor := s.cursorPoint()
				id, ok := s.startImageTranslationTask(service, image, preview, settings.Pinned)
				if !ok {
					s.showTranslation(anchor, translationState{status: translateFailed, image: true, preview: preview, failure: "已有两个图片翻译任务正在进行，请稍后再试。", pinned: settings.Pinned})
					return
				}
				s.showImageTranslationTask(id, anchor)
			})
			return
		}
		// Esc、无有效矩形的点击、以及替换进行中的选择器，都是退出应用自管选区界面的正常方式；
		// 它们不是翻译失败，不应留下卡片。
		if errors.Is(err, context.Canceled) {
			slog.Info("screenshot capture cancelled", "event", "screenshot_capture_cancelled", "duration", time.Since(startedAt))
			return
		}
		slog.Warn("screenshot capture failed", "event", "screenshot_capture_failed", "duration", time.Since(startedAt), "error", err)
		s.onMain(func() {
			s.showTranslation(requestedAt, translationState{status: translationFailureStatus(err), image: true, failure: presentation.FriendlyError(err), pinned: settings.Pinned})
		})
	}()
}

// onMain 在拥有它的线程上执行修改视图状态的工作：有窗口就经 window.Update 执行
// （它会请求显示该变化的帧），所有窗口都关闭时则经 Application.WhenReady。
// 后者对截图任务至关重要：其图片可能在托盘浮窗关闭后才到达，但创建翻译卡片仍必须发生在 UI 线程。
//
// 调用方是 goroutine，因此更新体只能在此调用内部操作状态。
func (s *State) onMain(update func()) {
	if window := s.translateWindow; window != nil {
		window.Update(update)
		return
	}
	if s.host.RunOnMain != nil {
		s.host.OnMain(update)
		return
	}
	// 单测构造的小状态没有运行中的应用，同步执行是演练状态转移的唯一方式，绝不是生产线程的逃生口。
	update()
}

// translationTimeout 限定单次翻译的端到端时长。HTTP 客户端有自己更短的超时；
// 这是包含剪贴板读取在内所有环节的上限。
const translationTimeout = 20 * time.Second

const (
	// screenshotCaptureTimeout 限定被放弃的 Boku 区域选择器。
	screenshotCaptureTimeout = 45 * time.Second
	// screenshotTranslationTimeout 刻意独立于选区捕获。视觉端点不提供任务/进度 API，
	// 因此一个打开的 HTTP 请求无法证明模型是排队、推理还是卡住。两分钟是后台任务可被用户取消的上限，
	// 并不代表服务端进度。
	screenshotTranslationTimeout = 2 * time.Minute
)

// translationFailureStatus 把错误映射为卡片显示的状态。
//
// 先单独检查「已关闭」错误，因为这是唯一不是问题的答案：关掉工具的用户得到的正是所要的，
// 若告诉他们服务挂了，会让他们去找并不存在的故障。
func translationFailureStatus(err error) translateStatus {
	switch {
	case errors.Is(err, domain.ErrTranslateDisabled):
		return translateOff
	case errors.Is(err, domain.ErrTranslateUnavailable):
		return translateUnconfigured
	case errors.Is(err, domain.ErrCredentialMissing):
		return translateUnconfigured
	case errors.Is(err, domain.ErrInvalidArgument):
		return translateNoText
	case errors.Is(err, domain.ErrScreenRecordingDenied):
		return translateNeedsScreenRecording
	default:
		return translateFailed
	}
}

// showTranslation 把卡片放到指针处、显示并取得键盘。
func (s *State) showTranslation(at mygo.Point, state translationState) {
	s.showTranslationInactive(at, state)
	s.focusTranslation(s.translateWindow)
}

// showTranslationInactive 把卡片放到指针处并显示，但不取得键盘。
//
// 这是快捷键调用的方式：在待翻译文本读取前保持不夺键盘，因为读取依赖用户所在的应用，
// 而取得键盘会使其无法应答（工具设计 §2.2）。focusTranslation 是另一半，
// 二者总成对出现——快捷键一按下卡片即出现，键盘随读取完成而取得。
func (s *State) showTranslationInactive(at mygo.Point, state translationState) {
	s.translation = state
	// 卡片内容已变，需要请求测量变化的帧。
	s.cardChanged()
	window := s.translateWindow
	if window == nil {
		window = s.newCardWindow(
			func() {
				if s.translateWindow == window {
					s.translateWindow = nil
					s.translationVisible = false
				}
				s.cancelTranslationClose()
			},
			func() {
				// 键盘转走了：结束正在进行的拖动，以及可能已启动的关闭。
				s.endCardDrag()
				s.scheduleTranslationClose(window)
			},
			func() { s.cancelTranslationClose() },
		)
		s.translateWindow = window
	}
	s.placeTranslation(window, at)
	s.fitTranslationWindow()
	window.ShowInactive()
	s.translationVisible = true
}

// focusTranslation 让卡片取得键盘：它是 showTranslationInactive 的另一半，
// 在读取完卡片对应的文本后调用。
//
// 届时调用方读取的那张卡片可能已消失——读取期间被关闭或被新卡片替换——
// 已结束的请求不能把旧卡片重新推到用户面前。
func (s *State) focusTranslation(window CardWindow) {
	if window == nil || window != s.translateWindow {
		return
	}
	window.Focus()
}

// CardWindow 是状态机驱动的翻译卡片接口：显示、取得键盘、隐藏，并接收三种事件。
//
// 用接口而非框架窗口，是为了在没有窗口的环境下演练卡片的行为（状态、固定、延迟关闭）。
// 在 App.Run 之前创建并不会失败，而是等待事件循环，那将是一个永不返回的测试。
//
// Geometry 是内容矩形而非窗口外框：卡片量的是它绘制到的区域，而带边框的窗口绘制区小于给定尺寸。
// Windows 上可调整大小的窗口即使无标题栏也保留 8 点边框（实测 DisableResize: false 时
// 设为 478×222 只绘制 462×214），按外框定尺寸的卡片会裁掉最后 8 点，而脚部恰在那里。
type CardWindow interface {
	// ShowInactive 把窗口显示但不夺键盘，Focus 取得键盘。二者分离是因为待翻译文本从用户所在
	// 应用中读取，卡片在读取完成前不能夺走该应用的键盘（工具设计 §2.2）。
	// 框架自带的 Show 一步完成显示和聚焦，正是这里要拆开的两种行为，故不使用。
	ShowInactive()
	Focus()
	Hide()
	Update(func())
	SetContentBounds(mygo.Rectangle)
	ContentBounds() mygo.Rectangle
	OnClosed(func()) func()
	OnBlur(func()) func()
	OnFocus(func()) func()
}

// newCardWindow 构建真实的卡片。若状态被给了工厂则用它，测试即如此。
func (s *State) newCardWindow(onClosed, onBlur, onFocus func()) CardWindow {
	if s.cardWindows != nil {
		return s.cardWindows(onClosed, onBlur, onFocus)
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     appName + " 划词翻译",
		Width:     translateWidth,
		Height:    translateHeight,
		MinWidth:  translateWidth,
		MinHeight: translateMinHeight,
		// 窗口以隐藏状态创建，由状态机显示。不加此项时框架在构建时就会显示并聚焦，
		// 即在读取选区前就夺走键盘，使卡片无法读取（工具设计 §2.2）。
		Hidden:    true,
		Frameless: true,
		// 不可调整大小：卡片高度随内容逐帧测量，拖边缘会被立即还原；
		// 而可调整大小的窗口即使无标题栏也保留边框，会占掉卡片底部 8 点不被绘制。
		DisableResize: true,
		Transparent:   true,
		AlwaysOnTop:   true,
		SkipTaskbar:   true,
		// macOS 上用 Panel（非激活 NSPanel）：浮于所有 Space 和全屏应用之上，
		// 取得键盘也不会激活 Boku，用户所在应用保持活跃。普通窗口会在卡片取得键盘时抢占前台，
		// 对一张关于用户选区内容的卡片而言是错的。Windows 保留普通窗口，其 ShowInactive/Focus 已具所需语义。
		Panel: runtime.GOOS == "darwin",
		// 使用平台自身的窗口阴影，而非在视图里绘制：视图里画阴影需要窗口比卡片大以留出空间，
		// 那圈透明边距会吞掉点击（实测卡片外 30 点的点击仍落在窗口内），点击卡片旁边就无法关闭它。
		// 平台阴影画在窗口外，不占卡片、不占桌面点击。实测绘制区仍恰为卡片大小（外框 478×150，绘制 478×150）。
		DisableShadow: false,
		Content:       ui.View(s.translateView),
	})
	window.OnClosed(onClosed)
	window.OnBlur(onBlur)
	window.OnFocus(onFocus)
	return window
}

// placeTranslation 把卡片放到指针附近：先放下方偏右，没空间时再像菜单一样翻转或滑回。
func (s *State) placeTranslation(window CardWindow, at mygo.Point) {
	s.translationHeight = translateHeight
	window.SetContentBounds(cardBounds(at, s.screenNear(at), s.translationHeight))
}

// cardBounds 以算术方式给出卡片位置：按内容尺寸的矩形，贴近指针且落在可用范围内。
//
// 它只依赖参数而不依赖屏幕，因此翻转与滑动无需屏幕即可测试。
//
// 全部使用窗口 API 接受的 point，与设计尺寸同单位，刻意不做缩放：
// SetContentBounds/ContentBounds 已是 DIP，再乘显示器缩放会在 150% 显示器上把 478 点变成 717 点。
func cardBounds(at mygo.Point, work mygo.Rectangle, height int) mygo.Rectangle {
	x := at.X + translateGap
	y := at.Y + translateGap
	if x+translateWidth > work.X+work.Width {
		x = at.X - translateGap - translateWidth
	}
	if y+height > work.Y+work.Height {
		y = at.Y - translateGap - height
	}
	return mygo.Rectangle{
		X:      clampInt(x, work.X, max(work.X+work.Width-translateWidth, work.X)),
		Y:      clampInt(y, work.Y, max(work.Y+work.Height-height, work.Y)),
		Width:  translateWidth,
		Height: height,
	}
}

// screenNear 是卡片可用的空间：所有显示器工作区的并集。
//
// 不能用指针所在显示器的工作区：MyGo 按各显示器自身缩放转换 point 报告其矩形，
// 物理上并排的两块屏转换后会重叠（实测副屏矩形覆盖了主屏右侧一部分），
// 卡片被夹进另一块屏的工作区，表现为「位置异常跳动」。用并集在单屏上结果相同，多屏时只意味着卡片可跨接缝，
// 那正是用户放它的地方。
func (s *State) screenNear(at mygo.Point) mygo.Rectangle {
	if s.cursorSet {
		// 预先给定指针的状态没有屏幕可问（屏幕模块需要运行中的应用），
		// 返回一个足够大的工作区以便演练算术。
		return mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080}
	}
	return workAreaOf(mygo.Screen.Displays())
}

// workAreaOf 是所有显示器工作区的并集：无论框架把指针归属到哪块屏，卡片都可在其中放置。
func workAreaOf(displays []mygo.Display) mygo.Rectangle {
	if len(displays) == 0 {
		return mygo.Rectangle{}
	}
	work := displays[0].WorkArea
	for _, display := range displays[1:] {
		area := display.WorkArea
		left, top := min(work.X, area.X), min(work.Y, area.Y)
		right := max(work.X+work.Width, area.X+area.Width)
		bottom := max(work.Y+work.Height, area.Y+area.Height)
		work = mygo.Rectangle{X: left, Y: top, Width: right - left, Height: bottom - top}
	}
	return work
}

// measureCard 让卡片高度贴合内容。
//
// 一帧构建卡片并读回其高度，窗口随即在边界内缩放到该高度。超过最大高度的卡片被裁掉而非滚动：
// 一张以高度为测量对象的卡片里没有滚动容器。
//
// 一帧能读到的是上一帧布局出的框（MyGo 的 Element.Bounds 是上一帧的矩形），
// 所以卡片总是按旧状态被测量。这就是下面这些帧的必要性：答案到达在第 1 帧改变内容，
// 第 1 帧量到的却是旧的加载卡片（恰是窗口现有高度），于是什么都不缩放、不再请求下一帧，
// 卡片会停在旧高度直到别的东西推动它。因此卡片主动请求自己测量所需的帧：
// 内容变化后一帧（量到画出的变化），窗口给出新尺寸后一帧（按新尺寸重新布局）。
// 两者由 cardSettle 计数，且都是从帧内部请求，所以是排在本帧之后而非并入本帧。
func (s *State) measureCard(c *ui.Context, card ui.Element) {
	if s.cardMeasuring || s.translation.dragging {
		return
	}
	if s.translateWindow == nil {
		// 卡片画在窗口之外（测试帧、离屏渲染），没有可缩放的窗口。
		return
	}
	if c.Vibrancy() {
		// 弹出框材质自带形状，卡片在其下改变大小会闪烁。
		return
	}
	box := card.Bounds()
	if box.H <= 0 {
		// 上一帧中卡片尚不存在，暂无可测量的内容，它存在于本帧。
		s.nextCardFrame(c)
		return
	}
	work := s.screenNear(s.cursorPoint())
	// 窗口是整数点而卡片不是：向上取整使卡片留在窗口内，且从不超出其应有的零头。
	// 直接加一点会在脚下留一条窗口——卡片底边与背后内容间的一道细线。
	measured := int(math.Ceil(float64(box.H)))
	s.cardHeight = measured
	if s.fitCard(work, measured) {
		// 窗口刚被赋予新尺寸：下一帧按它重新布局卡片。
		s.nextCardFrame(c)
		return
	}
	s.nextCardFrame(c)
}

// nextCardFrame 在卡片还欠自己帧时再请求一帧。
func (s *State) nextCardFrame(c *ui.Context) {
	if s.cardSettle <= 0 {
		return
	}
	s.cardSettle--
	c.Invalidate()
}

// cardChanged 表示卡片内容已不同于上一帧绘制的内容，因此绘制它的帧之后必须跟一帧来测量它。
// 欠两帧：一帧绘制变化，一帧按测量出的尺寸重新布局。
func (s *State) cardChanged() { s.cardSettle = 2 }

// fitCard 把窗口缩放到卡片绘制出的大小，保持位置并留在可用范围内，返回窗口是否真的被赋予了新尺寸。
// 尺寸均为 point，与其他处一致；不做缩放的理由见 cardBounds。
func (s *State) fitCard(work mygo.Rectangle, measured int) bool {
	window := s.translateWindow
	if window == nil || measured <= 0 {
		return false
	}
	height := clampInt(measured, translateMinHeight, translateMaxHeight)
	if height == s.translationHeight {
		return false
	}
	s.translationHeight = height
	bounds := window.ContentBounds()
	if bounds.Width == translateWidth && bounds.Height == height {
		return false
	}
	// 卡片保持左上角向下增长；若会越出屏幕则像菜单一样上滑。
	y := clampInt(bounds.Y, work.Y, max(work.Y+work.Height-height, work.Y))
	window.SetContentBounds(mygo.Rectangle{
		X:      clampInt(bounds.X, work.X, max(work.X+work.Width-translateWidth, work.X)),
		Y:      y,
		Width:  translateWidth,
		Height: height,
	})
	return true
}

// translationHeight 是卡片此刻想要的高度：测得的高度，或首帧之前的初始高度。

// fitTranslationWindow 按摆放方式缩放卡片：宽度用设计值，高度用卡片测得值。
func (s *State) fitTranslationWindow() {
	window := s.translateWindow
	if window == nil {
		return
	}
	work := s.screenNear(s.cursorPoint())
	s.fitCard(work, s.translationHeight)
}

// scheduleTranslationClose 在卡片被忽略达到设置时长后关闭它。
//
// 延迟的意义：用户看译文时可能点击背后页面继续阅读但仍想保留卡片。固定卡片不会自动关闭，
// 计时期间才被固定的卡片也不会。
func (s *State) scheduleTranslationClose(window CardWindow) {
	if s.translation.pinned {
		return
	}
	delay := translate.DefaultCloseDelay
	if s.translate != nil {
		if settings, err := s.translate.Settings(); err == nil {
			delay = settings.CloseDelayOr()
		}
	}
	s.cancelTranslationClose()
	// 计时器只请求关闭；它读的状态在 Update 内于主线程操作，与其他视图状态一样。
	timer := time.AfterFunc(delay, func() {
		window.Update(func() {
			if s.translateWindow != window || s.translation.pinned {
				return
			}
			s.closeTranslation()
		})
	})
	s.translateCloseTimer = timer
}

// cancelTranslationClose 取消已请求的关闭：用户回到了卡片，或自行关闭了它。
func (s *State) cancelTranslationClose() {
	if s.translateCloseTimer != nil {
		s.translateCloseTimer.Stop()
		s.translateCloseTimer = nil
	}
}

// closeTranslation 隐藏卡片（而非销毁）：下次按键复用，从未创建的窗口不花代价。
//
// 隐藏同时结束进行中的拖动；否则离屏却仍以为指针在拖它的卡片，会在指针再次经过时跳动。
func (s *State) closeTranslation() {
	s.cancelTranslationClose()
	s.endCardDrag()
	s.translationVisible = false
	if s.translateWindow != nil {
		s.translateWindow.Hide()
	}
}

// toggleTranslationPin 翻转卡片是否自动关闭，并请求绘制新固定状态的那一帧。
func (s *State) toggleTranslationPin() {
	s.translation.pinned = !s.translation.pinned
	slog.Info("translation card pin changed", "event", "translation_card_pin_changed", "pinned", s.translation.pinned, "image", s.translation.image)
	if s.translation.imageTaskID != 0 {
		if task := s.imageTranslationTask(s.translation.imageTaskID); task != nil {
			task.pinned = s.translation.pinned
		}
	}
	if s.translation.pinned {
		s.cancelTranslationClose()
	}
	if s.translateWindow != nil {
		s.cardChanged()
		s.translateWindow.Update(func() {})
	}
}

// toggleReadings 展开或收起译文下的注音，并请求按新高度绘制卡片的帧。
// 首次展开才向翻译器请求词读法：翻译没有等待它，卡片也不等。
func (s *State) toggleReadings() {
	s.translation.readings = !s.translation.readings
	if s.translation.readings && s.translation.annotation.status == annotationIdle {
		s.requestAnnotation()
		return
	}
	if s.translateWindow != nil {
		s.cardChanged()
		s.translateWindow.Update(func() {})
	}
}

// requestAnnotation 向翻译器请求原文的词及其读法。
//
// 这是独立请求，在用户展开注音时发出，所以注音在此之前不花代价：翻译只请求一段文字，
// 而背后的分片（慢的部分）由需要的人主动请求。它在 goroutine 中运行，经 onMain 回来。
func (s *State) requestAnnotation() {
	service := s.translate
	text := s.translation.source
	if service == nil || strings.TrimSpace(text) == "" {
		return
	}
	s.translation.annotation = annotationState{status: annotationLoading}
	if s.translateWindow != nil {
		s.cardChanged()
		s.translateWindow.Update(func() {})
	}

	request := translate.Request{Text: text, Target: s.translation.result.TargetLang, Annotate: true}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), translationTimeout)
		defer cancel()
		result, err := service.Translate(ctx, request)
		s.onMain(func() {
			if s.translation.source != text {
				// 卡片已换成别的内容：这个答案属于一张已消失的翻译。
				return
			}
			switch {
			case err == nil:
				s.translation.annotation = annotationState{
					status:   annotationReady,
					segments: result.Segments,
					failure:  result.Note,
				}
			default:
				// 注音是第二个请求，可能在翻译本身成功后单独失败。
				slog.Warn("the readings could not be fetched", "error", err)
				s.translation.annotation = annotationState{
					status:  annotationFailed,
					failure: presentation.FriendlyError(err),
				}
			}
			// 注音改变了卡片高度，需要请求测量它的帧，与翻译一样。
			s.cardChanged()
			s.fitTranslationWindow()
		})
	}()
}

// readingsAvailable 决定卡片是否提供注音：原文为带读法或外来词释义的语言，
// 且设置中开启了二者之一。这决定是否显示打开注音的行——打开不了任何东西的控件比没有更糟。
func (s *State) readingsAvailable() bool {
	if s.translation.status != translateReady ||
		s.translation.result.SourceLang != domain.LangJapanese ||
		strings.TrimSpace(s.translation.source) == "" {
		return false
	}
	settings, err := s.translateSettings()
	if err != nil {
		return true
	}
	return settings.Furigana || settings.LoanwordGloss
}

// annotatedWords 是展开注音时显示的内容：每个有内容的词一条（汉字词给读法，外来词给原词），按出现顺序。
//
// 不是整句：读者打开它要的是读不出的词。同一个词出现两次合并为一条；写法相同但读法不同（同一汉字两种读法）
// 则是两条。
func (s *State) annotatedWords() []domain.Segment {
	showReading, showGloss := true, true
	if settings, err := s.translateSettings(); err == nil {
		showReading, showGloss = settings.Furigana, settings.LoanwordGloss
	}
	segments := s.translation.annotation.segments
	words := make([]domain.Segment, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		note, _ := annotationNote(segment, showReading, showGloss)
		if note == "" {
			continue
		}
		key := segment.Text + "\x00" + note
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		words = append(words, segment)
	}
	return words
}

// annotationNote 是某个词要说明的内容，以及它是否外来词自身的原词（设计中唯一用品牌绿写的注记）。
// 单一函数是为了让 chip 与列表一致：列表计为词注记的就是 chip 显示在上方的，去重依据也相同。
func annotationNote(segment domain.Segment, showReading, showGloss bool) (string, bool) {
	return presentation.AnnotationNote(segment, showReading, showGloss)
}

// dragCard 是卡片脚部的拖动把手。
//
// 指针事件来自框架：按下的元素会持续接收移动直到松开，因此从脚部开始的按下即使指针移出也继续拖动——
// 这才像拖动而非追着跑。
//
// 移动量是指针自按下以来的位移，而非指针位置：按下时记住指针和窗口位置，每次移动都把窗口放在起点加位移处。
// 事件自身的 X/Y 是指针在随卡片移动的框内的位置，加减它会使卡片跳到指针再追着跑；只有位移这个数不依赖卡片已移动。
//
// 只有按住时才拖动：脚部也会在无按下时看到指针经过，失去键盘后正在退出的卡片不能被指针经过而移动。
//
// 固定卡片也照常可拖：固定关乎失焦时保持打开，而非保持位置。
func (s *State) dragCard(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		window := s.translateWindow
		if window == nil {
			return false
		}
		s.translation.dragging = true
		s.translation.dragFrom = window.ContentBounds()
		s.translation.dragPointer = s.cursorPoint()
		s.cancelTranslationClose()
		return true
	case ui.InputPointerMove:
		window := s.translateWindow
		if window == nil || !s.translation.dragging || ev.Button != 0 {
			return false
		}
		at := s.cursorPoint()
		work := s.screenNear(at)
		bounds := s.translation.dragFrom
		bounds.X = clampInt(
			bounds.X+int(at.X-s.translation.dragPointer.X),
			work.X, max(work.X+work.Width-bounds.Width, work.X),
		)
		bounds.Y = clampInt(
			bounds.Y+int(at.Y-s.translation.dragPointer.Y),
			work.Y, max(work.Y+work.Height-bounds.Height, work.Y),
		)
		window.SetContentBounds(bounds)
		return true
	case ui.InputPointerUp:
		if !s.translation.dragging {
			return false
		}
		s.translation.dragging = false
		return true
	}
	return false
}

// endCardDrag 清除未释放的拖动：卡片被隐藏，或键盘转走而平台未通知脚部，
// 否则卡片会终生跟随指针。
func (s *State) endCardDrag() {
	s.translation.dragging = false
}

// copyTranslation 把卡片的一侧写回剪贴板。
//
// 走剪贴板工具而非直接调用平台，使这次写入与应用的其它写入一样被排除在历史之外：
// 轮询不能把译文读回来记成新的复制。条目本身就是被复制的文本，带剪贴板工具计算的哈希，
// 因此用户复制的译文就是一条普通剪贴板条目。
func (s *State) copyTranslation(which string) {
	if s.clipboard == nil {
		return
	}
	value := s.translation.source
	if which == "target" {
		value = s.translation.result.TranslatedText
	}
	if strings.TrimSpace(value) == "" {
		return
	}
	slog.Info("translation copied", "event", "translation_copied", "part", which, "image", s.translation.image, "chars", len([]rune(value)))
	s.translation.copied = which
	s.translation.copiedAt = time.Now()
	window := s.translateWindow
	if window != nil {
		// 复制反馈标记占了一个操作的位置，是需要重新测量的变化。
		s.cardChanged()
		window.Update(func() {})
	}
	entry := domain.ClipboardEntry{
		ID:          "translate-" + which,
		Kind:        domain.ClipKindText,
		Content:     value,
		Display:     value,
		ContentHash: kit.ClipContentHash(value),
		CopiedAt:    time.Now().UTC(),
	}
	// 写入在主线程之外运行，完成后收回标记：复制是平台调用，点击所在帧不能等它。
	s.clipboard.CopyAsync(entry, func(err error) {
		if err != nil {
			// 平台拒绝的复制在卡片中不可见：标记收回，什么也不说。
			slog.Warn("the translation could not be copied to the clipboard", "error", err)
		}
		if window == nil {
			return
		}
		time.AfterFunc(copiedMarkDuration, func() {
			window.Update(func() {
				if s.translation.copied == which && time.Since(s.translation.copiedAt) >= copiedMarkDuration {
					s.translation.copied = ""
					// 标记消失，卡片又略有变化。
					s.cardChanged()
				}
			})
		})
	})
}

// copiedLabel 是复制操作在反馈期间显示的文案。
func (s *State) copiedLabel(which string) bool {
	return s.translation.copied == which && time.Since(s.translation.copiedAt) < 2*copiedMarkDuration
}

// cursorPoint 是指针的屏幕坐标，卡片据此定位。它在主线程读取，快捷键回调也在此运行。
//
// 测试没有指针也没有运行中的应用（在 App.Run 前问平台是编程错误），所以可预先给定一个点，
// 仅在未给定时才问平台。
func (s *State) cursorPoint() mygo.Point {
	if s.cursorSet {
		return s.cursor
	}
	point := mygo.Screen.CursorScreenPoint()
	if point.X == 0 && point.Y == 0 {
		// 平台不应答时取主屏中心，而非左上角——那是无人看得见的卡片会去的地方。
		display := mygo.Screen.PrimaryDisplay()
		return mygo.Point{
			X: display.WorkArea.X + display.WorkArea.Width/2,
			Y: display.WorkArea.Y + display.WorkArea.Height/2,
		}
	}
	return point
}

// clampInt 把值限制在范围内，无论从哪端越界。
func clampInt(value, low, high int) int {
	if high < low {
		high = low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
