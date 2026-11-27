package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contractdeepseek "boku/internal/contract/deepseek"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/logging"
	panelcodex "boku/internal/panel/codex"
	paneldeepseek "boku/internal/panel/deepseek"
	panelgrok "boku/internal/panel/grok"
	"boku/internal/preferences"
	"boku/internal/translate"
)

func TestServicesLoadAndPersistQuickWindowLayout(t *testing.T) {
	userData := t.TempDir()
	path := filepath.Join(userData, "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"tools":{"quickWindowLayout":{"order":["anime","codex","deepseek","bilibili"],"hidden":["bilibili"]},"customKey":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	layout := preferences.NewQuickWindowLayout()
	_, _, _, _, _, _, _, appearance, prefs, recorder, _, diarySvc, _, err := services(userData, "test", layout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Stop)
	if diarySvc != nil {
		t.Cleanup(func() { _ = diarySvc.Close() })
	}
	if layout.Order()[0] != "playtime" || layout.Order()[1] != "anime" || layout.Visible("bilibili") {
		t.Fatalf("loaded layout=%v", layout.Order())
	}
	layout.MoveToEnd("anime")
	layout.SetVisible("codex", false)
	if err := layout.Save(context.Background(), prefs); err != nil {
		t.Fatal(err)
	}
	if err := appearance.Save(context.Background(), prefs, preferences.AppearanceDark); err != nil {
		t.Fatal(err)
	}
	reloaded := preferences.NewQuickWindowLayout()
	_, _, _, _, _, _, _, _, _, again, _, diaryAgain, _, err := services(userData, "test", reloaded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Stop)
	if diaryAgain != nil {
		t.Cleanup(func() { _ = diaryAgain.Close() })
	}
	if reloaded.Order()[len(reloaded.Order())-1] != "anime" || reloaded.Visible("codex") || reloaded.Visible("bilibili") {
		t.Fatalf("saved layout=%v", reloaded.Order())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"customKey": "keep"`) {
		t.Fatalf("unowned setting lost: %s", contents)
	}
}

// 装配是 Provider 与 Adapter、面板与 Provider 唯一可能配错的地方；这属于启动错误，无需窗口即可检查。
func TestServicesAssembleThePanels(t *testing.T) {
	userData := t.TempDir()
	panels, anime, bilibili, secrets, clip, translation, _, appearance, prefs, recorder, _, diarySvc, _, err := services(userData, "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	t.Cleanup(recorder.Stop)
	if diarySvc != nil {
		t.Cleanup(func() { _ = diarySvc.Close() })
	}
	if panels == nil || anime == nil || bilibili == nil || secrets == nil || clip == nil || translation == nil || diarySvc == nil {
		t.Fatal("services returned no services")
	}
	// 外观是同一设置文件的一个 section，经 store 注册；它的修改必须是 store 能接受并写入的。
	if appearance.Mode() != "system" {
		t.Errorf("the appearance starts at %q, want system", appearance.Mode())
	}
	if err := prefs.Update(context.Background(), "appearance", func() error {
		appearance.Set("dark")
		return nil
	}); err != nil {
		t.Fatalf("writing the appearance: %v", err)
	}
	if appearance.Mode() != "dark" {
		t.Errorf("the appearance after a save = %q, want dark", appearance.Mode())
	}
	// 剪贴板默认关闭，且此处不读取系统剪贴板：装配服务不等于使用它。
	if settings := clip.Settings(); settings.Monitoring || settings.RegionVisible {
		t.Errorf("the clipboard starts enabled: %+v", settings)
	}
	if err := bilibili.Configure(context.Background(), true, []int64{672328094}, 5, 2, true, true); err != nil {
		t.Fatalf("saving Bilibili settings: %v", err)
	}
	if got := bilibili.Settings(); len(got.UIDs) != 1 || got.UIDs[0] != 672328094 || got.DynamicCount != 2 || !got.LiveNotifications || !got.DynamicNotifications {
		t.Errorf("Bilibili settings were not retained: %+v", got)
	}

	listed := panels.List()
	if len(listed) != 3 {
		t.Fatalf("the application has %d panels, want the balance, Codex and Grok: %+v", len(listed), listed)
	}
	if listed[0].ProviderID != contractdeepseek.ProviderID {
		t.Errorf("the first panel's provider = %q, want %q", listed[0].ProviderID, contractdeepseek.ProviderID)
	}
	if listed[1].ProviderID != panelcodex.ProviderID {
		t.Errorf("the second panel's provider = %q, want %q", listed[1].ProviderID, panelcodex.ProviderID)
	}
	if listed[2].ProviderID != panelgrok.ProviderID {
		t.Errorf("the third panel's provider = %q, want %q", listed[2].ProviderID, panelgrok.ProviderID)
	}

	descriptor, err := panels.Descriptor(contractdeepseek.ProviderID)
	if err != nil {
		t.Fatalf("Descriptor failed: %v", err)
	}
	if descriptor.DisplayName == "" || descriptor.DocsURL == "" || descriptor.Endpoint == "" {
		t.Errorf("descriptor = %+v, want a display name, a docs link and an endpoint", descriptor)
	}
	if descriptor.CredentialKeys[0] != contractdeepseek.APIKeyCredential {
		t.Errorf("credential keys = %v", descriptor.CredentialKeys)
	}
	if descriptor.UsageURL != paneldeepseek.UsageURL {
		t.Errorf("descriptor usage link = %q, want %q", descriptor.UsageURL, paneldeepseek.UsageURL)
	}
	// 面板标题打开的页面来自 Provider 自己的 descriptor，经用例传到视图；装配处是这条链接可能悄然丢失却不影响编译的地方。
	if view := panels.View(listed[0].ID); view.UsageURL != paneldeepseek.UsageURL {
		t.Errorf("the panel opens %q, want the provider's page %q", view.UsageURL, paneldeepseek.UsageURL)
	}

	// 额度面板同样装配；它的凭据是 Codex CLI 自己保存的，而非 Boku 的 key——descriptor 要的就是数据源认的那个名字。
	quota, err := panels.Descriptor(panelcodex.ProviderID)
	if err != nil {
		t.Fatalf("Descriptor failed for the quota: %v", err)
	}
	if quota.DisplayName == "" || quota.DocsURL == "" || quota.Endpoint == "" {
		t.Errorf("the quota descriptor = %+v, want a display name, a docs link and an endpoint", quota)
	}
	if len(quota.CredentialKeys) != 1 || quota.CredentialKeys[0] != panelcodex.AccessTokenCredential {
		t.Errorf("the quota asks for %v, want the CLI's token", quota.CredentialKeys)
	}
	if quota.UsageURL != panelcodex.UsageURL {
		t.Errorf("the quota's usage link = %q, want %q", quota.UsageURL, panelcodex.UsageURL)
	}
	if view := panels.View(listed[1].ID); len(view.Metrics) != len(panelcodex.Metrics()) {
		t.Errorf("the quota panel shows %d metrics, want %d", len(view.Metrics), len(panelcodex.Metrics()))
	}

	// 没有存凭据时，余额面板直接说明而不是请求网络：此处不发任何请求。额度面板的凭据是 Codex CLI 自己的登录，本机可能已有，读取它不是本测试的职责。
	view := panels.View(listed[0].ID)
	if view.HasSnapshot {
		t.Error("a panel with no credential claims a snapshot")
	}
	if _, err := panels.Refresh(context.Background(), listed[0].ID); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("Refresh error = %v, want ErrCredentialMissing", err)
	}
	if !panels.View(listed[0].ID).NeedsCredentials() {
		t.Error("the view does not report the missing credential")
	}

	// 凭据文件位于调用者指定的用户数据目录下，且在存入内容之前不会创建。
	if path := secrets.Path(); path == "" {
		t.Error("the credential store has no path")
	} else if filepath.Base(path) != credential.FileName {
		t.Errorf("the credential file is %q, want %q", filepath.Base(path), credential.FileName)
	}
}

// 日志与应用其它文件放在一起，但有单独目录：便于查看，也便于整体删除。
func TestLogDirectoryIsUnderTheUserData(t *testing.T) {
	t.Setenv(logging.DirEnv, "")
	userData := t.TempDir()
	want := filepath.Join(userData, "logs")
	if got := logDirectory(userData); got != want {
		t.Errorf("logDirectory(%q) = %q, want %q", userData, got, want)
	}
}

// BOKU_LOG_DIR 可移动日志位置，供排障使用：文件可整体交出。
func TestLogDirectoryFollowsTheEnvironment(t *testing.T) {
	elsewhere := filepath.Join(t.TempDir(), "somewhere else")
	t.Setenv(logging.DirEnv, "  "+elsewhere+"  ")
	if got := logDirectory(t.TempDir()); got != elsewhere {
		t.Errorf("logDirectory() = %q, want the environment's %q", got, elsewhere)
	}
}

// 启动时的第一条记录，是事后定位窗口尚未存在时故障的地方；因此启动必须打开日志、进程级安装它并记录本次运行——且只在调用者指定的用户数据目录下。
func TestStartLoggingRecordsTheLaunchUnderTheUserData(t *testing.T) {
	keepStandardLoggers(t)
	t.Setenv(logging.DirEnv, "")
	userData := t.TempDir()

	logFile, err := startLogging(userData, "")
	if err != nil {
		t.Fatalf("startLogging: %v", err)
	}
	lives := filepath.Join(userData, "logs")
	if !strings.HasPrefix(logFile.File(), lives+string(filepath.Separator)) {
		t.Errorf("the log is at %q, want it under %q", logFile.File(), lives)
	}
	// Install 会把标准 logger 与 slog 的默认 logger 装到这里；两者都是进程级的，所以只能在此检查。
	slog.Info("through the default logger")
	if err := logFile.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(logFile.File())
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	text := string(data)
	for _, want := range []string{`msg="boku started"`, "version=dev", "retentionDays=30", "through the default logger"} {
		if !strings.Contains(text, want) {
			t.Errorf("the log does not have %q in it:\n%s", want, text)
		}
	}
}

// keepStandardLoggers 在测试结束后还原进程的 logger：安装日志的测试在运行期间独占整个进程。
func keepStandardLoggers(t *testing.T) {
	t.Helper()
	previousLogger := slog.Default()
	previousOut := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		log.SetOutput(previousOut)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
}

// 托盘应用没有控制台，只打印出来的 panic 就是无人能解释的崩溃。它会被记录下来——然后原样再次抛出，进程结束方式不变。
func TestRecordPanicsWritesItDownAndRaisesItAgain(t *testing.T) {
	keepStandardLoggers(t)
	dir := t.TempDir()
	logFile, err := logging.Open(logging.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	slog.SetDefault(logFile.Logger())
	t.Cleanup(func() { _ = logFile.Close() })

	func() {
		defer func() {
			reason := recover()
			if reason == nil {
				t.Fatal("recordPanics swallowed the panic")
			}
			if reason != "the tray could not be created" {
				t.Errorf("the panic came back as %v, want it unchanged", reason)
			}
		}()
		func() {
			defer recordPanics()
			panic("the tray could not be created")
		}()
	}()

	_ = logFile.Close()
	data, err := os.ReadFile(logFile.File())
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "the tray could not be created") {
		t.Errorf("the panic is not in the log:\n%s", text)
	}
	if !strings.Contains(text, "goroutine") {
		t.Errorf("the log has no stack:\n%s", text)
	}
}

// 装配处也是翻译器注册、并与 descriptor 所指的凭据和设置配对的地方：翻译器缺设置存储、或读不到自己的设置，都是应在此发现的启动错误。
func TestServicesAssembleTheTranslators(t *testing.T) {
	_, _, _, _, _, translation, own, _, _, recorder, _, diarySvc, _, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	t.Cleanup(recorder.Stop)
	if diarySvc != nil {
		t.Cleanup(func() { _ = diarySvc.Close() })
	}
	views, err := translation.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	// 应用自带的三个。用户自己的服务不在其中：用户添加之前没有，为一个没人配置的服务生成翻译器会是一行无法作答的条目。
	if len(views) != 3 {
		t.Fatalf("the application has %d translators, want 3: %+v", len(views), views)
	}
	byID := map[string]bool{}
	for _, view := range views {
		byID[view.ID] = true
		if view.DisplayName == "" || view.DocsURL == "" {
			t.Errorf("translator %s is missing its name or its documentation link: %+v", view.ID, view)
		}
	}
	for _, want := range []string{"free.microsoft", "free.bilibili", "deepseek"} {
		if !byID[want] {
			t.Errorf("translator %s is not registered: %+v", want, views)
		}
	}
	// 免费引擎是全新安装时的两个选择，且无需任何配置即可使用——这正是它们的意义。
	seen := 0
	for _, view := range views {
		if !strings.HasPrefix(view.ID, "free.") {
			continue
		}
		seen++
		if !view.Configured || view.NeedsCredential {
			t.Errorf("the free engine %s is not usable out of the box: %+v", view.ID, view)
		}
	}
	if seen != 2 {
		t.Errorf("the application has %d free engines, want two", seen)
	}
	if settings := translationSettingsOf(t, translation); settings.ProviderOr() != "free.microsoft" {
		t.Errorf("a fresh installation translates with %q, want the Microsoft engine", settings.ProviderOr())
	}

	// 用户自建的服务完整走一遍：存储、注册，然后成为工具能真正询问的翻译器。
	id, err := translation.AddCustomTranslator(context.Background(), translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地测试服务",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	if err := own.registerAll(); err != nil {
		t.Fatalf("registering the user's own services failed: %v", err)
	}
	views, err = translation.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	found := false
	for _, view := range views {
		if view.ID != id {
			continue
		}
		found = true
		if view.DisplayName != "本地测试服务" {
			t.Errorf("the service is called %q, want the name the user gave it", view.DisplayName)
		}
		if view.Endpoint != "http://127.0.0.1:11434/v1/chat/completions" {
			t.Errorf("endpoint = %q", view.Endpoint)
		}
		if !view.Configured {
			t.Error("the service the user just added is not configured")
		}
	}
	if !found {
		t.Errorf("the added service has no translator: %+v", views)
	}
	// 第一个服务使用裸 id，这是可能有多个服务之前写下的文件所保存的形式。
	if id != "custom" {
		t.Errorf("the first service was stored as %q, want custom", id)
	}
}

// translationSettingsOf 读取工具的配置，读不到就让测试失败：装配后读不回来本身就是一种错误。
func translationSettingsOf(t *testing.T, translation *translate.Translate) translate.Settings {
	t.Helper()
	settings, err := translation.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	return settings
}
