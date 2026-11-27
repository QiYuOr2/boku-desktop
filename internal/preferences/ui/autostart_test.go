package ui

import (
	"boku/internal/presentation"
	"errors"
	"testing"

	"boku/internal/platform/autostart"
)

type testAutostart struct {
	on bool
	// onErr 与 setErr 是各次回答的失败原因；nil 表示成功。
	onErr  error
	setErr error
	// writes 按发生顺序记录每次 Start 和 Stop。
	writes []bool
	// keys 记录开机启动被说成存放的位置。
	keys []string
}

func (a *testAutostart) Read(autostart.Launcher) (bool, error) {
	if a.onErr != nil {
		return false, a.onErr
	}
	return a.on, nil
}

func (a *testAutostart) Location(l autostart.Launcher) (string, error) {
	return "test-start/" + l.Label, nil
}

func (a *testAutostart) Start(autostart.Launcher) error {
	a.writes = append(a.writes, true)
	if a.setErr != nil {
		return a.setErr
	}
	a.on = true
	return nil
}

func (a *testAutostart) Stop(autostart.Launcher) error {
	a.writes = append(a.writes, false)
	if a.setErr != nil {
		return a.setErr
	}
	a.on = false
	return nil
}

func autostartState(t *testing.T, manager autostart.Manager) *State {
	t.Helper()
	state := &State{}
	state.autostart = manager
	return state
}

func TestAutostartStateReadsThePlatform(t *testing.T) {
	manager := &testAutostart{}
	state := autostartState(t, manager)
	on, enabled, status := state.autostartState()
	if !enabled || on || status.text != "" {
		t.Fatalf("a start that is off = on %v, enabled %v, status %q; want off and enabled", on, enabled, status.text)
	}
	manager.on = true
	if on, enabled, _ := state.autostartState(); !enabled || !on {
		t.Errorf("a start that is on = on %v, enabled %v; want both true", on, enabled)
	}
}

// 没有注入管理器的 State（测试或未装配的构建）不画开关，询问它也安全。
func TestAutostartWithoutAManager(t *testing.T) {
	state := &State{}
	on, enabled, status := state.autostartState()
	if on || enabled {
		t.Errorf("without a manager = on %v, enabled %v; want neither", on, enabled)
	}
	if status.text != "" {
		t.Errorf("without a manager the section said %q, want nothing", status.text)
	}
	// 即便如此按下也不能 panic，也不能写任何地方。
	state.setAutostart(true)
	if status := state.autostartStatus; status.text != "" {
		t.Errorf("setAutostart without a manager said %q, want nothing", status.text)
	}
}

func TestAutostartSwitchWritesTheStart(t *testing.T) {
	manager := &testAutostart{}
	state := autostartState(t, manager)
	state.setAutostart(true)
	if len(manager.writes) != 1 || !manager.writes[0] {
		t.Fatalf("the writes were %v, want one Start", manager.writes)
	}
	if on, _, status := state.autostartState(); !on || status.text != "" {
		t.Errorf("after turning it on = on %v, status %q; want on and no complaint", on, status.text)
	}
	state.setAutostart(false)
	if len(manager.writes) != 2 || manager.writes[1] {
		t.Fatalf("the writes were %v, want a Stop after the Start", manager.writes)
	}
	if on, _, status := state.autostartState(); on || status.text != "" {
		t.Errorf("after turning it off = on %v, status %q; want off and no complaint", on, status.text)
	}
}

// 系统拒绝的写入应让开关停在原位并说明原因：页面不能画出并未发生的启动。
func TestAutostartSwitchReportsARefusedWrite(t *testing.T) {
	manager := &testAutostart{setErr: errors.New("the registry refused the write")}
	state := autostartState(t, manager)
	state.setAutostart(true)
	on, _, status := state.autostartState()
	if on {
		t.Error("the switch is on after a write that failed")
	}
	if status.text == "" || status.kind != presentation.NoteDanger {
		t.Errorf("after a refused write the status = %q, %v; want a danger line", status.text, status.kind)
	}
}

func TestAutostartUnsupportedPlatform(t *testing.T) {
	manager := &testAutostart{onErr: autostart.ErrUnsupported}
	state := autostartState(t, manager)
	_, enabled, status := state.autostartState()
	if enabled {
		t.Error("the switch is drawn on a platform with no login start")
	}
	if status.text != autostartUnsupportedNote || status.kind != presentation.NotePlain {
		t.Errorf("the note = %q, %v; want the unsupported line", status.text, status.kind)
	}
}

// 用户在系统设置里关掉的登录项，与从未登记的不同：开关为关，但提示说明去哪里允许，
// 而不是谎称功能缺失或损坏。
func TestAutostartNeedsApproval(t *testing.T) {
	manager := &testAutostart{onErr: autostart.ErrNeedsApproval}
	state := autostartState(t, manager)
	on, enabled, status := state.autostartState()
	if on || !enabled {
		t.Errorf("a login item needing approval = on %v, enabled %v; want off and enabled", on, enabled)
	}
	if status.text != autostartApprovalNote || status.kind != presentation.NotePlain {
		t.Errorf("the status = %q, %v; want the approval note", status.text, status.kind)
	}
}

func TestAutostartStartNeedsApproval(t *testing.T) {
	manager := &testAutostart{setErr: autostart.ErrNeedsApproval}
	state := autostartState(t, manager)
	state.setAutostart(true)
	if status := state.autostartStatus; status.text != autostartApprovalNote || status.kind != presentation.NotePlain {
		t.Errorf("after a start needing approval the status = %q, %v; want the approval note", status.text, status.kind)
	}
}

func TestAutostartStartNotInstalled(t *testing.T) {
	manager := &testAutostart{setErr: autostart.ErrNotInstalled}
	state := autostartState(t, manager)
	state.setAutostart(true)
	if status := state.autostartStatus; status.text != autostartNotInstalledNote || status.kind != presentation.NoteDanger {
		t.Errorf("after a start outside Applications the status = %q, %v; want the install note", status.text, status.kind)
	}
}

// 读取失败不等于「关闭」：页面应说无法判断，而不是在无人确知时告诉用户启动已关闭。
func TestAutostartReadFailureIsNotOff(t *testing.T) {
	manager := &testAutostart{onErr: errors.New("the launch agent could not be read")}
	state := autostartState(t, manager)
	_, enabled, status := state.autostartState()
	if enabled {
		t.Error("the switch is drawn as working when the platform could not be read")
	}
	if status.kind != presentation.NoteDanger || status.text == "" {
		t.Errorf("the status = %q, %v; want a danger line", status.text, status.kind)
	}
}
