package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// The settings window is created when it is first asked for and closed for
// real afterwards: a tray application should cost nothing while nobody is
// configuring it.
const (
	settingsWidth  = 420
	settingsHeight = 460
)

// deepseekBrandColor draws the DeepSeek mark. The brand's blue, written
// here because it is what the provider's logo is drawn in, and not the
// theme's accent, which the user's system decides.
var deepseekBrandColor = ui.Hex("#4D6BFE")

// The connect form's answers.
const (
	statusNone = iota
	statusOK
	statusError
)

// connectForm is the settings window's state: what the user typed, what is
// already stored (masked, never in the clear), and what the last attempt
// answered. It lives on the main thread with the rest of the view state.
type connectForm struct {
	apiKey        string
	masked        string
	hasCredential bool
	busy          bool
	status        int
	statusText    string
	confirmDelete bool
}

// target is the provider the settings window configures. It is read from the
// panel on screen and the provider's own descriptor, so the window knows no
// vendor of its own.
type target struct {
	panelID       string
	providerID    string
	displayName   string
	docsURL       string
	endpoint      string
	credentialKey string
}

// target returns what to configure: the first panel's provider.
func (s *State) target() (target, bool) {
	listed := s.panelService.List()
	if len(listed) == 0 {
		return target{}, false
	}
	panel := listed[0]
	descriptor, err := s.panelService.Descriptor(panel.ProviderID)
	if err != nil || len(descriptor.CredentialKeys) == 0 {
		return target{}, false
	}
	return target{
		panelID:       panel.ID,
		providerID:    descriptor.ID,
		displayName:   descriptor.DisplayName,
		docsURL:       descriptor.DocsURL,
		endpoint:      descriptor.Endpoint,
		credentialKey: descriptor.CredentialKeys[0],
	}, true
}

// openSettings shows the settings window, creating it the first time.
func (s *State) openSettings() {
	if s.settingsWindow != nil {
		s.settingsWindow.Show()
		s.settingsWindow.Focus()
		return
	}
	s.loadCredential()
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     appName + " 设置",
		Width:     settingsWidth,
		Height:    settingsHeight,
		MinWidth:  360,
		MinHeight: 400,
		Content:   ui.View(s.settingsView),
	})
	s.settingsWindow = window
	window.OnClosed(func() {
		if s.settingsWindow == window {
			s.settingsWindow = nil
		}
	})
}

// settingsView builds the settings window: a page of settings, of which
// connecting a data source, the clipboard and the tools are sections. The
// window has no header of its own — its title bar already says it is Boku's
// settings — so the sections start at the top, and the groups still to come
// (shortcuts, the data directory, the vault) are added as further blocks.
func (s *State) settingsView(c *ui.Context) {
	t := c.Theme()
	c.Root().Background(t.Background)
	configured, ok := s.target()
	s.page(c, page{
		body: func(c *ui.Context) {
			if !ok {
				block(c, "数据源", func() {
					ui.Text(c, "还没有可以配置的数据源。").
						FontSize(t.Rem(0.9)).
						TextColor(t.TextMuted)
				})
			} else {
				// One block after another, with a divider between them: the
				// groups still to come are added the same way.
				s.connectBody(c, configured)
				ui.Divider(c)
			}
			s.clipboardBody(c)
		},
	})
}

// clipboardBody is the clipboard group: the two switches and the way to the
// history.
//
// Both switches start off, and the form says why: the clipboard is the most
// sensitive thing Boku can read. Turning monitoring off stops the reading
// rather than hiding it, and the form says that too, because "off" that
// still reads would be a lie.
//
// What each switch reports is done in its OnClick, not by comparing what was
// drawn with what is stored: a click is applied to the control's own value,
// and the switch it flips is read from that. Comparing the two after the
// fact would need the value to survive a pass, and a view is built several
// times with local state each time.
func (s *State) clipboardBody(c *ui.Context) {
	t := c.Theme()
	if s.clipboard == nil {
		return
	}
	block(c, "剪贴板", func() {
		settings := s.clipboard.Settings()
		ui.Text(c, "剪贴板内容可能包含密码、验证码与私人对话；默认不读取。").
			FontSize(t.Rem(0.75)).
			TextColor(t.TextMuted).
			Wrap()

		monitor := settings.ClipboardMonitoring
		ui.Checkbox(c, &monitor, "监听剪贴板（每 "+intervalLabel(settings)+" 读取一次）").
			FontSize(t.Rem(0.82)).
			OnClick(func() { s.setClipboardMonitoring(monitor) })

		visible := settings.ClipboardRegionVisible
		ui.Checkbox(c, &visible, "在快捷窗口底部显示剪贴板内容").
			FontSize(t.Rem(0.82)).
			OnClick(func() { s.setClipboardRegionVisible(visible) })

		if !settings.ClipboardMonitoring {
			ui.Text(c, "关闭监听后不再读取剪贴板，历史里已有的记录仍然保留。").
				FontSize(t.Rem(0.72)).
				TextColor(t.TextMuted).
				Wrap()
		}

		ui.Row(c).Gap(t.Space(2)).Children(func() {
			if button := ui.Button(c, "打开剪贴板历史"); button.Clicked() {
				s.openHistory()
			}
		})

		if text := s.clipboardStatusLine(); text != "" {
			ui.Text(c, text).FontSize(t.Rem(0.78)).TextColor(t.TextMuted).Wrap()
		}
	})
}

