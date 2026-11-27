// Command Boku 是桌面应用：托盘图标，左键点击在其旁打开快速窗口。
//
// 本文件是装配应用的唯一位置：HTTP 客户端、凭据存储、Provider 注册表、用例、视图与两个窗口。它装配的一切都是独立包，下层对上层一无所知。
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/anime"
	"boku/internal/app"
	"boku/internal/bilibili"
	"boku/internal/clipboard"
	"boku/internal/credential"
	"boku/internal/diary"
	"boku/internal/domain"
	"boku/internal/flash"
	"boku/internal/live"
	"boku/internal/logging"
	"boku/internal/panel"
	codexpanel "boku/internal/panel/codex"
	deepseekpanel "boku/internal/panel/deepseek"
	grokpanel "boku/internal/panel/grok"
	"boku/internal/platform/autostart"
	"boku/internal/platform/gamepad"
	"boku/internal/platform/httpc"
	"boku/internal/platform/popoverappearance"
	"boku/internal/platform/regioncapture"
	"boku/internal/platform/selection"
	clipboarddevice "boku/internal/platform/sysclip"
	"boku/internal/playtime"
	"boku/internal/preferences"
	"boku/internal/settings"
	"boku/internal/translate"
	deepseektranslator "boku/internal/translate/deepseek"
	freetranslator "boku/internal/translate/free"
	"boku/internal/update"
)

// 必须在 Run 之前设置应用名：MyGo 在事件循环启动时才读取注册到系统的名字，而那时 init 早已执行完。
func init() {
	mygo.App.SetName("Boku")
}

