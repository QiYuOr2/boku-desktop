package app

import (
	updateui "boku/internal/update/ui"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/panel"
	"boku/internal/panel/deepseek"
)

// testUpdates 是测试驱动的 UpdateService。
type testUpdates struct {
	mu       sync.Mutex
	state    domain.UpdateState
	checks   int
	installs int
	// checkErr 与 installErr 是两个调用在设置时返回的错误。
	checkErr   error
	installErr error
	// onChange 是状态安装的监听器。它在下述调用所在的 goroutine 上被调用，与真实服务一致。
	onChange func()
}

func (u *testUpdates) State() domain.UpdateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

func (u *testUpdates) Check(context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checks++
	return u.checkErr
}

func (u *testUpdates) Install(context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.installs++
	return u.installErr
}

func (u *testUpdates) OnChange(fn func()) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.onChange = fn
}

// report 设置服务报告的状态，然后像真实服务那样通知监听器：在状态之后，且在其所在
// goroutine 上。
func (u *testUpdates) report(state domain.UpdateState) {
	u.mu.Lock()
	u.state = state
	notify := u.onChange
	u.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (u *testUpdates) counts() (checks, installs int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.checks, u.installs
}

// testNotifier 是测试驱动的 updateNotifier：它被要求显示什么，以及是否被告知可以显示。
type testNotifier struct {
	supported bool
	notices   []testNotice
	// open 是最后一条通知的回调，即点击所运行的东西。
	open func()
}

type testNotice struct {
	id, title, body string
}

func (n *testNotifier) Supported() bool { return n.supported }

func (n *testNotifier) Notify(id, title, body string, open func()) {
	n.notices = append(n.notices, testNotice{id: id, title: title, body: body})
	n.open = open
}

// updateStateWith 在给定假实现上构建真实的表现为层状态，并按装配那样挂上更新服务。
func updateStateWith(t *testing.T, updates UpdateService, notifier desktopNotifier) *State {
	t.Helper()
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "0.1.0")
	state.notifier = notifier
	state.AttachUpdates(updates)
	if updates != nil {
		host := state.featureHost()
		host.RunOnMain = state.refresh
		state.updateUI = updateui.New(updates, notifier, host, state.showUpdateSettings, state.version)
	}
	return state
}

// 托盘菜单只多一项，且只在更新就绪时：其他任何状态加的行都什么也做不了。
func TestUpdateMenuLabelTable(t *testing.T) {
	cases := []struct {
		name  string
		state domain.UpdateState
		label string
		show  bool
	}{
		{"idle", domain.UpdateState{Status: domain.UpdateIdle}, "", false},
		{"checking", domain.UpdateState{Status: domain.UpdateChecking, Version: "1.0.0"}, "", false},
		{"up to date", domain.UpdateState{Status: domain.UpdateUpToDate}, "", false},
		{"downloading", domain.UpdateState{Status: domain.UpdateDownloading, Version: "1.0.0"}, "", false},
		{"failed", domain.UpdateState{Status: domain.UpdateFailed, Detail: "x"}, "", false},
		{"unsupported", domain.UpdateState{Status: domain.UpdateUnsupported}, "", false},
		{
			name:  "ready with a version",
			state: domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"},
			label: "重启并更新到 0.1.0-beta.6",
			show:  true,
		},
		{
			name:  "ready without a version",
			state: domain.UpdateState{Status: domain.UpdateReady},
			label: "重启并更新",
			show:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			label, show := updateMenuLabel(c.state)
			if label != c.label || show != c.show {
				t.Errorf("updateMenuLabel(%v) = %q, %v; want %q, %v",
					c.state.Status, label, show, c.label, c.show)
			}
		})
	}
}

