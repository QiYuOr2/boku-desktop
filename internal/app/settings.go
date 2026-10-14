package app

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/tools"
)

// The settings window is a page of sections, and the sections are peers:
// the keys, the translation tool, the clipboard, the general switches and
// the about box. Nothing nests inside anything else, and no section wears a
// card of its own — a line between two sections is what says they are two.
//
// The window is created when it is first asked for and closed for real
// afterwards: a tray application should cost nothing while nobody is
// configuring it.
const (
	settingsWidth = 560
	// settingsHeight is the room the page wants: five sections and the
	// footer. A page with more on it scrolls rather than growing the window
	// past the screen.
	settingsHeight = 660
)

// deepseekBrandColor draws the DeepSeek mark. The brand's blue, written
// here because it is what the provider's logo is drawn in, and not the
// theme's accent, which the user's system decides.
var deepseekBrandColor = ui.Hex("#4D6BFE")

// customTranslatorID is the translator whose settings the user fills in
// themselves. It is named here as well as in the storage package because the
// window has two things about it that no other translator has: an address
// and a name.
const customTranslatorID = "custom"

// The connect form's answers.
const (
	statusNone = iota
	statusOK
	statusError
)

// cardStatus is what one API key card last said: its own line, because the
// cards are independent and a failure in one must not be drawn under
// another.
type cardStatus struct {
	status int
	text   string
}

// connectForm is the settings window's state: what the user typed in each
// card, what is already stored (masked, never in the clear), and what the
// last attempt answered.
//
// A card per provider rather than one form: the providers are configured
// independently, and the key one of them holds must not look like the key
// another was given.
type connectForm struct {
	// keys is what the user typed, per translator id, and only that. A
	// stored credential never goes into a field.
	keys map[string]string
	// stored is the stored credential as the store reports it to the
	// interface — its last four characters.
	stored map[string]string
	// hasCredential is whether each provider has one.
	hasCredential map[string]bool
	// customName and customURL are the user's own service, as typed. They
	// are not credentials: they live in the settings file, in the clear.
	customName string
	customURL  string
	// busy is the card that is being tested or saved, or empty.
	busy string
	// status is what each card's last attempt said.
	status map[string]cardStatus
	// inputCard is the card the credential field is showing, and input is
	// what has been typed into it.
	//
	// The field needs one string that outlives a frame — the framework binds
	// it by address — while the cards are drawn in a loop and only one of
	// them has a field at a time. So the typing lives here, and it is filed
	// under its card as soon as another card's field is built.
	inputCard string
	input     string
}

// translateCard is one translator as the window shows it: what it is called,
// where it asks, and whether it can be used now.
type translateCard struct {
	id              string
	displayName     string
	docsURL         string
	endpoint        string
	credentialKey   string
	needsCredential bool
	configured      bool
}

// openSettings shows the settings window, creating it the first time.
func (s *State) openSettings() {
	if s.settingsWindow != nil {
		s.settingsWindow.Show()
		s.settingsWindow.Focus()
		return
	}
	s.loadCredentials()
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     appName + " 设置",
		Width:     settingsWidth,
		Height:    settingsHeight,
		MinWidth:  settingsWidth,
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

// settingsView builds the settings window: a column of sections with a
// hairline between them, and the version at its foot.
//
// The window draws no title bar of its own: the system's title bar already
// names it, and repeating that name inside the window is a second title. It
// draws no navigation either: the sections are a scroll away from each
// other, and a window this size has no room to give a column of its own to
// five words.
func (s *State) settingsView(c *ui.Context) {
	useDesign(c)
	windowFrame(c).Children(func() {
		body := ui.Scroll(c).FillWidth().Grow(1)
		body.Children(func() {
			content(c, false, func() {
				s.apiKeySection(c)
				betweenSections(c)
				s.translateSection(c)
				betweenSections(c)
				s.clipboardSection(c)
				betweenSections(c)
				s.generalSection(c)
				betweenSections(c)
				s.aboutSection(c)
			})
		})
		footer(c, s.versionLine())
	})
}

// apiKeySection is where the services are configured: one card per
// translator, each with what it needs, where to get it, and whether what is
// stored works.
//
// Every card is the same shape, so that a second service is a second card
// rather than a second form: the name and its state at the top, the field
// under it, the two actions under that, and one line saying where the
// credential comes from.
func (s *State) apiKeySection(c *ui.Context) {
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, s.clipboardMark, colorStoneInk, "API Key", "配置各 AI 服务密钥")

			noticePanel(c, "凭据目前以明文保存",
				"存放在 "+s.credentialPath()+"。加密密钥库（Task 008）完成前，请不要把别人的 Key 存在这里。")

			cards := s.translateCards()
			if len(cards) == 0 {
				notice(c, notePlain, "这个版本没有可配置的翻译服务。")
				return
			}
			stacked(c, float32(12), func() {
				for _, card := range cards {
					s.apiKeyCard(c, card)
				}
			})
		})
	})
}