// intervalLabel names the poll interval the way the switch reads.
func intervalLabel(settings domain.ToolSettings) string {
	interval := settings.Interval()
	if interval%time.Second == 0 {
		return strconv.Itoa(int(interval/time.Second)) + " 秒"
	}
	return strconv.Itoa(int(interval/time.Millisecond)) + " 毫秒"
}

// setClipboardMonitoring flips the switch and says what happened, so the
// user is not left guessing whether it worked.
func (s *State) setClipboardMonitoring(enabled bool) {
	err := s.clipboard.SetMonitoring(context.Background(), enabled)
	settings := s.clipboard.Settings()
	switch {
	case err != nil:
		s.clipboardStatus = "开关没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	case settings.ClipboardMonitoring:
		s.clipboardStatus = "已开始读取剪贴板。"
	default:
		s.clipboardStatus = "已停止读取剪贴板。"
	}
	s.Refresh()
}

// setClipboardRegionVisible flips the visibility of the quick window's
// clipboard line.
func (s *State) setClipboardRegionVisible(visible bool) {
	err := s.clipboard.SetRegionVisible(context.Background(), visible)
	settings := s.clipboard.Settings()
	switch {
	case err != nil:
		s.clipboardStatus = "开关没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	case settings.ClipboardRegionVisible:
		s.clipboardStatus = "快捷窗口底部会显示剪贴板内容。"
	default:
		s.clipboardStatus = "快捷窗口底部不再显示剪贴板内容。"
	}
	s.Refresh()
}

// clipboardStatusLine is the last thing the clipboard switches said.
func (s *State) clipboardStatusLine() string { return s.clipboardStatus }

// providerMark is the icon the header shows for a provider: its own mark,
// which is what tells the user at a glance which account they are
// configuring. A provider the application has no mark for gets none, and
// the header shows its title alone.
func (s *State) providerMark(providerID string) brandedMark {
	if providerID != "deepseek" {
		return brandedMark{}
	}
	return brandedMark{svg: s.deepseekMark, color: deepseekBrandColor}
}

// connectBody is the provider's own block: its mark and name, the credential
// itself, what is already stored, and the two ways to use it. The address the
// provider asks is not shown: it is the provider's business, not the user's.
func (s *State) connectBody(c *ui.Context, configured target) {
	t := c.Theme()
	block(c, "", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			if mark := s.providerMark(configured.providerID); mark.svg != nil {
				ui.Image(c, mark.svg).Height(t.Space(8)).TextColor(mark.color)
			}
			ui.Text(c, configured.displayName).FontSize(t.Rem(0.95)).Bold()
		})

		ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
			ui.Text(c, credentialLabel(configured.credentialKey)).FontSize(t.Rem(0.8)).Bold()
			c.Key("credential." + configured.providerID + "." + configured.credentialKey)
			field := ui.TextInput(c, &s.form.apiKey)
			field.Password().Placeholder("sk-…").SingleLine()
			field.Disabled(s.form.busy)

			if s.form.hasCredential {
				s.storedCredential(c, configured)
			}

			ui.Row(c).Gap(t.Space(2)).Children(func() {
				if button := ui.Button(c, "测试连接"); button.Clicked() {
					s.testCredential(configured)
				}
				if button := ui.PrimaryButton(c, "保存"); button.Clicked() {
					s.saveCredential(configured)
				}
			})

			s.statusLine(c, configured)

			ui.Text(c, "凭据目前以明文保存（"+s.credentialPath()+"）。").
				FontSize(t.Rem(0.72)).
				TextColor(t.Warning).
				Wrap()
			ui.Text(c, "加密密钥库（Task 008）完成前，请不要把别人的 Key 存在这里。").
				FontSize(t.Rem(0.72)).
				TextColor(t.TextMuted).
				Wrap()
		})
	})
}

// storedCredential says that a credential is already stored, as its last four
// characters, and offers to remove it — after asking once.
func (s *State) storedCredential(c *ui.Context, configured target) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Text(c, "已配置："+s.form.masked).
			FontSize(t.Rem(0.8)).
			TextColor(t.Success)
		ui.Spacer(c)
		if s.form.confirmDelete {
			ui.Text(c, "删除？").FontSize(t.Rem(0.78)).TextColor(t.Danger)
			if button := textButton(c, "确认"); button.Clicked() {
				s.deleteCredential(configured)
			}
			if button := textButton(c, "取消"); button.Clicked() {
				s.form.confirmDelete = false
			}
			return
		}
		if button := textButton(c, "删除"); button.Clicked() {
			s.form.confirmDelete = true
		}
	})
}

