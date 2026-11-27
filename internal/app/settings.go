package app

import (
	"boku/internal/presentation"

	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"

	preferencesui "boku/internal/preferences/ui"
)

// 设置窗口只在首次被请求时创建，之后会被真正关闭：
// 托盘应用在无人配置时应不占资源。
const (
	// 页面宽度固定为 560，旁边一列文字不得挤窄下方的表单。
	settingsSidebarWidth = 132
	settingsPageWidth    = 560
	settingsWidth        = settingsPageWidth + settingsSidebarWidth
	// 页面在目标分区上方保留的空间，让用户看出分区从这里开始，而非贴着窗口边缘。
	settingsAnchor = 16
	// 页面期望的高度：五个分区加页脚；内容更多时滚动，而不是把窗口撑出屏幕。
	settingsHeight = 660
)

type settingsSectionKind int

const (
	sectionAPIKey settingsSectionKind = iota
	sectionTranslate
	sectionClipboard
	sectionDiary
	sectionFlash
	sectionAnime
	sectionBilibili
	sectionQuickLayout
	sectionGeneral
	sectionAbout
	sectionLive
)

// settingsPage 是一个设置分区及其侧栏入口。
// 二者是同一样东西而非两份手工对齐的列表：在此加一个分区，侧栏与页面就会同时出现，
// 入口因此不可能指向不存在的分区。
type settingsPage struct {
	kind  settingsSectionKind
	name  string
	mark  *ui.SVG
	build func(c *ui.Context, head *ui.Handle)
}

func (s *State) settingsPages() []*settingsPage {
	pages := []*settingsPage{
		{kind: sectionAPIKey, name: "API Key", mark: s.keyMark, build: s.credentialUI.ApiKeySection},
		{kind: sectionTranslate, name: "划词翻译", mark: s.translationMark, build: s.translateSection},
	}
	if s.liveUI != nil {
		pages = append(pages, &settingsPage{kind: sectionLive, name: "实时翻译", mark: s.liveMark, build: s.liveSection})
	}
	pages = append(pages, &settingsPage{kind: sectionClipboard, name: "剪贴板", mark: s.clipboardMark, build: s.clipboardSection})
	if s.diaryUI != nil {
		pages = append(pages, &settingsPage{kind: sectionDiary, name: "日记", mark: s.diaryMark, build: s.diarySection})
	}
	if s.flashUI != nil {
		pages = append(pages, &settingsPage{kind: sectionFlash, name: "闪念", mark: s.flashMark, build: s.flashSection})
	}
	return append(pages,
		&settingsPage{kind: sectionAnime, name: "动画", mark: s.tvMark, build: s.animeSection},
		&settingsPage{kind: sectionBilibili, name: "B站", mark: s.bilibiliMark, build: s.bilibiliSection},
		&settingsPage{kind: sectionQuickLayout, name: "面板布局", mark: s.layoutMark, build: s.layoutUI.SettingsView},
		&settingsPage{kind: sectionGeneral, name: "通用", mark: s.settingsMark, build: s.generalSection},
		&settingsPage{kind: sectionAbout, name: "关于", mark: s.infoMark, build: s.aboutSection},
	)
}

func (s *State) bilibiliSection(c *ui.Context, head *ui.Handle) {
	s.bilibiliUI.SettingsView(c, head, s.readingDesign(s.bilibiliMark))
}

func (s *State) animeSection(c *ui.Context, head *ui.Handle) {
	s.animeUI.SettingsView(c, head, s.readingDesign(s.tvMark))
}

// openSettings 显示设置窗口，首次调用时创建它。
func (s *State) openSettings() {
	s.syncActivationPolicy()
	s.RefreshSelectionAccess()
	if s.settingsWindow != nil {
		s.settingsWindow.Show()
		s.settingsWindow.Focus()
		return
	}
	s.credentialUI.LoadCredentials()
	if s.diaryUI != nil {
		s.diaryUI.ReloadSyncSettings()
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     appName + " 设置",
		Width:     settingsWidth,
		Height:    settingsHeight,
		MinWidth:  settingsWidth,
		MinHeight: 420,
		Content:   ui.View(s.settingsView),
	})
	s.settingsWindow = window
	s.syncActivationPolicy()
	window.OnFocus(func() { s.RefreshSelectionAccess() })
	window.OnClosed(func() {
		if s.settingsWindow == window {
			s.settingsWindow = nil
			s.syncActivationPolicy()
		}
	})
}