// apiKeyCard is one service: its name, its state, its credential, and the
// two things that can be done with it.
func (s *State) apiKeyCard(c *ui.Context, card translateCard) {
	ui.Column(c).FillWidth().
		Background(colorSurface).
		Border(1, colorBorder).
		Radius(radiusField).
		Padding(float32(12)).
		Children(func() {
			stacked(c, float32(10), func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					// A provider's own mark heads its card: it is the logo the
					// user knows, drawn in the provider's own color on a wash
					// of it, and never replaced by a generic picture.
					s.brandMark(c, card.id)
					ui.Text(c, card.displayName).
						FontSize(sizeBody).
						FontWeight(600).
						TextColor(colorTextSoft).
						SingleLine()
					ui.Spacer(c)
					configuredBadge(c, card.configured)
				})

				// The user's own service needs an address and a name before
				// it has anything to send to: they are the two fields that
				// make it a service rather than a word.
				if card.id == customTranslatorID {
					stacked(c, float32(8), func() {
						sectionLabel(c, "名称")
						s.customNameField(c)
						sectionLabel(c, "Base URL")
						s.customURLField(c)
					})
				}

				stacked(c, float32(8), func() {
					sectionLabel(c, credentialLabel(card.credentialKey))
					s.credentialField(c, card)
					if s.credentialIsShown(card) {
						statusNote(c, "已配置："+s.form.stored[card.id])
					}
					ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
						if button := designButton(c, "测试连接", buttonSecondary); button.Clicked() {
							s.testCredential(card)
						}
						if button := designButton(c, "保存", buttonPrimary); button.Clicked() {
							s.saveCredential(card)
						}
					})
					if card.id == customTranslatorID {
						notice(c, notePlain, "保存后即可作为翻译服务使用；本地服务可以留空 Key。")
					} else {
						notice(c, notePlain, "余额可在悬浮窗查看 · "+docsAddress(card.docsURL))
					}
					s.cardStatusLine(c, card)
				})
			})
		})
}

// configuredBadge says whether a service is configured, in the color that
// answers the question: green when it is, quiet when it is not.
func configuredBadge(c *ui.Context, configured bool) {
	tile, ink, label := colorStoneTile, colorStoneInk, "未配置"
	if configured {
		tile, ink, label = colorEmeraldTile, colorEmeraldInk, "已配置"
	}
	kindBadge(c, tile, ink, label)
}

// credentialField is the credential itself. It holds what the user is typing
// and never what is stored: a stored credential is a fact about the account
// rather than a value to retype, so the field stays empty and draws its own
// hint whether something is stored or not.
func (s *State) credentialField(c *ui.Context, card translateCard) {
	s.showCardInput(card.id)
	field := ui.TextInput(c, &s.form.input)
	field.Password().Placeholder(credentialHint(card)).SingleLine()
	field.Disabled(s.form.busy != "")
	inputFrame(c, field).Font("monospace")
}

// showCardInput points the one credential field at a card, filing what was
// typed into the card it belonged to.
//
// The window draws a field per card but binds them all to one string — the
// framework needs an address that outlives a frame, and a map entry has no
// address that does — so the value follows the field around: what a user
// typed into one card is still there when the frame comes back to it.
func (s *State) showCardInput(id string) {
	if s.form.inputCard == id {
		return
	}
	if s.form.inputCard != "" {
		if s.form.keys == nil {
			s.form.keys = map[string]string{}
		}
		s.form.keys[s.form.inputCard] = s.form.input
	}
	s.form.inputCard = id
	s.form.input = ""
	if s.form.keys != nil {
		s.form.input = s.form.keys[id]
	}
}

// keyValue is what was typed into a card, for the actions that read it. It is
// the same reading whether the card is the one on screen or the one before
// it: a test button can be pressed in the frame the field is drawn in.
func (s *State) keyValue(id string) string { return s.cardInput(id) }

// clearCardInput forgets what was typed into a card, once it has been saved.
func (s *State) clearCardInput(id string) {
	if s.form.keys != nil {
		s.clearCardInput(id)
	}
	if s.form.inputCard == id {
		s.form.input = ""
	}
}

// customNameField and customURLField are the two fields of the user's own
// service. They are not credentials: they are on the window's own face, not
// masked, and they are stored in the settings file.
func (s *State) customNameField(c *ui.Context) {
	field := ui.TextInput(c, &s.form.customName)
	field.Placeholder("例如：本地 Ollama").SingleLine()
	field.Disabled(s.form.busy != "")
	inputFrame(c, field)
}

func (s *State) customURLField(c *ui.Context) {
	field := ui.TextInput(c, &s.form.customURL)
	field.Placeholder("https://api.example.com/v1").SingleLine()
	field.Disabled(s.form.busy != "")
	inputFrame(c, field).Font("monospace")
}

