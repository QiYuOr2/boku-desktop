package ui

import (
	"boku/internal/presentation"
	"errors"

	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/panel"
)

// 快速窗口里的面板：每个面板一个区块，区块之间画分隔线。
// 面板按标题指标的类型决定形状（比例画成进度条，数值显示为数字），
// 因此形态由 provider 上报的数据决定，这里不出现任何厂商名。
//
// 失败的面板要说明原因，绝不能用 0 代替没有拿到的答案。
func (s *State) QuickView(c *ui.Context, dark bool) {
	s.dark = dark
	panels := s.panelService.List()
	if len(panels) == 0 {
		presentation.QuickBlock(c, func() {
			ui.Text(c, "还没有面板。").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
		})
		return
	}
	for index, listed := range panels {
		if index > 0 {
			presentation.QuickDivider(c)
		}
		s.panelBlock(c, s.panelService.View(listed.ID))
	}
}

func (s *State) ProviderMark(id string) presentation.PanelMark { return s.providerMark(id) }

func (s *State) Block(c *ui.Context, panelID string, dark bool) {
	s.dark = dark
	c.Key(panelID)
	s.panelBlock(c, s.panelService.View(panelID))
}

func (s *State) panelBlock(c *ui.Context, view panel.View) {
	presentation.QuickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(8)).Children(func() {
			s.panelHead(c, view)
			if IsQuota(view) {
				s.quotaBody(c, view)
				return
			}
			s.cardNumbers(c, view)
		})
	})
}

// panelHead 是区块首行：图标与名称，右侧是数据时间与刷新入口。
// 额度区块会把账户档位作为徽标放在名称旁，下一行是窗口周期、重置时间与用量。
func (s *State) panelHead(c *ui.Context, view panel.View) {
	ui.Column(c).FillWidth().Gap(float32(2)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				presentation.QuickTile(c, s.providerMark(view.Panel.ProviderID))
				ui.Text(c, view.Panel.Title).
					FontSize(presentation.SizeQuickName).
					FontWeight(600).
					TextColor(presentation.PanelInk).
					SingleLine()
				if IsQuota(view) {
					if plan := Account(view); plan != "" {
						presentation.KindBadge(c, presentation.BadgeStoneFill, presentation.BadgeStoneInk, plan)
					}
				}
			})
			ui.Spacer(c)
			freshness(c, view)
			if button := presentation.RefreshButton(c, s.refreshMark, view.Loading, view.Err != nil && view.HasSnapshot); button.Clicked() && !view.Loading {
				s.RefreshPanelAsync(view.Panel.ID, true)
			}
		})
		if !IsQuota(view) {
			return
		}
		line, used := quotaTitle(read(view), view.Snapshot.ObservedAt), ""
		if share, ok := Used(view); ok {
			used = "已用 " + strconv.FormatInt(share, 10) + "%"
		}
		if line == "" && used == "" {
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			if line != "" {
				presentation.CardNote(c, line)
			}
			ui.Spacer(c)
			if used != "" {
				// 与进度条同源的百分比：单看进度条看不出已经用掉多少。
				ui.Text(c, used).
					FontSize(presentation.SizeQuickFine).
					TextColor(presentation.PanelFaint).
					SingleLine()
			}
		})
	})
}

var (
	deepseekBlue = ui.Hex("#4D6BFE")
	// deepseekBar 是深色页面上进度条用的同色品牌蓝：进度条与其中的小数字是
	// 画在页面上、而非瓦片内，因此需要更亮的蓝。
	deepseekBar = ui.Hex("#7C93FF")
	codexBlack  = ui.Hex("#111111")
)

// providerMark 描述区块标记的绘制方式：provider 自己的图形、底瓦色、图形色，
// 以及该区块内进度条的颜色。图形用应用自带资源，瓦片用 provider 自己的品牌色。
// 没有资源的 provider 使用应用的中性仪表占位图。
func (s *State) providerMark(providerID string) presentation.PanelMark {
	switch providerID {
	case "deepseek":
		bar := deepseekBlue
		if s.dark {
			bar = deepseekBar
		}
		return presentation.PanelMark{SVG: s.deepseekMark, Tile: deepseekBlue, Ink: presentation.ColorOnMark, Bar: bar}
	case "codex":
		// 单色瓦片随主题反转；进度条画在页面上，保留各自对比度合适的墨色。
		bar := codexBlack
		if s.dark {
			bar = presentation.ColorTextSoft
		}
		return presentation.PanelMark{SVG: s.codexMark, Tile: presentation.CodexTile, Ink: presentation.CodexInk, Bar: bar, Scale: 4.0 / 3.0}
	case "grok":
		bar := codexBlack
		if s.dark {
			bar = presentation.ColorTextSoft
		}
		return presentation.PanelMark{SVG: s.grokMark, Tile: presentation.CodexTile, Ink: presentation.CodexInk, Bar: bar}
	default:
		return presentation.PanelMark{SVG: s.gaugeMark, Tile: presentation.ColorMuted, Ink: presentation.ColorTextMuted, Bar: presentation.ColorTextMuted}
	}
}

