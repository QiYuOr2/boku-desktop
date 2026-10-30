package ui

import (
	"boku/internal/presentation"
	"boku/internal/translate"
	"context"
	"github.com/egoist/mygo/ui"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// translateSection is the translation tool's own settings: whether it runs,
// what it asks, how much it annotates, what its window does, and which key
// opens it.
//
// Everything but the switch is drawn only while the tool is on. The rest of the
// section configures a tool that is not running — which service to ask, how to
// annotate, which key opens it — and a page full of controls for something the
// user has just turned off is a page that invites them to configure a thing
// that will not happen. The switch stays, because it is how they turn it back
// on.
func (s *State) translateSection(c *ui.Context, head *ui.Handle) {
	if s.translate == nil {
		return
	}
	settings := s.translateSettingsOr()
	presentation.Inset(c, func() {
		presentation.Stacked(c, float32(16), func() {
			presentation.SectionHead(c, head, s.translationMark, presentation.ColorStoneInk, "划词翻译", "任意语言 → 中文 · 免费基础 + AI 翻译")

			presentation.Stacked(c, float32(12), func() {
				enabled := settings.Enabled
				ui.Checkbox(c, &enabled, "启用划词翻译").
					FontSize(presentation.SizeBody).
					OnClick(func() { s.setTranslationEnabled(enabled) })

				if !showsTranslationControls(settings) {
					presentation.Notice(c, presentation.NotePlain, "划词翻译已关闭，快捷键不再占用。打开后可以在这里选择翻译服务。")
					s.translateNoticeLine(c)
					return
				}

				s.providerPicker(c, settings)
				s.annotationSwitches(c, settings)
				s.windowSwitches(c, settings)
				s.shortcutSwitches(c)
				s.translateNoticeLine(c)
			})
		})
	})
}

// translateNoticeLine is what the last change to the tool's settings said.
func (s *State) translateNoticeLine(c *ui.Context) {
	if s.translateNotice != "" {
		presentation.Notice(c, presentation.NotePlain, s.translateNotice)
	}
}

// showsTranslationControls reports whether the translation section draws the
// settings that configure the tool — the service picker, the annotation
// switches, the window switches and the shortcut — or only the switch that
// turns it on.
//
// It is the switch itself that decides, because it is the same fact: a tool
// that is not running has nothing to configure, and the only thing its section
// owes the user is the way back on.
func showsTranslationControls(settings translate.Settings) bool {
	return settings.Enabled
}

// providerPicker is the choice of translator, in the two groups the design
// draws: the free services (no key, nothing to configure) above, and the
// services that need an API key below.
//
// The split is not decoration. A user who has just installed Boku has to be
// able to answer "which of these do I have to do something about" at a glance,
// and the answer is "none of the first group".
//
// Each row is one translator, and picking one picks that translator: the free
// engines are two choices, not one choice with a pool behind it. A user who
// picks 微软 Edge 翻译 asks it, and if it fails the card says so rather than
// quietly handing the text to the other endpoint — the choice is theirs, and
// the answer is about the one they made.
func (s *State) providerPicker(c *ui.Context, settings translate.Settings) {
	cards := pickerTranslateCards(s.translateCards())
	presentation.Stacked(c, float32(8), func() {
		presentation.SectionLabel(c, "翻译服务")

		free, keyed := splitTranslateCards(cards)
		if len(free) > 0 {
			presentation.Stacked(c, float32(6), func() {
				pickerGroupLabel(c, "基础翻译（免费 · 无需配置）")
				for _, card := range free {
					s.providerOption(c, card, settings)
				}
			})
		}
		if len(keyed) > 0 {
			presentation.Stacked(c, float32(6), func() {
				pickerGroupLabel(c, "AI 大模型（需配置 API Key）")
				for _, card := range keyed {
					s.providerOption(c, card, settings)
				}
			})
		}
		presentation.Notice(c, presentation.NotePlain, "免费服务无需密钥；AI 服务仅显示已配置 API Key 的可选。")
	})
}

// pickerTranslateCards is the rows the picker draws, in the order the registry
// gave them.
//
// The order is never rearranged — not even to put the service in use first —
// because a list of choices that moves while the user is choosing is a list the
// user cannot aim at: the row they press must still be the row they pressed.
//
// A service the user has to configure is hidden until they have: the user's own
// service has an address to type before it is anything, and a row that cannot be
// picked yet is a row that only takes up room. The ones that need a key are
// still shown unconfigured, because that is how the user learns what they could
// configure.
func pickerTranslateCards(cards []presentation.Connection) []presentation.Connection {
	pickable := make([]presentation.Connection, 0, len(cards))
	for _, card := range cards {
		if card.ID == customTranslatorID && !card.Configured {
			continue
		}
		pickable = append(pickable, card)
	}
	return pickable
}

// splitTranslateCards is the picker's two groups: the free engines, and the
// services that have something to configure.
//
// The user's own service is in the second group even though its key is
// optional: its card is the one with an address to type, so it belongs under
// the heading that says something has to be configured.
func splitTranslateCards(cards []presentation.Connection) (free, keyed []presentation.Connection) {
	for _, card := range cards {
		if isFreeTranslator(card.ID) {
			free = append(free, card)
			continue
		}
		keyed = append(keyed, card)
	}
	return free, keyed
}

// pickerGroupLabel is the small line above a group of rows: smaller and
// quieter than sectionLabel, because it names the group rather than the form.
func pickerGroupLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(presentation.SizeSmall).FontWeight(500).TextColor(presentation.ColorTextMuted)
}

