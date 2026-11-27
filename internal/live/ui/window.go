package ui

import (
	"math"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/live"
	"boku/internal/platform/windowshape"
	"boku/internal/presentation"
)

const (
	liveWidth     = 540
	liveMinHeight = 64
	liveMaxHeight = 480

	// 卡片边框盒 = 文本行 + 此内边距 + 边框；windowView 在窗口栏上使用相同数值。
	captionPadTop        float32 = 10
	captionPadRight      float32 = 8
	captionPadBottom     float32 = 8
	captionPadLeft       float32 = 14
	captionBorder        float32 = 1
	captionSourceSize    float32 = 13
	captionTargetSize    float32 = 16
	captionStatusSize    float32 = 11
	captionGroupGap      float32 = 8
	captionControlHeight float32 = 24
	captionBodyGap       float32 = 6
	captionSourceLines           = 3
	// 含标题和两个选项，使弹窗能向下展开。
	captionMenuRoom = 128
)

var (
	// 文字不随窗口栏透明度变化，且至少保持 80% 不透明。
	subtitleInk   = ui.RGBA(255, 255, 255, 1)
	subtitleMuted = ui.RGBA(255, 255, 255, 0.92)
	subtitleFaint = ui.RGBA(255, 255, 255, 0.84)
	subtitleHot   = ui.RGBA(255, 255, 255, 0.14)
	subtitleSlow  = ui.RGBA(251, 191, 36, 0.95)
)

func (s *State) show() {
	if s == nil || s.live == nil {
		return
	}
	if s.window == nil {
		window := s.newCaption()
		s.window = window
		s.forgetCaption()
		window.OnClosed(func() {
			if s.window == window {
				s.window = nil
				s.forgetCaption()
			}
			go s.live.Stop()
			s.host.Refresh()
		})
		s.place(window)
	}
	s.window.ShowInactive()
}

func (s *State) newCaption() *mygo.Window {
	content := ui.View(s.windowView)
	if runtime.GOOS == "windows" {
		var tray *mygo.Tray
		if s.tray != nil {
			tray = s.tray()
		}
		if tray != nil {
			// 普通透明窗口的边框不透明：栏的 alpha 只影响自身背景，
			// 系统标题栏按钮由 HUD 毛玻璃绘制，而托盘浮层会把该 alpha
			// 合成到背后内容上。文字保持实心，不要整体淡化窗口。
			// SetContentSize 会把它吸回托盘图标位置，后续移动须用
			// SetContentBounds。
			return mygo.NewFlyout(mygo.FlyoutOptions{
				Tray:      tray,
				Width:     liveWidth,
				Height:    liveMaxHeight,
				Hidden:    true,
				Focusable: false,
				Content:   content,
			})
		}
	}
	return mygo.NewWindow(mygo.WindowOptions{
		Title:           "实时翻译",
		Width:           liveWidth,
		Height:          liveMaxHeight,
		MinWidth:        liveWidth,
		MinHeight:       liveMinHeight,
		MaxWidth:        liveWidth,
		MaxHeight:       liveMaxHeight,
		Hidden:          true,
		Frameless:       true,
		DisableResize:   true,
		DisableShadow:   true,
		Transparent:     true,
		BackgroundColor: "#00000000",
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		// 非激活面板让用户正在收听的 App 保持在前。Windows 走上面的浮层；
		// 这里用于 macOS，以及托盘尚不存在时的回退。
		Panel:   runtime.GOOS == "darwin",
		Content: content,
	})
}

func (s *State) close() {
	window := s.window
	if window == nil {
		return
	}
	// 先丢弃窗口再 Close，让设置开关本帧就关掉；OnClosed 仍会停止监听。
	s.window = nil
	s.forgetCaption()
	window.Close()
}

func (s *State) forgetCaption() {
	s.clipped = -1
}

