package presentation

import "github.com/egoist/mygo/ui"

// SettingsPanel 是单层设置分组面板（借鉴 cosscom/ui FramePanel p-0 + Separator）：
// 承载在 ColorCanvas 上，内部只放扁平 SettingsRow 与 SettingsDivider，严禁嵌套卡片。
func SettingsPanel(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().
		Background(ColorSurface).
		Border(1, ColorBorder).
		Radius(RadiusPanel).
		Children(body)
}

// SettingsGroup 在单层面板上方附加可选的安静分组名，用于区分同一分区内的逻辑子组。
func SettingsGroup(c *ui.Context, title string, body func()) {
	if title == "" {
		SettingsPanel(c, body)
		return
	}
	ui.Column(c).FillWidth().Gap(FieldGap).Children(func() {
		ui.Row(c).FillWidth().Padding(0, float32(4)).Children(func() {
			ui.Text(c, title).
				FontSize(SizeSmall).
				FontWeight(500).
				TextColor(ColorTextMuted).
				SingleLine()
		})
		SettingsPanel(c, body)
	})
}

// SettingsRow 是设置面板内的一项，统一提供 16×16 留白与 8px 内部垂直间距。
func SettingsRow(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().
		Padding(SettingRowPadY, SettingRowPadX).
		Gap(FieldGap).
		Children(body)
}

// SettingsRowCompact 用于列表项、键值行与单选选项等重复条目，纵向收紧至 12px（仍遵循 4px 栅格）。
func SettingsRowCompact(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().
		Padding(SettingRowPadYCompact, SettingRowPadX).
		Gap(FieldGap).
		Children(body)
}

// SettingsDivider 是设置面板内相邻设置项之间的通栏 1px 细线。
func SettingsDivider(c *ui.Context) {
	ui.Row(c).FillWidth().Height(1).Shrink(0).Background(ColorHairline)
}

// SettingsSwitchRow 绘制标准布尔设置项：左为名称（及必要的约束/状态行），右为 Switch。
func SettingsSwitchRow(c *ui.Context, title, note string, on *bool, onToggle func(bool)) {
	SettingsRow(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(GroupGap).Children(func() {
			settingRowTitle(c, title, note)
			sw := ui.Switch(c, on).Label(title).Shrink(0)
			if onToggle != nil {
				sw.OnClick(func() { onToggle(*on) })
			}
		})
	})
}

// SettingsControlRow 绘制标准控件设置项：左为名称（及必要的约束/状态行），右为对齐控件。
func SettingsControlRow(c *ui.Context, title, note string, control func()) {
	SettingsRow(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(GroupGap).Children(func() {
			settingRowTitle(c, title, note)
			if control != nil {
				control()
			}
		})
	})
}

// SettingsKVRow 绘制只读键值行，常用于关于信息或同步状态摘要。
func SettingsKVRow(c *ui.Context, label, value string, mono bool) {
	SettingsRowCompact(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(GroupGap).Children(func() {
			ui.Text(c, label).
				FontSize(SizeBody).
				FontWeight(500).
				TextColor(ColorTextMuted).
				SingleLine().
				Shrink(0)
			ui.Spacer(c)
			text := ui.Text(c, value).
				FontSize(SizeSmall).
				TextColor(ColorText).
				SingleLine()
			if mono {
				text.Font("monospace")
			}
		})
	})
}

// PanelRowRadius 返回单层 SettingsPanel 内按首尾位置贴合圆角的四边半径，
// 防止悬停或选中铺底溢出面板圆角。
func PanelRowRadius(first, last bool) (tl, tr, br, bl float32) {
	inner := float32(RadiusPanel - 1)
	if first {
		tl, tr = inner, inner
	}
	if last {
		br, bl = inner, inner
	}
	return tl, tr, br, bl
}

func settingRowTitle(c *ui.Context, title, note string) {
	ui.Column(c).Grow(1).Gap(float32(4)).Children(func() {
		ui.Text(c, title).
			FontSize(SizeBody).
			FontWeight(500).
			TextColor(ColorText).
			Wrap()
		if note != "" {
			ui.Text(c, note).
				FontSize(SizeSmall).
				TextColor(ColorTextMuted).
				Wrap().
				LineHeight(1.5)
		}
	})
}