// 发布标签的前导 "v" 属于仓库而非版本号：界面书写时不带它，而完全没有版本号的文本
// 不是装饰。
func TestUpdateVersionTrimsTheTag(t *testing.T) {
	cases := map[string]string{
		"v0.1.0-beta.6": "0.1.0-beta.6",
		"0.1.0":         "0.1.0",
		" v1.2.3 ":      "1.2.3",
		"":              "",
		"   ":           "",
	}
	for in, want := range cases {
		if got := updateVersion(in); got != want {
			t.Errorf("updateVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdateDialogState(t *testing.T) {
	cases := []struct {
		name    string
		state   domain.UpdateState
		current string
		want    updateui.DialogInfo
	}{
		{
			name:    "checking shows a Loading Latest version",
			state:   domain.UpdateState{Status: domain.UpdateChecking},
			current: "v0.1.0",
			want:    updateui.DialogInfo{Latest: "正在获取", Status: "正在获取最新版本…", Loading: true},
		},
		{
			name:    "up to date compares with the current version",
			state:   domain.UpdateState{Status: domain.UpdateUpToDate},
			current: "v0.1.0",
			want:    updateui.DialogInfo{Latest: "0.1.0", Status: "已是最新版本"},
		},
		{
			name:    "ready names the new version",
			state:   domain.UpdateState{Status: domain.UpdateReady, Version: "v0.2.0"},
			current: "0.1.0",
			want:    updateui.DialogInfo{Latest: "0.2.0", Status: "新版本已准备就绪"},
		},
		{
			name:    "failure does not invent a Latest version",
			state:   domain.UpdateState{Status: domain.UpdateFailed, Detail: "网络连接被拒绝。"},
			current: "",
			want:    updateui.DialogInfo{Latest: "未能获取", Status: "检查更新失败：网络连接被拒绝。"},
		},
		{
			name:    "unsupported is settled instead of Loading forever",
			state:   domain.UpdateState{Status: domain.UpdateUnsupported},
			current: "0.1.0",
			want:    updateui.DialogInfo{Latest: "不可用", Status: "当前构建不支持自动更新"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := updateui.DialogState(c.state, c.current); got != c.want {
				t.Errorf("updateui.DialogState(%+v, %q) = %+v, want %+v", c.state, c.current, got, c.want)
			}
		})
	}
}

// 没有更新服务的 State 即没有更新的构建：该行不绘制，菜单保持原样，被请求时不自查、
// 也不 panic。
func TestUpdateWithoutAService(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "0.1.0")
	state.notifier = notifier

	if status := state.updateState().Status; status != domain.UpdateUnsupported {
		t.Errorf("a state with no service reported %v, want unsupported", status)
	}
	if items := state.updateMenuItems(); len(items) != 0 {
		t.Errorf("a state with no service put %d items in the menu, want none", len(items))
	}
	labels := menuLabels(state.menu())
	want := []string{"打开 Boku", "设置…", "", "退出 Boku"}
	if len(labels) != len(want) {
		t.Fatalf("the menu is %v, want %v", labels, want)
	}
	for index, label := range want {
		if labels[index] != label {
			t.Errorf("menu item %d = %q, want %q", index, labels[index], label)
		}
	}

	// 此处用户能触达的每个动作都应是空操作而非 panic。
	state.startUpdateCheck()
	state.installUpdate()
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "9.9.9"})
	if len(notifier.notices) != 0 {
		t.Errorf("a state with no service sent %d notices, want none", len(notifier.notices))
	}
}

// menuLabels 按顺序列出托盘菜单各项的名称；分隔符为空字符串：测试关心的是它有哪些行。
func menuLabels(menu *mygo.Menu) []string {
	items := menu.Items()
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

// 挂上的服务即视图所读的服务，托盘菜单只在更新就绪时才增加它那一行。
func TestUpdateMenuFollowsTheService(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateUpToDate}}
	state := updateStateWith(t, updates, &testNotifier{supported: true})
	if items := state.updateMenuItems(); len(items) != 0 {
		t.Fatalf("an up-to-date state put %d items in the menu, want none", len(items))
	}
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	items := state.updateMenuItems()
	if len(items) != 1 {
		t.Fatalf("a ready update put %d items in the menu, want one", len(items))
	}
	if got, want := items[0].Label, "重启并更新到 0.1.0-beta.6"; got != want {
		t.Errorf("the menu item is %q, want %q", got, want)
	}
	labels := menuLabels(state.menu())
	if len(labels) != 5 || labels[2] != "重启并更新到 0.1.0-beta.6" {
		t.Errorf("the menu is %v, want the update line between 设置… and the way out", labels)
	}
	// 再次就绪且未改变：仍恰好一行。
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if items := state.updateMenuItems(); len(items) != 1 {
		t.Errorf("a second report of the same ready update put %d items in the menu, want one", len(items))
	}
}

