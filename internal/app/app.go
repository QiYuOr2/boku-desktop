// Package app 是表现层：视图状态、MyGo 绘制的视图，以及展示它们的窗口。
//
// 它拥有托盘图标和 flyout，因为 MyGo 的回调和视图都在主线程上运行并共享同一份状态。
// 一切都在这条线程上创建和修改；State 的任何字段都不得从 goroutine 触碰，
// 后台工作的结果通过 Window.Update 交回。
//
// 面板本身不是这里的状态：用例持有它们，因此窗口关闭时完成的刷新不会丢失结果，
// 视图只读取已知内容，自身不做任何工作。
package app

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	animeui "boku/internal/anime/ui"
	"boku/internal/app/assets"
	bilibiliui "boku/internal/bilibili/ui"
	"boku/internal/clipboard"
	clipboardui "boku/internal/clipboard/ui"
	"boku/internal/credential"
	credentialui "boku/internal/credential/ui"
	"boku/internal/diary"
	diaryui "boku/internal/diary/ui"
	"boku/internal/domain"
	"boku/internal/flash"
	flashui "boku/internal/flash/ui"
	"boku/internal/live"
	liveui "boku/internal/live/ui"
	"boku/internal/panel"
	panelui "boku/internal/panel/ui"
	"boku/internal/platform/autostart"
	"boku/internal/playtime"
	playtimeui "boku/internal/playtime/ui"
	"boku/internal/preferences"
	preferencesui "boku/internal/preferences/ui"
	"boku/internal/presentation"
	"boku/internal/translate"
	translateui "boku/internal/translate/ui"
	updateui "boku/internal/update/ui"
)

const (
	appName = "Boku"
	// toolTip 是托盘图标上悬停时系统显示的文本：只有应用名。
	toolTip     = appName
	panelWidth  = 320
	panelHeight = 380
	// quickTaskRowHeight 是 flyout 中一个图片任务预留的垂直空间；
	// 实际行高由内容决定，这是保守估计。
	quickTaskRowHeight = 52
	// quickEnabledHeightShare 是打开阅读块（今日动画更新或被关注的 UP）时 flyout 的开启高度占比。
	//
	// 窗口不按内容调整大小：从视图测量内容会跟随滚动偏移，
	// 拖动滚动条会让窗口在指针下改变高度。因此在工作区绘制前就按比例定高，读时不会移动。
	quickEnabledHeightShare = 0.30
	// trayIconSize 是图标的设备像素尺寸：Retina 下为 16 点，
	// MyGo 在 Windows 与 Linux 上会按屏幕密度缩放。
	trayIconSize = 32
)

// Deps 是 main 用来装配表现层的依赖。
type Deps struct {
	Panels   *panel.Panels
	Anime    AnimeService
	Bilibili BilibiliService
	Secrets  *credential.Secrets
	// Clipboard 读写的系统剪贴板。没有它的 State 不绘制快捷窗口的剪贴板行，也不绘制历史窗口。
	Clipboard *clipboard.Clipboard
	// Translate 是划词翻译工具。没有它的 State 不绘制翻译窗口，也不绑定其快捷键。
	Translate *translate.Translate
	// Flash 是文字闪念文档。没有它的 State 不绘制输入行，也没有历史。
	Flash *flash.Service
	// Diary 是个人日记服务。DiaryUnavailable 为 true 时仍保留入口并提示数据库不可读，绝不覆盖原文件。
	Diary            *diary.Service
	DiaryUnavailable bool
	// Live 是本地语音翻译。没有它的 State 不绘制浮动窗口，也不绑定其快捷键。
	Live       *live.Service
	Screenshot domain.ScreenshotDevice
	// RegionCapture 读取物理桌面像素。存在时 App 使用自带的跨平台选区覆盖层，
	// 不调用系统截图 UI，也不使用剪贴板。
	RegionCapture RegionCapture
	// RegisterOwnServices 为用户自建的每个服务注册翻译器。窗口在用户添加后调用它：
	// 设置里有、注册表却不认识的服务是一行无法回答的行。从不添加服务的测试里它为 nil。
	RegisterOwnServices func() error
	// Shortcuts 绑定全局快捷键。没有它的 State 仍能运行：绑定失败的快捷键只是少一条入口，不是启动失败。
	Shortcuts ShortcutRegistrar
	Version   string
	Quit      func()
	// Appearance 是用户选的主题。它是表现层自己的设置，存在功能键旁边；
	// nil 表示「跟随桌面」，也是默认值。
	Appearance *Appearance
	// AppearanceStore 是写入该选择的设置存储。没有它的 State 仍会在本次运行内改变外观。
	AppearanceStore        AppearanceStore
	QuickWindowLayout      *preferences.QuickWindowLayout
	QuickWindowLayoutStore preferences.LayoutStore
	// ApplyAppearance 把所选外观交给桌面，使标题栏、菜单和系统绘制的窗口边框与 Boku 的绘制一致。
	// 无桌面的测试里它为 nil。
	ApplyAppearance func(AppearanceMode)
	// PopoverAppearance 给原生 popover 提供强制深色模式所需的深色外观。
	// 平台无法做到时为 nil，此时强制深色使用 App 自己的卡片。
	PopoverAppearance PopoverAppearance
	// Autostart 是平台的登录自启动：「开机自动启动」读写的对象。
	// 未注入时为 nil，没有它的 State 不绘制开关——一个什么都做不了的开关比没有更糟。
	Autostart autostart.Manager
	// Playtime 是录制会话的托盘读数。nil 时不绘制任何内容。
	Playtime *playtime.Recorder
	// Updates 是更新检查。没有它的 State 不绘制更新行、不启动检查、也不加入托盘菜单。
	// 该接口是表现层自己的，因此这里不 import internal/update 或 MyGo 的更新器。
	Updates UpdateService
}