// cardStatusLine reports what one card's last attempt said, under that card.
func (s *State) cardStatusLine(c *ui.Context, card translateCard) {
	if s.form.busy == card.id {
		ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在连接 "+card.displayName+"…").
				FontSize(sizeSmall).
				TextColor(colorTextMuted)
		})
		return
	}
	state, ok := s.form.status[card.id]
	if !ok {
		return
	}
	switch state.status {
	case statusOK:
		notice(c, noteSuccess, state.text)
	case statusError:
		notice(c, noteDanger, state.text)
	}
}

// translateSection is the translation tool's own settings: whether it runs,
// what it asks, how much it annotates, what its window does, and which key
// opens it.
func (s *State) translateSection(c *ui.Context) {
	if s.translate == nil {
		return
	}
	settings := s.translateSettingsOr()
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, s.translationMark, colorStoneInk, "划词翻译", "任意语言 → 中文 · AI 翻译")

			stacked(c, float32(12), func() {
				enabled := settings.Enabled
				ui.Checkbox(c, &enabled, "启用划词翻译").
					FontSize(sizeBody).
					OnClick(func() { s.setTranslationEnabled(enabled) })

				s.providerPicker(c, settings)
				s.annotationSwitches(c, settings)
				s.windowSwitches(c, settings)
				s.shortcutSwitches(c)
				if s.translateNotice != "" {
					notice(c, notePlain, s.translateNotice)
				}
			})
		})
	})
}

// providerPicker is the choice of translator: one row per registered
// service, with whether it can be used now. A service with no key is shown
// and cannot be picked, rather than hidden — the user has to know it exists
// to know what to configure.
func (s *State) providerPicker(c *ui.Context, settings domain.TranslationSettings) {
	cards := s.translateCards()
	stacked(c, float32(8), func() {
		sectionLabel(c, "使用的 AI")
		stacked(c, float32(6), func() {
			for _, card := range cards {
				s.providerOption(c, card, settings)
			}
		})
		notice(c, notePlain, "仅显示已配置 API Key 的服务可选。")
	})
}

// providerOption is one row of the picker: the service's name, its state, and
// a dot that says which one is in use.
//
// The whole row is the target rather than the dot alone: a row that is about
// one choice should answer anywhere on it. A service with nothing configured
// is drawn but cannot be chosen, which is what tells the user what to
// configure.
func (s *State) providerOption(c *ui.Context, card translateCard, settings domain.TranslationSettings) {
	selected := settings.ProviderOr() == card.id
	row := ui.ButtonBase(c).Label("使用 "+card.displayName).
		FillWidth().
		Padding(float32(8), float32(12)).
		Radius(radiusField).
		Border(1, colorBorder)
	switch {
	case selected:
		row.Background(colorBrand.Alpha(0.06)).Border(1, colorBrand)
	case row.Hovered() && card.configured:
		row.Background(colorHover)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			// The dot says which one is in use without taking the keyboard:
			// the row itself is the control.
			ui.Box(c).Size(float32(12), float32(12)).Radius(999).Shrink(0).
				Border(1, chooseColor(selected, colorBorder)).
				Background(chooseColor(selected, colorSurface)).
				Children(func() {
					if selected {
						ui.Box(c).Size(float32(6), float32(6)).Radius(999).
							Background(colorBrand)
					}
				})
			ui.Text(c, card.displayName).
				FontSize(sizeBody).
				TextColor(chooseTextColor(card.configured)).
				SingleLine()
			ui.Spacer(c)
			if card.configured {
				statusNote(c, "已配置")
			} else {
				ui.Text(c, "未配置").FontSize(sizeTiny).TextColor(colorTextMuted).SingleLine()
			}
		})
	})
	if row.Clicked() && card.configured && !selected {
		s.setTranslationProvider(card.id)
	}
}

// chooseColor picks between two colors for the dot that says which service
// is in use.
func chooseColor(chosen bool, fallback ui.Color) ui.Color {
	if chosen {
		return colorBrand
	}
	return fallback
}

// chooseTextColor is what a service's name is written in: the body color
// when it can be used, and the muted one when it cannot.
func chooseTextColor(configured bool) ui.Color {
	if configured {
		return colorTextSoft
	}
	return colorTextMuted
}

// annotationSwitches are the Japanese enhancements: what the translator is
// asked to say about each word.
func (s *State) annotationSwitches(c *ui.Context, settings domain.TranslationSettings) {
	stacked(c, float32(10), func() {
		sectionLabel(c, "日语增强（仅日语原文）")
		furigana := settings.Furigana
		ui.Checkbox(c, &furigana, "汉字标注片假名读音").
			FontSize(sizeBody).
			OnClick(func() { s.setTranslationFurigana(furigana) })
		notice(c, notePlain, "例：機械語 → キカイゴ")

		gloss := settings.LoanwordGloss
		ui.Checkbox(c, &gloss, "片假名标注对应英文原词").
			FontSize(sizeBody).
			OnClick(func() { s.setTranslationLoanwordGloss(gloss) })
		notice(c, notePlain, "例：コンパイラ → compiler")
	})
}

