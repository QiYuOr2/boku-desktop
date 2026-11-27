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

func (s *State) translateSection(c *ui.Context, head *ui.Handle) {
	if s.translate == nil {
		return
	}
	settings := s.translateSettingsOr()
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHead(c, head, s.translationMark, presentation.ColorTextMuted, "划词翻译", "")

			enabled := settings.Enabled
			note := ""
			if !showsTranslationControls(settings) {
				note = "快捷键不再占用。"
			}
			presentation.SettingsPanel(c, func() {
				presentation.SettingsSwitchRow(c, "启用划词翻译", note, &enabled, s.setTranslationEnabled)
			})

			if !showsTranslationControls(settings) {
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
}

func (s *State) translateNoticeLine(c *ui.Context) {
	if s.translateNotice != "" {
		presentation.Notice(c, presentation.NotePlain, s.translateNotice)
	}
}

func showsTranslationControls(settings translate.Settings) bool {
	return settings.Enabled
}

// providerPicker 每个引擎是独立选项，选中哪个就只请求哪个（免费引擎是两个选择，
// 不是一个背后有池子的选择）；某个失败就如实报错，不悄悄改投另一个端点。
func (s *State) providerPicker(c *ui.Context, settings translate.Settings) {
	cards := pickerTranslateCards(s.translateCards())
	free, keyed := splitTranslateCards(cards)
	if len(free) > 0 {
		presentation.SettingsGroup(c, "基础翻译 · 免费", func() {
			for index, card := range free {
				if index > 0 {
					presentation.SettingsDivider(c)
				}
				s.providerOption(c, card, settings, index == 0, index == len(free)-1)
			}
		})
	}
	if len(keyed) > 0 {
		presentation.SettingsGroup(c, "AI 大模型", func() {
			for index, card := range keyed {
				if index > 0 {
					presentation.SettingsDivider(c)
				}
				s.providerOption(c, card, settings, index == 0, index == len(keyed)-1)
			}
		})
	}
}

// pickerTranslateCards 保持注册表给出的顺序，从不重排（包括不把在用服务提到最前）：
// 选择列表在用户挑选时移动会让人点错行。
// 用户自己的服务在填好地址前隐藏；需要 key 的服务即使未配置也照常显示，
// 让用户知道可以配置什么。
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

// splitTranslateCards 把选择器分成免费引擎和需配置服务两组。用户自己的服务归入后者
// （即使其 key 可选），因为它的卡片需要填地址。
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

// providerOption 渲染选择器的一行；整行都是点击目标。未配置的服务会画出但不可选。
// 免费 一词只用于免费引擎：用户自建服务虽也无需 key，但价格 Boku 无从得知。
func (s *State) providerOption(c *ui.Context, card presentation.Connection, settings translate.Settings, first, last bool) {
	selected := settings.ProviderOr() == card.ID && card.Configured
	tl, tr, br, bl := presentation.PanelRowRadius(first, last)
	row := ui.ButtonBase(c).Label("使用 "+card.DisplayName).
		FillWidth().
		Padding(presentation.SettingRowPadYCompact, presentation.SettingRowPadX).
		Radius(tl, tr, br, bl).
		Disabled(!card.Configured)
	if card.Configured {
		row.Cursor(ui.CursorPointer)
	}
	switch {
	case selected:
		row.Background(presentation.ColorBrandTint)
	case row.Hovered() && card.Configured:
		row.Background(presentation.ColorHover)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.RowGap).Children(func() {
			ui.Box(c).Size(float32(16), float32(16)).Radius(999).Shrink(0).Center().
				Border(1, chooseColor(selected, presentation.ColorBorder)).
				Background(presentation.ColorSurface).
				Children(func() {
					if selected {
						ui.Box(c).Size(float32(8), float32(8)).Radius(999).
							Background(presentation.ColorBrand)
					}
				})
			ui.Text(c, card.DisplayName).
				FontSize(presentation.SizeBody).
				FontWeight(500).
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
				ui.Text(c, "未配置").FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
			}
		})
	})
	if row.Clicked() && card.Configured && !selected {
		s.setTranslationProvider(card.ID)
	}
}