// AnimeService 是动画功能面向表现层的小契约，定义在此使 app 不依赖该功能包。
type AnimeService interface {
	Enabled() bool
	Username() string
	RefreshMinutes() int
	Configure(context.Context, bool, string, int) error
	View() ([]domain.EpisodeRelease, time.Time, bool, error)
	Refresh(context.Context) error
	RefreshIfNeeded(context.Context) error
	Cover(subjectID int64) []byte
}

// BilibiliService 是 UP 监控功能面向表现层的小契约，使 app 不依赖该功能包。
type BilibiliService interface {
	Enabled() bool
	UIDs() []int64
	RefreshMinutes() int
	DynamicCount() int
	Notifications() (live, dynamic bool)
	Configure(context.Context, bool, []int64, int, int, bool, bool) error
	View() ([]domain.FollowedUpdate, time.Time, bool, error)
	Refresh(context.Context) error
	RefreshIfNeeded(context.Context) error
	// Avatar 返回某个创作者已抓取的头像字节，或 nil。
	Avatar(int64) []byte
}

// State 是整个表现层状态，仅在主线程读写。
type State struct {
	app               *mygo.Application
	panelService      *panel.Panels
	panelUI           *panelui.State
	animeService      AnimeService
	bilibiliService   BilibiliService
	animeUI           *animeui.ViewModel
	bilibiliUI        *bilibiliui.ViewModel
	secretService     *credential.Secrets
	credentialUI      *credentialui.State
	clipboard         *clipboard.Clipboard
	playtime          *playtime.Recorder
	playtimePanel     *playtimeui.Panel
	clipboardUI       *clipboardui.State
	translate         *translate.Translate
	translationUI     *translateui.State
	flashUI           *flashui.State
	diaryUI           *diaryui.State
	liveUI            *liveui.State
	liveMark          *ui.SVG
	version           string
	quit              func()
	appearance        *Appearance
	appearanceStore   AppearanceStore
	applyAppearance   func(AppearanceMode)
	popoverAppearance PopoverAppearance
	preferencesUI     *preferencesui.State
	layoutUI          *preferencesui.LayoutState
	quickWindowLayout *preferences.QuickWindowLayout
	layoutMark        *ui.SVG
	updates           UpdateService
	notifier          desktopNotifier
	updateUI          *updateui.State
	dark              bool
	systemDark        bool
	trayIcon          []byte
	deepseekMark      *ui.SVG
	gaugeMark         *ui.SVG
	tvMark            *ui.SVG
	bilibiliMark      *ui.SVG
	chevronRightMark  *ui.SVG
	clipboardMark     *ui.SVG
	refreshMark       *ui.SVG
	historyText       *ui.SVG
	historyImage      *ui.SVG
	historyLink       *ui.SVG
	historyCode       *ui.SVG
	settingsMark      *ui.SVG
	quitMark          *ui.SVG
	diaryMark         *ui.SVG
	flashMark         *ui.SVG
	screenshotMark    *ui.SVG
	qrMark            *ui.SVG
	copyMark          *ui.SVG
	pinMark           *ui.SVG
	closeMark         *ui.SVG
	translationMark   *ui.SVG
	keyMark           *ui.SVG
	infoMark          *ui.SVG
	tray              *mygo.Tray
	panel             *mygo.Window
	panelPopover      bool
	panelInjectedDark bool
	settingsWindow    *mygo.Window
	historyWindow     *mygo.Window
	flashCapture      *mygo.Window
	flashHistory      *mygo.Window
	diaryWindow       *mygo.Window
	ownServices       func() error
	panelStatus       string
	settingsScroll    ui.ScrollState
	settingsSection   settingsSectionKind
	settingsReveal    *settingsSectionKind
	settingsAiming    bool
	settingsAimAt     settingsSectionKind
	settingsRoom      float32
}

// RegionCapture 是 App 自有选区选择器背后的窄平台边界。
// 矩形使用平台读取器采用的坐标空间：macOS 上是屏幕点，Windows 上是物理虚拟桌面像素。
type RegionCapture = translateui.RegionCapture