// windowSwitches are the card's own behavior: whether it stays, and how long
// it waits before closing itself.
func (s *State) windowSwitches(c *ui.Context, settings domain.TranslationSettings) {
	stacked(c, float32(10), func() {
		sectionLabel(c, "浮窗行为")
		pinned := settings.Pinned
		ui.Checkbox(c, &pinned, "默认固定（Pin）浮窗").
			FontSize(sizeBody).
			OnClick(func() { s.setTranslationPinned(pinned) })

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			sectionLabel(c, "关闭延迟（毫秒）")
			ui.Spacer(c)
			s.closeDelayField(c, settings)
		})
		notice(c, notePlain, "浮窗失去焦点后等待这段时间再关闭；固定后不再自动关闭。")
	})
}

// closeDelayField is the delay as a number field: a duration in
// milliseconds, the unit a person thinks in.
//
// The value is written when the user asks for it rather than on every
// keystroke: a half-typed number is not a setting, and writing one would make
// the window close itself the moment a user selected the old value to
// replace it.
func (s *State) closeDelayField(c *ui.Context, settings domain.TranslationSettings) {
	field := ui.TextInput(c, &s.delayInput)
	field.Placeholder(strconv.FormatInt(settings.CloseDelayOr().Milliseconds(), 10)).SingleLine()
	field.Disabled(s.form.busy != "")
	inputFrame(c, field).Width(float32(88)).Font("monospace")

	text := strings.TrimSpace(s.delayInput)
	milliseconds := settings.CloseDelayOr().Milliseconds()
	switch {
	case text == "":
	case !isDigits(text):
		notice(c, noteDanger, "关闭延迟需要一个非负的毫秒数。")
		return
	default:
		if value, err := strconv.ParseInt(text, 10, 64); err == nil {
			milliseconds = value
		}
	}
	if button := designButton(c, "应用", buttonSecondary); button.Clicked() {
		s.setTranslationCloseDelay(time.Duration(milliseconds) * time.Millisecond)
	}
}

// isDigits reports whether a string is a run of decimal digits, which is what
// a millisecond count has to be.
func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shortcutSwitches is the key that opens the window: one binding, because the
// tool has one action.
//
// The button is the recorder. Pressing it waits for the next combination; the
// combination is bound and remembered, and one the system or another
// application already holds leaves the binding that was there in place, with
// a line saying so. The design's other keys — 朗读原文、固定浮窗、关闭浮窗 —
// are not implemented, and are not drawn as buttons that would do nothing.
func (s *State) shortcutSwitches(c *ui.Context) {
	stacked(c, float32(8), func() {
		sectionLabel(c, "快捷键")

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, "划词后弹出翻译").FontSize(sizeBody).TextColor(colorTextSoft).SingleLine()
				ui.Text(c, "选中文字后按此键显示浮窗").FontSize(sizeTiny).TextColor(colorTextMuted).SingleLine()
			})
			s.shortcutButton(c)
		})

		// What is bound now, or why nothing is. A registration that failed
		// has to be visible rather than silent, which is what
		// docs/tools.md §6 asks for.
		text, kind := s.shortcutLine()
		notice(c, kind, text)
		if note, noteKind := s.shortcutNote(); note != "" {
			notice(c, noteKind, note)
		}
		if platform := shortcutPlatformNote(); platform != "" {
			notice(c, notePlain, platform)
		}
		notice(c, notePlain, "点击快捷键可重新录制；与系统或其他软件冲突时请更换。")
		notice(c, notePlain, "朗读、固定与关闭的组合键尚未实现。")

		if button := designButton(c, "恢复默认快捷键", buttonSecondary); button.Clicked() {
			s.resetShortcut()
		}
	})
}

