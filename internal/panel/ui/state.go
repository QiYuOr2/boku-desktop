package ui

import (
	"boku/internal/panel"
	"boku/internal/presentation"
	"context"
	"github.com/egoist/mygo/ui"
)

type State struct {
	panelService                         *panel.Panels
	host                                 presentation.Host
	openSettings                         func()
	deepseekMark, gaugeMark, refreshMark *ui.SVG
	dark, detailsOpen                    bool
	loginNotice                          string
}

func New(service *panel.Panels, host presentation.Host, open func(), deepseek, gauge, refresh *ui.SVG) *State {
	return &State{panelService: service, host: host, openSettings: open, deepseekMark: deepseek, gaugeMark: gauge, refreshMark: refresh}
}

func (s *State) Refresh() { s.host.Refresh() }

func (s *State) startCredentialLogin(providerID, name string) {
	s.loginNotice = ""
	s.Refresh()
	service, host := s.panelService, s.host
	go func() {
		err := service.StartCredentialLogin(providerID)
		host.OnMain(func() {
			if err != nil {
				s.loginNotice = loginFailure(err, name)
			} else {
				s.loginNotice = "已在终端里打开登录；完成后点刷新。"
			}
			s.Refresh()
		})
	}()
}

func (s *State) SettingsView(c *ui.Context) {
	if s.panelService == nil {
		return
	}
	for _, item := range s.panelService.OptionalPanels() {
		on := item.On
		label, note := switchCopy(item)
		ui.Checkbox(c, &on, label).FontSize(presentation.SizeBody).OnClick(func() { s.setPanelOn(item.Panel.ID, on) })
		presentation.Notice(c, presentation.NotePlain, note)
	}
}

func (s *State) setPanelOn(id string, on bool) {
	err := s.panelService.SetPanelOn(context.Background(), id, on)
	text := "已关闭：悬浮窗不再显示它，也不再读取它的数据。"
	if err != nil {
		text = "开关没能写进设置文件：" + presentation.FriendlyError(err) + "（本次运行内已生效）"
	} else if on {
		text = "已开启：悬浮窗会重新显示它。"
	}
	s.host.Status(text)
	s.Refresh()
}

func switchCopy(item panel.OptionalPanel) (string, string) {
	if item.Panel.ProviderID == "codex" {
		return "本地 Codex 额度监控", "读取本机 Codex 命令行的登录状态，在悬浮窗显示额度；关闭后不再读取。"
	}
	return item.Panel.Title, "关闭后悬浮窗不再显示它，也不再读取它的数据。"
}