// statusLine reports the last attempt: what is happening, or what happened.
func (s *State) statusLine(c *ui.Context, configured target) {
	t := c.Theme()
	switch {
	case s.form.busy:
		ui.Row(c).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在连接 "+configured.displayName+"…").
				FontSize(t.Rem(0.8)).
				TextColor(t.TextMuted)
		})
	case s.form.status == statusOK:
		ui.Text(c, s.form.statusText).FontSize(t.Rem(0.8)).TextColor(t.Success)
	case s.form.status == statusError:
		ui.Text(c, s.form.statusText).FontSize(t.Rem(0.8)).TextColor(t.Danger).Wrap()
	}
}

// loadCredential reads what is already stored, as the interface may show it.
// It runs on the main thread, over a local file, when the window opens.
func (s *State) loadCredential() {
	s.form = connectForm{}
	configured, ok := s.target()
	if !ok {
		return
	}
	masked, found, err := s.secretService.Masked(context.Background(), configured.providerID, configured.credentialKey)
	if err != nil {
		s.form.status = statusError
		s.form.statusText = friendlyError(err)
		return
	}
	s.form.masked = masked
	s.form.hasCredential = found
}

// testCredential asks the provider whether a key works, without storing it.
// The request runs off the main thread; the answer comes back through
// Window.Update, which is the only way a goroutine may touch this state.
func (s *State) testCredential(configured target) {
	if s.form.busy {
		return
	}
	key := strings.TrimSpace(s.form.apiKey)
	if key == "" {
		s.form.status = statusError
		s.form.statusText = "请先填入 " + credentialLabel(configured.credentialKey) + "。"
		return
	}
	window := s.settingsWindow
	panels := s.panelService
	providerID, credentialKey := configured.providerID, configured.credentialKey
	candidates := map[string]string{credentialKey: key}
	s.form.busy = true
	s.form.status = statusNone
	s.form.statusText = ""

	go func() {
		err := panels.TestProvider(context.Background(), providerID, candidates)
		window.Update(func() {
			s.form.busy = false
			if err != nil {
				s.form.status = statusError
				s.form.statusText = friendlyError(err)
				return
			}
			s.form.status = statusOK
			s.form.statusText = "连接成功，可以保存了。"
		})
	}()
}

// saveCredential stores the key and asks the panel to fetch with it.
func (s *State) saveCredential(configured target) {
	if s.form.busy {
		return
	}
	key := strings.TrimSpace(s.form.apiKey)
	if key == "" {
		s.form.status = statusError
		s.form.statusText = "请先填入 " + credentialLabel(configured.credentialKey) + "。"
		return
	}
	window := s.settingsWindow
	secrets := s.secretService
	providerID, credentialKey := configured.providerID, configured.credentialKey
	s.form.busy = true
	s.form.status = statusNone
	s.form.statusText = ""

	go func() {
		err := secrets.Set(context.Background(), providerID, credentialKey, key)
		window.Update(func() {
			s.form.busy = false
			if err != nil {
				s.form.status = statusError
				s.form.statusText = friendlyError(err)
				return
			}
			s.form.apiKey = ""
			s.form.status = statusOK
			s.form.statusText = "已保存。"
			s.loadCredentialAfterSave()
			// The panel now has what it was missing: fetch with it.
			s.refreshAsync(true)
		})
	}()
}

// deleteCredential removes the stored key, after the form asked once.
func (s *State) deleteCredential(configured target) {
	if s.form.busy {
		return
	}
	window := s.settingsWindow
	secrets := s.secretService
	providerID, credentialKey := configured.providerID, configured.credentialKey
	s.form.busy = true
	s.form.status = statusNone
	s.form.statusText = ""

	go func() {
		err := secrets.Delete(context.Background(), providerID, credentialKey)
		window.Update(func() {
			s.form.busy = false
			if err != nil {
				s.form.status = statusError
				s.form.statusText = friendlyError(err)
				return
			}
			s.loadCredentialAfterSave()
			s.form.status = statusOK
			s.form.statusText = "已删除。"
		})
	}()
}

// loadCredentialAfterSave refreshes the masked line without clearing the
// message that was just set.
func (s *State) loadCredentialAfterSave() {
	status, text := s.form.status, s.form.statusText
	confirm := s.form.confirmDelete
	s.loadCredential()
	s.form.status, s.form.statusText, s.form.confirmDelete = status, text, confirm
}

// credentialPath names the file the credentials are in, for the warning the
// form shows.
func (s *State) credentialPath() string {
	if path := s.secretService.Path(); path != "" {
		return path
	}
	return "本机文件"
}

// credentialLabel names a credential the way a person would.
func credentialLabel(key string) string {
	switch key {
	case "apiKey":
		return "API Key"
	case "adminKey":
		return "Admin Key"
	case "steamId":
		return "Steam ID"
	case "":
		return "凭据"
	default:
		return key
	}
}