// shortcutButton is the recording control: it shows the binding, and pressing
// it waits for the next combination.
//
// While it is recording it keeps the keyboard — the framework sends every key
// of the focused element to HandleInput — so the combination the user presses
// is read rather than acted on by the window.
func (s *State) shortcutButton(c *ui.Context) ui.Element {
	accelerator := s.shortcut.accelerator
	if accelerator == "" {
		accelerator = s.translateSettingsOr().AcceleratorOr()
	}
	label := friendlyAccelerator(accelerator)
	if s.shortcut.recording {
		label = "按下组合键…"
	}
	button := ui.ButtonBase(c).Label("快捷键 "+friendlyAccelerator(accelerator)).
		MinWidth(float32(140)).
		Height(heightControl-4).
		Padding(0, float32(12)).
		Radius(radiusField).
		Border(1, colorBorder).
		Background(colorSurface).
		Focusable().
		Cursor(ui.CursorPointer)
	switch {
	case s.shortcut.recording:
		button.Background(colorBrand.Alpha(0.10)).Border(1, colorBrand)
	case button.Hovered() || button.Focused():
		button.Background(colorHover)
	}
	button.Children(func() {
		text := ui.Text(c, label).
			FontSize(sizeSmall).
			Font("monospace").
			SingleLine()
		if s.shortcut.recording {
			text.TextColor(colorBrand)
		} else {
			text.TextColor(colorTextSoft)
		}
	})
	// The keys arrive here only while recording, and only this button takes
	// them: what is pressed elsewhere in the window is not a binding.
	button.HandleInput(s.captureShortcut)
	if s.shortcut.recording {
		// The keyboard has to be here for the keys to arrive at all.
		button.AutoFocus()
	}
	if button.Clicked() {
		if s.shortcut.recording {
			s.cancelShortcutCapture()
		} else {
			s.beginShortcutCapture()
		}
	}
	return button
}

// clipboardSection is the clipboard tool: the two switches, the record range
// and the way to the history.
//
// Both switches start off, and the section says why: the clipboard is the most
// sensitive thing Boku can read. Turning monitoring off stops the reading
// rather than hiding it, and the form says that too, because "off" that
// still reads would be a lie.
func (s *State) clipboardSection(c *ui.Context) {
	if s.clipboard == nil {
		return
	}
	settings := s.clipboard.Settings()
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, s.clipboardMark, colorStoneInk, "剪贴板", "监听与预览设置")

			stacked(c, float32(12), func() {
				notice(c, notePlain, "剪贴板内容可能包含密码、验证码与私人对话；默认不读取。")

				monitor := settings.ClipboardMonitoring
				ui.Checkbox(c, &monitor, "监听剪贴板（每 "+intervalLabel(settings)+" 读取一次）").
					FontSize(sizeBody).
					OnClick(func() { s.setClipboardMonitoring(monitor) })

				visible := settings.ClipboardRegionVisible
				ui.Checkbox(c, &visible, "在快捷窗口底部显示剪贴板内容").
					FontSize(sizeBody).
					OnClick(func() { s.setClipboardRegionVisible(visible) })

				// The record range: how much of what was copied is kept. It
				// is a choice among three rather than a number, and the line
				// under it says what shortening it costs, because the entries
				// that no longer fit are dropped rather than hidden.
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

// generalSection is the application's own behavior: what it does when it
// starts, and where its data is.
//
// Two of the three switches are drawn disabled with a line saying why: the
// application cannot register itself to start on this platform yet, and a
// switch that looks like a switch but does nothing is worse than one that
// says it is not there.
func (s *State) generalSection(c *ui.Context) {
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, s.settingsMark, colorStoneInk, "通用", "启动与外观")

			stacked(c, float32(10), func() {
				alwaysOnTop := s.alwaysOnTop
				ui.Checkbox(c, &alwaysOnTop, "始终置顶悬浮窗").
					FontSize(sizeBody).
					OnClick(func() { s.setAlwaysOnTop(alwaysOnTop) })

				unsupported := false
				ui.Checkbox(c, &unsupported, "开机自动启动").FontSize(sizeBody).Disabled(true)
				notice(c, notePlain, "尚未实现：需要平台的自启动注册，会在后续任务里接上。")
				ui.Checkbox(c, &unsupported, "启动时最小化到托盘").FontSize(sizeBody).Disabled(true)
				notice(c, notePlain, "尚未实现：应用没有主窗口，启动即常驻托盘。")
			})

			stacked(c, float32(8), func() {
				sectionLabel(c, "数据目录")
				ui.Text(c, s.dataPath()).
					FontSize(sizeSmall).
					TextColor(colorInputValue).
					Font("monospace").
					Wrap()
				if button := designButton(c, "在文件管理器中显示", buttonSecondary); button.Clicked() {
					s.revealDataPath()
				}
			})
		})
	})
}

// aboutSection is what the application is: the version, what it was built
// with, and where its files are. There is no update check: Boku has no server
// to ask, so a button that offered one would be a button that fails.
func (s *State) aboutSection(c *ui.Context) {
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, s.settingsMark, colorStoneInk, "关于", "版本与构建")

			ui.Column(c).FillWidth().
				Background(colorField).
				Border(1, colorBorder).
				Radius(radiusField).
				Padding(float32(12)).
				Children(func() {
					stacked(c, float32(6), func() {
						aboutLine(c, "版本", s.versionLine())
						aboutLine(c, "平台", runtime.GOOS+" / "+runtime.GOARCH)
						aboutLine(c, "构建", runtime.Version())
						aboutLine(c, "凭据", s.credentialPath())
					})
				})

			if button := designButton(c, "复制版本信息", buttonSecondary); button.Clicked() {
				s.copyVersionInfo()
			}
			notice(c, notePlain, "Boku 没有自己的服务器：面板与翻译都由你配置的服务直接应答。")
		})
	})
}