func (s *State) place(window *mygo.Window) {
	area := mygo.Screen.PrimaryDisplay().WorkArea
	bottom := area.Y + area.Height - 72
	height := liveMaxHeight
	y := bottom - height
	if y < area.Y {
		y = area.Y
		height = max(bottom-y, liveMinHeight)
	}
	// 固定顶边下方留出完整字幕所需空间。
	window.SetContentBounds(mygo.Rectangle{
		X: area.X + (area.Width-liveWidth)/2, Y: y, Width: liveWidth, Height: height,
	})
}

func (s *State) windowView(c *ui.Context) {
	s.useDesign(c)
	view := s.live.Snapshot()
	fill, edge := subtitleChrome(view.Settings.Opacity)
	var captionBottom, popupBottom float32
	root := c.Root().Background(ui.Transparent).DrawOver(func(_ *ui.Painter, _ ui.Rect) {
		// 下拉浮层的遮罩在字幕之后绘制；须在两者都上报当前布局后再适配一次。
		s.fitCaption(int(math.Ceil(float64(max(captionBottom, popupBottom)))))
	})
	root.Children(func() {
		c.Key("caption")
		bar := ui.Column(c).Absolute().Left(0).Right(0).
			Background(fill).
			Radius(float32(10)).
			Border(captionBorder, edge).
			Padding(captionPadTop, captionPadRight, captionPadBottom, captionPadLeft).
			Clip().Top(0).Justify(ui.Start).MaxHeight(float32(liveMaxHeight)).
			DragWindow().Cursor(ui.CursorMove).
			DrawOver(func(_ *ui.Painter, box ui.Rect) {
				captionBottom = box.Y + box.H
			})
		bar.Children(func() {
			c.Key("body")
			ui.Column(c).FillWidth().Shrink(0).Gap(captionBodyGap).Children(func() {
				s.controlRow(c, view).DrawOver(func(_ *ui.Painter, box ui.Rect) {
					// 在 macOS 适配时保留浮层的窗口边缘留白。
					popupBottom = box.Y + box.H + 4
				})
				ui.Column(c).FillWidth().Gap(captionGroupGap).Children(func() {
					s.caption(c, view)
				})
			})
		})
	})
}

func (s *State) caption(c *ui.Context, view live.View) {
	view = visibleCaptions(c, view)
	if len(view.Lines) == 0 && view.Draft == "" {
		captionLine(c, "日语会出现在这里", captionSourceSize, 0, subtitleFaint)
		captionLine(c, "译文", captionTargetSize, 500, subtitleFaint)
	} else {
		source, target := captionSpans(view)
		ui.RichText(c, source...).Wrap().Shrink(0)
		if len(target) > 0 {
			ui.RichText(c, target...).Wrap().Shrink(0)
		}
	}
	if status, ink := captionStatus(view); status != "" {
		captionLine(c, status, captionStatusSize, 0, ink)
	}
}

func captionSpans(view live.View) (source, target []ui.Span) {
	appendText := func(spans []ui.Span, text string, size float32, weight int, ink ui.Color) []ui.Span {
		text = strings.Join(strings.Fields(text), " ")
		if len(spans) > 0 {
			text = " " + text
		}
		return append(spans, ui.Span{Text: text, Size: size, Weight: weight, Color: ink})
	}
	for i, line := range view.Lines {
		current := view.Draft == "" && i == len(view.Lines)-1
		sourceInk, targetInk := subtitleFaint, subtitleMuted
		if current {
			sourceInk, targetInk = subtitleMuted, subtitleInk
		}
		source = appendText(source, line.Source, captionSourceSize, 0, sourceInk)
		if line.Target != "" {
			target = appendText(target, line.Target, captionTargetSize, 500, targetInk)
		}
	}
	if view.Draft != "" {
		source = appendText(source, view.Draft, captionSourceSize, 0, subtitleMuted)
	}
	return source, target
}

func captionLine(c *ui.Context, text string, size float32, weight int, ink ui.Color) {
	ui.RichText(c, ui.Span{Text: text, Size: size, Weight: weight}).TextColor(ink).Wrap().Shrink(0)
}

