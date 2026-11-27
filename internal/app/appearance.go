package app

import (
	"github.com/egoist/mygo"

	"boku/internal/preferences"
	preferencesui "boku/internal/preferences/ui"
)

// 本文件是用户选择的外观：浅色、深色或跟随桌面。
//
// 它是表现层拥有的唯一设置。选择写在 settings.json 中功能键旁边，外观本身在 design.go 中：
// 本文件决定 design.go 安装哪套配色，并告知桌面，使 Boku 的窗口（标题栏、popover 材质）
// 与其中的页面外观一致。
//
// 该设置和功能设置一样是一个 Section；存储对它一无所知。"system" 是默认值，
// 也是缺失或不可读值的含义：从未选择外观的用户得到其电脑当前所在的外观。

type AppearanceMode = preferences.AppearanceMode
type Appearance = preferences.Appearance
type AppearanceStore = preferences.Store

const (
	AppearanceSystem = preferences.AppearanceSystem
	AppearanceLight  = preferences.AppearanceLight
	AppearanceDark   = preferences.AppearanceDark
)

const appearanceThemeKey = preferences.ThemeKey

// NewAppearanceSection 是兼容构造器，调用方正在迁移到 preferences.NewAppearanceSection。
func NewAppearanceSection() *Appearance { return preferences.NewAppearanceSection() }

// ThemeSource 是交给桌面的外观。它让标题栏和菜单与页面一致：
// Boku 绘制自己的颜色，但包围它们的窗口是系统的。
func ThemeSource(mode AppearanceMode) mygo.ThemeSource {
	switch mode.Normalize() {
	case AppearanceLight:
		return mygo.ThemeLight
	case AppearanceDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

// PopoverAppearance 给已创建的原生 popover 提供强制深色模式所需的深色外观。
// 这是表现层知道平台桥接存在的唯一地方；main 提供实现，nil 表示平台没有，
// 此时强制深色使用应用卡片。
//
// 它刻意只有一个方法：当 MyGo 暴露 popover 自身的外观时，main 替换（或移除）实现即可，
// 视图和状态机都不变。
type PopoverAppearance interface {
	SetDark(window uintptr) bool
}

// SyncAppearance 记录桌面的状态并重绘所有已打开的窗口。main 在应用就绪时调用一次，
// 并从 mygo.Theme.OnUpdated 再次调用：自动模式下，日落时变深的桌面会让 Boku 随之变深。
func (s *State) SyncAppearance(systemDark bool) {
	s.systemDark = systemDark
	s.dark = s.appearance.Dark(systemDark)
	s.syncPopoverAppearance()
	s.Refresh()
}

// syncPopoverAppearance 让已打开快捷窗口的载体与当前生效的外观一致。
//
// 应用卡片总随配色重绘，无需处理。原生 popover 才是脆弱的一环：自动跟随桌面，
// 强制深色需要注入深色材质，强制浅色则根本不能用 popover（材质会跟随深色桌面），
// 需关闭它，由下次打开绘制卡片。已被注入深色材质的 popover 无法就地回到桌面外观，
// 因此切回自动时也关闭它。
func (s *State) syncPopoverAppearance() {
	if s.panel == nil || !s.panelPopover {
		return
	}
	if !s.quickWindowUsesPopover() {
		s.panel.Close()
		return
	}
	switch s.appearance.Mode() {
	case AppearanceSystem:
		if s.panelInjectedDark {
			s.panel.Close()
		}
	case AppearanceDark:
		if s.popoverAppearance == nil {
			return
		}
		if s.popoverAppearance.SetDark(s.panel.NativeHandle()) {
			s.panelInjectedDark = true
			return
		}
		// popover 的原生窗口可能在 flyout 创建后稍晚才被接收。再多请求一帧并在此重试；
		// 已关闭或被替换的窗口由身份检查放过。
		panel := s.panel
		panel.Update(func() {
			if s.panel != panel || !s.panelPopover {
				return
			}
			s.panelInjectedDark = s.popoverAppearance.SetDark(panel.NativeHandle())
		})
	}
}

// setAppearance 保存用户的选择并立即显示在屏幕上。
//
// 它走存储的 Update 而非逐步修改文件，使主题变更不会把另一个功能在此期间改动的设置写回。
// 写入失败仍会在本次运行内改变外观并在控件下方说明：用户要求如此，
// 忽略点击的窗口是更糟的答复。
func (s *State) setAppearance(mode AppearanceMode) {
	mode = mode.Normalize()
	if s.preferencesUI == nil {
		s.preferencesUI = preferencesui.New(nil, s.featureHost())
	}
	s.preferencesUI.SaveAppearance(s.appearance, s.appearanceStore, mode)
	// 固定模式在此已知；自动模式是桌面的答复，装配在被告知新来源后会重新读取（SyncAppearance）。
	// 这里是给无事可告知的状态（例如测试）的答复。
	s.dark = s.appearance.Dark(s.systemDark)
	s.syncPopoverAppearance()
	if s.applyAppearance != nil {
		s.applyAppearance(mode)
	}
	s.Refresh()
}

func (s *State) appearanceStatusLine() string {
	if s.preferencesUI == nil {
		return ""
	}
	return s.preferencesUI.AppearanceStatus()
}