// aboutLine is one row of the about box: what it is on the left, what it is
// on the right, both on the field's own face.
func aboutLine(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine().Shrink(0)
		ui.Spacer(c)
		ui.Text(c, value).
			FontSize(sizeSmall).
			TextColor(colorText).
			Font("monospace").
			SingleLine()
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
	err := s.clipboard.SetMonitoring(backgroundContext(), enabled)
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
	err := s.clipboard.SetRegionVisible(backgroundContext(), visible)
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
// dropped here, not at the next copy.
func (s *State) setClipboardHistorySize(size int) {
	if s.clipboard == nil {
		return
	}
	err := s.clipboard.SetHistorySize(backgroundContext(), size)
	if err != nil {
		s.clipboardStatus = "没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	}
	s.Refresh()
}

// clipboardStatusLine is the last thing the clipboard switches said.
func (s *State) clipboardStatusLine() string { return s.clipboardStatus }

// setTranslationEnabled turns the tool on or off.
func (s *State) setTranslationEnabled(enabled bool) {
	s.saveTranslationSetting("划词翻译已打开。", func(ctx context.Context) error {
		return s.translate.SetEnabled(ctx, enabled)
	})
}

// setTranslationFurigana turns the reading of Japanese kanji on or off.
func (s *State) setTranslationFurigana(on bool) {
	s.saveTranslationSetting("已更新汉字读音的标注。", func(ctx context.Context) error {
		return s.translate.SetFurigana(ctx, on)
	})
}

// setTranslationLoanwordGloss turns the English original of a katakana
// loanword on or off.
func (s *State) setTranslationLoanwordGloss(on bool) {
	s.saveTranslationSetting("已更新外来语原词的标注。", func(ctx context.Context) error {
		return s.translate.SetLoanwordGloss(ctx, on)
	})
}

// setTranslationPinned decides whether the window starts pinned.
func (s *State) setTranslationPinned(pinned bool) {
	s.saveTranslationSetting("浮窗行为已保存。", func(ctx context.Context) error {
		return s.translate.SetPinned(ctx, pinned)
	})
}

// setTranslationCloseDelay decides how long the window waits before closing
// itself.
func (s *State) setTranslationCloseDelay(delay time.Duration) {
	s.saveTranslationSetting("关闭延迟已保存。", func(ctx context.Context) error {
		return s.translate.SetCloseDelay(ctx, delay)
	})
	s.delayInput = ""
}

// setTranslationProvider chooses the translator to use.
func (s *State) setTranslationProvider(id string) {
	s.saveTranslationSetting("已改用 "+id+"。", func(ctx context.Context) error {
		return s.translate.SetProvider(ctx, id)
	})
}

// saveTranslationSetting writes one switch and says what happened. Every
// translation switch goes through here, so that a failure to write the file
// is answered the same way everywhere: the switch did not change, and the
// window says so instead of showing a setting that is not stored.
func (s *State) saveTranslationSetting(ok string, save func(context.Context) error) {
	if s.translate == nil {
		return
	}
	if err := save(backgroundContext()); err != nil {
		s.translateNotice = "没能写进设置文件：" + friendlyError(err)
	} else {
		s.translateNotice = ok
	}
	s.Refresh()
}

// translateCards is the translators the window offers, in the order it
// shows them, with whether each can be used now. It is what the API key
// section draws a card for and what the picker draws a row for.
func (s *State) translateCards() []translateCard {
	if s.translate == nil {
		return nil
	}
	views, err := s.translate.Providers(backgroundContext())
	if err != nil {
		return nil
	}
	cards := make([]translateCard, 0, len(views))
	for _, view := range views {
		cards = append(cards, translateCard{
			id:              view.ID,
			displayName:     view.DisplayName,
			docsURL:         view.DocsURL,
			endpoint:        view.Endpoint,
			credentialKey:   view.CredentialKey,
			needsCredential: view.NeedsCredential,
			configured:      view.Configured,
		})
	}
	return cards
}

// translateSettings is the translation configuration, for the card's view.
func (s *State) translateSettings() (domain.TranslationSettings, error) {
	if s.translate == nil {
		return domain.TranslationDefaults(), nil
	}
	return s.translate.Settings()
}

// translateSettingsOr is the same configuration, or its defaults when it
// cannot be read: a window that cannot read a settings file shows what a
// fresh installation has rather than nothing.
func (s *State) translateSettingsOr() domain.TranslationSettings {
	settings, err := s.translateSettings()
	if err != nil {
		return domain.TranslationDefaults()
	}
	return settings
}

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

// brandMark is a provider's mark at the head of its card: the logo on a wash
// of its own brand color, in the room the section heads use for theirs. A
// provider the application has no mark for gets the wash alone, so that the
// two cards' names still start at the same place.
func (s *State) brandMark(c *ui.Context, providerID string) {
	mark := s.providerMark(providerID)
	ui.Box(c).Size(float32(28), float32(28)).Shrink(0).
		Radius(radiusTile).
		Background(brandTint).
		Center().
		Children(func() {
			if mark.svg == nil {
				return
			}
			ui.Icon(c, mark.svg).Size(float32(18), float32(18)).Shrink(0).TextColor(mark.color)
		})
}

// storedCredential says that a credential is already stored, as its last four
// characters. It is a state to be told about rather than a row of actions:
// replacing what is stored is done by typing over it, and a save is what
// replaces it, so there is nothing here to press.
func (s *State) storedCredential(c *ui.Context, card translateCard) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		statusNote(c, "已配置："+s.form.stored[card.id])
	})
}