// New 构建托盘图标和 App 状态。调用方仍须在 MyGo 事件循环就绪后运行 mygo.App.WhenReady 中的 Setup：
// 在此之前任何窗口或托盘都无法存在。
func New(app *mygo.Application, deps Deps) (*State, error) {
	if app == nil {
		return nil, fmt.Errorf("app: no application")
	}
	if deps.Panels == nil {
		return nil, fmt.Errorf("app: no panel service")
	}
	if deps.Secrets == nil {
		return nil, fmt.Errorf("app: no secret service")
	}
	// 不关心主题的测试所构建的 State 仍有一个主题：跟随桌面是用户从未选择时的结果。
	appearance := deps.Appearance
	if appearance == nil {
		appearance = NewAppearanceSection()
	}
	trayIcon, err := assets.Icon(trayIconSize)
	if err != nil {
		return nil, err
	}
	// 快捷窗口的块带着各自提供者的标记，仓库里的这些标记在这里一并解析。
	deepseekMark, err := ui.ParseSVG(assets.DeepSeek())
	if err != nil {
		return nil, fmt.Errorf("app: parse the DeepSeek mark: %w", err)
	}
	gaugeMark, err := ui.ParseSVG(assets.Gauge())
	if err != nil {
		return nil, fmt.Errorf("app: parse the placeholder mark: %w", err)
	}
	codexMark, err := ui.ParseSVG(assets.Codex())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Codex mark: %w", err)
	}
	grokMark, err := ui.ParseSVG(assets.Grok())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Grok mark: %w", err)
	}
	tvMark, err := ui.ParseSVG(assets.TV())
	if err != nil {
		return nil, fmt.Errorf("app: parse the TV mark: %w", err)
	}
	bilibiliMark, err := ui.ParseSVG(assets.TVBilibili())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Bilibili mark: %w", err)
	}
	chevronRightMark, err := ui.ParseSVG(assets.ChevronRight())
	if err != nil {
		return nil, fmt.Errorf("app: parse the chevron-right mark: %w", err)
	}
	clipMark, err := ui.ParseSVG(assets.Clipboard())
	if err != nil {
		return nil, fmt.Errorf("app: parse the clipboard mark: %w", err)
	}
	refreshMark, err := ui.ParseSVG(assets.Refresh())
	if err != nil {
		return nil, fmt.Errorf("app: parse the refresh mark: %w", err)
	}
	historyText, err := ui.ParseSVG(assets.HistoryText())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history text mark: %w", err)
	}
	historyImage, err := ui.ParseSVG(assets.HistoryImage())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history image mark: %w", err)
	}
	historyLink, err := ui.ParseSVG(assets.HistoryLink())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history link mark: %w", err)
	}
	historyCode, err := ui.ParseSVG(assets.HistoryCode())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history code mark: %w", err)
	}
	settingsMark, err := ui.ParseSVG(assets.Settings())
	if err != nil {
		return nil, fmt.Errorf("app: parse the settings mark: %w", err)
	}
	diaryMark, err := ui.ParseSVG(assets.Diary())
	if err != nil {
		return nil, fmt.Errorf("app: parse the diary mark: %w", err)
	}
	flashMark, err := ui.ParseSVG(assets.Lightbulb())
	if err != nil {
		return nil, fmt.Errorf("app: parse the flash mark: %w", err)
	}
	quitMark, err := ui.ParseSVG(assets.Quit())
	if err != nil {
		return nil, fmt.Errorf("app: parse the quit mark: %w", err)
	}
	screenshotMark, err := ui.ParseSVG(assets.Screenshot())
	if err != nil {
		return nil, fmt.Errorf("app: parse the screenshot mark: %w", err)
	}
	qrMark, err := ui.ParseSVG(assets.QR())
	if err != nil {
		return nil, fmt.Errorf("app: parse the QR mark: %w", err)
	}
	copyMark, err := ui.ParseSVG(assets.Copy())
	if err != nil {
		return nil, fmt.Errorf("app: parse the copy mark: %w", err)
	}
	pinMark, err := ui.ParseSVG(assets.Pin())
	if err != nil {
		return nil, fmt.Errorf("app: parse the pin mark: %w", err)
	}
	closeMark, err := ui.ParseSVG(assets.Close())
	if err != nil {
		return nil, fmt.Errorf("app: parse the close mark: %w", err)
	}
	commandMark, err := ui.ParseSVG(assets.Command())
	if err != nil {
		return nil, fmt.Errorf("app: parse the command mark: %w", err)
	}
	returnMark, err := ui.ParseSVG(assets.Return())
	if err != nil {
		return nil, fmt.Errorf("app: parse the return mark: %w", err)
	}
	// 翻译区的标记是应用自己的绘图，不是品牌标记：它作为 Boku 设置中一个区块的标题，而非代表某项服务。
	translationMark, err := ui.ParseSVG(assets.Translate())
	if err != nil {
		return nil, fmt.Errorf("app: parse the translation mark: %w", err)
	}
	liveMark, err := ui.ParseSVG(assets.Live())
	if err != nil {
		return nil, fmt.Errorf("app: parse the live translation mark: %w", err)
	}
	// 侧边栏要到达每个区块，包括两个原本没有自己标记的：密钥区和关于框。
	keyMark, err := ui.ParseSVG(assets.Key())
	if err != nil {
		return nil, fmt.Errorf("app: parse the key mark: %w", err)
	}
	infoMark, err := ui.ParseSVG(assets.Info())
	if err != nil {
		return nil, fmt.Errorf("app: parse the info mark: %w", err)
	}
	layoutMark, err := ui.ParseSVG(assets.Layout())
	if err != nil {
		return nil, fmt.Errorf("app: parse the layout mark: %w", err)
	}
	eyeMark, err := ui.ParseSVG(assets.Eye())
	if err != nil {
		return nil, fmt.Errorf("app: parse the eye mark: %w", err)
	}
	eyeOffMark, err := ui.ParseSVG(assets.EyeOff())
	if err != nil {
		return nil, fmt.Errorf("app: parse the hidden eye mark: %w", err)
	}
	gripMark, err := ui.ParseSVG(assets.Grip())
	if err != nil {
		return nil, fmt.Errorf("app: parse the drag mark: %w", err)
	}
	steamMark, err := ui.ParseSVG(assets.Steam())
	if err != nil {
		return nil, fmt.Errorf("app: parse the playtime mark: %w", err)
	}
	dualSenseMark, err := ui.ParseSVG(assets.DualSense())
	if err != nil {
		return nil, fmt.Errorf("app: parse the DualSense mark: %w", err)
	}
	switchProMark, err := ui.ParseSVG(assets.SwitchPro())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Switch Pro mark: %w", err)
	}
	xboxMark, err := ui.ParseSVG(assets.Xbox())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Xbox mark: %w", err)
	}
	quickLayout := deps.QuickWindowLayout
	if quickLayout == nil {
		quickLayout = preferences.NewQuickWindowLayout()
	}
	state := &State{
		app:               app,
		panelService:      deps.Panels,
		animeService:      deps.Anime,
		bilibiliService:   deps.Bilibili,
		animeUI:           animeui.New(deps.Anime),
		bilibiliUI:        bilibiliui.New(deps.Bilibili),
		secretService:     deps.Secrets,
		clipboard:         deps.Clipboard,
		playtime:          deps.Playtime,
		playtimePanel:     &playtimeui.Panel{},
		translate:         deps.Translate,
		ownServices:       deps.RegisterOwnServices,
		version:           deps.Version,
		quit:              deps.Quit,
		appearance:        appearance,
		appearanceStore:   deps.AppearanceStore,
		applyAppearance:   deps.ApplyAppearance,
		popoverAppearance: deps.PopoverAppearance,
		updates:           deps.Updates,
		notifier:          mygoNotifier{},
		dark:              appearance.Dark(false),
		trayIcon:          trayIcon,
		deepseekMark:      deepseekMark,
		gaugeMark:         gaugeMark,
		tvMark:            tvMark,
		bilibiliMark:      bilibiliMark,
		chevronRightMark:  chevronRightMark,
		clipboardMark:     clipMark,
		refreshMark:       refreshMark,
		historyText:       historyText,
		historyImage:      historyImage,
		historyLink:       historyLink,
		historyCode:       historyCode,
		settingsMark:      settingsMark,
		quitMark:          quitMark,
		diaryMark:         diaryMark,
		flashMark:         flashMark,
		screenshotMark:    screenshotMark,
		qrMark:            qrMark,
		copyMark:          copyMark,
		pinMark:           pinMark,
		closeMark:         closeMark,
		translationMark:   translationMark,
		liveMark:          liveMark,
		keyMark:           keyMark,
		infoMark:          infoMark,
		layoutMark:        layoutMark,
		quickWindowLayout: quickLayout,
	}
	// 更新服务在 State 之前构建，因此在这里一次性安装它的变更监听器，在任何人索要检查之前：
	// 没有监听器的服务是一行永远不会重绘的行。
	var connections credentialui.Connections
	if deps.Translate != nil {
		connections = deps.Translate
	}
	state.credentialUI = credentialui.New(connections, deps.Secrets, state.featureHost(), func() error {
		if state.ownServices != nil {
			return state.ownServices()
		}
		return nil
	}, func() { state.refreshAsync(true) }, keyMark)
	state.translationUI = translateui.New(translateui.Config{Service: deps.Translate, Clipboard: deps.Clipboard, Screenshot: deps.Screenshot, RegionCapture: deps.RegionCapture, Shortcuts: deps.Shortcuts, Host: state.featureHost(), Dark: func() bool { return state.dark }, Notifier: state.notifier, ConnectionsBusy: func() bool { return state.credentialUI.Form.Busy != "" }, CopyMark: copyMark, PinMark: pinMark, CloseMark: closeMark, ScreenshotMark: screenshotMark, TranslationMark: translationMark})
	state.panelUI = panelui.New(deps.Panels, state.featureHost(), state.openSettings, deepseekMark, codexMark, grokMark, gaugeMark, refreshMark)
	state.preferencesUI = preferencesui.New(deps.Autostart, state.featureHost())
	state.layoutUI = preferencesui.NewLayout(quickLayout, deps.QuickWindowLayoutStore, state.featureHost(), layoutMark, eyeMark, eyeOffMark, gripMark, map[string]presentation.PanelMark{
		"deepseek": state.panelUI.ProviderMark("deepseek"),
		"codex":    state.panelUI.ProviderMark("codex"),
		"grok":     state.panelUI.ProviderMark("grok"),
		"anime":    {SVG: tvMark},
		"bilibili": {SVG: bilibiliMark},
		"playtime": {SVG: steamMark},
	})
	state.layoutUI.SetOffer(func(id string) bool {
		if id != "playtime" {
			return true
		}
		return state.playtime != nil && state.playtime.Available()
	})
	state.clipboardUI = clipboardui.New(deps.Clipboard, state.featureHost(), state.openHistory)
	if deps.Flash != nil {
		state.flashUI = flashui.New(flashui.Config{
			Service:     deps.Flash,
			Host:        state.featureHost(),
			CopyText:    state.copyFlashText,
			RebindAll:   state.RegisterShortcuts,
			ShowCapture: state.openFlashCapture,
			HideCapture: state.hideFlashCapture,
			Mark:        state.flashMark,
			CloseMark:   state.closeMark,
			CommandMark: commandMark,
			ReturnMark:  returnMark,
		})
	}
	if deps.Diary != nil || deps.DiaryUnavailable {
		state.diaryUI = diaryui.New(deps.Diary, state.featureHost(), state.diaryMark, state.openDiarySettings)
	}
	if deps.Live != nil {
		state.liveUI = liveui.New(liveui.Config{
			Service:      deps.Live,
			Host:         state.featureHost(),
			Dark:         func() bool { return state.dark },
			OpenSettings: state.openSettings,
			Mark:         liveMark,
			CloseMark:    closeMark,
			Tray:         func() *mygo.Tray { return state.tray },
		})
	}
	if state.flashUI != nil {
		state.translationUI.SetExtraRebind(func(register func(string, func()) error, text, screenshot string) {
			state.flashUI.Rebind(register, text, screenshot)
		})
	}
	state.bilibiliUI.AttachHost(state.featureHost())
	state.animeUI.AttachHost(state.featureHost(), deps.Secrets)
	state.prepareUpdates(deps.Updates)
	state.playtimePanel.SetControllerMarks(dualSenseMark, switchProMark, xboxMark)
	return state, nil
}