func captionStatus(view live.View) (string, ui.Color) {
	switch {
	case view.Slow:
		return "翻译稍慢", subtitleSlow
	case view.Notice != "":
		return view.Notice, subtitleSlow
	default:
		return view.Status, subtitleFaint
	}
}

// 句界不额外换行。只用实际绘制的 span，在三条换行后的日语行内保留
// 最新的完整句子对。
func visibleCaptions(c *ui.Context, view live.View) live.View {
	width := float32(liveWidth) - captionPadLeft - captionPadRight - 2*captionBorder
	budget := float32(liveMaxHeight) - captionPadTop - captionPadBottom - 2*captionBorder - captionControlHeight - captionBodyGap
	_, sourceLineHeight := c.MeasureText(width, ui.Span{Text: "あ", Size: captionSourceSize})
	if status, _ := captionStatus(view); status != "" {
		_, statusHeight := c.MeasureText(width, ui.Span{Text: status, Size: captionStatusSize})
		budget -= statusHeight + captionGroupGap
	}
	start := len(view.Lines)
	for i := len(view.Lines) - 1; i >= 0; i-- {
		candidate := view
		candidate.Lines = view.Lines[i:]
		source, target := captionSpans(candidate)
		_, sourceHeight := c.MeasureText(width, source...)
		height := sourceHeight
		if len(target) > 0 {
			_, targetHeight := c.MeasureText(width, target...)
			height += targetHeight + captionGroupGap
		}
		if sourceHeight > sourceLineHeight*captionSourceLines || height > budget {
			// 过长的当前句整句保留，只有更早的句子退出。
			if i == len(view.Lines)-1 && view.Draft == "" {
				start = i
			}
			break
		}
		start = i
	}
	view.Lines = view.Lines[start:]
	return view
}

func subtitleChrome(opacity int) (ui.Color, ui.Color) {
	if opacity < 30 {
		opacity = 30
	}
	if opacity > 100 {
		opacity = 100
	}
	alpha := float32(opacity) / 100
	return ui.RGBA(18, 18, 18, alpha), ui.RGBA(255, 255, 255, 0.28*alpha)
}

func (s *State) fitCaption(height int) {
	if s.window == nil || height <= 0 {
		return
	}
	if runtime.GOOS == "windows" {
		// 保持 Windows 合成表面稳定，仅暴露内容区域。
		if height != s.clipped && windowshape.ClipTop(s.window.NativeHandle(), height) {
			s.clipped = height
		}
		return
	}
	bounds := s.window.ContentBounds()
	height = max(liveMinHeight, min(height, liveMaxHeight))
	if bounds.Height == height {
		return
	}
	bounds.Height = height
	s.window.SetContentBounds(bounds)
}

func (s *State) controlRow(c *ui.Context, view live.View) ui.Element {
	var popup ui.Element
	ui.Row(c).FillWidth().Shrink(0).MinHeight(captionControlHeight).AlignItems(ui.Center).Gap(float32(4)).Children(func() {
		popup = s.inputPicker(c, view)
		ui.Spacer(c)
		if view.SettingsURL != "" {
			s.word(c, "打开系统设置", false, func() { c.OpenURL(view.SettingsURL) })
		}
		if !view.Ready && s.openSettings != nil {
			s.word(c, "去下载", false, s.openSettings)
		}
		label := "开始"
		if view.Listening {
			label = "停止"
		}
		s.word(c, label, view.Listening, func() {
			s.show()
			go s.live.Toggle()
		})
		s.closeButton(c)
	})
	return popup
}