// 菜单项的点击启动安装，且在交换文件时不得停留在主线程：回调立即返回。
func TestUpdateMenuClickInstallsOffTheMainThread(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"}}
	state := updateStateWith(t, updates, &testNotifier{supported: true})
	items := state.updateMenuItems()
	if len(items) != 1 {
		t.Fatalf("a ready update put %d items in the menu, want one", len(items))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		items[0].Click(items[0], nil)
	}()
	waitClosed(t, done, "the menu item's click")

	// 菜单启动的是安装而非检查：它就是「重启以更新」那一行。
	if err := waitForCalls(t, updates, 0, 1); err != nil {
		t.Fatal(err)
	}
}

// 每个版本只显示一次通知，而非状态每被轮询一次就一次——对已告知过的版本也完全不再显示。
func TestUpdateNoticeOncePerVersion(t *testing.T) {
	updates := &testUpdates{}
	notifier := &testNotifier{supported: true}
	state := updateStateWith(t, updates, notifier)

	ready := domain.UpdateState{Status: domain.UpdateReady, Version: "v0.1.0-beta.6"}
	for range 5 {
		state.noteReadyUpdate(ready)
	}
	if len(notifier.notices) != 1 {
		t.Fatalf("the notice was shown %d times, want once", len(notifier.notices))
	}
	got := notifier.notices[0]
	if got.id != updateui.NoticeID {
		t.Errorf("the notice's id is %q, want %q", got.id, updateui.NoticeID)
	}
	if got.title == "" || got.body == "" {
		t.Errorf("the notice is %q / %q, want a title and a body", got.title, got.body)
	}
	if !contains([]string{got.body}, "0.1.0-beta.6") {
		t.Errorf("the notice's body is %q, want the version in it", got.body)
	}

	// 更新的版本是第二条通知：这是用户尚未收到的新消息。
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "v0.2.0"})
	if len(notifier.notices) != 2 {
		t.Fatalf("a second version sent %d notices, want two", len(notifier.notices))
	}

	// 非就绪状态什么也不说，用户忽略后同一版本再次就绪也不再提示。
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateChecking})
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateFailed})
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "v0.2.0"})
	if len(notifier.notices) != 2 {
		t.Errorf("a settled status sent a notice: %d notices, want two", len(notifier.notices))
	}
}

// 不能显示通知的桌面会被静默跳过：那一行才是始终存在的通知。
func TestUpdateNoticeSkippedWhenUnsupported(t *testing.T) {
	notifier := &testNotifier{supported: false}
	state := updateStateWith(t, &testUpdates{}, notifier)
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"})
	if len(notifier.notices) != 0 {
		t.Errorf("an unsupported desktop showed %d notices, want none", len(notifier.notices))
	}
}

// 没有版本号的就绪更新只能播报一次：没有东西能把第二条通知与第一条区分开。
func TestUpdateNoticeWithoutAVersionIsShownOnce(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := updateStateWith(t, &testUpdates{}, notifier)
	ready := domain.UpdateState{Status: domain.UpdateReady}
	state.noteReadyUpdate(ready)
	state.noteReadyUpdate(ready)
	if len(notifier.notices) != 1 {
		t.Fatalf("the notice was shown %d times, want once", len(notifier.notices))
	}
	// 之后出现的版本号是它自己的新消息。
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"})
	if len(notifier.notices) != 2 {
		t.Errorf("the versioned notice was shown %d times, want a second one", len(notifier.notices)-1)
	}
}

// 通知的点击会在打开设置窗口时定位到含该按钮的分区：这是通知除「稍后重启」外的全部目的。
//
// 打开窗口需要运行中的应用，故测试的是做出决定的那一半——点击定位到「关于」分区——
// 而非随后打开的窗口。
func TestUpdateNoticeClickRevealsTheAboutSection(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := updateStateWith(t, &testUpdates{}, notifier)
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"})
	if notifier.open == nil {
		t.Fatal("the notice has no click action")
	}
	// 点击对页面做了什么，不含外层的窗口。
	state.openSettingsSection(sectionAbout)
	if state.settingsReveal == nil || *state.settingsReveal != sectionAbout {
		t.Errorf("the notice's click reveals %v, want the about section", state.settingsReveal)
	}
}