// AttachUpdates 给 State 装上更新服务，或替换它已有的。
// 装配在服务晚于（或在）State 之后构建时调用它：服务的变更监听器可能从另一个 goroutine 触发，
// 在 State 已存在后安装它，意味着每次报告都能送达可重绘的 State。
//
// 传给 New 的服务也会在那里安装同一个监听器，因此装配只需二者之一。
// 两者都没有的 State 只是没有更新功能，关于更新的一切都消失而非损坏。
func (s *State) AttachUpdates(updates UpdateService) {
	s.prepareUpdates(updates)
}

// Setup 添加托盘图标并接好其点击。必须在应用就绪后的主线程调用。
func (s *State) Setup() error {
	if s.tray != nil {
		return fmt.Errorf("app: the tray already exists")
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon: s.trayIcon,
		// 黑白透明图片让 macOS 给菜单栏图标着色；Windows 与 Linux 忽略该标志，保留 PNG 的颜色。
		IconIsTemplate: runtime.GOOS == "darwin",
		ToolTip:        toolTip,
		// 不设 Menu：只要托盘有菜单，MyGo 就会在左键单击时显示它，从而使左键无法打开面板。
		// 菜单只在右键单击期间设置。
	})
	if err != nil {
		return fmt.Errorf("app: create the tray: %w", err)
	}
	s.tray = tray
	s.logTrayBounds()
	s.RefreshSelectionAccess()
	requestScreenRecording()
	if runtime.GOOS == "linux" {
		// AppIndicator 只能显示菜单、不报告点击，因此那里由菜单打开面板。
		tray.SetMenu(s.menu())
		return nil
	}
	tray.OnClick(s.Toggle)
	tray.OnRightClick(s.popUpMenu)
	return nil
}

