package app

import (
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

// The update check is tested where it is a decision rather than a drawing:
// what the dialog says, whether the tray menu gains a line, whether the
// notice is shown and how often. What the dialog looks like is a runtime
// acceptance, like the rest of the views.
//
// The fake service below stands where internal/update stands in production:
// it reports a state the test sets, counts what it was asked, and calls back
// the way the real service does — from another goroutine.

// testUpdates is an UpdateService the test drives.
type testUpdates struct {
	mu       sync.Mutex
	state    domain.UpdateState
	checks   int
	installs int
	// checkErr and installErr are what the two calls answer with, if set.
	checkErr   error
	installErr error
	// onChange is the listener the state installed. It is called on the
	// goroutine the calls below run on, as the real service's is.
	onChange func()
	// sawErr records whether a call answered with an error before it was
	// counted — what it answers, not what it does.
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

// report sets the state the service reports, then tells the listener the way
// the real one does: after the state, and from whatever goroutine it is on.
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

// testNotifier is an updateNotifier the test drives: what it was asked to
// show, and whether it was told it could.
type testNotifier struct {
	supported bool
	notices   []testNotice
	// open is the callback of the last notice, which is what a click runs.
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

// updateStateWith builds the real presentation state over the given fakes,
// with an update service attached the way the assembly attaches one.
func updateStateWith(t *testing.T, updates UpdateService, notifier desktopNotifier) *State {
	t.Helper()
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.notifier = notifier
	state.AttachUpdates(updates)
	return state
}

// The tray menu gains one item, and only for a ready update: every other
// status would add a line that can never do anything.
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

// A release tag's leading "v" belongs to the repository, not to the number:
// the interface is written without it, and text with no version at all is
// not decoration.
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
		want    updateDialogInfo
	}{
		{
			name:    "checking shows a loading latest version",
			state:   domain.UpdateState{Status: domain.UpdateChecking},
			current: "v0.1.0",
			want:    updateDialogInfo{latest: "正在获取", status: "正在获取最新版本…", loading: true},
		},
		{
			name:    "up to date compares with the current version",
			state:   domain.UpdateState{Status: domain.UpdateUpToDate},
			current: "v0.1.0",
			want:    updateDialogInfo{latest: "0.1.0", status: "已是最新版本"},
		},
		{
			name:    "ready names the new version",
			state:   domain.UpdateState{Status: domain.UpdateReady, Version: "v0.2.0"},
			current: "0.1.0",
			want:    updateDialogInfo{latest: "0.2.0", status: "新版本已准备就绪"},
		},
		{
			name:    "failure does not invent a latest version",
			state:   domain.UpdateState{Status: domain.UpdateFailed, Detail: "网络连接被拒绝。"},
			current: "",
			want:    updateDialogInfo{latest: "未能获取", status: "检查更新失败：网络连接被拒绝。"},
		},
		{
			name:    "unsupported is settled instead of loading forever",
			state:   domain.UpdateState{Status: domain.UpdateUnsupported},
			current: "0.1.0",
			want:    updateDialogInfo{latest: "不可用", status: "当前构建不支持自动更新"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := updateDialogState(c.state, c.current); got != c.want {
				t.Errorf("updateDialogState(%+v, %q) = %+v, want %+v", c.state, c.current, got, c.want)
			}
		})
	}
}

// A State with no update service is a build without one: the row is not
// drawn, the menu is what it always was, and nothing checks on its own or
// panics when asked to.
func TestUpdateWithoutAService(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
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

	// Every action a user could reach here is a no-op rather than a panic.
	state.startUpdateCheck()
	state.installUpdate()
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "9.9.9"})
	if len(notifier.notices) != 0 {
		t.Errorf("a state with no service sent %d notices, want none", len(notifier.notices))
	}
}

// menuLabels names the tray menu's items, in order. A separator is the empty
// string: what the menu is tested for is which lines it has.
func menuLabels(menu *mygo.Menu) []string {
	items := menu.Items()
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

// A service that is attached is what the views read, and the tray menu gains
// its line only while an update is ready.
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
	// Ready again, unchanged: still exactly one line.
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if items := state.updateMenuItems(); len(items) != 1 {
		t.Errorf("a second report of the same ready update put %d items in the menu, want one", len(items))
	}
}

// The menu item's click starts the install, and it must not sit on the main
// thread while the files are swapped: the callback returns at once.
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

	// The menu starts the install, not a check: it is the line that says
	// "restart to update".
	if err := waitForCalls(t, updates, 0, 1); err != nil {
		t.Fatal(err)
	}
}

