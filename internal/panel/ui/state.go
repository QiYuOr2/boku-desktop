package ui

import (
	"boku/internal/panel"
	"boku/internal/presentation"
	"context"
	"errors"

	"github.com/egoist/mygo/ui"
)

type State struct {
	panelService                                              *panel.Panels
	host                                                      presentation.Host
	openSettings                                              func()
	deepseekMark, codexMark, grokMark, gaugeMark, refreshMark *ui.SVG
	dark, detailsOpen                                         bool
	loginNotice                                               string
	device                                                    deviceLogin
}

type deviceLogin struct {
	providerID string
	generation int
	code       string
	url        string
	open       string
	busy       bool
	notice     string
}

func New(service *panel.Panels, host presentation.Host, open func(), deepseek, codex, grok, gauge, refresh *ui.SVG) *State {
	return &State{panelService: service, host: host, openSettings: open, deepseekMark: deepseek, codexMark: codex, grokMark: grok, gaugeMark: gauge, refreshMark: refresh}
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
		if item.Panel.ProviderID == "grok" && s.panelService.DeviceLoggedIn(item.Panel.ProviderID) {
			if button := presentation.DesignButton(c, "退出 Grok 登录", presentation.ButtonSecondary); button.Clicked() {
				s.logoutDevice(item.Panel.ProviderID, item.Panel.ID)
			}
		}
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
	switch item.Panel.ProviderID {
	case "codex":
		return "本地 Codex 额度监控", "读取本机 Codex 命令行的登录状态，在悬浮窗显示额度；关闭后不再读取。"
	case "grok":
		return "Grok 额度", "用 xAI 设备码登录后，在悬浮窗显示订阅额度；关闭后不再请求，也不退出登录。"
	default:
		return item.Panel.Title, "关闭后悬浮窗不再显示它，也不再读取它的数据。"
	}
}

func (s *State) startDeviceLogin(providerID, panelID string) {
	s.device.generation++
	generation := s.device.generation
	s.device.providerID = providerID
	s.device.busy = true
	s.device.notice = ""
	s.device.code = ""
	s.Refresh()
	service, host := s.panelService, s.host
	go func() {
		prompt, err := service.BeginDeviceLogin(context.Background(), providerID)
		if err != nil {
			host.OnMain(func() {
				if s.device.generation != generation {
					return
				}
				s.device.busy = false
				s.device.notice = presentation.FriendlyError(err)
				s.Refresh()
			})
			return
		}
		host.OnMain(func() {
			if s.device.generation != generation {
				return
			}
			s.device.code = prompt.UserCode
			s.device.url = prompt.VerificationURL
			s.device.open = prompt.VerificationURL
			s.Refresh()
		})
		err = service.FinishDeviceLogin(context.Background(), providerID)
		host.OnMain(func() {
			if s.device.generation != generation {
				return
			}
			s.device.busy = false
			s.device.code = ""
			if err != nil && !errors.Is(err, context.Canceled) {
				s.device.notice = presentation.FriendlyError(err)
				s.Refresh()
				return
			}
			s.device.notice = ""
			if err == nil {
				s.RefreshPanelAsync(panelID, true)
				return
			}
			s.Refresh()
		})
	}()
}

func (s *State) cancelDeviceLogin(providerID string) {
	s.device.generation++
	s.device.busy = false
	s.device.code = ""
	s.device.notice = ""
	s.panelService.CancelDeviceLogin(providerID)
	s.Refresh()
}

func (s *State) logoutDevice(providerID, panelID string) {
	s.cancelDeviceLogin(providerID)
	service, host := s.panelService, s.host
	go func() {
		err := service.LogoutDeviceLogin(context.Background(), providerID)
		host.OnMain(func() {
			if err != nil {
				s.host.Status("退出登录没能写进凭据文件：" + presentation.FriendlyError(err))
			} else {
				s.host.Status("已退出 Grok 登录。")
			}
			s.RefreshPanelAsync(panelID, true)
		})
	}()
}