// providerOption is one row of the picker: the service's name, its state, and
// a dot that says which one is in use.
//
// The whole row is the target rather than the dot alone: a row that is about
// one choice should answer anywhere on it. A service with nothing configured
// is drawn but cannot be chosen, which is what tells the user what to
// configure.
//
// The state on the right is the design's own three words: a free engine is
// 免费 — the reason it can be picked at all — a keyed one with a credential is
// 已配置, and one without is 未配置. The chip is the sky one the design gives
// it, because it says "nothing to do here" rather than "you did this right".
//
// 免费 is the free engines' own word, and only theirs: the user's own service
// needs no key either, but it is not one of the free ones — it is a service
// the user runs, and calling it 免费 would be telling them something about a
// price Boku does not know.
func (s *State) providerOption(c *ui.Context, card presentation.Connection, settings translate.Settings) {
	selected := settings.ProviderOr() == card.ID && card.Configured
	row := ui.ButtonBase(c).Label("使用 "+card.DisplayName).
		FillWidth().
		Padding(float32(8), float32(12)).
		Radius(presentation.RadiusField).
		Border(1, presentation.ColorBorder).
		Disabled(!card.Configured)
	switch {
	case selected:
		row.Background(presentation.ColorBrandTint).Border(1, presentation.ColorBrand)
	case row.Hovered() && card.Configured:
		row.Background(presentation.ColorHover)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			// The dot says which one is in use without taking the keyboard:
			// the row itself is the control.
			ui.Box(c).Size(float32(12), float32(12)).Radius(999).Shrink(0).
				Border(1, chooseColor(selected, presentation.ColorBorder)).
				Background(chooseColor(selected, presentation.ColorSurface)).
				Children(func() {
					if selected {
						ui.Box(c).Size(float32(6), float32(6)).Radius(999).
							Background(presentation.ColorBrand)
					}
				})
			ui.Text(c, card.DisplayName).
				FontSize(presentation.SizeBody).
				TextColor(chooseTextColor(card.Configured)).
				SingleLine()
			if card.SupportsImage {
				presentation.KindBadge(c, presentation.ColorSkyTile, presentation.ColorSkyInk, "支持图片翻译")
			}
			ui.Spacer(c)
			switch {
			case isFreeTranslator(card.ID):
				presentation.KindBadge(c, presentation.ColorSkyTile, presentation.ColorSkyInk, "免费")
			case card.Configured:
				presentation.StatusNote(c, "已配置")
			default:
				ui.Text(c, "未配置").FontSize(presentation.SizeTiny).TextColor(presentation.ColorTextMuted).SingleLine()
			}
		})
	})
	if row.Clicked() && card.Configured && !selected {
		s.setTranslationProvider(card.ID)
	}
}