func chooseColor(chosen bool, fallback ui.Color) ui.Color {
	if chosen {
		return presentation.ColorBrand
	}
	return fallback
}

func chooseTextColor(configured bool) ui.Color {
	if configured {
		return presentation.ColorText
	}
	return presentation.ColorTextMuted
}

func (s *State) annotationSwitches(c *ui.Context, settings translate.Settings) {
	presentation.SettingsGroup(c, "日语增强", func() {
		furigana := settings.Furigana
		presentation.SettingsSwitchRow(c, "汉字标注片假名读音", "", &furigana, s.setTranslationFurigana)
		presentation.SettingsDivider(c)
		gloss := settings.LoanwordGloss
		presentation.SettingsSwitchRow(c, "片假名标注对应英文原词", "", &gloss, s.setTranslationLoanwordGloss)
	})
}

func (s *State) windowSwitches(c *ui.Context, settings translate.Settings) {
	presentation.SettingsGroup(c, "浮窗行为", func() {
		pinned := settings.Pinned
		presentation.SettingsSwitchRow(c, "默认固定浮窗", "固定后不再自动关闭。", &pinned, s.setTranslationPinned)
		presentation.SettingsDivider(c)
		presentation.SettingsRow(c, func() {
			var invalid bool
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.GroupGap).Children(func() {
				presentation.SectionLabel(c, "自动关闭延迟（毫秒）")
				ui.Spacer(c)
				invalid = s.closeDelayField(c, settings)
			})
			if invalid {
				presentation.Notice(c, presentation.NoteDanger, "关闭延迟需要一个非负的毫秒数。")
			}
		})
	})
}

// closeDelayField 只在用户点「应用」时写入，而不是每次按键：输入一半的数字不是设置，
// 写入它会让窗口在用户选中旧值准备替换的瞬间就自行关闭。
func (s *State) closeDelayField(c *ui.Context, settings translate.Settings) bool {
	invalid := false
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
		field := ui.TextInput(c, &s.delayInput)
		field.Placeholder(strconv.FormatInt(settings.CloseDelayOr().Milliseconds(), 10)).SingleLine()
		field.Disabled(s.connectionsBusy())
		presentation.InputFrame(c, field).Width(float32(88)).Font("monospace")

		text := strings.TrimSpace(s.delayInput)
		milliseconds := settings.CloseDelayOr().Milliseconds()
		switch {
		case text == "":
		case !isDigits(text):
			invalid = true
			return
		default:
			if value, err := strconv.ParseInt(text, 10, 64); err == nil {
				milliseconds = value
			}
		}
		if button := presentation.DesignButton(c, "应用", presentation.ButtonSecondary); button.Clicked() {
			s.setTranslationCloseDelay(time.Duration(milliseconds) * time.Millisecond)
		}
	})
	return invalid
}

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

func (s *State) shortcutSwitches(c *ui.Context) {
	presentation.SettingsGroup(c, "快捷键", func() {
		presentation.SettingsControlRow(c, "划词翻译", "", func() {
			s.shortcutButton(c, shortcutText)
		})
		presentation.SettingsDivider(c)
		presentation.SettingsControlRow(c, "截图翻译", "", func() {
			s.shortcutButton(c, shortcutScreenshot)
		})
		presentation.SettingsDivider(c)
		presentation.SettingsRowCompact(c, func() {
			// 当前绑定值，或未绑定的原因。注册失败必须可见而非静默，这是工具设计 §6 的要求。
			text, kind := s.shortcutLine()
			presentation.Notice(c, kind, text)
			if note, noteKind := s.shortcutNote(); note != "" {
				presentation.Notice(c, noteKind, note)
			}
			if platform := presentation.ShortcutPlatformNote(); platform != "" {
				presentation.Notice(c, presentation.NotePlain, platform)
			}
			needsAccess := selectionPlatformNote() != "" && !s.selectionGranted
			if needsAccess {
				presentation.Notice(c, presentation.NotePlain, selectionPlatformNote())
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
				if needsAccess {
					if button := presentation.DesignButton(c, "获取辅助功能权限", presentation.ButtonSecondary); button.Clicked() {
						if !s.requestSelectionAccess() && os.Getenv("MYGO_DEV") != "" {
							c.OpenURL(accessibilitySettingsURL)
						}
					}
				}
				ui.Spacer(c)
				if button := presentation.DesignButton(c, "恢复默认快捷键", presentation.ButtonWarning).Shrink(0); button.Clicked() {
					s.resetShortcut()
				}
			})
		})
	})
}