// 服务的监听器在服务挂上时安装，它做的只是请求重绘——它运行在服务的 goroutine 上，
// 所以不能做别的事。
func TestAttachUpdatesInstallsTheListener(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateIdle}}
	state := testState(t, newTestProvider(t), withCredential(newTestStore(t)), "0.1.0")
	state.AttachUpdates(updates)
	if updates.onChange == nil {
		t.Fatal("the service was given no listener")
	}
	// 挂上 nil 是没有更新的构建该做的；它不得从已存在的服务那里拿走监听器。
	state.AttachUpdates(nil)
	if updates.onChange == nil {
		t.Error("attaching nothing removed the listener")
	}
	if state.updates != updates {
		t.Error("attaching nothing replaced the service")
	}
}

// 无论快速窗口是否曾打开，通知都能到达用户：更新在后台下载完成，服务报告的变更是一条
// 独立通往同一决定的路径。
//
// 本测试中没有窗口，而这正是要紧的情形——监听器在此运行于服务调用它的 goroutine 上，
// 与真实服务一致。
func TestUpdateNoticeFromTheServiceChange(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateChecking}}
	notifier := &testNotifier{supported: true}
	state := updateStateWith(t, updates, notifier)
	if updates.onChange == nil {
		t.Fatal("the service was given no listener")
	}
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if err := waitForNotices(notifier, 1); err != nil {
		t.Fatalf("a ready update reported by the service: %v", err)
	}
	// 帧自身的检查已无事可做，同一版本第二次报告也一样。
	state.refreshReadyUpdate()
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if got := len(notifier.notices); got != 1 {
		t.Errorf("the notice was shown %d times, want once", got)
	}
}

// waitForNotices 等待通知到达：监听器由服务的 goroutine 调用，通知随报告稍后到达。
func waitForNotices(notifier *testNotifier, want int) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := len(notifier.notices); got >= want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the notice count is %d, want %d", len(notifier.notices), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitClosed 在必须立即返回的回调没有几乎立即返回时使测试失败。
func waitClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

// waitForCalls 等待服务的调用达到要求的次数，否则报告：按压在各 goroutine 中运行，
// 故一次按压的效果在它返回后稍晚到达。
func waitForCalls(t *testing.T, updates *testUpdates, checks, installs int) error {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		gotChecks, gotInstalls := updates.counts()
		if gotChecks >= checks && gotInstalls >= installs {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the service was asked for %d checks and %d installs, want at least %d and %d",
				gotChecks, gotInstalls, checks, installs)
		}
		time.Sleep(time.Millisecond)
	}
}

// 交给 New 的服务是装配接线的另一种方式，它必须得到同一个变更监听器：没有监听器的服务
// 只会被绘制一次而永不重绘。main.go 走的就是这条路径。
func TestAServicePassedToNewGetsItsListener(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateIdle}}
	store := withCredential(newTestStore(t))
	provider := newTestProvider(t)
	panels, err := panel.NewPanels(
		testPanelRegistry(t, provider),
		store,
		panel.NewSection(),
		nil,
		[]panel.PanelDef{{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()}},
	)
	if err != nil {
		t.Fatalf("NewPanels: %v", err)
	}
	secrets, err := credential.NewSecrets(store)
	if err != nil {
		t.Fatalf("NewSecrets: %v", err)
	}
	state, err := New(mygo.App, Deps{
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: testClipboard(t),
		Version:   "0.1.0",
		Quit:      func() {},
		Updates:   updates,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if updates.onChange == nil {
		t.Fatal("the service passed to New was given no listener")
	}
	// 而且它就是服务报告所指向的监听器：就绪更新能到达该行与通知共同读取的决定。
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if got := state.updateState(); got.Status != domain.UpdateReady {
		t.Errorf("the state after a report = %s, want ready", got.Status)
	}
}