// trayDebugEnv 让 Setup 报告系统认为托盘图标所在的位置。
//
// 不要把零矩形读作「图标从未添加」。MyGo 调用 Shell_NotifyIconGetRect，
// 而它在 Windows 11 上对每个托盘图标都返回 E_FAIL——2025-11 硬件上对 Chrome、Steam、
// PowerToys、Raycast 及本 App 窗口实测皆然。非零矩形值得获取；零表示「无答案」，不是「无图标」。
const trayDebugEnv = "BOKU_TRAY_DEBUG"

func (s *State) logTrayBounds() {
	if os.Getenv(trayDebugEnv) == "" {
		return
	}
	r := s.tray.Bounds()
	log.Printf("tray: bounds x=%d y=%d w=%d h=%d (zeros mean the shell did not "+
		"answer; see the comment on trayDebugEnv)", r.X, r.Y, r.Width, r.Height)
}

// Destroy 移除托盘图标。MyGo 在应用退出时于主线程调用它；
// 留下图标会让系统显示一个死图标，直到指针移过它。
func (s *State) Destroy() {
	if s.tray != nil {
		s.tray.Destroy()
		s.tray = nil
	}
}

// Refresh 请求已打开的窗口绘制一帧。后台答复（一次新的剪贴板读取、一次开关切换）调用它，
// 且可从其它 goroutine 安全调用：Window.Update 会把它送到主线程，而主线程是唯一可以触碰此状态的线程。
func (s *State) Refresh() {
	s.featureHost().Refresh()
}

// featureHost 是功能 UI 包使用的 Shell 适配器。它守在 app 边界，使任何功能都无需 import app.State 或 MyGo 窗口。
func (s *State) featureHost() presentation.Host {
	return presentation.Host{
		RequestRefresh: func() { s.refresh(nil) },
		RunOnMain: func(fn func()) {
			go mygo.RunOnMain(func() { fn(); s.Refresh() })
		},
		FitQuickWindow: s.fitQuickWindow,
		SetStatus: func(text string) {
			s.panelStatus = text
		},
	}
}