// quotaBody 绘制额度：进度条及其代表的数字，无可绘制内容时给出后续操作。
//
// 进度条填充的是「已用」而非「剩余」：面板指标是剩余份额（见 provider 合约 §7），
// 随窗口消耗而增长的进度条更符合直觉。失败、空结果和未知窗口都不画填充，
// 这样「已用 0%」是一个答案，而不是缺失的答案。
func (s *State) quotaBody(c *ui.Context, view panel.View) {
	switch {
	case view.NeedsCredentials():
		if s.panelService.HasDeviceLogin(view.Panel.ProviderID) {
			s.deviceLogin(c, view)
			return
		}
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NotePlain, "还没有登录：这个额度来自本机已经登录好的命令行工具。")
			s.loginAction(c, view)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
			actionRow(c, "重试", func() { s.RefreshPanelAsync(view.Panel.ID, true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
		})
	default:
		used, known := Used(view)
		label := "已用 —"
		if known {
			label = "已用 " + strconv.FormatInt(used, 10) + "%"
		}
		// 进度条使用区块自身的颜色，让额度读起来像是该区块的一部分。
		mark := s.providerMark(view.Panel.ProviderID)
		presentation.ProgressBar(c, used, mark.Bar, label, "已用额度")
		windows := readWindows(view)
		if len(windows) > 1 && windows[1].hasLeft {
			long := windows[1]
			used := 100 - long.remaining
			ui.Column(c).FillWidth().Gap(float32(4)).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					presentation.CardNote(c, quotaTitle(long, view.Snapshot.ObservedAt))
					ui.Spacer(c)
					ui.Text(c, "已用 "+strconv.FormatInt(used, 10)+"%").
						FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint).SingleLine()
				})
				presentation.ProgressBar(c, used, mark.Bar, "已用 "+strconv.FormatInt(used, 10)+"%", "长周期已用额度")
			})
		}
	}

	// 刷新失败时进度条仍留在屏幕上：旧答案只要标明是旧的，就比没有好。
	if view.Err != nil && view.HasSnapshot {
		presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
	}
	if message := view.Snapshot.Message; message != "" {
		presentation.Notice(c, presentation.NotePlain, message)
	}
	if s.loginNotice != "" {
		presentation.Notice(c, presentation.NotePlain, s.loginNotice)
	}
}

func (s *State) deviceLogin(c *ui.Context, view panel.View) {
	if s.device.open != "" {
		page := s.device.open
		s.device.open = ""
		c.OpenURL(page)
	}
	ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
		if s.device.busy && s.device.providerID == view.Panel.ProviderID && s.device.code != "" {
			presentation.Notice(c, presentation.NotePlain, "在验证页输入 "+s.device.code+"，完成后这里会自己刷新。")
			ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
				if s.device.url != "" {
					if button := presentation.DesignButton(c, "打开验证页", presentation.ButtonPrimary); button.Clicked() {
						c.OpenURL(s.device.url)
					}
				}
				if button := presentation.DesignButton(c, "取消", presentation.ButtonSecondary); button.Clicked() {
					s.cancelDeviceLogin(view.Panel.ProviderID)
				}
				ui.Spacer(c)
			})
		} else if s.device.busy && s.device.providerID == view.Panel.ProviderID {
			presentation.Notice(c, presentation.NotePlain, "正在向 xAI 申请验证码…")
		} else {
			presentation.Notice(c, presentation.NotePlain, "登录 Grok 后查看订阅额度。令牌只发给 auth.x.ai 和 cli-chat-proxy.grok.com。")
			ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
				if button := presentation.DesignButton(c, "登录 Grok", presentation.ButtonPrimary); button.Clicked() {
					s.startDeviceLogin(view.Panel.ProviderID, view.Panel.ID)
				}
				ui.Spacer(c)
			})
		}
		if s.device.notice != "" {
			presentation.Notice(c, presentation.NoteDanger, s.device.notice)
		}
	})
}