// The notice is shown once per version, not once per frame the state is
// polled in — and not at all for a version the user has already been told
// about.
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
	if got.id != updateNoticeID {
		t.Errorf("the notice's id is %q, want %q", got.id, updateNoticeID)
	}
	if got.title == "" || got.body == "" {
		t.Errorf("the notice is %q / %q, want a title and a body", got.title, got.body)
	}
	if !contains([]string{got.body}, "0.1.0-beta.6") {
		t.Errorf("the notice's body is %q, want the version in it", got.body)
	}

	// A newer version is a second notice: it is news the user has not had.
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "v0.2.0"})
	if len(notifier.notices) != 2 {
		t.Fatalf("a second version sent %d notices, want two", len(notifier.notices))
	}

	// A status that is not ready says nothing, and neither does the same
	// version becoming ready again after the user ignored it.
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateChecking})
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateFailed})
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "v0.2.0"})
	if len(notifier.notices) != 2 {
		t.Errorf("a settled status sent a notice: %d notices, want two", len(notifier.notices))
	}
}

// A desktop that cannot show notices is skipped silently: the row is the
// notice that is always there.
func TestUpdateNoticeSkippedWhenUnsupported(t *testing.T) {
	notifier := &testNotifier{supported: false}
	state := updateStateWith(t, &testUpdates{}, notifier)
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"})
	if len(notifier.notices) != 0 {
		t.Errorf("an unsupported desktop showed %d notices, want none", len(notifier.notices))
	}
}

// A ready update with no version can only be announced once: there is
// nothing to tell a second notice apart from the first.
func TestUpdateNoticeWithoutAVersionIsShownOnce(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := updateStateWith(t, &testUpdates{}, notifier)
	ready := domain.UpdateState{Status: domain.UpdateReady}
	state.noteReadyUpdate(ready)
	state.noteReadyUpdate(ready)
	if len(notifier.notices) != 1 {
		t.Fatalf("the notice was shown %d times, want once", len(notifier.notices))
	}
	// A version that appears later is news of its own.
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"})
	if len(notifier.notices) != 2 {
		t.Errorf("the versioned notice was shown %d times, want a second one", len(notifier.notices)-1)
	}
}

// The notice's click opens the settings window on the section that has the
// button: it is the notice's whole purpose, after "restart when you like".
//
// Opening a window needs a running application, so what is tested is the
// half that decides — the click reveals the about section — rather than the
// window that then opens.
func TestUpdateNoticeClickRevealsTheAboutSection(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := updateStateWith(t, &testUpdates{}, notifier)
	state.noteReadyUpdate(domain.UpdateState{Status: domain.UpdateReady, Version: "1.0.0"})
	if notifier.open == nil {
		t.Fatal("the notice has no click action")
	}
	// What the click does to the page, without the window around it.
	state.openSettingsSection(sectionAbout)
	if state.settingsReveal == nil || *state.settingsReveal != sectionAbout {
		t.Errorf("the notice's click reveals %v, want the about section", state.settingsReveal)
	}
}

// The state's listener is installed when the service is attached, and what
// it does is ask for a repaint — it runs on the service's goroutine, so it
// must not do anything else.
func TestAttachUpdatesInstallsTheListener(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateIdle}}
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.AttachUpdates(updates)
	if updates.onChange == nil {
		t.Fatal("the service was given no listener")
	}
	// Attaching nothing is what a build without updates does; it must not
	// take the listener from a service that is already there.
	state.AttachUpdates(nil)
	if updates.onChange == nil {
		t.Error("attaching nothing removed the listener")
	}
	if state.updates != updates {
		t.Error("attaching nothing replaced the service")
	}
}

// The notice reaches the user whether or not the quick window was ever
// opened: an update finishes downloading in the background, so the change the
// service reports is a path of its own to the same decision the frame makes.
//
// No window exists in this test, which is the case that matters — the
// listener runs here on the goroutine the service called it from, the way
// the real one does.
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
	// The frame's own check finds nothing left to do, and neither does a
	// second report of the same version.
	state.refreshReadyUpdate()
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if got := len(notifier.notices); got != 1 {
		t.Errorf("the notice was shown %d times, want once", got)
	}
}

// waitForNotices waits for the notices to arrive: the listener is called by
// the service's goroutine, so the notice follows the report by a moment.
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

// waitClosed fails the test if a callback that must return at once has not
// returned almost immediately.
func waitClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

// waitForCalls waits for the service's calls to reach the counts asked for,
// and says so if they do not: the presses run in goroutines, so what a
// press does arrives a moment after it returns.
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

// A service handed to New is the other way an assembly can wire updates, and
// it has to get the same change listener: a service without one is a row that
// is drawn once and never redrawn. This is the path main.go takes.
func TestAServicePassedToNewGetsItsListener(t *testing.T) {
	updates := &testUpdates{state: domain.UpdateState{Status: domain.UpdateIdle}}
	store := withCredential(newTestStore())
	provider := newTestProvider()
	panels, err := panel.NewPanels(
		testRegistry{provider: provider, adapter: testAdapter{provider: provider}},
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
		Clipboard: testClipboard(t, nil, nil),
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
	// And it is the one the service reports to: a ready update reaches the
	// decision the row and the notice both read.
	updates.report(domain.UpdateState{Status: domain.UpdateReady, Version: "0.1.0-beta.6"})
	if got := state.updateState(); got.Status != domain.UpdateReady {
		t.Errorf("the state after a report = %s, want ready", got.Status)
	}
}