// chooseColor picks between two colors for the dot that says which service
// is in use.
func chooseColor(chosen bool, fallback ui.Color) ui.Color {
	if chosen {
		return presentation.ColorBrand
	}
	return fallback
}

// chooseTextColor is what a service's name is written in: the body color
// when it can be used, and the muted one when it cannot.
func chooseTextColor(configured bool) ui.Color {
	if configured {
		return presentation.ColorTextSoft
	}
	return presentation.ColorTextMuted
}

// annotationSwitches are the Japanese enhancements: what the translator is
// asked to say about each word.
func (s *State) annotationSwitches(c *ui.Context, settings translate.Settings) {
	presentation.Stacked(c, float32(10), func() {
		presentation.SectionLabel(c, "日语增强（仅日语原文）")
		furigana := settings.Furigana
		ui.Checkbox(c, &furigana, "汉字标注片假名读音").
			FontSize(presentation.SizeBody).
			OnClick(func() { s.setTranslationFurigana(furigana) })
		presentation.Notice(c, presentation.NotePlain, "例：機械語 → キカイゴ")

		gloss := settings.LoanwordGloss
		ui.Checkbox(c, &gloss, "片假名标注对应英文原词").
			FontSize(presentation.SizeBody).
			OnClick(func() { s.setTranslationLoanwordGloss(gloss) })
		presentation.Notice(c, presentation.NotePlain, "例：コンパイラ → compiler")
	})
}

// windowSwitches are the card's own behavior: whether it stays, and how long
// it waits before closing itself.
func (s *State) windowSwitches(c *ui.Context, settings translate.Settings) {
	presentation.Stacked(c, float32(10), func() {
		presentation.SectionLabel(c, "浮窗行为")
		pinned := settings.Pinned
		ui.Checkbox(c, &pinned, "默认固定（Pin）浮窗").
			FontSize(presentation.SizeBody).
			OnClick(func() { s.setTranslationPinned(pinned) })

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			presentation.SectionLabel(c, "关闭延迟（毫秒）")
			ui.Spacer(c)
			s.closeDelayField(c, settings)
		})
		presentation.Notice(c, presentation.NotePlain, "浮窗失去焦点后等待这段时间再关闭；固定后不再自动关闭。")
	})
}