// credentialIsShown reports whether a card is still showing only the stored
// credential: something is stored and the field is empty, so there is nothing
// new to save or to test. Typing into the field is what leaves this state.
func (s *State) credentialIsShown(card translateCard) bool {
	return s.form.hasCredential[card.id] && strings.TrimSpace(s.cardInput(card.id)) == ""
}

// cardInput is what a card's field holds, whether or not that card is the
// one the field is currently showing: what was typed into another card is
// filed under it until the frame comes back to it.
func (s *State) cardInput(id string) string {
	if s.form.inputCard == id {
		return strings.TrimSpace(s.form.input)
	}
	if s.form.keys == nil {
		return ""
	}
	return strings.TrimSpace(s.form.keys[id])
}

// loadCredentials reads what is already stored, as the interface may show it.
// It runs on the main thread, over local files, when the window opens.
//
// What is stored does not go into a field: the field is where a new key is
// typed, and a value that is only shown is a value a save could read back as
// the credential. The stored credential is named by the status line instead —
// its last four characters — while the field keeps its own hint.
func (s *State) loadCredentials() {
	translation := s.translateSettingsOr()
	s.form = connectForm{
		keys:          map[string]string{},
		stored:        map[string]string{},
		hasCredential: map[string]bool{},
		status:        map[string]cardStatus{},
		customName:    translation.Custom.DisplayName,
		customURL:     translation.Custom.BaseURL,
	}
	for _, card := range s.translateCards() {
		if !card.needsCredential {
			continue
		}
		masked, found, err := s.secretService.Masked(backgroundContext(), card.id, card.credentialKey)
		if err != nil {
			s.form.status[card.id] = cardStatus{status: statusError, text: friendlyError(err)}
			continue
		}
		s.form.stored[card.id] = masked
		s.form.hasCredential[card.id] = found
	}
}

// reloadCredentialsAfterSave refreshes the masked lines and the cards' state
// without clearing the message that was just set, nor what the user has typed
// in the other cards.
func (s *State) reloadCredentialsAfterSave() {
	status, keys := s.form.status, s.form.keys
	name, url := s.form.customName, s.form.customURL
	s.loadCredentials()
	s.form.status = status
	if keys != nil {
		s.form.keys = keys
	}
	s.form.customName, s.form.customURL = name, url
}

// testCredential asks a service whether a key works, without storing it. The
// request runs off the main thread; the answer comes back through
// Window.Update, which is the only way a goroutine may touch this state.
func (s *State) testCredential(card translateCard) {
	if s.form.busy != "" || s.translate == nil {
		return
	}
	if s.credentialIsShown(card) {
		s.setCardStatus(card.id, statusError, "要先填入新的 "+
			credentialLabel(card.credentialKey)+"，再测试连接。")
		return
	}
	if card.needsCredential && strings.TrimSpace(s.keyValue(card.id)) == "" {
		s.setCardStatus(card.id, statusError, "请先填入 "+credentialLabel(card.credentialKey)+"。")
		return
	}
	address, name := s.customValues()
	if card.id == customTranslatorID && address == "" {
		s.setCardStatus(card.id, statusError, "请先填写 Base URL。")
		return
	}
	window := s.settingsWindow
	service := s.translate
	override := map[string]string{}
	if value := strings.TrimSpace(s.keyValue(card.id)); value != "" {
		override[card.credentialKey] = value
	}
	s.form.busy = card.id
	s.setCardStatus(card.id, statusNone, "")

	go func() {
		ctx := context.Background()
		// A test of the user's own service saves the address first, because
		// the translator reads its address from the settings: testing what
		// was typed has to make what was typed the address.
		if card.id == customTranslatorID {
			_ = service.SetCustomTranslator(ctx, domain.CustomTranslatorSettings{
				BaseURL:     address,
				DisplayName: name,
			})
		}
		err := service.TestTranslator(ctx, card.id, override)
		window.Update(func() {
			s.form.busy = ""
			if err != nil {
				s.setCardStatus(card.id, statusError, friendlyError(err))
				return
			}
			s.setCardStatus(card.id, statusOK, "连接成功，可以保存了。")
		})
	}()
}