// settingsView 构建设置窗口：命名各分区的侧栏、承载这些分区的页面，以及底部的版本号。
//
// 窗口不绘制自己的标题栏：系统标题栏已经命名了它，窗口内再重复一次就是第二个标题。
func (s *State) settingsView(c *ui.Context) {
	s.useDesign(c)
	presentation.WindowFrame(c).Children(func() {
		ui.Row(c).FillWidth().Grow(1).AlignItems(ui.Stretch).Children(func() {
			s.settingsSidebar(c)
			ui.Column(c).Fill().Grow(1).Children(func() {
				// 页面占满列剩余空间并在其中滚动：尺寸由内容决定的滚动容器没有可滚动的余地。
				body := ui.Scroll(c).FillWidth().Grow(1).TrackScroll(&s.settingsScroll)
				body.Children(func() {
					presentation.Content(c, false, func() {
						s.settingsPageContent(c, body)
					})
				})
			})
		})
		presentation.Footer(c, s.versionLine())
	})
	s.updateCheckDialogView(c)
}

func (s *State) settingsPageContent(c *ui.Context, body ui.Element) {
	pages := s.settingsPages()
	sections := make([]settingsSectionBox, 0, len(pages))
	for index, page := range pages {
		if index > 0 {
			presentation.BetweenSections(c)
		}
		// 每个分区是一个盒子，算术便有盒子自身的顶边可用：MyGo 给元素的是前一帧布局出的
		// 盒子，也就是点击能作用到的第一个盒子。它的标题是内部自成一个盒子：侧栏跟随的是
		// 标题所在，而非分区起点。
		//
		// 保存的是 handle 而非 element：element 属于构建它的帧，handle 才能让一块视图在
		// 下一帧被重新找到。
		section := ui.Box(c).FillWidth()
		record := settingsSectionBox{kind: page.kind}
		section.Bind(&record.box)
		build := page.build
		section.Children(func() { build(c, &record.head) })
		sections = append(sections, record)
	}
	s.settingsScrollTo(c, sections, body)
}

// settingsSectionBox 是侧栏读取的一个分区：它是什么，以及它所在盒子与标题所在盒子的
// handle。用 handle 而非 element：element 属于构建它的帧，而侧栏每一帧都要重新读取。
type settingsSectionBox struct {
	kind settingsSectionKind
	box  ui.Handle
	head ui.Handle
}

// settingsScrollTo 把页面送向侧栏请求的分区，并在页面到达前保持该入口为打开状态。
//
// 页面是滑动而非跳转：偏移量作为 ScrollTo 交给滚动容器，由 MyGo 逐帧滑过去。
func (s *State) settingsScrollTo(c *ui.Context, sections []settingsSectionBox, body ui.Element) {
	s.settingsRoom = body.Bounds().H
	s.settingsSection = s.sectionInView(c, sections, body)
	// 处理侧栏自身的请求：分区盒子是它在当前页面位置绘制的位置，把页面向下移动对应距离即把
	// 该分区带到锚点处。MyGo 给元素的是前一帧的盒子，故在点击后的那一帧执行，而非点击当帧。
	if pending := s.settingsReveal; pending != nil {
		// 用户点击的入口在页面移动期间始终是打开的那个：页面在途位置不能说明它要去哪，
		// 点亮途经的分区会被用户看成闪烁。
		s.settingsSection = *pending
		s.settingsAim(c, *pending, sections)
		if !s.settingsScroll.Scrolling() {
			// 页面给定的滚动停止时即已到达。入口交给随后那一帧的滚动决定——那一帧的
			// 盒子已按页面当前所在布局——使入口与分区永不失配。
			s.settingsReveal, s.settingsAiming, s.settingsAimAt = nil, false, 0
		}
	}
}

// settingsAim 把页面指向某分区，或指向紧接着它被点击的分区：第一个仍在移动时再次点击，
// 会从页面当前位置重新定向滚动，因此最后点击的入口胜出。
//
// 分区盒子随其所在页面移动，故目标每确定一次就固定下来，不再每帧重算：每帧重定向会追一个
// 不断逃离滚动的目标。页面无空间把某分区带到锚点（页尾分区永远到不了顶部）时，页面停在末尾，
// 这由容器自己完成。
//
// 盒子是前一帧布局出的偏移处所绘制的位置，而目标是偏移量：两者与页面当前位置相加。
func (s *State) settingsAim(c *ui.Context, kind settingsSectionKind, sections []settingsSectionBox) {
	if s.settingsAiming && s.settingsAimAt == kind {
		return
	}
	// kind 不是页面索引。实时翻译插在划词翻译之后，闪念又是可选的，所以枚举顺序与侧栏
	// 绘制的列表不是同一个列表。用序号会把实时翻译滚到恰好处于该编号的分区。
	index, ok := settingsSectionIndex(kind, sections)
	if !ok {
		return
	}
	box := c.Resolve(sections[index].box)
	if box.Bounds().H <= 0 {
		return
	}
	s.settingsAiming, s.settingsAimAt = true, kind
	s.settingsScroll.ScrollTo(0, s.settingsScroll.Y+box.Bounds().Y-settingsAnchor, ui.Smooth)
}