func main() {
	// 一个进程一个托盘图标。二次启动把命令行交给已在运行的实例后退出，由该实例把快速窗口提到前面（桌面设计 §2）。锁在任何窗口开启前获取，因此二次启动的开销仅限交接本身。
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatalf("boku: locate the user data directory: %v", err)
	}
	version := mygo.App.Version()

	// 日志最先打开，先于任何可能失败的步骤：它是事后定位窗口存在前故障的地方，也是 known-issues 要求「先讲事实再猜测」时唯一记录事实之处。
	logFile, err := startLogging(userData, version)
	if err != nil {
		// 日志无法写入只是少了一种诊断途径，不应阻止应用启动：记录改走 stderr，与有文件之前一样。
		log.Printf("boku: %v; this run is not being recorded", err)
	} else {
		// flush 失败要直接报到 stderr 而非丢弃：它正是崩溃相关的那几行——可能从用户即将查看的文件里缺失的原因。这里不经 slog 记录，因为日志本身出了问题；记录直接写 stderr，与文件中其它无处可去的失败一致。
		defer func() {
			if err := logFile.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "boku: the log could not be flushed: %v\n", err)
			}
		}()
		// 托盘应用没有控制台，只打印出来的 panic 就是无人能解释的崩溃。它会被记录，然后原样再次抛出——进程结束方式不归这个 defer 管。
		defer recordPanics()
	}

	quickWindowLayout := preferences.NewQuickWindowLayout()
	panels, animeUpdates, bilibiliUpdates, secrets, clipboard, translate, own, appearance, prefs, playtimeRecorder, flashes, diaryService, liveSession, err := services(userData, version, quickWindowLayout)
	if err != nil {
		log.Fatalf("boku: %v", err)
	}

	// 更新功能在任何窗口之前装配，因为界面第一帧就要读它的状态；但它直到应用就绪才启动：检查应在托盘存在之后进行，且首次查询源本就还有一分钟。
	updates, err := newUpdates(userData, applicationVersion(version))
	if err != nil {
		// 无法准备更新功能的构建只是没有这个功能，并非无法启动：该功能旨在保持更新，拒绝运行是最糟的做法。
		log.Printf("boku: %v; this run will not check for updates", err)
		updates = nil
	}

	// 原生 popover 外观是 macOS 强制深色模式的临时桥接。以小型接口注入，日后换成 MyGo API 时只改此处装配，不触及视图。
	var popoverAppearance app.PopoverAppearance
	if popoverappearance.Available() {
		popoverAppearance = popoverappearance.New()
	}

	// 开机启动是平台自己的注册——Windows 上是 Run 值，macOS 上是 launch agent——以注入方式提供而非直接访问：设置窗口知道这个开关，而只有此处知道各桌面如何保存它。没有该注册项的桌面，管理器会说明，开关也随之一并说明。
	loginStart := autostart.New()

	// 轮询 goroutine 上的剪贴板读取会回到主线程这里。构建服务时 state 尚不存在，所以回调经一个稍后赋值的变量读取：在此之前的重绘没有可重绘的内容。
	var state *app.State
	clipboard.SetEmit(func() {
		if state != nil {
			state.Refresh()
		}
	})
	if liveSession != nil {
		liveSession.SetChanged(func() {
			if state != nil {
				state.Refresh()
			}
		})
	}

	state, err = app.New(mygo.App, app.Deps{
		Panels:           panels,
		Anime:            animeUpdates,
		Bilibili:         bilibiliUpdates,
		Secrets:          secrets,
		Clipboard:        clipboard,
		Playtime:         playtimeRecorder,
		Translate:        translate,
		Flash:            flashes,
		Diary:            diaryService,
		DiaryUnavailable: diaryService == nil,
		Live:             liveSession,
		RegionCapture:    regioncapture.New(),
		// 设置窗口在用户添加自建服务后调用：它的翻译器必须在下一次翻译询问前存在。
		RegisterOwnServices: own.registerAll,
		Shortcuts:           app.MyGoShortcuts(),
		Version:             version,
		Quit:                mygo.App.Quit,
		// 主题是 presentation 自己的设置：与各功能的键存在一起，但只有此窗口知道各模式的含义。
		Appearance:             appearance,
		AppearanceStore:        prefs,
		QuickWindowLayout:      quickWindowLayout,
		QuickWindowLayoutStore: prefs,
		ApplyAppearance: func(mode app.AppearanceMode) {
			mygo.Theme.SetSource(app.ThemeSource(mode))
			// 强制外观会遮蔽桌面自身的外观（对 Theme.IsDark 而言），取消强制（自动）后又重新显露。趁新来源生效时立即重读，使选择「自动」显示桌面外观，而不是上次被告知的那个。
			state.SyncAppearance(mygo.Theme.IsDark())
		},
		PopoverAppearance: popoverAppearance,
		Autostart:         loginStart,
		// 关于框绘制的内容与托盘菜单新增的项：此处无服务时，构建完全不画更新行——测试与装配失败都得到这种结果。
		Updates: updateService(updates),
	})
	if err != nil {
		log.Fatal(err)
	}
	bilibiliUpdates.SetEmit(state.NoteBilibiliChanges)

	// 抵达运行中实例的启动是用户在要窗口，而非要第二个应用：托盘已存在，故处理器只把快速窗口提到前面。
	mygo.App.OnSecondInstance(func([]string, string) {
		state.Activate()
	})

	// macOS：重新打开应用（Finder、`open -a Boku`）是激活事件而非第二个进程——LaunchServices 会给运行中实例发 applicationShouldHandleReopen，MyGo 将其报告为 OnActivate。上面的二次启动路径是 Windows 与 Linux 的。
	if runtime.GOOS == "darwin" {
		mygo.App.OnActivate(func(hasVisibleWindows bool) {
			if !hasVisibleWindows {
				state.Activate()
			}
		})
		// 用户可能刚在系统设置里打开了辅助功能开关；返回本应用时正是停止显示请求的时机。
		mygo.App.OnDidBecomeActive(func() {
			state.RefreshSelectionAccess()
		})
	}

	// macOS：托盘是入口，故让应用不出现在 Dock。Windows 与 Linux 忽略此项。
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// 保存的外观要在 Run 之前交给桌面，这是 MyGo 的要求：标题栏、菜单与 macOS popover 材质在第一帧之前就画好，先亮后切换的 Boku 会闪一下。自动模式下这里是系统来源；运行期间也跟随它。
	mygo.Theme.SetSource(app.ThemeSource(appearance.Mode()))
	mygo.Theme.OnUpdated(func() {
		state.SyncAppearance(mygo.Theme.IsDark())
	})
	// 快速窗口是唯一的窗口，关闭它不得退出应用：除非有人监听，MyGo 会在最后一个窗口关闭时退出。
	mygo.App.OnWindowAllClosed(func() {})
	// 更新检查是一个独立 goroutine，随进程结束：应用退出时仍在查询源的检查只会落到空处。
	quitUpdates := func() {
		if updates != nil {
			updates.Stop()
		}
	}
	quitBilibili := func() { bilibiliUpdates.Stop() }
	padCtx, stopPads := context.WithCancel(context.Background())
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		slog.Info("boku is quitting")
		stopPads()
		// 剪贴板轮询是 goroutine，须先于进程结束：后端即将消失时到达的读取是没人要的读取。
		clipboard.Stop()
		playtimeRecorder.Stop()
		quitUpdates()
		quitBilibili()
		// 遗留绑定的快捷键属于已不存在的进程：下次启动会发现自己被占用。
		if liveSession != nil {
			liveSession.Close()
		}
		state.FlushDiary()
		if diaryService != nil {
			_ = diaryService.Close()
		}
		state.UnregisterShortcuts()
		state.Destroy()
	})
	mygo.App.WhenReady(func() {
		// 系统的答案只有应用运行后才可得；自动模式下第一帧用的就是它。
		state.SyncAppearance(mygo.Theme.IsDark())
		if err := state.Setup(); err != nil {
			log.Fatal(err)
		}
		// 快捷键需要运行中的应用；只有用户开启时才启动监控。
		state.RegisterShortcuts()
		clipboard.Start(nil)
		playtimeRecorder.SetChanged(func() {
			mygo.RunOnMain(func() {
				if state != nil {
					state.CachePlaytimeCovers()
					state.Refresh()
				}
			})
		})
		playtimeRecorder.Start()
		gamepad.Watch(padCtx, gamepad.RefreshEvery, func(pads []gamepad.Pad) {
			mygo.RunOnMain(func() {
				if state != nil {
					state.SetPlaytimeControllers(pads)
					state.Refresh()
				}
			})
		})
		bilibiliUpdates.Start(context.Background())
		state.StartDiaryAutoSync()
		// 更新检查最后启动：它首次查询源还有一分钟，而托盘才是启动的目的。无法自我更新的构建不会得到定时器——服务自己知道，关于框也不会为它画行。
		if updates != nil {
			updates.Start(context.Background())
		}
		slog.Info("boku is ready")
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// services 在窗口出现之前装配好一切：凭据存储、设置、系统剪贴板、HTTP 客户端、Provider 注册表、面板与翻译工具。
//
// 独立成函数是为了让测试检查接线——Provider 与 Adapter 不匹配，或面板破坏了 Provider 的契约，都属于启动错误，值得在没有图形会话时捕获。
func services(userData, version string, layouts ...*preferences.QuickWindowLayout) (*panel.Panels, *anime.Service, *bilibili.Service, *credential.Secrets, *clipboard.Clipboard, *translate.Translate, *ownServices, *preferences.Appearance, *settings.Store, *playtime.Recorder, *flash.Service, *diary.Service, *live.Service, error) {
	quickWindowLayout := preferences.NewQuickWindowLayout()
	if len(layouts) > 0 && layouts[0] != nil {
		quickWindowLayout = layouts[0]
	}
	// 凭据：独立文件，绝不入库。存储只在此处命名。
	//
	// 值为明文，这是刻意的选择而非未完成品：用户没有口令可输入，就没有地方保存一把「读目录的人」读不到的密钥，而用放在旁边的密钥加密的文件只是看起来像保护。安全设计 §5 说明了真正保护它们的是什么——0600 权限、Windows 上的 ACL、值绝不进日志或错误、以及一个直接说明该文件是什么的窗口。
	secretStore, err := credential.Open(filepath.Join(userData, credential.FileName))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open the credential store: %w", err)
	}
	secrets, err := credential.NewSecrets(secretStore)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the credential store: %w", err)
	}

	// 应用自己的设置与剪贴板历史，就在凭据旁边：两个文件，各归读取它的功能所有。历史为何是文件而非存储设计 §3 所说的表，见 internal/clipboard/store.go。
	//
	// 设置文件是共享的，因此每个有设置的功能都注册到 store：store 本身不知道里面有什么，功能只拿到自己拥有的键。
	clipboardSettings := clipboard.NewSection()
	translateSettings := translate.NewSection()
	panelSettings := panel.NewSection()
	animeSettings := anime.NewSection()
	bilibiliSettings := bilibili.NewSection()
	playtimeSettings := playtime.NewSection()
	// 外观是 presentation 而非某个功能的选择，但以同样方式存储：同一文件里独立的 section，经同一 store 写入。
	appearance := preferences.NewAppearanceSection()
	flashSettings := flash.NewSection()
	diarySettings := diary.NewSection()
	liveSettings := live.NewSection()
	prefs, err := settings.New(
		filepath.Join(userData, "settings.json"),
		clipboardSettings,
		flashSettings,
		diarySettings,
		translateSettings,
		panelSettings,
		animeSettings,
		bilibiliSettings,
		playtimeSettings,
		appearance,
		quickWindowLayout,
		liveSettings,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open the settings store: %w", err)
	}
	// 文件在这里读一次：一帧不得读文件，已在内存中持有设置的服务也不得再读。
	if err := prefs.Load(context.Background()); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("read the settings: %w", err)
	}
	flashes, err := flash.Open(filepath.Join(userData, flash.FileName), flashSettings, prefs, domain.SystemClock{})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open flashes: %w", err)
	}
	liveSession, err := live.Open(filepath.Join(userData, live.ModelsDirName), filepath.Join(userData, live.GlossaryFileName), liveSettings, prefs, domain.SystemClock{})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open live translation: %w", err)
	}
	history, err := clipboard.NewHistory(filepath.Join(userData, "clipboard_history.json"))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open the clipboard history: %w", err)
	}

	// 公开的 Bangumi 请求需要可识别的 User-Agent。在公共客户端里带上项目地址，也让其它请求可追溯，同时不泄露任何用户专属的值。
	client := httpc.New(httpc.Options{UserAgent: "Boku/" + applicationVersion(version) + " (+https://github.com/QiYuOr2/boku)"})
	diaryService, diaryErr := diary.OpenService(userData, diarySettings, prefs, secretStore, client, domain.SystemClock{})
	if diaryErr != nil {
		// 日记数据库损坏或来自更高版本时绝不覆盖原文件，也不阻止托盘应用其余功能启动。
		slog.Error("open diary service", "err", diaryErr)
	}
	animeService, err := anime.NewWithCache(client, animeSettings, prefs, anime.NewCache(filepath.Join(userData, "anime_today.json")), secretStore)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare anime updates: %w", err)
	}

	bilibiliService, err := bilibili.NewWithCache(client, bilibiliSettings, prefs, bilibili.NewCache(filepath.Join(userData, "bilibili_followed.json")))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare Bilibili UP monitoring: %w", err)
	}

	// Provider 与 Adapter 成对注册：缺 Adapter 的 Provider 只会在之后、在用户面前失败。
	registry := panel.NewRegistry()
	if err := registry.Register(
		deepseekpanel.New(client),
		deepseekpanel.NewAdapter(),
	); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the deepseek provider: %w", err)
	}
	if err := registry.Register(
		codexpanel.New(client),
		codexpanel.NewAdapter(),
	); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the codex provider: %w", err)
	}
	if err := registry.Register(
		grokpanel.New(client),
		grokpanel.NewAdapter(),
	); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the grok provider: %w", err)
	}
	if err := registry.Validate(); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("the provider registry is incomplete: %w", err)
	}

	// Codex 额度的凭据不属于 Boku：它是 Codex CLI 已做的 ChatGPT 登录，就地读取并在内存中刷新（安全设计 §9）。
	codexLogin := codexpanel.NewLogin(codexpanel.LoginOptions{Client: client})
	grokLogin := grokpanel.NewLogin(grokpanel.LoginOptions{Client: client, Secrets: secrets})
	if err := grokLogin.Remember(context.Background()); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("read the Grok login: %w", err)
	}

	// 应用展示的面板。新增一个就在这里加一条定义；面板服务会用 Provider 自己的契约校验它。标为 optional 的面板用户可在设置窗口关掉，其开关与其它应用设置存在同一文件里。
	panels, err := panel.NewPanels(registry, secretStore, panelSettings, prefs, []panel.PanelDef{
		{
			Panel:   deepseekpanel.Panel(),
			Metrics: deepseekpanel.Metrics(),
		},
		{
			Panel:              codexpanel.Panel(),
			Metrics:            codexpanel.Metrics(),
			OptionalMetricKeys: codexpanel.OptionalMetricKeys(),
			// 额度来自本机 Codex CLI 自己的登录，因此读取它需用户允许。
			Optional: true,
		},
		{
			Panel:              grokpanel.Panel(),
			Metrics:            grokpanel.Metrics(),
			OptionalMetricKeys: grokpanel.OptionalMetricKeys(),
			Optional:           true,
		},
	}, codexLogin, grokLogin)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the panels: %w", err)
	}
	// 开关在这里读一次：一帧不得读文件。
	if err := panels.LoadChoices(context.Background()); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("read the panel switches: %w", err)
	}

	// 剪贴板在平台拥有的线程上读写，所以每次调用都经 MyGo 的调度器。
	device := clipboarddevice.New(func(fn func()) bool {
		done := make(chan struct{})
		go func() {
			defer close(done)
			mygo.RunOnMain(fn)
		}()
		select {
		case <-done:
			return true
		case <-time.After(clipboarddevice.DefaultTimeout):
			return false
		}
	})
	clip, err := clipboard.NewClipboard(clipboard.ClipboardOptions{
		Device:  device,
		Store:   history,
		Section: clipboardSettings,
		// 开关写入所经的 store。文件上面已读过一次，故这里只写。
		Settings: prefs,
		Clock:    domain.SystemClock{},
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the clipboard: %w", err)
	}

	// 工具可询问的翻译器。与面板一样，新增一个就在这里加一次注册；与 Provider 不同，翻译器独立存在：它直接以领域自己的语言作答，无需配对 Adapter。
	translators := translate.NewRegistry()
	if err := translators.Register(deepseektranslator.New(client)); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the deepseek translator: %w", err)
	}
	// 免费引擎：无需 key、无需账号，正是全新安装后能按快捷键就得到答案、而不是被导到一个「去哪买」页面的原因。它们是两个翻译器而非一个，因为设置窗口把它们作为两个选择——用户选哪个引擎，就询问哪个。
	if err := translators.Register(freetranslator.NewMicrosoft(client)); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the free microsoft translator: %w", err)
	}
	if err := translators.Register(freetranslator.NewBilibili(client)); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the free bilibili translator: %w", err)
	}
	// 用户自己运行的服务：每个已存服务一个翻译器，各有自己的 id、地址与名称。存在哪些是设置文件的答案，用户从设置窗口添加——因此注册表拿到的是「追上用户新增内容」的能力，而不是在此写死的一组。
	own := newOwnServices(client, translators, translateSettings)
	if err := own.registerAll(); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	translation, err := translate.NewTranslate(translate.TranslateOptions{
		Translators: translators,
		Secrets:     secretStore,
		Section:     translateSettings,
		Settings:    prefs,
		// 先询问选区、再询问剪贴板：用户选中的就是其本意，剪贴板则是最后复制的内容。当应用什么都不发布时，选区设备会借用剪贴板——请应用复制，再把剪贴板放回——因此工具被告知把这份借来的值排除在历史之外。
		Selection: selection.New(selection.Options{
			Clipboard:  device,
			BeforeCopy: clip.IgnoreTheNextCopy,
			// 剪贴板的写入计数器是平台自身的，选区工具用的是 Windows 用的同一个。macOS 上，没有它，复制回退无法区分「未选中任何内容」和「又复制了同样的文本」。
			Sequence: device.ChangeCount,
		}),
		Clipboard: device,
		Clock:     domain.SystemClock{},
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the translation tool: %w", err)
	}
	// 游戏时长是本地且在运行时可选：读不了的文件由 Open 内部记日志，无法列出进程的桌面干脆不启动记录器。
	playtimeRecorder := playtime.Open(userData, playtimeSettings)
	return panels, animeService, bilibiliService, secrets, clip, translation, own, appearance, prefs, playtimeRecorder, flashes, diaryService, liveSession, nil
}