// refresh 是带「重绘请求后要在主线程执行的工作」的 Refresh：有结果要交出的 goroutine 以闭包形式在此交出，
// 而不自行触碰状态。
//
// 窗口是 MyGo 通往主线程的方式（Window.Update 向它投递后返回），因此第一个存在的窗口承载工作，
// 其余窗口只负责重绘。没有窗口打开时无人承载，而丢弃它会丢失调用方自己做不到的事——
// 例如快捷窗口关闭时完成的更新通知。于是改为投递到一个 goroutine 上，这样做是安全的，
// 因为该工作只是请求 MyGo 显示通知：这个请求本身由框架送到主线程，其中不触碰任何状态。
func (s *State) refresh(after func()) {
	if s.panel != nil {
		s.panel.Update(refreshCallback(after))
		after = nil
	}
	if s.settingsWindow != nil {
		s.settingsWindow.Update(refreshCallback(after))
		after = nil
	}
	if s.historyWindow != nil {
		s.historyWindow.Update(refreshCallback(after))
		after = nil
	}
	if s.flashCapture != nil {
		s.flashCapture.Update(refreshCallback(after))
		after = nil
	}
	if s.flashHistory != nil {
		s.flashHistory.Update(refreshCallback(after))
		after = nil
	}
	if s.diaryWindow != nil {
		s.diaryWindow.Update(refreshCallback(after))
		after = nil
	}
	if s.translationUI != nil && s.translationUI.Repaint(refreshCallback(after)) {
		after = nil
	}
	if s.liveUI != nil && s.liveUI.Repaint(refreshCallback(after)) {
		after = nil
	}
	if after != nil {
		go after()
	}
}

// refreshCallback 把「只重绘」的请求变成可调用的空操作。MyGo 的 Window.Update 总会调用其参数，
// 因此 nil 不是请求单纯重绘的合法方式。第一个打开的窗口接收 after；其后每个窗口接收空操作，
// 保持「额外工作恰好执行一次」的规则。
func refreshCallback(after func()) func() {
	if after != nil {
		return after
	}
	return func() {}
}

// menu 是托盘菜单：托盘 App 的唯一出口，它没有主窗口可关。
func (s *State) menu() *mygo.Menu {
	items := []*mygo.MenuItem{
		{Label: "打开 " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { s.Toggle() }},
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { s.openSettings() }},
	}
	if s.diaryUI != nil {
		items = append(items, &mygo.MenuItem{Label: "日记", Click: func(*mygo.MenuItem, *mygo.Window) { s.openDiary() }})
	}
	if s.flashUI != nil {
		items = append(items,
			&mygo.MenuItem{Label: "记一条闪念", Click: func(*mygo.MenuItem, *mygo.Window) { s.openFlashCapture() }},
			&mygo.MenuItem{Label: "闪念历史", Click: func(*mygo.MenuItem, *mygo.Window) { s.openFlashHistory() }},
		)
	}
	// 就绪的更新在此赢得一行，位于设置与出口之间：菜单是应用被别的窗口挡住时唯一可达的地方。
	// 其它状态都不配一行——在四行菜单里，一个永远无事可做的条目只是噪音。
	items = append(items, s.updateMenuItems()...)
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出 " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { s.quit() }},
	)
	return mygo.NewMenu(items)
}

// popUpMenu 仅在本次右键单击时在图标旁显示菜单，如框架的 menubar 示例所做：
// 若菜单一直设置着，每次左键单击都会变成菜单，而左键正是打开面板的方式。
func (s *State) popUpMenu() {
	if s.panel != nil {
		s.panel.Close()
	}
	s.tray.SetMenu(s.menu())
	defer s.tray.SetMenu(nil)
	s.tray.PopUpMenu()
}

// Toggle 在托盘图标旁打开快捷窗口，若已打开则关闭。它是托盘点击、菜单项和全局快捷键调用的入口，
// 因此必须立刻返回，且绝不等待任何东西。
func (s *State) Toggle() {
	if s.panel != nil {
		s.panel.Close()
		return
	}
	s.Open()
}

// Activate 把快捷窗口带到前台：这是应用第二次启动所请求的，因为运行中的实例拥有托盘、自身没有窗口可抬起。
// 已打开的窗口被聚焦而非再次打开，因此第二次启动不会留下两个窗口。
//
// 尚不存在的托盘没有任何东西可作锚点：MyGo 的 NewFlyout 拒绝无托盘或父窗口的 flyout，
// 而在应用仍在启动时到来的第二次启动没有东西可显示。
func (s *State) Activate() {
	if s.tray == nil {
		return
	}
	if s.panel != nil {
		s.panel.Focus()
		return
	}
	s.Open()
}

// quickWindowUsesPopover 报告快捷窗口能否就是系统 popover。自动外观下可以：
// 其材质跟随桌面，因而与 Boku 的页面相配。固定深色外观只有注入深色材质时才能用；
// 固定浅色则绘制应用自己的卡片，因为深色桌面上的原生 popover 会在浅色页面背后变深。
func (s *State) quickWindowUsesPopover() bool {
	switch s.appearance.Mode() {
	case AppearanceSystem:
		// 材质跟随桌面，页面亦然。
		return true
	case AppearanceDark:
		// 材质不跟随被强制的外观；注入的外观才是让原生 popover 在此可用的原因。
		return s.popoverAppearance != nil
	default:
		// 强制浅色保留应用卡片：平台桥接刻意只用于深色，
		// 而深色桌面上的原生 popover 会在浅色页面背后变深。
		return false
	}
}