// closeDelayField is the delay as a number field: a duration in
// milliseconds, the unit a person thinks in.
//
// The value is written when the user asks for it rather than on every
// keystroke: a half-typed number is not a setting, and writing one would make
// the window close itself the moment a user selected the old value to
// replace it.
func (s *State) closeDelayField(c *ui.Context, settings translate.Settings) {
	field := ui.TextInput(c, &s.delayInput)
	field.Placeholder(strconv.FormatInt(settings.CloseDelayOr().Milliseconds(), 10)).SingleLine()
	field.Disabled(s.connectionsBusy())
	presentation.InputFrame(c, field).Width(float32(88)).Font("monospace")

	text := strings.TrimSpace(s.delayInput)
	milliseconds := settings.CloseDelayOr().Milliseconds()
	switch {
	case text == "":
	case !isDigits(text):
		presentation.Notice(c, presentation.NoteDanger, "关闭延迟需要一个非负的毫秒数。")
		return
	default:
		if value, err := strconv.ParseInt(text, 10, 64); err == nil {
			milliseconds = value
		}
	}
	if button := presentation.DesignButton(c, "应用", presentation.ButtonSecondary); button.Clicked() {
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
	presentation.Stacked(c, float32(8), func() {
		presentation.SectionLabel(c, "快捷键")

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, "划词后弹出翻译").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextSoft).SingleLine()
				ui.Text(c, "选中文字后按此键显示浮窗").FontSize(presentation.SizeTiny).TextColor(presentation.ColorTextMuted).SingleLine()
			})
			s.shortcutButton(c, shortcutText)
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, "截图翻译").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextSoft).SingleLine()
				label := presentation.FriendlyAccelerator(s.translateSettingsOr().ScreenshotAcceleratorOr())
				if label == "" {
					label = "已关闭"
				}
				ui.Text(c, "框选屏幕后直接使用 AI 翻译 · "+label).FontSize(presentation.SizeTiny).TextColor(presentation.ColorTextMuted).SingleLine()
			})
			s.shortcutButton(c, shortcutScreenshot)
		})

		// What is bound now, or why nothing is. A registration that failed
		// has to be visible rather than silent, which is what
		// §6 of the tool design asks for.
		text, kind := s.shortcutLine()
		presentation.Notice(c, kind, text)
		if note, noteKind := s.shortcutNote(); note != "" {
			presentation.Notice(c, noteKind, note)
		}
		if platform := presentation.ShortcutPlatformNote(); platform != "" {
			presentation.Notice(c, presentation.NotePlain, platform)
		}
		if note := selectionPlatformNote(); note != "" && !s.selectionGranted {
			presentation.Notice(c, presentation.NotePlain, note)
			// One button, named for what the user is trying to get. It asks
			// the system to put Boku in the Accessibility list; the system
			// prompt is what shows the switch, and it offers System Settings
			// itself. Opening the pane for a release build would cover that
			// prompt; a development build is the exception, below.
			if button := presentation.DesignButton(c, "获取辅助功能权限", presentation.ButtonSecondary); button.Clicked() {
				if !s.requestSelectionAccess() && os.Getenv("MYGO_DEV") != "" {
					// mygo dev starts the app as a child of the terminal, and
					// TCC attributes the request to the terminal: the system
					// suppresses the prompt for a development build. Open the
					// pane instead, where the developer can add Boku Dev.
					c.OpenURL(accessibilitySettingsURL)
				}
			}
		}
		presentation.Notice(c, presentation.NotePlain, "点击快捷键可重新录制；与系统或其他软件冲突时请更换。")

		// An inline action at the end of the block, not another full-width
		// control: resetting is a correction to the binding above it, and a
		// block button would read as the section's main action.
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if button := presentation.DesignButton(c, "恢复默认快捷键", presentation.ButtonWarning).Shrink(0); button.Clicked() {
				s.resetShortcut()
			}
		})
	})
}

// requestSelectionAccess asks macOS to add Boku to its Accessibility list.
// The system owns the switch: the application can only ask, and the user then
// turns it on. The note says so rather than pretending the click granted it.
func (s *State) requestSelectionAccess() bool {
	granted := s.RequestSelectionAccess()
	slog.Info("selection access requested", "granted", granted)
	if granted {
		s.translateNotice = ""
	} else {
		s.translateNotice = "已请求辅助功能权限：请在「系统设置 > 隐私与安全性 > 辅助功能」中打开 Boku 的开关。"
	}
	// The request may have been granted before the call returned (an app
	// that already has the permission), or the system prompt may still be
	// up; the focus/activation refresh catches the latter.
	s.RefreshSelectionAccess()
	return granted
}

// RequestSelectionAccess asks the platform to show its permission prompt when
// the translation tool is on. The settings button is the caller: the settings
// window makes the app regular, which is what macOS needs before it shows the
// Accessibility prompt. The app does not ask at launch or from the shortcut:
// those run while it is accessory, and the system records such a request
// without showing a prompt.
func (s *State) RequestSelectionAccess() bool {
	if s.translate == nil {
		return false
	}
	settings, err := s.translate.Settings()
	if err != nil || !settings.Enabled {
		return false
	}
	return s.translate.RequestSelectionAccess()
}

