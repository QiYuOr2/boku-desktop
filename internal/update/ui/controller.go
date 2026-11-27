package ui

import (
	"boku/internal/domain"
	"boku/internal/presentation"
	"context"
	"errors"
	"github.com/egoist/mygo/ui"
	"log/slog"
)

// 同一版本通知复用固定 ID，避免重复。
const NoticeID = "boku-update-ready"

func downloadingLine(state domain.UpdateState) string {
	return DownloadingLine(state)
}

func updateMenuLabel(state domain.UpdateState) (label string, show bool) {
	return MenuLabel(state)
}

func updateNotice(state domain.UpdateState) (title, body string) {
	return Notice(state)
}

func updateVersion(version string) string {
	return Version(version)
}

// 服务可从任意 goroutine 回调；状态和通知仅在主线程处理。
func (s *State) onUpdateChanged() { s.host.OnMain(s.RefreshReadyUpdate) }

func (s *State) RefreshReadyUpdate() {
	state := s.UpdateState()
	s.NoteReadyUpdate(state)
}

func (s *State) UpdateState() domain.UpdateState {
	if s.updates == nil {
		return domain.UpdateState{Status: domain.UpdateUnsupported}
	}
	return s.updates.State()
}

// 更新检查不得阻塞主线程。
func (s *State) StartUpdateCheck() {
	updates := s.updates
	if updates == nil {
		return
	}
	go func() {
		if err := updates.Check(context.Background()); err != nil {
			if errors.Is(err, domain.ErrUpdateUnsupported) {
				// 开发版和包管理构建不支持自更新，无需记录。
				return
			}
			slog.Info("the update check did not finish", "error", err)
		}
	}()
}

// 安装会写入文件并重启，必须异步执行。
func (s *State) InstallUpdate() {
	updates := s.updates
	if updates == nil {
		return
	}
	go func() {
		if err := updates.Install(context.Background()); err != nil {
			slog.Warn("the update could not be installed", "error", err)
			s.host.Refresh()
		}
	}()
}

// 仅通知本次运行中首次就绪的版本；该方法只在主线程调用，无需加锁。
func (s *State) NoteReadyUpdate(state domain.UpdateState) {
	if s.updates == nil || state.Status != domain.UpdateReady {
		return
	}
	version := updateVersion(state.Version)
	// 每个版本只通知一次。无名版本的就绪更新也只能通知一次，因为此时没有别的
	// 东西能把它与第一次通知区分开。
	notified := version
	if notified == "" {
		notified = updateNotifiedWithoutVersion
	}
	if s.updateNotified == notified {
		return
	}
	s.updateNotified = notified
	if s.notifier == nil || !s.notifier.Supported() {
		return
	}
	title, body := updateNotice(state)
	s.notifier.Notify(NoticeID, title, body, s.openSettings)
}

// updateNotifiedWithoutVersion 在 updateNotified 中代表一条无名版本的就绪更新
// 通知。它不会与真实版本冲突：版本经 updateVersion 处理，会裁剪且不会生成此值。
const updateNotifiedWithoutVersion = "\x00"

// UpdateCheckButton 绘制关于框的常驻操作：按需检查，让用户无需等应用自行发起
// 的下一次检查即可再次询问。
func (s *State) UpdateCheckButton(c *ui.Context) {
	if s.updates == nil {
		return
	}
	if button := presentation.DesignButton(c, "检查更新", presentation.ButtonSecondary); button.Clicked() {
		s.OpenUpdateCheckDialog()
	}
}

// OpenUpdateCheckDialog 先打开对话框再请求检查。Check 本身保持异步：按钮回调
// 在主线程，而对话框在后续帧读取服务状态。
func (s *State) OpenUpdateCheckDialog() {
	if s.updates == nil {
		return
	}
	s.updateCheckDialog = true
	s.StartUpdateCheck()
}

// DialogInfo 是显式检查对话框里的纯展示摘要。把文案移出视图，使版本映射可测试，
// 而无需测像素或 MyGo 窗口。
type DialogInfo struct {
	Latest  string
	Status  string
	Loading bool
}

func DialogState(state domain.UpdateState, current string) DialogInfo {
	current = updateVersion(current)
	if current == "" {
		current = "dev"
	}
	version := updateVersion(state.Version)
	switch state.Status {
	case domain.UpdateChecking:
		return DialogInfo{Latest: "正在获取", Status: "正在获取最新版本…", Loading: true}
	case domain.UpdateDownloading:
		if version == "" {
			version = "正在准备"
		}
		return DialogInfo{Latest: version, Status: downloadingLine(state), Loading: true}
	case domain.UpdateUpToDate:
		return DialogInfo{Latest: current, Status: "已是最新版本"}
	case domain.UpdateReady:
		if version == "" {
			version = "已找到新版本"
		}
		return DialogInfo{Latest: version, Status: "新版本已准备就绪"}
	case domain.UpdateFailed:
		if state.Detail != "" {
			return DialogInfo{Latest: "未能获取", Status: "检查更新失败：" + state.Detail}
		}
		return DialogInfo{Latest: "未能获取", Status: "检查更新失败"}
	case domain.UpdateUnsupported:
		return DialogInfo{Latest: "不可用", Status: "当前构建不支持自动更新"}
	default:
		return DialogInfo{Latest: "尚未获取", Status: "等待检查更新"}
	}
}

// UpdateCheckDialogView 绘制关于区「检查更新」按钮打开的对话框。它与 updateRow
// 有意分开：行是被动状态，用于定时检查；而此对话框是对用户刚发起请求的回应。
func (s *State) UpdateCheckDialogView(c *ui.Context) {
	ui.Modal(c, &s.updateCheckDialog, func() {
		state := s.UpdateState()
		info := DialogState(state, s.version)
		ui.Column(c).Width(float32(340)).Gap(float32(12)).Children(func() {
			ui.Text(c, "检查更新").FontSize(presentation.SizeTitle).FontWeight(600).TextColor(presentation.ColorText)
			ui.Column(c).FillWidth().
				Background(presentation.ColorField).
				Border(1, presentation.ColorBorder).
				Radius(presentation.RadiusField).
				Padding(float32(10)).
				Gap(float32(8)).
				Children(func() {
					updateDialogVersionLine(c, "最新版本", info.Latest)
					updateDialogVersionLine(c, "当前版本", updateVersionOrDev(s.version))
				})
			ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if info.Loading {
					ui.Spinner(c)
				}
				ui.Text(c, info.Status).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextBody).Wrap()
			})
			ui.Row(c).FillWidth().Justify(ui.End).Gap(float32(8)).Children(func() {
				if state.Status == domain.UpdateReady {
					if button := presentation.DesignButton(c, "重启并更新", presentation.ButtonPrimary); button.Clicked() {
						s.InstallUpdate()
					}
				}
				if state.Status == domain.UpdateFailed {
					if button := presentation.DesignButton(c, "重试", presentation.ButtonSecondary); button.Clicked() {
						s.StartUpdateCheck()
					}
				}
				if button := presentation.DesignButton(c, "关闭", presentation.ButtonSecondary); button.Clicked() {
					s.updateCheckDialog = false
				}
			})
		})
	})
}

func updateDialogVersionLine(c *ui.Context, label, version string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted)
		ui.Spacer(c)
		ui.Text(c, version).FontSize(presentation.SizeSmall).Font("monospace").TextColor(presentation.ColorText).SingleLine()
	})
}

func updateVersionOrDev(version string) string {
	if version = updateVersion(version); version != "" {
		return version
	}
	return "dev"
}