// loginAction 通往额度面板所需的登录：启动该工具自己的登录，或打开其文档。
func (s *State) loginAction(c *ui.Context, view panel.View) {
	name := s.providerName(view.Panel.ProviderID)
	ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
		if button := presentation.DesignButton(c, "登录 "+name, presentation.ButtonPrimary); button.Clicked() {
			s.startCredentialLogin(view.Panel.ProviderID, name)
		}
		if button := presentation.DesignButton(c, "查看说明", presentation.ButtonSecondary); button.Clicked() {
			if descriptor, err := s.panelService.Descriptor(view.Panel.ProviderID); err == nil && descriptor.DocsURL != "" {
				c.OpenURL(descriptor.DocsURL)
			}
		}
		ui.Spacer(c)
	})
}

// providerName 返回 provider 的显示名，用于界面文案；名称属于 provider 而非本文件。
func (s *State) providerName(providerID string) string {
	if descriptor, err := s.panelService.Descriptor(providerID); err == nil && strings.TrimSpace(descriptor.DisplayName) != "" {
		return descriptor.DisplayName
	}
	return "这个数据源"
}

// startCredentialLogin 启动凭据来源的登录，并在按钮处报告结果，
// 而不是把失败静默吞掉。
//
// 该工作在主线程之外运行：macOS 上查找 CLI 需要询问用户的登录 shell，可能耗时；
// 点击回调必须立即返回，绝不能等待其它进程。
// loginFailure 说明登录为何无法启动，措辞指向按钮对应的服务。
func loginFailure(err error, name string) string {
	switch {
	case errors.Is(err, domain.ErrLoginCommandMissing):
		return "没有找到 " + name + " 的命令行工具：先安装它，再回来点这里。"
	case errors.Is(err, domain.ErrLoginUnsupported):
		return "这个平台没有可以打开登录的终端：请在终端里手动完成 " + name + " 的登录。"
	default:
		return presentation.FriendlyError(err)
	}
}

// cardNumbers 只绘制卡片确实拥有的内容：空状态（无凭据、无快照、失败）给出操作提示
// 而非数字；有快照时显示其标题指标，较小的数字折叠起来，等用户主动展开。
func (s *State) cardNumbers(c *ui.Context, view panel.View) {
	switch {
	case view.NeedsCredentials():
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NotePlain, "还没有配置凭据。配置后即可读取面板数据。")
			actionRow(c, "去配置", s.openSettings)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
			actionRow(c, "重试", func() { s.RefreshPanelAsync(view.Panel.ID, true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
		})
	default:
		balance(c, view, &s.detailsOpen)
	}

	// 刷新失败时数字仍留在屏幕上：旧答案只要标明是旧的，就比没有好；
	// 标题行会说明失败原因并提供重试。
	if view.Err != nil && view.HasSnapshot {
		presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
	}
	if message := view.Snapshot.Message; message != "" {
		presentation.Notice(c, presentation.NotePlain, message)
	}
}

// balance 绘制面板的数字：标题指标，其余折叠在其下。
func balance(c *ui.Context, view panel.View, detailsOpen *bool) {
	if len(view.Metrics) == 0 {
		return
	}
	headline := view.Metrics[0]
	rows := make([]metricRow, 0, len(view.Metrics)-1)
	for _, metric := range view.Metrics[1:] {
		rows = append(rows, metricRow{
			label: metric.Label,
			value: formatMetric(view.Snapshot, metric),
		})
	}
	if len(rows) == 0 {
		// 没有可折叠的内容：背后空无一物的展开控件就是欺骗用户。
		detailsOpen = nil
	}

	ui.Column(c).FillWidth().Children(func() {
		balanceHeadline(c, view, headline, detailsOpen)
		if detailsOpen == nil || !*detailsOpen {
			return
		}
		metricRows(c, rows)
	})
}

func balanceHeadline(c *ui.Context, view panel.View, headline domain.Metric, detailsOpen *bool) {
	ui.Row(c).FillWidth().AlignItems(ui.End).Children(func() {
		ui.Column(c).Gap(float32(2)).Children(func() {
			ui.Text(c, headline.Label).
				FontSize(presentation.SizeQuickFine).
				TextColor(presentation.PanelFaint).
				SingleLine()
			balanceNumber(c, view, headline)
		})
		ui.Spacer(c)
		if detailsOpen != nil {
			disclosure(c, "明细", detailsOpen)
		}
	})
}