// applicationVersion 是作为 User-Agent 发送的版本：直接 `go run` 没有版本，空的 "Boku/" 什么也没说。它也是更新功能比对所用的版本，因此无版本的构建在那里同样报 "dev"，并以此为比较依据。
func applicationVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

// updateService 在无服务时把 nil 交给 presentation。用函数而非三元式，是因为装在 app.UpdateService 里的类型化 nil 指针对接口而言不为 nil：没有更新功能的构建必须传接口零值，否则关于框会画一行永远填不上的项。
func updateService(updates *update.Service) app.UpdateService {
	if updates == nil {
		return nil
	}
	return updates
}

// logDirectory 是每日日志所在：用户数据目录下独立的目录，使日志便于查看也便于整体删除。BOKU_LOG_DIR 可移动它们，供排障使用——文件可整体交出。
func logDirectory(userData string) string {
	if dir := strings.TrimSpace(os.Getenv(logging.DirEnv)); dir != "" {
		return dir
	}
	return filepath.Join(userData, "logs")
}

// startLogging 打开当天的日志，把它设为进程自己的，并记录本次启动。
//
// 与 services 一样独立成函数，便于测试无需窗口即可检查：一次运行的第一条记录，是事后定位其它一切存在之前所发生故障的地方。
func startLogging(userData, version string) (*logging.Log, error) {
	logFile, err := logging.Open(logging.Options{
		Dir:   logDirectory(userData),
		Level: logging.LevelFromEnv(),
	})
	if err != nil {
		return nil, err
	}
	logFile.Install()
	slog.Info("boku started",
		"version", applicationVersion(version),
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
		"data", userData,
		"log", logFile.File(),
		"retentionDays", logging.RetentionDays,
	)
	return logFile, nil
}

// recordPanics 记下进程为何将死，再让它按原方式死去：原因留在文件里供下次启动查看，进程结束方式不变。
func recordPanics() {
	reason := recover()
	if reason == nil {
		return
	}
	slog.Error("boku panicked", "reason", reason, "stack", string(debug.Stack()))
	panic(reason)
}