// requestSelectionAccess 请求 macOS 把 Boku 加入辅助功能列表。
// 开关由系统掌握：应用只能请求，再由用户打开。提示文案据此说明，而不是假装点击就已授权。
func (s *State) requestSelectionAccess() bool {
	granted := s.RequestSelectionAccess()
	slog.Info("selection access requested", "granted", granted)
	if granted {
		s.translateNotice = ""
	} else {
		s.translateNotice = "已请求辅助功能权限：请在「系统设置 > 隐私与安全性 > 辅助功能」中打开 Boku 的开关。"
	}
	// 请求可能在调用返回前就已获批（应用本就有权限），或系统弹窗仍未关闭；
	// 后一种情况由焦点/激活时的刷新兜住。
	s.RefreshSelectionAccess()
	return granted
}

// RequestSelectionAccess 在划词翻译开启时请求平台弹出授权提示。调用方是设置按钮：
// 设置窗口会把应用变成 regular，这是 macOS 显示辅助功能提示的前提。不在启动或快捷键里
// 请求：那时进程处于 accessory 状态，系统只会记录请求而不显示提示。
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

// shortcutButton 是录制控件：显示当前绑定，按下后等待下一个组合键。
// 录制期间它持有键盘（框架会把聚焦元素的每个按键都送到 HandleInput），
// 所以用户按下的组合会被读取，而不会由窗口执行。
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

// setTranslationEnabled 开关工具，同时开/关全局快捷键。
// 快捷键跟随开关，而不是在触发时才读取：本进程持有的键别的应用就用不了，
// 关闭的工具必须放掉它；重新打开则重新占用。二者合为一个动作，因为用户只有两种状态：
// 「关，且键空闲」和「开，且键能唤起」。
func (s *State) setTranslationEnabled(enabled bool) {
	slog.Info("translation enabled setting changed", "event", "translation_enabled_changed", "enabled", enabled)
	s.saveTranslationSetting("划词翻译已打开。", func(ctx context.Context) error {
		return s.translate.SetEnabled(ctx, enabled)
	})
	s.bindShortcutForSwitch()
}

func (s *State) setTranslationFurigana(on bool) {
	slog.Info("furigana setting changed", "event", "translation_furigana_changed", "enabled", on)
	s.saveTranslationSetting("已更新汉字读音的标注。", func(ctx context.Context) error {
		return s.translate.SetFurigana(ctx, on)
	})
}

func (s *State) setTranslationLoanwordGloss(on bool) {
	slog.Info("loanword gloss setting changed", "event", "translation_loanword_gloss_changed", "enabled", on)
	s.saveTranslationSetting("已更新外来语原词的标注。", func(ctx context.Context) error {
		return s.translate.SetLoanwordGloss(ctx, on)
	})
}

func (s *State) setTranslationPinned(pinned bool) {
	slog.Info("translation default pin setting changed", "event", "translation_default_pin_changed", "pinned", pinned)
	s.saveTranslationSetting("浮窗行为已保存。", func(ctx context.Context) error {
		return s.translate.SetPinned(ctx, pinned)
	})
}

func (s *State) setTranslationCloseDelay(delay time.Duration) {
	slog.Info("translation close delay changed", "event", "translation_close_delay_changed", "delay", delay)
	s.saveTranslationSetting("关闭延迟已保存。", func(ctx context.Context) error {
		return s.translate.SetCloseDelay(ctx, delay)
	})
	s.delayInput = ""
}

func (s *State) setTranslationProvider(id string) {
	slog.Info("translation provider changed", "event", "translation_provider_changed", "provider", id)
	s.saveTranslationSetting("已改用 "+id+"。", func(ctx context.Context) error {
		return s.translate.SetProvider(ctx, id)
	})
}

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
