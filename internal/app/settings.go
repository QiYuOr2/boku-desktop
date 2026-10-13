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
	settingsWidth = 420
	// settingsHeight is the room the page wants today: the title bar, the
	// two sections and the footer. A page with more sections on it scrolls
	// rather than growing the window past the screen.
	settingsHeight = 640
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
	// apiKey is what the user typed, and only that: a stored credential is
	// never put in the field, so what the field shows is the key being
	// replaced and never the one being kept. An empty field over a stored
	// credential is the state the design draws.
	apiKey string
	// stored is the stored credential as the store reports it to the
	// interface — its last four characters — which is what the status line
	// under the field names.
	stored        string
	hasCredential bool
	busy          bool
	status        int
	statusText    string
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
		MinHeight: 420,
		Content:   ui.View(s.settingsView),
	})
	s.settingsWindow = window
	window.OnClosed(func() {
		if s.settingsWindow == window {
			s.settingsWindow = nil
		}
	})
}

// settingsView builds the settings window: a column of sections with a hairline
// between them, and the version at its foot.
//
// The sections are peers: connecting a data source, the clipboard, and the
// groups still to come (shortcuts, the data directory, the vault) each state
// their own subject with a mark, a name and one line. Nothing nests inside
// anything else, and no section wears a card of its own.
//
// The window draws no title bar of its own: the system's title bar already
// names it, and repeating that name inside the window is a second title.
func (s *State) settingsView(c *ui.Context) {
	useDesign(c)
	configured, ok := s.target()
	windowFrame(c).Children(func() {
		body := ui.Scroll(c).FillWidth().Grow(1)
		body.Children(func() {
			content(c, false, func() {
				if !ok {
					inset(c, func() {
						sectionHead(c, s.clipboardMark, colorStoneInk, "数据源", "还没有可配置的数据源")
						notice(c, notePlain, "配置一个面板之后，这里可以填入它需要的凭据。")
					})
					betweenSections(c)
				} else {
					s.connectBody(c, configured)
					betweenSections(c)
				}
				s.clipboardBody(c)
			})
		})
		footer(c, s.versionLine())
	})
}

// connectBody is the data source section: the provider's mark and name, the
// credential itself, what is already stored, and the two ways to use it.
//
// The address the provider asks is not shown: it is the provider's business,
// not the user's.
func (s *State) connectBody(c *ui.Context, configured target) {
	mark := s.providerMark(configured.providerID)
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, mark.svg, mark.color, configured.displayName, credentialLabel(configured.credentialKey)+" 配置")

			stacked(c, float32(12), func() {
				sectionLabel(c, credentialLabel(configured.credentialKey))
				s.credentialField(c, configured)

				// What the credential is now: while one is stored, the line
				// under the field says so, as its last four characters.
				// Typing over it is the whole of replacing it.
				if s.credentialIsShown() {
					s.storedCredential(c)
				}

				ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
					if button := designButton(c, "测试连接", buttonSecondary); button.Clicked() {
						s.testCredential(configured)
					}
					if button := designButton(c, "保存", buttonPrimary); button.Clicked() {
						s.saveCredential(configured)
					}
				})

				s.statusLine(c, configured)

				noticePanel(c, "凭据目前以明文保存", "存放在 "+s.credentialPath()+"。"+
					"加密密钥库（Task 008）完成前，请不要把别人的 Key 存在这里。")
			})
		})
	})
}

// credentialField is the credential itself. It holds what the user is typing
// and never what is stored: a stored credential is a fact about the account
// rather than a value to retype, so the field stays empty and draws its own
// "sk-......" hint whether something is stored or not.
//
// It is always ready to be typed in, because typing is what replaces what is
// stored: there is no way in of its own to ask for a replacement, and nothing
// is replaced until a save reads the field. What is stored is named by the line
// under the field, as its last four characters — masked there, and nowhere
// else.
func (s *State) credentialField(c *ui.Context, configured target) {
	field := ui.TextInput(c, &s.form.apiKey)
	field.Password().Placeholder("sk-......").SingleLine()
	field.Disabled(s.form.busy)
	inputFrame(c, field).Font("monospace")
}

// clipboardBody is the clipboard section: the two switches, the record range
// and the way to the history.
//
// Both switches start off, and the section says why: the clipboard is the most
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
	if s.clipboard == nil {
		return
	}
	settings := s.clipboard.Settings()
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, s.clipboardMark, colorStoneInk, "剪贴板", "监听与预览设置")

			stacked(c, float32(12), func() {
				notice(c, notePlain, "剪贴板内容可能包含密码、验证码与私人对话；默认不读取。")

				stacked(c, float32(10), func() {
					monitor := settings.ClipboardMonitoring
					ui.Checkbox(c, &monitor, "监听剪贴板（每 "+intervalLabel(settings)+" 读取一次）").
						FontSize(sizeBody).
						OnClick(func() { s.setClipboardMonitoring(monitor) })

					visible := settings.ClipboardRegionVisible
					ui.Checkbox(c, &visible, "在快捷窗口底部显示剪贴板内容").
						FontSize(sizeBody).
						OnClick(func() { s.setClipboardRegionVisible(visible) })
				})

				// The record range: how much of what was copied is kept. It is
				// a choice among three rather than a number, and the line under
				// it says what shortening it costs, because the entries that no
				// longer fit are dropped rather than hidden.
				sizes := domain.ClipboardHistorySizes()
				selected := 0
				for index, size := range sizes {
					if size == settings.HistorySize() {
						selected = index
					}
				}
				stacked(c, float32(8), func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						sectionLabel(c, "记录范围")
						ui.Spacer(c)
						// The control takes its own width: a segmented control
						// stretched across the section would put its segments
						// wherever the room happens to fall.
						picker := segmented(c, &selected, historySizeLabels(sizes)...)
						if picker.Changed() {
							s.setClipboardHistorySize(sizes[selected])
						}
					})
					notice(c, notePlain, "只保留最近 "+strconv.Itoa(settings.HistorySize())+
						" 条；缩短范围会立刻丢弃更早的记录。")
					if !settings.ClipboardMonitoring {
						notice(c, notePlain, "关闭监听后不再读取剪贴板，历史里已有的记录仍然保留。")
					}
				})

				if button := s.historyButton(c); button.Clicked() {
					s.openHistory()
				}

				if text := s.clipboardStatusLine(); text != "" {
					notice(c, notePlain, text)
				}
			})
		})
	})
}