// customValues is the user's own service as typed, cleaned the way the domain
// cleans it.
func (s *State) customValues() (address, name string) {
	cleaned := domain.CustomTranslatorSettings{
		BaseURL:     s.form.customURL,
		DisplayName: s.form.customName,
	}.Normalize()
	return cleaned.BaseURL, cleaned.DisplayName
}

// saveCredential stores the key and, for the user's own service, the address
// and the name.
func (s *State) saveCredential(card translateCard) {
	if s.form.busy != "" || s.translate == nil {
		return
	}
	if s.credentialIsShown(card) {
		s.setCardStatus(card.id, statusError, "要先填入新的 "+
			credentialLabel(card.credentialKey)+"，再保存。")
		return
	}
	key := strings.TrimSpace(s.keyValue(card.id))
	if card.needsCredential && key == "" {
		s.setCardStatus(card.id, statusError, "请先填入 "+credentialLabel(card.credentialKey)+"。")
		return
	}
	address, name := s.customValues()
	if card.id == customTranslatorID && address == "" && key == "" {
		s.setCardStatus(card.id, statusError, "自定义服务至少需要一个 Base URL。")
		return
	}
	window := s.settingsWindow
	secrets := s.secretService
	service := s.translate
	id, credentialKey := card.id, card.credentialKey
	s.form.busy = card.id
	s.setCardStatus(card.id, statusNone, "")

	go func() {
		var err error
		if key != "" {
			err = secrets.Set(context.Background(), id, credentialKey, key)
		}
		if err == nil && id == customTranslatorID {
			err = service.SetCustomTranslator(context.Background(), domain.CustomTranslatorSettings{
				BaseURL:     address,
				DisplayName: name,
			})
		}
		window.Update(func() {
			s.form.busy = ""
			if err != nil {
				s.setCardStatus(id, statusError, friendlyError(err))
				return
			}
			s.clearCardInput(id)
			s.reloadCredentialsAfterSave()
			s.setCardStatus(id, statusOK, "已保存。")
			// A panel now has what it was missing: fetch with it.
			s.refreshAsync(true)
		})
	}()
}

// setCardStatus records what one card's last attempt said.
func (s *State) setCardStatus(id string, status int, text string) {
	if s.form.status == nil {
		s.form.status = map[string]cardStatus{}
	}
	s.form.status[id] = cardStatus{status: status, text: text}
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

// credentialHint is what an empty credential field shows: the shape of the
// key the service hands out, so that a user who pastes the wrong thing can
// see that it is the wrong thing.
func credentialHint(card translateCard) string {
	if !card.needsCredential {
		return "可选"
	}
	if card.credentialKey == "apiKey" {
		return "sk-......"
	}
	return credentialLabel(card.credentialKey)
}

// docsAddress is a documentation link written the way a person reads it: the
// host and path, without the scheme, because the line it sits on is a hint
// rather than a link.
func docsAddress(url string) string {
	trimmed := strings.TrimSpace(url)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	return trimmed
}

// dataPath is where the application keeps its files, or a line saying that
// it does not have one.
func (s *State) dataPath() string {
	path, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || strings.TrimSpace(path) == "" {
		return "本机用户目录"
	}
	return path
}

// revealDataPath opens the data directory in the system's file manager. It
// is what a user wants after reading a path: not to copy it, but to look.
func (s *State) revealDataPath() {
	path, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || strings.TrimSpace(path) == "" {
		return
	}
	mygo.Shell.OpenPath(path)
}

// copyVersionInfo puts what the about box says on the clipboard, because a
// bug report wants it and a person should not have to retype it.
func (s *State) copyVersionInfo() {
	if s.clipboard == nil {
		return
	}
	info := strings.Join([]string{
		s.versionLine(),
		runtime.GOOS + "/" + runtime.GOARCH,
		runtime.Version(),
	}, " · ")
	entry := domain.ClipboardEntry{
		ID:          "about-version",
		Kind:        domain.ClipKindText,
		Content:     info,
		Display:     info,
		ContentHash: tools.ClipContentHash(info),
		CopiedAt:    time.Now().UTC(),
	}
	s.clipboard.CopyAsync(entry, nil)
}

// setAlwaysOnTop remembers the preference. The quick window is created again
// on every open, so the switch takes effect the next time it is shown.
func (s *State) setAlwaysOnTop(on bool) {
	s.alwaysOnTop = on
	s.Refresh()
}

// backgroundContext is what a background job with nothing to cancel is
// given: the work carries its own timeouts, and a job that outlives the
// window that asked for it is still worth finishing.
func backgroundContext() context.Context { return context.Background() }