func settingsSectionIndex(kind settingsSectionKind, sections []settingsSectionBox) (int, bool) {
	for index, section := range sections {
		if section.kind == kind {
			return index, true
		}
	}
	return 0, false
}

// sectionInView 是侧栏标记为打开的分区：标题越过页面中线的最后一个分区。标题进入页面上半部
// 即视为读者在看该分区，故入口在下一个标题越过中线时才切换，而非等它到顶。
//
// 比较在屏幕上进行：盒子是元素绘制的位置，页面中线是页面滚动区域的中间。
//
// 页尾有一处例外：末尾分区永远带不到中线（下方无内容可滚），页面停在末尾时打开它能显示出
// 标题的最后一个分区；没有这条，侧栏底部的入口永远无法被点亮。
func (s *State) sectionInView(c *ui.Context, sections []settingsSectionBox, body ui.Element) settingsSectionKind {
	active := settingsSectionKind(0)
	if len(sections) == 0 {
		return active
	}
	viewport := body.Bounds()
	// 新窗口尚无提交的布局。在有视口可比较标题之前保持当前选择；此处的零滚动范围并不
	// 表示读者到了页面末尾。
	if viewport.H <= 0 {
		return s.settingsSection
	}
	middle := viewport.Y + viewport.H/2
	atEnd := s.settingsScroll.Y >= s.settingsScroll.MaxY-1
	last := len(sections) - 1
	for _, section := range sections {
		// 前一帧未布局的分区没有盒子可读，不能从零得出结论：它不是位于页顶的标题，
		// 而是尚未绘制的标题。
		head := c.Resolve(section.head)
		if head.Bounds().H <= 0 {
			continue
		}
		if head.Bounds().Y > middle {
			break
		}
		active = section.kind
	}
	if atEnd && active != sections[last].kind {
		head := c.Resolve(sections[last].head).Bounds()
		if head.H > 0 && head.Y <= viewport.Y+viewport.H {
			active = sections[last].kind
		}
	}
	return active
}

func (s *State) settingsSidebar(c *ui.Context) {
	active := s.settingsSection
	ui.Column(c).
		Width(float32(settingsSidebarWidth)).
		Shrink(0).
		Background(presentation.ColorSidebar).
		// 侧栏的边界只在朝向页面的那一侧：那是两块区域相接的地方。
		BorderWidth(0, 0, 0, 1).
		BorderColor(presentation.ColorBorder).
		Padding(float32(12), float32(8)).
		Gap(float32(2)).
		Children(func() {
			for _, page := range s.settingsPages() {
				if s.settingsNav(c, page, page.kind == active) {
					s.openSettingsSection(page.kind)
				}
			}
		})
}

// settingsNav 是侧栏的一个入口：分区的图标记与名称，页面显示该分区时以品牌绿填充。
//
// 整行都是点击目标而非其中文字：一行指向一个位置，就应在行内任意处响应。
func (s *State) settingsNav(c *ui.Context, page *settingsPage, open bool) bool {
	row := ui.ButtonBase(c).Label(page.name).
		FillWidth().
		Focusable().
		Role(ui.RoleTab).
		Selected(open).
		Cursor(ui.CursorPointer).
		Padding(float32(6), float32(8)).
		Radius(presentation.RadiusField).
		Justify(ui.Start).
		AlignItems(ui.Center).
		Gap(float32(8))
	ink := presentation.ColorTextBody
	switch {
	case open:
		row.Background(presentation.ColorBrand)
		ink = presentation.ColorOnBrand
	case row.Hovered() || row.Focused():
		row.Background(presentation.ColorHover)
	}
	if !open && row.Pressed() {
		row.Background(presentation.ColorActive)
	}
	icon := presentation.ColorTextMuted
	if open {
		icon = presentation.ColorOnBrand
	}
	row.Children(func() {
		if page.mark != nil {
			ui.Icon(c, page.mark).Size(float32(16), float32(16)).Shrink(0).TextColor(icon)
		}
		ui.Text(c, page.name).
			FontSize(presentation.SizeSmall).
			FontWeight(500).
			TextColor(ink).
			SingleLine()
	})
	return row.Clicked()
}