func (s *State) inputPicker(c *ui.Context, view live.View) ui.Element {
	input := view.Settings.Input
	label := "系统声音"
	if input == live.InputMicrophone {
		label = "麦克风"
	}
	c.Key("audio-input")
	sel := ui.SelectBase(c, &input)
	sel.Trigger.Label("音源").Value(label).Gap(float32(4)).
		Padding(float32(3), float32(7)).Radius(float32(6)).
		Border(float32(1), ui.RGBA(255, 255, 255, 0.16)).
		Background(ui.RGBA(255, 255, 255, 0.06)).Cursor(ui.CursorPointer)
	if runtime.GOOS != "windows" {
		sel.Trigger.HandleInput(func(ev ui.InputEvent) bool {
			opening := ev.Kind == ui.InputPointerUp && ev.Button == 0 ||
				ev.Kind == ui.InputKeyDown && (ev.Key == ui.KeyEnter || ev.Key == ui.KeySpace || ev.Key == ui.KeyDown || ev.Key == ui.KeyUp)
			if opening && s.window != nil {
				// 在 SelectBase 布局第一帧弹窗之前预留空间。
				s.fitCaption(max(s.window.ContentBounds().Height, captionMenuRoom))
			}
			return false
		})
	}
	if sel.Open() || sel.Trigger.Hovered() || sel.Trigger.Focused() {
		sel.Trigger.Background(subtitleHot)
	}
	sel.Trigger.Children(func() {
		ui.Text(c, label).FontSize(float32(12)).FontWeight(500).TextColor(subtitleMuted).SingleLine()
		angle := float32(90)
		if sel.Open() {
			angle = 270
		}
		presentation.DisclosureArrow(c, angle, subtitleFaint)
	})
	popup := sel.Popup(func(panel ui.Element) {
		panel.MinWidth(float32(120)).Margin(float32(4), float32(0), float32(0), float32(0)).
			Padding(float32(4)).Radius(float32(8)).
			Background(ui.RGBA(18, 18, 18, 0.96)).
			Border(float32(1), ui.RGBA(255, 255, 255, 0.18))
		for _, option := range []struct{ value, label string }{
			{live.InputSystem, "系统声音"},
			{live.InputMicrophone, "麦克风"},
		} {
			item := sel.Item(option.value).Label(option.label).FillWidth().Gap(float32(8)).
				Padding(float32(6), float32(8)).Radius(float32(4)).Cursor(ui.CursorPointer)
			if item.Highlighted() {
				item.Background(subtitleHot)
			}
			item.Children(func() {
				ui.Text(c, option.label).FontSize(float32(12)).TextColor(subtitleMuted).SingleLine()
				ui.Spacer(c)
				check := ""
				if option.value == input {
					check = "✓"
				}
				ui.Text(c, check).Width(float32(12)).FontSize(float32(12)).TextColor(subtitleInk).SingleLine()
			})
		}
	})
	if sel.Trigger.Changed() {
		s.pickInput(view, input)
	}
	return popup
}

func (s *State) pickInput(view live.View, input string) {
	listening := view.Listening
	s.setInput(input)
	if listening {
		go func() {
			s.live.Stop()
			s.live.Toggle()
		}()
	}
}

func (s *State) word(c *ui.Context, label string, on bool, act func()) {
	b := ui.ButtonBase(c).Label(label).
		Padding(float32(2), float32(6)).
		Radius(float32(4)).
		Cursor(ui.CursorPointer)
	ink := subtitleFaint
	if on {
		ink = subtitleInk
	}
	if b.Hovered() || b.Focused() {
		b.Background(subtitleHot)
		ink = subtitleInk
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(float32(12)).FontWeight(500).TextColor(ink).SingleLine()
	})
	if b.Clicked() && act != nil {
		act()
	}
}

func (s *State) closeButton(c *ui.Context) {
	b := ui.ButtonBase(c).Label("关闭").Tooltip("关闭").
		Padding(float32(4)).
		Radius(float32(4)).
		Cursor(ui.CursorPointer).
		Shrink(0)
	ink := subtitleMuted
	if b.Hovered() || b.Focused() {
		b.Background(subtitleHot)
		ink = subtitleInk
	}
	b.Children(func() {
		if s.closeMark != nil {
			ui.Icon(c, s.closeMark).Size(float32(14), float32(14)).Shrink(0).TextColor(ink)
			return
		}
		ui.Text(c, "关闭").FontSize(float32(12)).TextColor(ink).SingleLine()
	})
	if b.Clicked() {
		s.close()
	}
}
