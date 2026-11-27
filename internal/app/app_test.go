package app

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/credential"
	"boku/internal/panel"
	"boku/internal/panel/deepseek"
)

func TestNewRequiresItsDependencies(t *testing.T) {
	secrets, err := credential.NewSecrets(newTestStore(t))
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := panel.NewPanels(
		testPanelRegistry(t, newTestProvider(t)),
		newTestStore(t),
		panel.NewSection(),
		nil,
		[]panel.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if _, err := New(mygo.App, Deps{Secrets: secrets}); err == nil {
		t.Error("New accepted a state without a panel service")
	}
	if _, err := New(mygo.App, Deps{Panels: panels}); err == nil {
		t.Error("New accepted a state without a secret service")
	}
	if _, err := New(nil, Deps{Panels: panels, Secrets: secrets}); err == nil {
		t.Error("New accepted a state without an application")
	}
}

// 第二次启动会把快捷窗口带到前台，但绝不针对尚不存在的托盘：
// MyGo 的 NewFlyout 拒绝既无托盘也无父窗口的 flyout，
// 而启动过程中到来的第二次启动不能因此把它弄崩。
func TestActivateWithoutATrayOpensNothing(t *testing.T) {
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "0.1.0")
	state.Activate()
	if state.panel != nil {
		t.Error("Activate opened a quick window with no tray to anchor it to")
	}
}

// 没有版本号时页脚仍须显示些内容。
func TestVersionLineFallsBack(t *testing.T) {
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "")
	if got := state.versionLine(); got != "Boku dev" {
		t.Errorf("versionLine = %q, want Boku dev", got)
	}
	state.version = "1.2.3"
	if got := state.versionLine(); got != "Boku 1.2.3" {
		t.Errorf("versionLine = %q, want Boku 1.2.3", got)
	}
}

// refreshAsync 是界面调用的入口；它绝不能阻塞帧，并且只在确实有东西可请求时才请求。
func TestRefreshAsyncDoesNotBlockAndOnlyAsksWhenNeeded(t *testing.T) {
	provider := newTestProvider(t)
	gate := provider.hold()
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	state := testState(t, provider, withCredential(newTestStore(t)), "0.1.0")

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		state.refreshAsync(false)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("refreshAsync waited for the blocked HTTP request")
	}

	deadline := time.Now().Add(2 * time.Second)
	for provider.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if provider.count() != 1 {
		t.Fatalf("the provider was called %d times, want 1", provider.count())
	}
	release()

	// 等取数落定，第二次调用不得再次请求。
	deadline = time.Now().Add(2 * time.Second)
	for state.panelService.View(deepseek.PanelID).Loading && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if view := state.panelService.View(deepseek.PanelID); !view.HasSnapshot || view.Loading || view.Err != nil {
		t.Fatalf("the refresh did not produce a snapshot: %+v", view)
	}
	state.refreshAsync(false)
	time.Sleep(20 * time.Millisecond)
	if got := provider.count(); got != 1 {
		t.Errorf("a fresh panel was fetched again: %d calls, want 1", got)
	}
}

// Window.Update 会无条件调用其回调。因此即使刷新没有状态变化，也必须传给它一个可调用的空操作；
// 否则任何打开着的设置窗口在切换侧边栏时都会 panic。
func TestRefreshCallbackIsAlwaysCallable(t *testing.T) {
	refreshCallback(nil)()

	calls := 0
	refreshCallback(func() { calls++ })()
	if calls != 1 {
		t.Errorf("the refresh callback ran %d times, want once", calls)
	}
}