// balanceNumber 显示数字本身；当 provider 提供官网链接时，数字同时是跳转入口。
// 它带有指针光标与悬停时的品牌绿，让这个动作不至于被忽略。
func balanceNumber(c *ui.Context, view panel.View, headline domain.Metric) {
	number := formatMetric(view.Snapshot, headline)
	if view.UsageURL == "" {
		ui.Text(c, number).
			FontSize(presentation.SizeQuickNumber).
			FontWeight(700).
			TextColor(presentation.PanelNumber).
			SingleLine()
		return
	}
	button := ui.ButtonBase(c).
		Label("查看" + headline.Label + "明细").
		Tooltip("在官网查看明细").
		Cursor(ui.CursorPointer)
	button.Children(func() {
		text := ui.Text(c, number).
			FontSize(presentation.SizeQuickNumber).
			FontWeight(700).
			SingleLine()
		if button.Hovered() || button.Focused() {
			text.TextColor(presentation.ColorBrand)
		} else {
			text.TextColor(presentation.PanelNumber)
		}
	})
	if button.Clicked() {
		// 由系统打开链接，这里不等待结果；浏览器打不开不是本窗口的失败。
		c.OpenURL(view.UsageURL)
	}
}

// disclosure 是展开/收起标题下方各行的控件：一个标签加一个小箭头，
// 指针、Enter 和空格均可切换。它画在被展开内容的同一行上，而不是其上方——
// ui.Collapsible 做不到这点，因为它的触发器与面板同处一列。
func disclosure(c *ui.Context, label string, open *bool) {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(*open).
		Radius(float32(4)).
		Padding(float32(2), float32(6)).
		AlignItems(ui.Center).Gap(float32(2))
	switch {
	case button.Pressed():
		button = button.Background(presentation.PanelPress)
	case button.Hovered() || button.Focused():
		button = button.Background(presentation.PanelField)
	}
	button.Children(func() {
		ink := presentation.PanelFaint
		if button.Hovered() || button.Focused() {
			ink = presentation.ColorBrand
		}
		ui.Text(c, label).FontSize(presentation.SizeQuickFine).TextColor(ink).SingleLine()
		presentation.DisclosureArrow(c, turn(*open), ink)
	})
	if button.Clicked() {
		*open = !*open
	}
}

// cardDisclosure 是翻译卡片用的 disclosure：它上报点击而不翻转标志位，
// 因为卡片高度随内容变化，展开方需要重新请求测量它的帧。
func cardDisclosure(c *ui.Context, label string, open bool) bool {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(open).
		Radius(999).Padding(float32(2), float32(8)).
		AlignItems(ui.Center).Gap(float32(4))
	switch {
	case button.Pressed():
		button = button.Background(presentation.ColorActive)
	case button.Hovered():
		button = button.Background(presentation.ColorHover)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
		presentation.DisclosureArrow(c, turn(open), presentation.ColorTextMuted)
	})
	return button.Clicked()
}

func turn(open bool) float32 {
	if open {
		return 90
	}
	return 0
}

func actionRow(c *ui.Context, label string, run func()) {
	ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
		if button := presentation.DesignButton(c, label, presentation.ButtonSecondary); button.Clicked() && run != nil {
			run()
		}
		ui.Spacer(c)
	})
}

// freshness 显示数据有多旧。失败重试留下的数字用警告色标为旧数据，
// 因为旧数字仍比没有好。
func freshness(c *ui.Context, view panel.View) {
	switch {
	case view.HasSnapshot && view.Stale:
		ui.Text(c, "旧数据 · "+Clock(view.FetchedAt)).
			FontSize(presentation.SizeQuickFine).
			TextColor(presentation.ColorWarning).
			SingleLine()
	case view.HasSnapshot:
		ui.Text(c, Clock(view.FetchedAt)).
			FontSize(presentation.SizeQuickFine).
			TextColor(presentation.PanelFaint).
			SingleLine()
	}
}

func metricRows(c *ui.Context, rows []metricRow) {
	ui.Column(c).FillWidth().Gap(float32(2)).Children(func() {
		for _, row := range rows {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, row.label).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint)
				ui.Spacer(c)
				ui.Text(c, row.value).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelInk).SingleLine()
			})
		}
	})
}

type metricRow struct {
	label string
	value string
}

// formatMetric 显示一个观测值。快照里没有的指标显示为破折号而非 0：
// 0 是一个答案，破折号才表示没有答案。
func formatMetric(snapshot domain.Snapshot, metric domain.Metric) string {
	observation, ok := snapshot.Observation(metric.Key)
	if !ok {
		return "—"
	}
	number, ok := observation.Value.Number()
	if !ok {
		return "—"
	}
	currency, err := snapshot.MetricCurrency(metric)
	if err != nil {
		return "—"
	}
	return kit.FormatMoney(number, currency, metric.Precision)
}

func Clock(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Local().Format("15:04")
}