// historyButton is the way from the settings to the clipboard history: a
// full-width secondary action with the clipboard's mark on it, because the
// line it sits on is the section's own rather than a value's.
func (s *State) historyButton(c *ui.Context) ui.Element {
	b := ui.ButtonBase(c).Label("打开剪贴板历史").
		FillWidth().
		Height(heightControl).
		Radius(radiusField).
		Background(colorSurface).
		Border(1, colorBorder).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(colorHover)
	}
	b.Children(func() {
		// The mark and the words are one thing at the middle of the button,
		// not two things at its two ends.
		ui.Row(c).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Icon(c, s.clipboardMark).
				Size(float32(16), float32(16)).
				Shrink(0).
				TextColor(colorTextMuted)
			ui.Text(c, "打开剪贴板历史").FontSize(sizeBody).FontWeight(500).SingleLine()
		})
	})
	return b
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

// historySizeLabels names the record ranges the way the segments read.
func historySizeLabels(sizes []int) []string {
	labels := make([]string, 0, len(sizes))
	for _, size := range sizes {
		labels = append(labels, strconv.Itoa(size)+" 条")
	}
	return labels
}

// setClipboardHistorySize chooses the record range: what no longer fits is
// dropped here, not at the next copy. The line under the control is what says
// shortening the range costs the entries that no longer fit.
func (s *State) setClipboardHistorySize(size int) {
	if s.clipboard == nil {
		return
	}
	err := s.clipboard.SetHistorySize(context.Background(), size)
	if err != nil {
		s.clipboardStatus = "没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
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

// storedCredential says that a credential is already stored, as its last four
// characters. It is a state to be told about rather than a row of actions:
// replacing what is stored is done by typing over it, and a save is what
// replaces it, so there is nothing here to press.
//
// The line is a dot and a word in the success color. Whether a key is stored
// is what the user needs to know before typing another.
func (s *State) storedCredential(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		statusNote(c, "已配置："+s.form.stored)
	})
}

// statusLine reports the last attempt: what is happening, or what happened.
func (s *State) statusLine(c *ui.Context, configured target) {
	switch {
	case s.form.busy:
		ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在连接 "+configured.displayName+"…").
				FontSize(sizeSmall).
				TextColor(colorTextMuted)
		})
	case s.form.status == statusOK:
		notice(c, noteSuccess, s.form.statusText)
	case s.form.status == statusError:
		notice(c, noteDanger, s.form.statusText)
	}
}

// loadCredential reads what is already stored, as the interface may show it.
// It runs on the main thread, over a local file, when the window opens.
//
// What is stored does not go into the field: the field is where a new key is
// typed, and a value that is only shown is a value a save could read back as
// the credential. The stored credential is named by the status line instead —
// its last four characters — while the field keeps its own hint.
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
	s.form.stored = masked
	s.form.hasCredential = found
}

// credentialIsShown reports whether the window is still showing only the stored
// credential: something is stored and the field is empty, so there is nothing
// new to save or to test. Typing into the field is what leaves this state —
// there is no control of its own that asks for a replacement.
func (s *State) credentialIsShown() bool {
	return s.form.hasCredential && strings.TrimSpace(s.form.apiKey) == ""
}

// testCredential asks the provider whether a key works, without storing it.
// The request runs off the main thread; the answer comes back through
// Window.Update, which is the only way a goroutine may touch this state.
func (s *State) testCredential(configured target) {
	if s.form.busy {
		return
	}
	if s.credentialIsShown() {
		// What is stored has already been accepted once; what the user wants
		// to test is a replacement, which they have not typed yet.
		s.form.status = statusError
		s.form.statusText = "要先填入新的 " + credentialLabel(configured.credentialKey) + "，再测试连接。"
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
	if s.credentialIsShown() {
		// The field is showing what is stored, so there is nothing to save
		// yet: the value would be the mask, which is not a key.
		s.form.status = statusError
		s.form.statusText = "要先填入新的 " + credentialLabel(configured.credentialKey) + "，再保存。"
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

// loadCredentialAfterSave refreshes the masked line without clearing the
// message that was just set.
func (s *State) loadCredentialAfterSave() {
	status, text := s.form.status, s.form.statusText
	s.loadCredential()
	s.form.status, s.form.statusText = status, text
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