// quickWindowPlacement 决定快捷窗口从托盘图标出发的位置。
// popover 把箭头对准图标中心；应用卡片在 macOS 上出于同样原因居中，其余情况保持既有末端对齐。
func quickWindowPlacement(goos string, popover bool) mygo.Placement {
	if goos == "darwin" && !popover {
		return mygo.PlacementTop
	}
	return mygo.PlacementTopEnd
}

// Open 在托盘图标旁显示一个新的快捷窗口。
//
// 每次打开都重新创建窗口，而不是隐藏复用：flyout 在创建时贴靠托盘，复用会停在它首次显示的位置。
func (s *State) Open() {
	popover := s.quickWindowUsesPopover()
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      s.tray,
		Placement: quickWindowPlacement(runtime.GOOS, popover),
		Gap:       4,
		Width:     panelWidth,
		Height:    s.quickWindowHeight(),
		// Focusable：点击外部会关闭它，如系统菜单那样。
		Focusable: true,
		Shadow:    true,
		// Popover：macOS 上是 NSPopover，绘制材质、圆角和指向图标的箭头。
		// 自动外观下使用，注入深色材质时固定深色也使用；固定浅色则绘制应用卡片。
		// 其它平台忽略该选项，由视图自绘背景。
		Popover: popover,
		Content: ui.View(s.View),
	})
	s.panel = panel
	s.panelPopover = popover
	s.syncPopoverAppearance()
	panel.OnClosed(func() {
		if s.panel == panel {
			s.panel = nil
			s.panelPopover = false
			s.panelInjectedDark = false
		}
	})
	// 打开窗口正是刷新的时机，且每次托盘点击无论缓存如何都会再次请求：一次点击显示的数字应是此刻的数字，
	// 而非上次点击留下的。请求在主线程之外运行；视图从不自行启动工作，因为 MyGo 每帧会构建它多次。
	//
	// 动画服务可能已持有今日的磁盘缓存。在第一帧前把它的封面字节转成 UI 位图，
	// 使缓存支撑的打开不会在网络刷新仍在进行时闪现占位标记。这是表现线程的图像解码，不是 I/O。
	s.cacheAnimeCovers()
	s.CachePlaytimeCovers()
	// 被关注 UP 的块可能已持有上次刷新的行，其头像也可能已抓取。在第一帧前把它们转成位图并为它们定好 flyout 尺寸，
	// 使打开时不会显示稍后才解析出来的占位符。
	s.cacheBilibiliAvatars()
	s.fitQuickWindow()
	s.refreshAsync(true)
}

// quickWindowHeight 根据开关状态在半个屏幕高度上限内确定 flyout 尺寸。回退值让无屏幕的测试保持确定性。
func (s *State) quickWindowHeight() int {
	work := s.screenNear(s.cursorPoint())
	return quickWindowHeightForContent(s.translationUI.ImageTaskCount(), s.readingBlocksOn(), work.Height)
}

// readingBlocksOn 报告用户阅读的块——今日动画更新或被关注的 UP——是否开启。
// 任一开启都会让 flyout 以工作区的 quickEnabledHeightShare 高度打开，足以无需滚动地显示，又不占满屏幕。
func (s *State) readingBlocksOn() bool {
	if s.quickWindowLayout.Visible("anime") && s.animeService != nil && s.animeService.Enabled() && s.animeService.Username() != "" {
		return true
	}
	return s.quickWindowLayout.Visible("bilibili") && s.bilibiliService != nil && s.bilibiliService.Enabled() && len(s.bilibiliService.UIDs()) > 0
}

// quickWindowHeightForTasks 是仅按图片任务定尺寸的规则。让它独立于桌面，
// 使半屏保证无需真实托盘或显示器即可测试。
func quickWindowHeightForTasks(tasks, workHeight int) int {
	return quickWindowHeightForContent(tasks, false, workHeight)
}

// quickWindowHeightForContent 是完整定尺寸规则：
//
//   - 有阅读块开启：工作区的 30%，在面板下方显示几行；
//   - 否则：基础高度，每个图片任务一行；
//
// 并且绝不超工作区的一半。
//
// 高度仅由设置和任务数决定——绝不来自测量内容。测量是此前的尝试，它以唯一要紧的方式出错：
// 它读取的盒子跟随滚动偏移，拖动滚动条会在用户指针下改变窗口大小。
//
// 代价是：阅读块开启而内容很少时，flyout 可能比其行更高。这是用户看到后选择的权衡：
// 可预测的高度胜过变动的高度。
func quickWindowHeightForContent(tasks int, readingBlocks bool, workHeight int) int {
	maxHeight := workHeight / 2
	if maxHeight <= 0 {
		maxHeight = panelHeight
	}
	wanted := panelHeight + max(tasks, 0)*quickTaskRowHeight
	if readingBlocks {
		// 该占比是下限而非目标：在较矮的屏幕上 30% 可能低于基础高度，
		// 阅读块绝不能使 flyout 比没有它时更小。
		wanted = max(wanted, int(float64(workHeight)*quickEnabledHeightShare))
	}
	return clampInt(wanted, min(panelHeight, maxHeight), maxHeight)
}

// fitQuickWindow 在任务添加、完成、重试或移除后，或在块开关后应用高度规则。
// SetSize 是 MyGo 的 flyout API，它会同时把窗口重新贴靠到托盘锚点。
func (s *State) fitQuickWindow() {
	if s.panel != nil {
		s.panel.SetSize(panelWidth, s.quickWindowHeight())
	}
}