// shortcutButton is the recording control: it shows the binding, and pressing
// it waits for the next combination.
//
// While it is recording it keeps the keyboard — the framework sends every key
// of the focused element to HandleInput — so the combination the user presses
// is read rather than acted on by the window.
func (s *State) shortcutButton(c *ui.Context, kind shortcutKind) ui.Element {
	state := s.shortcutStateFor(kind)
	accelerator := state.accelerator
	if accelerator == "" && kind == shortcutText {
		accelerator = s.translateSettingsOr().AcceleratorOr()
	}
	if accelerator == "" && kind == shortcutScreenshot {
		accelerator = s.translateSettingsOr().ScreenshotAcceleratorOr()
	}
	clear := (*ui.SVG)(nil)
	if kind == shortcutScreenshot {
		clear = s.closeMark
	}
	return presentation.ShortcutField{
		Accelerator: accelerator,
		Recording:   state.recording,
		ClearMark:   clear,
		ClearLabel:  "清除截图翻译快捷键",
		OnInput:     s.captureShortcut,
		OnPress: func() {
			if state.recording {
				s.cancelShortcutCapture()
			} else {
				s.beginShortcutCapture(kind)
			}
		},
		OnClear: s.clearScreenshotShortcut,
	}.View(c)
}

// setTranslationEnabled turns the tool on or off — and, with it, the global
// shortcut.
//
// The shortcut follows the switch rather than the switch merely being read when
// the shortcut fires: a key this process holds is a key no other application
// can use, so a tool that is off must let it go. Turning it back on takes the
// key again, which is why this is one action rather than two: the two states a
// user can be in are "off, and the key is free" and "on, and the key opens it".
func (s *State) setTranslationEnabled(enabled bool) {
	slog.Info("translation enabled setting changed", "event", "translation_enabled_changed", "enabled", enabled)
	s.saveTranslationSetting("划词翻译已打开。", func(ctx context.Context) error {
		return s.translate.SetEnabled(ctx, enabled)
	})
	s.bindShortcutForSwitch()
}

// setTranslationFurigana turns the reading of Japanese kanji on or off.
func (s *State) setTranslationFurigana(on bool) {
	slog.Info("furigana setting changed", "event", "translation_furigana_changed", "enabled", on)
	s.saveTranslationSetting("已更新汉字读音的标注。", func(ctx context.Context) error {
		return s.translate.SetFurigana(ctx, on)
	})
}

// setTranslationLoanwordGloss turns the English original of a katakana
// loanword on or off.
func (s *State) setTranslationLoanwordGloss(on bool) {
	slog.Info("loanword gloss setting changed", "event", "translation_loanword_gloss_changed", "enabled", on)
	s.saveTranslationSetting("已更新外来语原词的标注。", func(ctx context.Context) error {
		return s.translate.SetLoanwordGloss(ctx, on)
	})
}

// setTranslationPinned decides whether the window starts pinned.
func (s *State) setTranslationPinned(pinned bool) {
	slog.Info("translation default pin setting changed", "event", "translation_default_pin_changed", "pinned", pinned)
	s.saveTranslationSetting("浮窗行为已保存。", func(ctx context.Context) error {
		return s.translate.SetPinned(ctx, pinned)
	})
}

// setTranslationCloseDelay decides how long the window waits before closing
// itself.
func (s *State) setTranslationCloseDelay(delay time.Duration) {
	slog.Info("translation close delay changed", "event", "translation_close_delay_changed", "delay", delay)
	s.saveTranslationSetting("关闭延迟已保存。", func(ctx context.Context) error {
		return s.translate.SetCloseDelay(ctx, delay)
	})
	s.delayInput = ""
}

// setTranslationProvider chooses the translator to use.
func (s *State) setTranslationProvider(id string) {
	slog.Info("translation provider changed", "event", "translation_provider_changed", "provider", id)
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
	if err := save(context.Background()); err != nil {
		s.translateNotice = "没能写进设置文件：" + presentation.FriendlyError(err)
	} else {
		s.translateNotice = ok
	}
	s.Refresh()
}
