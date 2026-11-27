package app

import (
	"log/slog"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	updateui "boku/internal/update/ui"
)

// 本文件是表现为层所见的更新检查：检查对话框、更新就绪时托盘菜单多出的一行，
// 以及应用不在屏幕上时告知就绪的通知。
//
// 表现为层只知道下面这个接口，别的一概不知。是否有更新、叫什么、如何下载、如何签名，
// 都在 internal/update 里基于 MyGo 自带更新器决定；本包不 import 它们，因为一个知道
// 订阅源是什么的视图就会去跟它对话。
//
// 没有服务的 State 是没有更新的构建——测试，或没装配任何东西的装配：不绘制该行、不自行
// 检查、菜单保持原样。此处任何东西都不许因此失败。

type UpdateService = updateui.Service

// desktopNotifier 是表现为层放置桌面通知的方式。做成接口只为一个原因：系统通知需要
// 运行中的应用，而该说什么、多久一次值得在没有窗口时测试。
type desktopNotifier interface {
	// Supported 报告桌面能否显示通知。
	Supported() bool
	// Notify 显示一条通知，被点击时运行 open。
	Notify(id, title, body string, open func())
}

// mygoNotifier 是真实的实现：框架自带的通知模块。
type mygoNotifier struct{}

func (mygoNotifier) Supported() bool { return mygo.NotificationsSupported() }

func (mygoNotifier) Notify(id, title, body string, open func()) {
	notice := mygo.NewNotification(mygo.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
		Group: "update",
	})
	notice.OnClick(open)
	if err := notice.Show(); err != nil {
		// 检查对话框仍会说明已就绪的内容：桌面无法显示通知并不算更新失败。
		slog.Warn("the update notice could not be shown", "error", err)
	}
}

func (s *State) prepareUpdates(updates UpdateService) {
	if updates == nil {
		return
	}
	s.updates = updates
	if s.notifier == nil {
		s.notifier = mygoNotifier{}
	}
	host := s.featureHost()
	s.updateUI = updateui.New(updates, s.notifier, host, s.showUpdateSettings, s.version)
}
func (s *State) showUpdateSettings() { s.openSettings(); s.openSettingsSection(sectionAbout) }
func (s *State) refreshReadyUpdate() {
	if s.updateUI != nil {
		s.updateUI.RefreshReadyUpdate()
	}
}
func (s *State) updateState() domain.UpdateState {
	if s.updateUI == nil {
		return domain.UpdateState{Status: domain.UpdateUnsupported}
	}
	return s.updateUI.UpdateState()
}
func (s *State) startUpdateCheck() {
	if s.updateUI != nil {
		s.updateUI.StartUpdateCheck()
	}
}
func (s *State) installUpdate() {
	if s.updateUI != nil {
		s.updateUI.InstallUpdate()
	}
}
func (s *State) noteReadyUpdate(state domain.UpdateState) {
	if s.updateUI != nil {
		s.updateUI.NoteReadyUpdate(state)
	}
}
func (s *State) updateCheckButton(c *ui.Context) {
	if s.updateUI != nil {
		s.updateUI.UpdateCheckButton(c)
	}
}
func (s *State) updateCheckDialogView(c *ui.Context) {
	if s.updateUI != nil {
		s.updateUI.UpdateCheckDialogView(c)
	}
}
func updateMenuLabel(state domain.UpdateState) (string, bool) { return updateui.MenuLabel(state) }
func updateVersion(version string) string                     { return updateui.Version(version) }

// updateMenuItems 是托盘菜单的更新行：更新就绪时一项，否则完全没有。
//
// 点击只启动一个 goroutine，因为菜单回调运行在主线程，而安装要写文件并重启应用。
func (s *State) updateMenuItems() []*mygo.MenuItem {
	if s.updates == nil {
		return nil
	}
	label, show := updateMenuLabel(s.updateState())
	if !show {
		return nil
	}
	return []*mygo.MenuItem{{
		Label: label,
		Click: func(*mygo.MenuItem, *mygo.Window) { s.installUpdate() },
	}}
}