// View 绘制快捷窗口的一帧：面板、其下的剪贴板行和页脚。它自身没有标题栏——
// flyout 从托盘图标悬挂，打开它的窗口才为应用命名——因此内容从窗口顶部开始。
//
// 窗口按应用自己的设计（design.go）绘制，而非框架默认主题：面板读作页面的第一个区块，它正是如此。
func (s *State) View(c *ui.Context) {
	s.useDesign(c)
	c.Root().Background(ui.Transparent)
	// 帧也是察觉就绪更新的地方，走与更新服务自身变更报告相同的判断（见 refreshReadyUpdate）：
	// 通知每版本只显示一次，因此两者中先看到的一方处理后，另一方无事可做。
	s.refreshReadyUpdate()
	box := ui.Column(c).Fill()
	switch {
	case c.Vibrancy(): // macOS 上位于 popover 自身材质之上
	case runtime.GOOS == "windows": // Windows 11 为 flyout 加圆角与轮廓
		box.Background(presentation.ColorSurface)
	case runtime.GOOS == "darwin":
		// 固定外观在 macOS 上绘制应用卡片：系统绘制阴影并用细线勾勒卡片，
		// 因此只绘其表面与圆角，不加第二道边框。
		box.Background(presentation.ColorSurface).Radius(presentation.RadiusCard)
	default:
		box.Background(presentation.ColorSurface).Radius(presentation.RadiusCard).Border(1, presentation.ColorBorder)
	}
	box.Children(func() {
		body := ui.Scroll(c).FillWidth().Grow(1)
		body.Children(func() {
			// 每个块自带其空间，两块之间的线属于窗口：设计中的块就是窗口，而非同一外边距内的行。
			s.quickLayoutBlocks(c)
			s.translationUI.QuickView(c)
		})
		// 剪贴板行收束内容：它关乎桌面而非某个面板，应位于窗口离数字最远的一端。
		// 其下的线属于窗口——从一边延伸到另一边——页脚在其中保留自己的空间。
		s.clipboardBottom(c)
		presentation.QuickEdgeLine(c)
		presentation.QuickWindowFooter(c, s.versionLine(), s.flyoutActions)
	})
}

// clipboardBottom 在用户要求时绘制快捷窗口的剪贴板区域。没有该开关时区域根本不存在——
// 没有分隔线、没有空行——因此从不需要它的窗口保持不变。
func (s *State) clipboardBottom(c *ui.Context) {
	if s.clipboard == nil {
		return
	}
	if !s.clipboard.Settings().RegionVisible {
		return
	}
	s.clipboardRegion(c)
}

// flyoutActions 是快捷窗口的页脚：用户始终能从这里做的事。每个按钮都带标签供悬停指针与辅助技术使用。
func (s *State) flyoutActions(c *ui.Context) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(4)).Children(func() {
		if s.diaryUI != nil {
			if button := presentation.QuietIconButton(c, s.diaryMark, "日记"); button.Clicked() {
				slog.Info("diary button clicked", "event", "diary_opened", "source", "quick_window")
				s.openDiary()
			}
		}
		if s.flashUI != nil {
			if button := presentation.QuietIconButton(c, s.flashMark, "闪念"); button.Clicked() {
				slog.Info("flash button clicked", "event", "flash_history_opened", "source", "quick_window")
				s.openFlashHistory()
			}
		}
		if s.translationUI.HasScreenshot() {
			if button := presentation.QuietIconButton(c, s.qrMark, "识别二维码"); button.Clicked() {
				slog.Info("QR recognition button clicked", "event", "qr_recognition_requested", "source", "quick_window")
				s.translationUI.StartQRRecognition()
			}
			if button := presentation.QuietIconButton(c, s.screenshotMark, "截图翻译"); button.Clicked() {
				slog.Info("screenshot translation button clicked", "event", "screenshot_translation_requested", "source", "quick_window")
				s.translationUI.StartScreenshotTranslation()
			}
		}
		if button := presentation.QuietIconButton(c, s.settingsMark, "设置"); button.Clicked() {
			slog.Info("settings button clicked", "event", "settings_opened", "source", "quick_window")
			s.openSettings()
		}
		if button := presentation.QuietIconButton(c, s.quitMark, "退出"); button.Clicked() {
			slog.Info("quit button clicked", "event", "quit_requested", "source", "quick_window")
			s.quit()
		}
	})
}

// versionLine 是页脚左侧显示的内容。
func (s *State) versionLine() string {
	version := strings.TrimSpace(s.version)
	if version == "" {
		version = "dev"
	}
	return appName + " " + version
}

// refreshAsync 在主线程之外获取列出的面板。
//
// force 请求无视缓存地刷新；否则只在快照缺失、过期或来自失败尝试时才请求某个面板。
// 每个面板在各自的 goroutine 中运行，因此一个慢提供者不会拖累另一个的数字。
// 答复不回到此处：用例持有它们，下面的重绘让视图读取它们。这就是关闭窗口时完成的刷新不丢失任何东西的原因。
func (s *State) refreshAsync(force bool) {
	for _, panel := range s.panelService.List() {
		s.panelUI.RefreshPanelAsync(panel.ID, force)
	}
	s.animeUI.RefreshAsync(false)
	s.bilibiliUI.RefreshAsync(false)
}