// openSettingsSection 响应侧栏点击：页面被带到该分区，入口在这一帧即标记为打开，
// 而不是等滚动到达处。
//
// 两半都重要：只读滚动会在页面移动时标记尚未离开的入口；而点击必须把分区留在身后，
// 因为移动在下一帧才计算，那时目标分区的盒子才存在。
func (s *State) openSettingsSection(kind settingsSectionKind) {
	pages := s.settingsPages()
	for index, page := range pages {
		if page.kind != kind {
			continue
		}
		s.settingsSection = kind
		s.settingsReveal = &pages[index].kind
		break
	}
	// 点击已在当前帧响应，页面在下一帧移动：帧是把请求带给滚动容器的载体。
	s.Refresh()
}

// clipboardSection 是剪贴板工具：两个开关、记录范围与通往历史的入口。
//
// 两个开关默认关闭，分区也说明了原因：剪贴板是 Boku 能读取的最敏感的东西。关闭监控是停止
// 读取而非隐藏，表单也这么写，因为「关了却仍在读」就是撒谎。
func (s *State) clipboardSection(c *ui.Context, head *ui.Handle) {
	s.clipboardUI.SettingsView(c, head, s.clipboardDesign())
}

// generalSection 是应用自身行为：启动时做什么、数据在哪、平台为用户启动什么。
//
// 最后一项是「开机自动启动」开关，它是关于机器的事实而非 Boku 存储的偏好：分区每帧向平台
// 询问（autostart.go），用户在别处禁用的启动会在此显示为关闭。只有在平台能注册启动项时才
// 绘制该开关，失败信息保留在下方可见。
func (s *State) generalSection(c *ui.Context, head *ui.Handle) {
	s.preferencesUI.GeneralView(c, head, preferencesui.GeneralProps{
		Mark: s.settingsMark, Appearance: s.appearance.Mode(), SetAppearance: s.setAppearance,
		AppearanceStatus: s.appearanceStatusLine(), Panels: s.panelUI.SettingsView, Status: s.panelStatus,
		DataPath: s.dataPath(), RevealDataPath: s.revealDataPath,
	})
}

// aboutSection 是应用本身：版本、构建环境、文件位置，以及检查是否有更新的按钮。
func (s *State) aboutSection(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHead(c, head, s.infoMark, presentation.ColorStoneInk, "关于", "版本与构建")

			ui.Column(c).FillWidth().
				Background(presentation.ColorField).
				Border(1, presentation.ColorBorder).
				Radius(presentation.RadiusField).
				Padding(float32(12)).
				Children(func() {
					presentation.Stacked(c, float32(6), func() {
						aboutLine(c, "版本", s.versionLine())
						aboutLine(c, "平台", runtime.GOOS+" / "+runtime.GOARCH)
						aboutLine(c, "构建", runtime.Version())
						aboutLine(c, "凭据", s.credentialUI.CredentialPath())
					})
				})

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if button := presentation.DesignButton(c, "复制版本信息", presentation.ButtonSecondary); button.Clicked() {
					s.copyVersionInfo()
				}
				s.updateCheckButton(c)
				ui.Spacer(c)
			})
		})
	})
}

func aboutLine(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine().Shrink(0)
		ui.Spacer(c)
		ui.Text(c, value).
			FontSize(presentation.SizeSmall).
			TextColor(presentation.ColorText).
			Font("monospace").
			SingleLine()
	})
}

// dataPath 是应用存放文件的位置，取不到时返回一行说明。
func (s *State) dataPath() string {
	path, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || strings.TrimSpace(path) == "" {
		return "本机用户目录"
	}
	return path
}

func (s *State) revealDataPath() {
	path, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || strings.TrimSpace(path) == "" {
		return
	}
	mygo.Shell.OpenPath(path)
}

// copyVersionInfo 把关于框的内容复制到剪贴板，因为提交 bug 需要它，用户不应重打一遍。
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
		ContentHash: kit.ClipContentHash(info),
		CopiedAt:    time.Now().UTC(),
	}
	s.clipboard.CopyAsync(entry, nil)
}
