package presentation

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// 本文件是 Boku 的设计系统：应用外观唯一的决定处，也是颜色与度量唯一书写的地方。
// 每个窗口都由这里的构件组装，因此快捷窗口、设置窗口与剪贴板历史读起来像同一个应用。
//
// 取值来自设计稿而非框架默认：shadcn 的 slate 边框（#E2E8F0）、stone 页面（#FAFAF9）、
// 品牌绿 #3B693D，三个窗口共用相同的圆角、控件高度与节间距。界面只说“这是什么”，
// 它长什么样由这里决定。
//
// 正确性：一帧内视图会被构建多次，MyGo 还会为每种指针或键盘状态重建一次，因此控件
// 在构建时可以询问自己是否悬停、按下或聚焦。下面这些状态就是这样表达的——背景取自
// 当前帧已知的信息，绝不在帧间保存值。这里不保存任何状态，构建两次没有代价。

// Palette 是设计系统的一个外观：应用绘制的每一种颜色，一起选定。
//
// 有两个，它们是同一套设计而非两套设计。名字在两者中含义相同——colorSurface
// 在任一外观里都是窗口的底面——只有值不同。浅色调色板是设计稿类名拼写的那个；
// 深色是它在深色页面上的版本：同样的 slate/stone 关系、同样的品牌绿，表面反转、
// 墨色提亮直到可读。
//
// 第三种外观就是第三套调色板。视图不知道装的是哪一套；它们请求一个名字，
// 由 design.go 决定这个名字在用户选择的外观里值多少。
type Palette struct {
	Anime, Bilibili     ReadingAccent
	CodexTile, CodexInk ui.Color
	SteamTile, SteamInk ui.Color
	// 表面。
	canvas, surface, sidebar, field, muted, hover, active, rowAlt ui.Color
	border, Hairline                                              ui.Color

	// 文本。
	text, textSoft, textBody, textMuted, textFaint, inputValue ui.Color

	// 品牌与状态。onBrand 是品牌实色上的墨色；onMark 是 provider 图形在其自带
	// 彩色磁贴内所用的白色。
	brand, brandHover, brandPressed, onBrand, onMark ui.Color
	// brandTint 与 brandTintStrong 是选中行所铺的两个强度的品牌色。
	brandTint, brandTintStrong ui.Color

	success, warning, danger                                 ui.Color
	warningFill, warningBorder, warningHover, warningPressed ui.Color

	// 记录副本所用的颜色：磁贴及其墨色。
	skyTile, skyInk, blueTile, blueInk           ui.Color
	violetTile, violetInk, amberTile, amberInk   ui.Color
	emeraldTile, emeraldInk, stoneTile, stoneInk ui.Color

	readingInk, skeleton ui.Color
	// tileEdge 在 provider 自带颜色本会与同深浅表面融在一起时给它描边。
	// 浅色外观里它是无。
	tileEdge ui.Color

	// 翻译卡片自己的调色板。
	sourceInk, particle, Eyebrow, bodyInk, footerInk ui.Color
	BadgeStoneFill, BadgeStoneInk                    ui.Color
	BadgeBlueFill, BadgeBlueInk                      ui.Color
	BadgeVioletFill, BadgeVioletInk                  ui.Color
	BadgeEmeraldFill, BadgeEmeraldInk                ui.Color
	cardFoot, cardLine, chip, chipInk                ui.Color

	// 快捷窗口自己的色阶。
	PanelInk, PanelNumber, PanelFaint, PanelBody ui.Color
	PanelLine, PanelField, PanelBorder           ui.Color
	PanelHover, PanelPress                       ui.Color
	// 游戏时长日历：空白天之上的四档，从短时段到两小时以上。
	playHeat1, playHeat2, playHeat3, playHeat4 ui.Color
}

// LightPalette 是设计稿类名所写的浅色版本：白页面、slate 边框、stone 墨色，
// 品牌绿置顶。
var LightPalette = Palette{
	CodexTile: ui.Hex("#111111"), CodexInk: ui.Hex("#FFFFFF"),
	SteamTile: ui.Hex("#171A21"), SteamInk: ui.Hex("#FFFFFF"),
	Anime: ReadingAccent{
		Ink: ui.Hex("#B74F5B"), Tint: ui.Hex("#FFF5F6"), Border: ui.Hex("#F9DDE0"),
		Hover: ui.Hex("#FFECEF"), HoverBorder: ui.Hex("#F4B8BE"),
		Tile: ui.Hex("#EA9199"), TileInk: ui.Hex("#FFFFFF"), BadgeFill: ui.Hex("#B74F5B"), BadgeInk: ui.Hex("#FFFFFF"),
	},
	Bilibili: ReadingAccent{
		Ink: ui.Hex("#BF315C"), Tint: ui.Hex("#FFF0F3"), Border: ui.Hex("#FCD5E0"),
		Hover: ui.Hex("#FFE8EF"), HoverBorder: ui.Hex("#F4B0C5"),
		Tile: ui.Hex("#FFF0F3"), TileInk: ui.Hex("#BF315C"), BadgeFill: ui.Hex("#C93666"), BadgeInk: ui.Hex("#FFFFFF"),
	},
	canvas:  ui.Hex("#F5F5F5"),
	surface: ui.Hex("#FFFFFF"),
	sidebar: ui.Hex("#FAFAFA"),
	field:   ui.Hex("#FAFAFA"),
	muted:   ui.Hex("#F4F4F5"),
	hover:   ui.Hex("#F4F4F5"),
	active:  ui.Hex("#E4E4E7"),
	rowAlt:  ui.Hex("#F8FAFC"),

	border:   ui.Hex("#E5E5E5"),
	Hairline: ui.Hex("#EAEAEA"),

	text:       ui.Hex("#181818"),
	textSoft:   ui.Hex("#181818"),
	textBody:   ui.Hex("#3F3F46"),
	textMuted:  ui.Hex("#6A6A6A"),
	textFaint:  ui.Hex("#6A6A6A"),
	inputValue: ui.Hex("#181818"),

	brand:           ui.Hex("#3B693D"),
	brandHover:      ui.Hex("#2F5631"),
	brandPressed:    ui.Hex("#294A2B"),
	onBrand:         ui.Hex("#FFFFFF"),
	onMark:          ui.Hex("#FFFFFF"),
	brandTint:       ui.RGBA(59, 105, 61, 0.06),
	brandTintStrong: ui.RGBA(59, 105, 61, 0.10),

	success:        ui.Hex("#059669"),
	warning:        ui.Hex("#B45309"),
	danger:         ui.Hex("#DC2626"),
	warningFill:    ui.Hex("#FFFBEB"),
	warningBorder:  ui.Hex("#FDE68A"),
	warningHover:   ui.Hex("#FEF3C7"),
	warningPressed: ui.Hex("#FDE68A"),

	skyTile:     ui.Hex("#F0F9FF"),
	skyInk:      ui.Hex("#0EA5E9"),
	blueTile:    ui.Hex("#EFF6FF"),
	blueInk:     ui.Hex("#3B82F6"),
	violetTile:  ui.Hex("#F5F3FF"),
	violetInk:   ui.Hex("#8B5CF6"),
	amberTile:   ui.Hex("#FFFBEB"),
	amberInk:    ui.Hex("#D97706"),
	emeraldTile: ui.Hex("#ECFDF5"),
	emeraldInk:  ui.Hex("#047857"),
	stoneTile:   ui.Hex("#F5F5F4"),
	stoneInk:    ui.Hex("#57534E"),

	readingInk: ui.Hex("#78716C"),
	skeleton:   ui.Hex("#F5F5F4"),
	tileEdge:   ui.Transparent,

	sourceInk: ui.Hex("#1C1917"),
	particle:  ui.Hex("#A8A29E"),
	Eyebrow:   ui.Hex("#A8A29E"),
	bodyInk:   ui.Hex("#292524"),
	footerInk: ui.Hex("#A8A29E"),

	BadgeStoneFill:   ui.Hex("#F5F5F4"),
	BadgeStoneInk:    ui.Hex("#78716C"),
	BadgeBlueFill:    ui.Hex("#EFF6FF"),
	BadgeBlueInk:     ui.Hex("#2563EB"),
	BadgeVioletFill:  ui.Hex("#F5F3FF"),
	BadgeVioletInk:   ui.Hex("#7C3AED"),
	BadgeEmeraldFill: ui.Hex("#ECFDF5"),
	BadgeEmeraldInk:  ui.Hex("#3B693D"),

	cardFoot: ui.Hex("#FCFCFC"),
	cardLine: ui.Hex("#F5F5F4"),
	chip:     ui.Hex("#FAFAF9"),
	chipInk:  ui.Hex("#44403C"),

	PanelInk:    ui.Hex("#292524"),
	PanelNumber: ui.Hex("#1C1917"),
	PanelFaint:  ui.Hex("#A8A29E"),
	PanelBody:   ui.Hex("#78716C"),
	PanelLine:   ui.Hex("#F5F5F4"),
	PanelField:  ui.Hex("#FAFAF9"),
	PanelBorder: ui.Hex("#E7E5E4"),
	PanelHover:  ui.Hex("#F5F5F4"),
	PanelPress:  ui.Hex("#E7E5E4"),
	playHeat1:   ui.RGBA(59, 105, 61, 0.22),
	playHeat2:   ui.RGBA(59, 105, 61, 0.45),
	playHeat3:   ui.Hex("#5C8A5E"),
	playHeat4:   ui.Hex("#3B693D"),
}

// DarkPalette 是同一套设计的夜间版本。页面是最深的表面，其它每个面都高出一档
// ——输入框、磁贴、悬停——从而保留浅色设计中“一个表面、在上面画线”的读法：线比
// 页面浅一档，没有刺眼的亮带。
//
// 品牌绿用框架深色强调色已经用的较亮那个（#6FA776），因为 #3B693D 会沉进
// #18181B 的页面；上面的墨色因此转深。类型磁贴取浅色家族 950 端、其墨色取 300 端，
// 使记录副本保留表明种类的颜色又不点亮整列。
var DarkPalette = Palette{
	CodexTile: ui.Hex("#E7E5E4"), CodexInk: ui.Hex("#18181B"),
	SteamTile: ui.Hex("#171A21"), SteamInk: ui.Hex("#66C0F4"),
	Anime: ReadingAccent{
		Ink: ui.Hex("#F0A9B2"), Tint: ui.Hex("#2B2228"), Border: ui.Hex("#49313B"),
		Hover: ui.Hex("#392932"), HoverBorder: ui.Hex("#80505E"),
		Tile: ui.Hex("#49313B"), TileInk: ui.Hex("#F0A9B2"), BadgeFill: ui.Hex("#713444"), BadgeInk: ui.Hex("#FFE4E9"),
	},
	Bilibili: ReadingAccent{
		Ink: ui.Hex("#F694B2"), Tint: ui.Hex("#2C2128"), Border: ui.Hex("#50313F"),
		Hover: ui.Hex("#3B2631"), HoverBorder: ui.Hex("#91516B"),
		Tile: ui.Hex("#50313F"), TileInk: ui.Hex("#F694B2"), BadgeFill: ui.Hex("#81304D"), BadgeInk: ui.Hex("#FFE4EC"),
	},
	canvas:  ui.Hex("#111113"),
	surface: ui.Hex("#18181B"),
	sidebar: ui.Hex("#141416"),
	field:   ui.Hex("#202024"),
	muted:   ui.Hex("#232327"),
	hover:   ui.Hex("#27272A"),
	active:  ui.Hex("#303036"),
	rowAlt:  ui.Hex("#1F1F23"),

	border:   ui.Hex("#2C2C31"),
	Hairline: ui.Hex("#29292E"),

	text:       ui.Hex("#F4F4F5"),
	textSoft:   ui.Hex("#F4F4F5"),
	textBody:   ui.Hex("#D4D4D8"),
	textMuted:  ui.Hex("#A1A1AA"),
	textFaint:  ui.Hex("#A1A1AA"),
	inputValue: ui.Hex("#F4F4F5"),

	brand:           ui.Hex("#6FA776"),
	brandHover:      ui.Hex("#84BA8A"),
	brandPressed:    ui.Hex("#5C8F63"),
	onBrand:         ui.Hex("#10240F"),
	onMark:          ui.Hex("#FFFFFF"),
	brandTint:       ui.RGBA(111, 167, 118, 0.16),
	brandTintStrong: ui.RGBA(111, 167, 118, 0.22),

	success:        ui.Hex("#34D399"),
	warning:        ui.Hex("#FBBF24"),
	danger:         ui.Hex("#F87171"),
	warningFill:    ui.Hex("#2A2113"),
	warningBorder:  ui.Hex("#5C4318"),
	warningHover:   ui.Hex("#332818"),
	warningPressed: ui.Hex("#3D301C"),

	skyTile:     ui.Hex("#082F49"),
	skyInk:      ui.Hex("#38BDF8"),
	blueTile:    ui.Hex("#172554"),
	blueInk:     ui.Hex("#93C5FD"),
	violetTile:  ui.Hex("#2E1065"),
	violetInk:   ui.Hex("#C4B5FD"),
	amberTile:   ui.Hex("#451A03"),
	amberInk:    ui.Hex("#FCD34D"),
	emeraldTile: ui.Hex("#022C22"),
	emeraldInk:  ui.Hex("#6EE7B7"),
	stoneTile:   ui.Hex("#292524"),
	stoneInk:    ui.Hex("#D6D3D1"),

	readingInk: ui.Hex("#A8A29E"),
	skeleton:   ui.Hex("#27272A"),
	tileEdge:   ui.Hex("#3F3F46"),

	sourceInk: ui.Hex("#FAFAF9"),
	particle:  ui.Hex("#78716C"),
	Eyebrow:   ui.Hex("#A8A29E"),
	bodyInk:   ui.Hex("#E7E5E4"),
	footerInk: ui.Hex("#A8A29E"),

	BadgeStoneFill:   ui.Hex("#292524"),
	BadgeStoneInk:    ui.Hex("#D6D3D1"),
	BadgeBlueFill:    ui.Hex("#172554"),
	BadgeBlueInk:     ui.Hex("#93C5FD"),
	BadgeVioletFill:  ui.Hex("#2E1065"),
	BadgeVioletInk:   ui.Hex("#C4B5FD"),
	BadgeEmeraldFill: ui.Hex("#022C22"),
	BadgeEmeraldInk:  ui.Hex("#6FA776"),

	cardFoot: ui.Hex("#1F1F23"),
	cardLine: ui.Hex("#27272A"),
	chip:     ui.Hex("#202024"),
	chipInk:  ui.Hex("#D6D3D1"),

	PanelInk:    ui.Hex("#E7E5E4"),
	PanelNumber: ui.Hex("#FAFAF9"),
	PanelFaint:  ui.Hex("#A8A29E"),
	PanelBody:   ui.Hex("#A8A29E"),
	PanelLine:   ui.Hex("#292524"),
	PanelField:  ui.Hex("#22201E"),
	PanelBorder: ui.Hex("#3A3735"),
	PanelHover:  ui.Hex("#292524"),
	PanelPress:  ui.Hex("#3A3735"),
	playHeat1:   ui.RGBA(111, 167, 118, 0.28),
	playHeat2:   ui.RGBA(111, 167, 118, 0.50),
	playHeat3:   ui.Hex("#5C8F63"),
	playHeat4:   ui.Hex("#84BA8A"),
}

// 各视图请求的颜色，以当前外观的值书写。
//
// 它们是包级值而非参数，因为设计系统是所有窗口共用的一个命名空间，而 MyGo 在主线程
// 上以一次同步流程构建窗口视图：applyDesign 在该流程开头安装外观，下面每个名字都在
// 其后读取。因此一个窗口不会半浅半深，后台 goroutine 也从不读这些值——只有视图读。
var (
	// --- 表面 -----------------------------------------------------------

	// ColorCanvas 是设置窗口等分组页面的中性底面画布，用来衬托单层白色/深色分组面板。
	ColorCanvas = LightPalette.canvas
	// ColorSurface 是窗口与设置面板自身的底面。
	ColorSurface = LightPalette.surface
	// ColorField 是输入框的底面，ColorMuted 是不代表任何具体事物磁贴的底面。
	ColorField = LightPalette.field
	ColorMuted = LightPalette.muted
	// ColorHover 与 ColorActive 是行或按钮在指针下、按下时所取的颜色。
	ColorHover  = LightPalette.hover
	ColorActive = LightPalette.active
	ColorRowAlt = LightPalette.rowAlt
	// ColorSidebar 是设置窗口侧栏的底面，是窗口里唯一不是自身白色的区域：
	// 设计给导航一个独立底面，使旁边的页面读作内容、这一列读作导航。
	ColorSidebar = LightPalette.sidebar

	ColorBorder = LightPalette.border
	// ColorHairline 是列表两项之间的线：比边框更淡，因为列表是一件事而非一排。
	ColorHairline = LightPalette.Hairline

	// --- 文本 ------------------------------------------------------------

	ColorText       = LightPalette.text
	ColorTextSoft   = LightPalette.textSoft
	ColorTextBody   = LightPalette.textBody
	ColorTextMuted  = LightPalette.textMuted
	ColorTextFaint  = LightPalette.textFaint
	ColorInputValue = LightPalette.inputValue

	// --- 品牌与状态 ------------------------------------------------------

	// ColorBrand 是表单唯一动作的绿，以及应用其它需要引起注意之处的绿。
	// 它也是主题的强调色，因此勾选框和选中的分段控件随之而来。
	ColorBrand        = LightPalette.brand
	ColorBrandHover   = LightPalette.brandHover
	ColorBrandPressed = LightPalette.brandPressed
	ColorOnBrand      = LightPalette.onBrand
	// ColorOnMark 是 provider 图形在其磁贴内的白。两个外观里都保持白色：
	// 磁贴是品牌色，上面的图形不是应用绿底上的文字。
	ColorOnMark          = LightPalette.onMark
	ColorBrandTint       = LightPalette.brandTint
	ColorBrandTintStrong = LightPalette.brandTintStrong

	ColorSuccess = LightPalette.success
	ColorWarning = LightPalette.warning
	ColorDanger  = LightPalette.danger

	// 警告面板：页面上唯一一条用户不应错过的提示。
	ColorWarningFill    = LightPalette.warningFill
	ColorWarningBorder  = LightPalette.warningBorder
	ColorWarningHover   = LightPalette.warningHover
	ColorWarningPressed = LightPalette.warningPressed

	// --- 记录副本所用的颜色 ----------------------------------------------

	// 每一对都是：行首磁贴的底面，以及图形和种类徽标所用的颜色。
	ColorSkyTile, ColorSkyInk         = LightPalette.skyTile, LightPalette.skyInk
	ColorBlueTile, ColorBlueInk       = LightPalette.blueTile, LightPalette.blueInk
	ColorVioletTile, ColorVioletInk   = LightPalette.violetTile, LightPalette.violetInk
	ColorAmberTile, ColorAmberInk     = LightPalette.amberTile, LightPalette.amberInk
	ColorEmeraldTile, ColorEmeraldInk = LightPalette.emeraldTile, LightPalette.emeraldInk
	ColorStoneTile, ColorStoneInk     = LightPalette.stoneTile, LightPalette.stoneInk

	// ColorReadingInk 是翻译窗口中词上方读音的墨色：设计的 stone-500。
	ColorReadingInk = LightPalette.readingInk
	// ColorSkeleton 是尚未到达的那行文本的底面。
	ColorSkeleton = LightPalette.skeleton
	// ColorTileEdge 在 provider 磁贴自身颜色会融进页面时给它描边。浅色外观没有。
	ColorTileEdge = LightPalette.tileEdge

	// --- 翻译卡片自己的调色板 --------------------------------------------

	// 设计用 stone 色阶而非应用其它处所用的 slate，因为卡片是页面正文的一部分
	// 而非表单：下面每个颜色都是其类名所指。
	ColorSourceInk = LightPalette.sourceInk // stone-900，原文
	ColorParticle  = LightPalette.particle  // stone-400，词间助词
	ColorEyebrow   = LightPalette.Eyebrow   // stone-400，“原文”标签
	ColorBodyInk   = LightPalette.bodyInk   // stone-800，译文
	ColorFooterInk = LightPalette.footerInk // stone-400，页脚那一行
	ColorLoanword  = LightPalette.brand     // 品牌绿，用于外来词

	// 各语言的徽标：设计赋予它的底面与墨色。
	BadgeStoneFill, BadgeStoneInk     = LightPalette.BadgeStoneFill, LightPalette.BadgeStoneInk     // 日本語
	BadgeBlueFill, BadgeBlueInk       = LightPalette.BadgeBlueFill, LightPalette.BadgeBlueInk       // English
	BadgeVioletFill, BadgeVioletInk   = LightPalette.BadgeVioletFill, LightPalette.BadgeVioletInk   // 한국어
	BadgeEmeraldFill, BadgeEmeraldInk = LightPalette.BadgeEmeraldFill, LightPalette.BadgeEmeraldInk // 中文

	// ColorCardFoot 是设计 stone-50 覆盖在卡片白底上一半强度，即肉眼所见：
	// #FAFAF9 以 50% 覆盖在 #FFFFFF 上。
	ColorCardFoot = LightPalette.cardFoot
	// ColorCardLine 是设计在卡片内画的细线：两块之间 stone-100，四周 stone-200/80。
	ColorCardLine = LightPalette.cardLine
	// 注释 chip：stone-100 描边的 stone-50，词为 stone-700，其上方读音或外来词原词。
	ColorChip    = LightPalette.chip    // stone-50
	ColorChipInk = LightPalette.chipInk // stone-700

	// --- 快捷窗口自己的色阶 ----------------------------------------------
	//
	// 浮窗悬于桌面而非页面之上，设计以 stone 绘制：其框架不是设置窗口用的 slate。
	// 下面每个名字都是设计自己的类，读取它的构件就是设计赋予该值的那个。
	PanelInk    = LightPalette.PanelInk    // stone-800，区块名
	PanelNumber = LightPalette.PanelNumber // stone-900，区块所讲数字
	PanelFaint  = LightPalette.PanelFaint  // stone-400，时间、标签、安静操作
	PanelBody   = LightPalette.PanelBody   // stone-500，剪贴板那一行
	PanelLine   = LightPalette.PanelLine   // stone-100，分隔线与页脚边
	PanelField  = LightPalette.PanelField  // stone-50，剪贴板行的底面
	PanelBorder = LightPalette.PanelBorder // stone-200，其细线
	PanelHover  = LightPalette.PanelHover  // stone-100，安静操作悬停所取
	PanelPress  = LightPalette.PanelPress  // stone-200，按下所取
	PlayHeat1   = LightPalette.playHeat1
	PlayHeat2   = LightPalette.playHeat2
	PlayHeat3   = LightPalette.playHeat3
	PlayHeat4   = LightPalette.playHeat4
)

// ApplyDesign 为即将构建窗口的那一趟安装一个外观。它是上面颜色的唯一写入者。
func ApplyDesign(dark bool) {
	p := LightPalette
	if dark {
		p = DarkPalette
	}
	ColorCanvas = p.canvas
	ColorSurface = p.surface
	AnimeAccent, BilibiliAccent = p.Anime, p.Bilibili
	CodexTile, CodexInk = p.CodexTile, p.CodexInk
	SteamTile, SteamInk = p.SteamTile, p.SteamInk
	ColorSidebar = p.sidebar
	ColorField = p.field
	ColorMuted = p.muted
	ColorHover = p.hover
	ColorActive = p.active
	ColorRowAlt = p.rowAlt
	ColorBorder = p.border
	ColorHairline = p.Hairline

	ColorText = p.text
	ColorTextSoft = p.textSoft
	ColorTextBody = p.textBody
	ColorTextMuted = p.textMuted
	ColorTextFaint = p.textFaint
	ColorInputValue = p.inputValue

	ColorBrand = p.brand
	ColorBrandHover = p.brandHover
	ColorBrandPressed = p.brandPressed
	ColorOnBrand = p.onBrand
	ColorOnMark = p.onMark
	ColorBrandTint = p.brandTint
	ColorBrandTintStrong = p.brandTintStrong
	ColorSuccess = p.success
	ColorWarning = p.warning
	ColorDanger = p.danger
	ColorWarningFill = p.warningFill
	ColorWarningBorder = p.warningBorder
	ColorWarningHover = p.warningHover
	ColorWarningPressed = p.warningPressed

	ColorSkyTile, ColorSkyInk = p.skyTile, p.skyInk
	ColorBlueTile, ColorBlueInk = p.blueTile, p.blueInk
	ColorVioletTile, ColorVioletInk = p.violetTile, p.violetInk
	ColorAmberTile, ColorAmberInk = p.amberTile, p.amberInk
	ColorEmeraldTile, ColorEmeraldInk = p.emeraldTile, p.emeraldInk
	ColorStoneTile, ColorStoneInk = p.stoneTile, p.stoneInk
	ColorReadingInk = p.readingInk
	ColorSkeleton = p.skeleton
	ColorTileEdge = p.tileEdge

	ColorSourceInk = p.sourceInk
	ColorParticle = p.particle
	ColorEyebrow = p.Eyebrow
	ColorBodyInk = p.bodyInk
	ColorFooterInk = p.footerInk
	ColorLoanword = p.brand
	BadgeStoneFill, BadgeStoneInk = p.BadgeStoneFill, p.BadgeStoneInk
	BadgeBlueFill, BadgeBlueInk = p.BadgeBlueFill, p.BadgeBlueInk
	BadgeVioletFill, BadgeVioletInk = p.BadgeVioletFill, p.BadgeVioletInk
	BadgeEmeraldFill, BadgeEmeraldInk = p.BadgeEmeraldFill, p.BadgeEmeraldInk
	ColorCardFoot = p.cardFoot
	ColorCardLine = p.cardLine
	ColorChip = p.chip
	ColorChipInk = p.chipInk

	PanelInk = p.PanelInk
	PanelNumber = p.PanelNumber
	PanelFaint = p.PanelFaint
	PanelBody = p.PanelBody
	PanelLine = p.PanelLine
	PanelField = p.PanelField
	PanelBorder = p.PanelBorder
	PanelHover = p.PanelHover
	PanelPress = p.PanelPress
	PlayHeat1 = p.playHeat1
	PlayHeat2 = p.playHeat2
	PlayHeat3 = p.playHeat3
	PlayHeat4 = p.playHeat4
}

// HeatColor 是游戏时长日历的一档。零是空白日。
func HeatColor(level int) ui.Color {
	switch level {
	case 1:
		return PlayHeat1
	case 2:
		return PlayHeat2
	case 3:
		return PlayHeat3
	default:
		if level >= 4 {
			return PlayHeat4
		}
		return PanelField
	}
}

// 应用的点数度量，遵循 4px 间距栅格与 8–40px 圆角阶梯。
const (
	// PageEdge 是设置与列表窗口的四周留白。
	PageEdge = 24.0
	// SectionGap 是设置页相邻大分区之间的留白（取代多余的节间分割线）。
	SectionGap = 32.0
	// GroupGap 分隔节标题与其主体，以及同一节内相邻分组面板。
	GroupGap = 16.0
	// RowGap 是一组内相邻行之间的空。
	RowGap = 12.0
	// FieldGap 是标签与其所指控件之间的空。
	FieldGap = 8.0

	// 设置面板与设置行内边距（严格 4px 倍数）。
	SettingRowPadX        = 16.0
	SettingRowPadY        = 16.0
	SettingRowPadYCompact = 12.0

	// RadiusPanel 是单层设置分组面板圆角；RadiusCard 是浮窗卡片圆角；
	// RadiusTile 与 RadiusField 均为 8px（位于 8–40px 阶梯内）。
	RadiusPanel = 12.0
	RadiusCard  = 12.0
	RadiusTile  = 8.0
	RadiusField = 8.0

	// HeightControl 是每个标准控件共用的高度：输入框、按钮与分段控件在同一行排成一行。
	HeightControl = 36.0
	// SizeTile 保留给图标底块；通用 UI 图标本体 18px 置入 24×24 透明外框内。
	SizeTile      = 32.0
	IconBoxSize   = 24.0
	IconGlyphSize = 18.0

	// 快捷窗口的区块按设计绘制：两侧 16，上 14 下 12，头部 20 点图形。
	QuickEdge     = 16.0
	QuickTop      = 14.0
	QuickBottom   = 12.0
	QuickTileSize = 20.0
	QuickMarkSize = 12.0
	// 及其字号：13 点名、11 点时间与标签、22 点数字、进度条上方 10 点一行。
	SizeQuickName   = 13.0
	SizeQuickFine   = 11.0
	SizeQuickNumber = 22.0
	SizeQuickTiny   = 10.0

	// API key 与密集设置行度量。
	SizeTileCompact     = 24.0
	SizeTitleCompact    = 18.0
	SizeSubtitleCompact = 12.0
	SizeCardName        = 14.0
	SizeFine            = 12.0
	HeightCardControl   = 32.0
	CardActionPad       = 12.0

	// 排版：标题 18px 加粗、正文与控件标签 14px、次要元信息 12px。
	SizeTitle    = 18.0
	SizeSection  = 14.0
	SizeSubtitle = 12.0
	SizeBody     = 14.0
	SizeSmall    = 12.0
	SizeTiny     = 10.0

	// 翻译卡片自己的色阶，读自设计的类：原文与译文 13px，标签与徽标 10px。
	// 其操作是 14px 图形加 4px 内边距，使 14 点字形成为 22 点目标区。
	SizeCardLabel  = 10.0
	SizeCardSource = 13.0
	SizeCardTarget = 13.0
	SizeCardFoot   = 11.0
	SizeCardMark   = 14.0
	// 注释 chip：词 12px，其上方读音或外来词原词 10px。chip 为 stone-50 配
	// stone-100 边，圆角 8，内边距 10×6。
	SizeChipWord = 12.0
	SizeChipNote = 10.0
	ChipRadius   = 8.0
	ChipPadX     = 10.0
	ChipPadY     = 6.0
	// CardEdge 是设计的 px-3.5 及其区块的纵向空：两侧 14，源块上 12 下 10。
	CardEdge    = 14.0
	CardHeadTop = 12.0
	CardHeadEnd = 10.0
	CardBodyTop = 10.0
	CardBodyEnd = 10.0
	// CardFootTop 与 CardFootEnd 是页脚线上下留的空：设计的 px-3 py-1.5 是 6，
	// 这里提到 10，使那条线不贴在卡片底边上（用户要求那里更宽些）。
	CardFootTop = 10.0
	CardFootEnd = 10.0
	// CardMarkPad 是设计操作图形周围的 p-1：4 点。配合 14 点字形构成设计
	// 自己的 22 点目标区。
	CardMarkPad = 4.0

	// SizeIcon 是卡片携带的操作图形。
	SizeIcon = 20.0
)

// Theme 为 MyGo 自有控件配色：其勾选框、输入框、焦点环、滚动条与分隔线取这些值，
// 使框架绘制的控件与本文件组装的构件读起来是同一套设计。
//
// 它取自与视图相同的调色板，而非框架自己的深色主题：勾选框由 MyGo 绘制，
// 它必须是按钮旁同一表面上的品牌绿。框架调色板只是 Boku 未命名属性
// （滚动条、反色、间距）的起点，不是第二套颜色。
func Theme(dark bool) *ui.Theme {
	p := LightPalette
	base := ui.LightTheme()
	if dark {
		p = DarkPalette
		base = ui.DarkTheme()
	}
	base.Background = p.surface
	base.Surface = p.field
	base.SurfaceHover = p.hover
	base.SurfacePressed = p.active
	base.Border = p.border
	base.Text = p.text
	base.TextMuted = p.textMuted
	base.Accent = p.brand
	base.AccentHover = p.brandHover
	base.AccentPressed = p.brandPressed
	base.AccentText = p.onBrand
	base.Danger = p.danger
	base.Warning = p.warning
	base.Success = p.success
	// 浅色外观的选中与焦点是它一直有的值。
	base.Selection = p.brand.Alpha(0.22)
	base.Focus = p.brand.Alpha(0.55)
	if dark {
		// 深色页面需要稍强的强调色铺底才看得见。
		base.Selection = p.brand.Alpha(0.35)
		base.Focus = p.brand.Alpha(0.60)
	}
	base.Radius = RadiusField
	// 每个窗口文本所用的字体族。它在所有桌面上把 CJK 字体排在拉丁字体之后，
	// 使中文字形无论系统语言是什么都形状一致：没有它，文本系统会按脚本回退，
	// 英文或日文 Windows 选到的字体与中文的不同。
	base.Font = UiFontFamily
	return base
}

// UiFontFamily 是应用文本所用的字体族：设计自己的字体栈，按一贯方式请求系统
// 拉丁字体，然后在放弃之前命名 CJK 字体。MyGo 接受逗号分隔的列表，并用第一个
// 拥有该字符的字体族绘制每个字符，因此下面的顺序也是回退顺序。
//
// 该栈的来源是 docs/ui.md §7；那里改了这里就要改。
const UiFontFamily = "\"Geist\", -apple-system, BlinkMacSystemFont, \"Segoe UI\", " +
	"\"PingFang SC\", \"Hiragino Sans GB\", \"Microsoft YaHei UI\", \"Microsoft YaHei\", " +
	"\"Noto Sans CJK JP\", \"Noto Sans CJK SC\", \"Yu Gothic\", sans-serif"

// useDesign 把正在构建的帧指向 Boku 设计的一个外观：安装下面辅助函数读取的调色板，
// 以及 MyGo 自绘控件所用的匹配主题。
//
// 视图在构建任何东西之前调用它一次：主题在元素构造时就被读取，因此在此调用之前
// 构建的控件会保留框架的颜色。外观由状态持有，因为它是用户在设置窗口里做的选择；
// 视图只负责说“按应用当前的样子绘制”。

// WindowFrame 是每个窗口构建所在的列：内容区从边到边的底面，其内的区域划分它。
func WindowFrame(c *ui.Context) ui.Element {
	return ui.Column(c).Fill().Background(ColorSurface)
}

// Footer 是窗口底部安静的一行：版本号，在窗口自身底面上、一条细线之下。
// 它是窗口的最后一行而非另一个区域，因此不带自己的色带。
//
// 这条细线是窗口自己的线而非两节之间的线：它从边到边，说明的是窗口结束之处。
// 版本号在其内，占据页脚操作所留的空间。
func Footer(c *ui.Context, version string) {
	Hairline(c)
	ui.Row(c).FillWidth().Shrink(0).Font("monospace").
		Background(ColorSidebar).
		Padding(float32(12), float32(16)).
		AlignItems(ui.Center).
		Children(func() { VersionLine(c, version) })
}

// VersionLine 是窗口底部安静的版本号行。
func VersionLine(c *ui.Context, version string) {
	ui.Text(c, version).FontSize(SizeSmall).TextColor(ColorTextMuted).SingleLine()
}

// HeadSize 是节如何介绍自己：图形所在的磁贴、图形在磁贴内留的空、磁贴与旁边名字
// 之间的空，以及两种字号。
type HeadSize struct {
	Tile     float32
	padX     float32
	padY     float32
	gap      float32
	title    float32
	subtitle float32
}

// 设置页绘制的两种标题：设计自己的、除一节外所有节都用的，以及 API key 节用的
// 较小那种——因为设计把它的每张卡片压进两行。
//
// 小标题的图形是 28 而设计的是 32，其后留的空是 14 而那种是 10：较小的图形不能
// 移动名字，因为各节的名字排成页面上一列（由 TestDesignSectionsArePeers 保证）。
var (
	HeadRegular = HeadSize{Tile: IconBoxSize, padX: 3, padY: 3, gap: 8, title: SizeTitle, subtitle: SizeSubtitle}
	HeadSmall   = HeadSize{Tile: IconBoxSize, padX: 3, padY: 3, gap: 8, title: SizeTitleCompact, subtitle: SizeSubtitleCompact}
)

// SectionHead 是设置窗口一节的标题：24×24 透明框内 18px 图标与 18px 加粗标题，
// 不渲染装饰性副标题。head 绑定到标题行以便侧栏滚动跟随。
func SectionHead(c *ui.Context, head *ui.Handle, mark *ui.SVG, Ink ui.Color, title, subtitle string) {
	SectionHeadSized(c, head, mark, Ink, title, subtitle, HeadRegular)
}

// SectionHeadSmall 与 SectionHead 同级，保持全部设置分区的视觉权重一致。
func SectionHeadSmall(c *ui.Context, head *ui.Handle, mark *ui.SVG, Ink ui.Color, title, subtitle string) {
	SectionHeadSized(c, head, mark, Ink, title, subtitle, HeadSmall)
}

// SectionHeadSized 绘制设置分区标题行。subtitle 参数保留签名为兼容既有调用方，按高优先级设计规范不渲染。
func SectionHeadSized(c *ui.Context, head *ui.Handle, mark *ui.SVG, Ink ui.Color, title, subtitle string, size HeadSize) {
	_ = subtitle
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(size.gap)
	if head != nil {
		row.Bind(head)
	}
	row.Children(func() {
		if mark != nil {
			ui.Box(c).Size(IconBoxSize, IconBoxSize).Shrink(0).Center().Children(func() {
				ui.Icon(c, mark).
					Size(IconGlyphSize, IconGlyphSize).
					Shrink(0).
					TextColor(Ink)
			})
		}
		ui.Text(c, title).FontSize(size.title).FontWeight(600).TextColor(ColorText).SingleLine()
	})
}

// SectionLabel 是控件或设置项的标签。
func SectionLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(SizeSection).FontWeight(500).TextColor(ColorText).SingleLine()
}

// InputFrame 给文本输入框以字段的外观：字段自身底面、一条细线、每个控件共用的
// 高度，以及设计赋予“被读取而非在此书写”的值所用的等宽字体。
func InputFrame(c *ui.Context, input ui.Element) ui.Element {
	return input.
		FillWidth().
		Height(HeightControl).
		Padding(0, float32(12)).
		Radius(RadiusField).
		Background(ColorField).
		Border(1, ColorBorder).
		FontSize(SizeBody).
		FixedLineHeight(InputLineHeight(SizeBody)).
		TextColor(ColorInputValue)
}

// InputLineHeight 是 fontSize 下单行输入框的行盒。MyGo 居中空字符串，那一行是
// UI 字体栈的拉丁字体。CJK 占位符来自更靠后的字体、更高，但它从较矮那一行的顶部
// 绘制，因而偏下。此高度适配那些字体，使空行与占位符共用同一个盒子。
func InputLineHeight(fontSize float32) float32 {
	return fontSize * 1.5
}

// FieldFrame 是卡片自身行有空间容纳的尺寸下的 InputFrame：32 点高、文字旁 10 点，
// 把行的其余部分留给作用于该值的操作。
//
// 字段是密集卡片所讲的核心，因此它取剩下的空间而非自己的宽度：由调用方增长它。
func FieldFrame(c *ui.Context, input ui.Element) ui.Element {
	return input.
		Height(HeightCardControl).
		Padding(0, float32(12)).
		Radius(RadiusField).
		Background(ColorField).
		Border(1, ColorBorder).
		FontSize(SizeSmall).
		FixedLineHeight(InputLineHeight(SizeSmall)).
		TextColor(ColorInputValue)
}

// Stacked 是一节内各构件的普通列，使节主体不必在每次调用里重复同样的间距。
func Stacked(c *ui.Context, gap float32, body func()) {
	ui.Column(c).FillWidth().Gap(gap).Children(body)
}

// ButtonTone 是按钮的用途，也就是调用方唯一需要说明的外观信息。
type ButtonTone int

const (
	// ButtonPrimary 是表单唯一的动作：实色填充、品牌绿、上面浅色文字。
	ButtonPrimary ButtonTone = iota
	// ButtonSecondary 是其它每个动作：内容自身底面、一条细线、悬停时上色。
	ButtonSecondary
	// ButtonWarning 是回退性动作：警告底面与边框、警告墨色，而非普通次要外观。
	ButtonWarning
)

// ButtonSize 是按钮占的空间：表单行上每个控件共用的高度、标签旁的空，以及标签字号。
type ButtonSize struct {
	height float32
	pad    float32
	font   float32
}

// 页面绘制的两种尺寸：设计自己的，以及 API key 卡片用的那种——那里一个字段和两个
// 操作共用一条 32 点的行。
var (
	ButtonRegular = ButtonSize{height: HeightControl, pad: 16, font: SizeBody}
	ButtonDense   = ButtonSize{height: HeightCardControl, pad: CardActionPad, font: SizeFine}
)

// DesignButton 以某一种色调构建一个动作。每种色调都保留按钮的完整行为——指针、
// 键盘、焦点环以及辅助技术读取的内容——因为两者底下都是框架的 ButtonBase。
func DesignButton(c *ui.Context, label string, tone ButtonTone) ui.Element {
	return ButtonSized(c, label, tone, ButtonRegular)
}

// CardButton 是密集卡片行有空间容纳尺寸下的 DesignButton：设计的 h-8 加标签旁 10 点。
// 它站在卡片字段旁，那里行上的三样东西必须成一排。
func CardButton(c *ui.Context, label string, tone ButtonTone) ui.Element {
	return ButtonSized(c, label, tone, ButtonDense)
}

// ButtonSized 以两种尺寸之一绘制按钮本身。
func ButtonSized(c *ui.Context, label string, tone ButtonTone, size ButtonSize) ui.Element {
	b := ui.ButtonBase(c).Label(label)
	switch tone {
	case ButtonPrimary:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(RadiusField).
			Background(ColorBrand).
			TextColor(ColorOnBrand).
			FontSize(size.font).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(ColorBrandPressed)
		case b.Hovered() || b.Focused():
			b = b.Background(ColorBrandHover)
		}
	case ButtonSecondary:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(RadiusField).
			Background(ColorSurface).
			Border(1, ColorBorder).
			TextColor(ColorText).
			FontSize(size.font).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(ColorActive)
		case b.Hovered() || b.Focused():
			b = b.Background(ColorHover)
		}
	case ButtonWarning:
		b = b.
			Height(size.height).
			Padding(0, size.pad).
			Radius(RadiusField).
			Background(ColorWarningFill).
			Border(1, ColorWarningBorder).
			TextColor(ColorWarning).
			FontSize(size.font).
			FontWeight(500).
			Justify(ui.Start).
			Cursor(ui.CursorPointer)
		switch {
		case b.Pressed():
			b = b.Background(ColorWarningPressed)
		case b.Hovered() || b.Focused():
			b = b.Background(ColorWarningHover)
		}
	}
	b.Children(func() { ui.Text(c, label).FontSize(size.font).FontWeight(500).SingleLine() })
	return b
}

// TextAction 是写成文字而非画成按钮的动作：卡片头部末尾安静的“删除”。它不带自己的
// 底面，因为其下两个按钮才是卡片的用途，名字旁再多一个会读成卡片的另一个动作；
// 它的墨色说明它做什么。它像这里每个动作一样响应自己范围内的指针。
func TextAction(c *ui.Context, label string, Ink ui.Color) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Radius(RadiusField).
		Padding(float32(4), float32(8)).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(ColorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(ColorHover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(SizeFine).FontWeight(500).TextColor(Ink).SingleLine()
	})
	return b
}

// QuietIconButton 是画成图形的动作：一个字形外加一圈内容自身底色的方形空间，用设计
// 赋予次要动作的淡墨绘制，指针下转为正文墨色。那圈空间使它成为目标区而非一张图；
// 它的标签说明它的含义，供停留的指针与辅助技术读取。
//
// 它是快捷窗口的页脚动作：设计在 14 点图形周围留 p-1，位于该窗口其余部分的 stone 底面上。
func QuietIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(CardMarkPad).
		Padding(CardMarkPad).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(PanelPress)
	case b.Hovered() || b.Focused():
		b = b.Background(PanelHover)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(SizeCardMark), float32(SizeCardMark)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(ColorTextSoft)
		} else {
			icon.TextColor(PanelFaint)
		}
	})
	return b
}

// DeclinesInput 不接受任何输入，是卡片动作所获得的处理函数。
//
// 它们周围的页脚会接收所有落下的指针事件，以充当拖动卡片的把手，而框架把一次按下
// 交给最内层的运行时输入处理器——因此没有它的动作，点击会被所在的整行吞掉，按钮永远
// 按不下去。什么都不做、让事件继续传下去，是框架自己的“护盾”，也是拖拽把手内动作
// 能工作的原因。
func DeclinesInput(ui.InputEvent) bool { return false }

// CardIconButton 是翻译卡片携带的动作：14 点图形加 4 点内边距，使 14 点字形成为
// 22 点目标区——设计的 p-1 与 w-3.5 h-3.5。
//
// 它的两种状态属于设计：指针下取淡 stone 底面且墨色加深。其余情况下字形保持次要
// 动作的淡墨。
func CardIconButton(c *ui.Context, mark *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(CardMarkPad).
		Cursor(ui.CursorPointer).
		HandleInput(DeclinesInput)
	switch {
	case b.Pressed():
		b = b.Background(ColorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(ColorCardLine)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(SizeCardMark), float32(SizeCardMark)).Shrink(0)
		if b.Hovered() || b.Focused() {
			icon.TextColor(ColorTextSoft)
		} else {
			icon.TextColor(ColorEyebrow)
		}
	})
	return b
}

// CardIconToggle 是带开启状态的 CardIconButton：当它代表的事物开启时，图形保持
// 主题的绿，这就是它要表达的全部。按下的图钉看起来与没按下的不同。
//
// 绿色取自主题而非在此命名，因为它本来就是：主题的强调色，设计恰好把它写成与
// 应用其它处的品牌绿相同的 #3B693D。
func CardIconToggle(c *ui.Context, mark *ui.SVG, label string, on bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(float32(4)).
		Padding(CardMarkPad).
		Cursor(ui.CursorPointer).
		HandleInput(DeclinesInput)
	switch {
	case b.Pressed():
		b = b.Background(ColorActive)
	case on:
		b = b.Background(ColorCardLine)
	case b.Hovered() || b.Focused():
		b = b.Background(ColorCardLine)
	}
	b.Children(func() {
		icon := ui.Icon(c, mark).Size(float32(SizeCardMark), float32(SizeCardMark)).Shrink(0)
		if on {
			icon.TextColor(c.Theme().Accent)
		} else if b.Hovered() || b.Focused() {
			icon.TextColor(ColorTextSoft)
		} else {
			icon.TextColor(ColorEyebrow)
		}
	})
	return b
}

// AnnotationChips 是译文下方的注释：每个有内容的词一个 chip，按词序从左到右排列
// 并换行。
//
// 一个 chip 是词及其上方读音——外来词则是其原词，用品牌绿。用 chip 而非整句上方的
// 注音，是因为画出的不是整句：整句在上方，这些是读者读不出的词。底面与圆角是设计的
// stone-50 配 stone-100、圆角 8。
func AnnotationChips(c *ui.Context, words []domain.Segment, showReading, showGloss bool) {
	ui.Row(c).FillWidth().Wrap().Justify(ui.Start).Gap(float32(6)).Children(func() {
		for _, segment := range words {
			if strings.TrimSpace(segment.Text) == "" {
				continue
			}
			note, loanword := AnnotationNote(segment, showReading, showGloss)
			AnnotationChip(c, segment.Text, note, loanword)
		}
	})
}

// AnnotationChip 是其中一个 chip：词，以及其上关于它的内容——stone-500 的读音，
// 或品牌绿的外来词原词。
func AnnotationChip(c *ui.Context, word, note string, loanword bool) {
	ui.Column(c).Shrink(0).AlignItems(ui.Center).
		Background(ColorChip).
		Border(1, ColorCardLine).
		Radius(ChipRadius).
		Padding(ChipPadY, ChipPadX, ChipPadY, ChipPadX).
		Children(func() {
			if note != "" {
				text := ui.Text(c, note).
					FontSize(SizeChipNote).
					TextColor(ColorReadingInk).
					SingleLine()
				if loanword {
					text.TextColor(ColorLoanword).FontWeight(500)
				}
				text.FixedLineHeight(float32(SizeChipNote + 2)).Shrink(0)
				ui.Spacer(c).Height(float32(4)).Shrink(0)
			}
			ui.Text(c, word).
				FontSize(SizeChipWord).
				TextColor(ColorChipInk).
				SingleLine().
				Shrink(0)
		})
}

// AnnotationBlock 是卡片第三块，位于译文下方：打开注释的那一行，以及打开后它持有的 chip。
//
// 它是独立的一块——上方有自己的细线——因为它是独立的事物：用户主动请求、会消耗一次
// 请求、且不属于上方译文的一部分。
//
// 该行贯穿卡片自身宽度、从边到边，使指针下它取的底面覆盖整行而非中间标签。标签与
// 箭头在其中保留卡片 14 点的边距，这正是设计闭合行用 `w-full … px-3.5` 的做法。
func AnnotationBlock(c *ui.Context, open bool, label string, toggle func()) {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(open).
		FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Radius(0).
		Padding(float32(8), float32(CardEdge), float32(8), float32(CardEdge)).
		HandleInput(DeclinesInput)
	switch {
	case button.Pressed():
		button = button.Background(ColorActive)
	case button.Hovered():
		button = button.Background(ColorChip)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(SizeCardFoot).TextColor(ColorReadingInk).SingleLine()
		DisclosureArrow(c, ArrowTurn(open), ColorReadingInk)
	})
	if button.Clicked() && toggle != nil {
		toggle()
	}
}

// ArrowTurn 是注释行箭头的旋转角度：闭合时朝下，打开时朝上，即设计自己的一对。
func ArrowTurn(open bool) float32 {
	if open {
		return -90
	}
	return 90
}

// LangBadge 是命名语言的小标签：翻译窗口里说明正在读什么的那一件。它戴设计为各语言
// 定的一对颜色——日本語 stone、English blue、한국어 violet、中文 emerald——10 点字，
// 纵向 1 点、横向 6 点的空。
func LangBadge(c *ui.Context, name string, code domain.Language) {
	if strings.TrimSpace(name) == "" {
		return
	}
	fill, Ink := LangBadgeColors(code)
	ui.Box(c).Shrink(0).
		Padding(float32(1), float32(6)).
		Radius(float32(4)).
		Background(fill).
		Children(func() {
			ui.Text(c, name).FontSize(SizeCardLabel).FontWeight(500).TextColor(Ink).SingleLine()
		})
}

// LangBadgeColors 是某语言徽标所用的那一对颜色，按设计的写法。
func LangBadgeColors(code domain.Language) (fill, Ink ui.Color) {
	switch code {
	case domain.LangChinese:
		return BadgeEmeraldFill, BadgeEmeraldInk
	case domain.LangEnglish:
		return BadgeBlueFill, BadgeBlueInk
	case domain.LangJapanese:
		return BadgeStoneFill, BadgeStoneInk
	case domain.LangKorean:
		return BadgeVioletFill, BadgeVioletInk
	default:
		return BadgeStoneFill, BadgeStoneInk
	}
}

// Eyebrow 是文本块上方的小标签：“原文”或“译文”。按设计的写法书写——10 点、中等字重、
// 大写并加字距、stone-400——使它命名该块而不与之争夺注意力。
func Eyebrow(c *ui.Context, label string) {
	ui.Text(c, strings.ToUpper(label)).
		FontSize(SizeCardLabel).
		FontWeight(500).
		TextColor(ColorEyebrow).
		SingleLine().
		Shrink(0)
}

// SkeletonBar 是占位行：即将到来的文本的形状，使等待答案的窗口看起来像窗口而非
// 出了错。它以卡片自身宽度度量，由卡片传入。
func SkeletonBar(c *ui.Context, width, fraction float32) {
	ui.Box(c).
		Height(float32(12)).
		Width(width * fraction).
		Radius(float32(4)).
		Background(ColorSkeleton)
}

// Segmented 是在几个短选项中的选择：一条带边框的轨道，选中的分段用品牌绿填充。
//
// 只有外观是本文件的：选择本身、移动它的箭头以及辅助技术读取的内容来自框架的
// SegmentedBase，其 Track 通过 Changed 报告新选择。
func Segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.
		Padding(float32(4)).
		Radius(RadiusPanel).
		Background(ColorMuted).
		Border(1, ColorBorder)
	parts.Track.Children(func() {
		for index := range labels {
			segment := parts.Segment(index).
				Padding(float32(4), float32(12)).
				Radius(RadiusField).
				FontSize(SizeSmall).
				FontWeight(500)
			if index == *selected {
				segment.Background(ColorBrand).TextColor(ColorOnBrand)
			} else {
				segment.TextColor(ColorTextMuted)
				if segment.Hovered() {
					segment.Background(ColorHover)
				}
			}
			segment.Children(func() { ui.Text(c, labels[index]).SingleLine() })
		}
	})
	return parts.Track
}

// NoteKind 是提示在告诉用户什么：出了错、做对了，或没什么特别。
type NoteKind int

const (
	NotePlain NoteKind = iota
	NoteWarning
	NoteDanger
	NoteSuccess
)

// NoteColor 是某类提示的书写颜色，以及代表它的图形的颜色。
func NoteColor(kind NoteKind) ui.Color {
	switch kind {
	case NoteWarning:
		return ColorWarning
	case NoteDanger:
		return ColorDanger
	case NoteSuccess:
		return ColorSuccess
	default:
		return ColorTextMuted
	}
}

// Notice 绘制一行安静的提示：控件下方的注记（“已保存。”），或一节在其开关下方的
// 说明。它是文字而非面板，因为一句话不是家具。
func Notice(c *ui.Context, kind NoteKind, text string) {
	if text == "" {
		return
	}
	ui.Text(c, text).
		FontSize(SizeSmall).
		TextColor(NoteColor(kind)).
		Wrap().
		LineHeight(1.5)
}

// NoticePanel 是页面上唯一一条不可错过的提示：用警告色着色的带边框色条，内含一行小字。
// 它是面板而非又一行正文，设计画得很紧——上下各 6——因为其下的行才是页面的用途。
func NoticePanel(c *ui.Context, text string) {
	ui.Box(c).FillWidth().
		Background(ColorWarningFill).
		Border(1, ColorWarningBorder).
		Radius(RadiusField).
		Padding(float32(6), float32(10)).
		Children(func() {
			ui.Text(c, text).
				FontSize(SizeFine).
				TextColor(ColorWarning).
				Wrap().
				LineHeight(1.4)
		})
}

// CardHint 是卡片携带的最小一行：上面那个值的来源。它比 Notice 更安静，
// 后者说的是刚刚发生了什么。
func CardHint(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(SizeTiny).TextColor(ColorTextMuted).SingleLine()
}

// CardNote 是块自身头部在需要再多一行时所携带的行：账户是什么、窗口何时重置。
// 设计给那一行 11 点淡墨，以及快捷窗口自己的 stone。
func CardNote(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(SizeQuickFine).TextColor(PanelFaint).SingleLine()
}

// BarHeight 是比例条的高度：设计的 h-1.5，是一根线而非一个块。
const BarHeight = 6.0

// ProgressBar 绘制一个比例：给定的整个空间作为轨道，其中已填充的部分，以及它所代表的
// 数字在轨道右端上方。
//
// 填充是直接绘制的而非布局的：它是轨道最终宽度的比例，只有绘制时才知道那个宽度——
// 若用之前一帧的宽度布局填充，会显示上一帧答案的份额。对辅助技术而言，它由名称、
// 范围与值构成。
//
// 颜色是块自己的，填充与数字相同：一条 bar 属于它所在的块，其上方的数字也是同一个
// 颜色——因此是一种颜色而非两种。它画在内容自身底面上，所以颜色必须在那里可读：
// 颜色是磁贴自身白色的 bar 会变成没有数字的 bar。
//
// 它没有的份额——没有剩余、尚未读取——画出空轨道。空轨道不是“0%”：由调用方
// 用文字说明是哪一种。
func ProgressBar(c *ui.Context, percent int64, fill ui.Color, label, name string) {
	ui.Column(c).FillWidth().Gap(float32(4)).AlignItems(ui.End).Children(func() {
		ui.Box(c).FillWidth().
			Height(float32(BarHeight)).
			Radius(999).
			Background(PanelLine).
			Role(ui.RoleMeter).
			Range(0, 100, float64(percent)).
			Label(name).
			Draw(func(p *ui.Painter, r ui.Rect) {
				share := percent
				switch {
				case share <= 0:
					return
				case share > 100:
					share = 100
				}
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(share) / 100, H: r.H}, fill, 999)
			})
	})
}

// PanelMark 是块图形如何绘制：provider 自己的图形、其所在磁贴的底面、图形所用颜色，
// 以及该块内比例条的颜色。
//
// 磁贴的墨色与 bar 的颜色是两回事，且分开保存：磁贴上的图形是白色（磁贴是 provider
// 自身颜色，白色在其上可读），而 bar 画在内容底面上，白色在那里读作什么都看不见。
type PanelMark struct {
	SVG  *ui.SVG
	Tile ui.Color
	Ink  ui.Color
	Bar  ui.Color
	// Scale 在不改变磁贴尺寸的情况下放大细节丰富的产品图形。
	// 零表示使用标准尺寸。
	Scale float32
}

// QuickBlock 是快捷窗口的一个块：设计在其周围留的空，使块读成一件东西，而不是碰巧
// 前后相接的几行。
func QuickBlock(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().
		Padding(QuickTop, QuickEdge, QuickBottom, QuickEdge).
		Children(body)
}

// QuickDivider 是快捷窗口两块之间的线：1 点 stone-100，位于块内同样的边距里。
func QuickDivider(c *ui.Context) {
	ui.Column(c).FillWidth().PaddingX(QuickEdge).Children(func() {
		ui.Row(c).FillWidth().Height(1).Shrink(0).Background(PanelLine)
	})
}

// QuickTile 是块头部的图形：provider 的图形置于自带磁贴内，使块在读到名字前就被认出。
// 没有图形的 provider 的块只得到磁贴，使两个块的名称仍从同一处开始。
func QuickTile(c *ui.Context, mark PanelMark) {
	box := ui.Box(c).Size(QuickTileSize, QuickTileSize).Shrink(0).
		Radius(RadiusTile).
		Background(mark.Tile).
		Center()
	// 磁贴与页面同样深的品牌保留描边，使块仍从一个图形开始，而不是从一个洞旁的名字
	// 开始。浅色外观没有描边可加。
	if ColorTileEdge.A > 0 {
		box.Border(1, ColorTileEdge)
	}
	box.Children(func() {
		if mark.SVG == nil {
			return
		}
		scale := mark.Scale
		if scale <= 0 {
			scale = 1
		}
		size := min(float32(QuickMarkSize)*scale, float32(QuickTileSize)-4)
		ui.Icon(c, mark.SVG).Size(size, size).Shrink(0).TextColor(mark.Ink)
	})
}

// StatusNote 是说明凭据是否已存储的那一行：成功色的圆点，以及同样颜色的已存内容。
func StatusNote(c *ui.Context, text string) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
		ui.Box(c).Size(float32(6), float32(6)).Radius(999).Shrink(0).Background(ColorSuccess)
		ui.Text(c, text).FontSize(SizeSmall).FontWeight(500).TextColor(ColorSuccess).SingleLine()
	})
}

// KindBadge 是列表行携带的小标签：这份拷贝是什么，用其自身图形所用的颜色。
func KindBadge(c *ui.Context, Tile, Ink ui.Color, label string) {
	ui.Box(c).Shrink(0).
		Padding(float32(1), float32(6)).
		Radius(float32(4)).
		Background(Tile).
		Children(func() {
			ui.Text(c, label).FontSize(SizeTiny).FontWeight(500).TextColor(Ink).SingleLine()
		})
}

// KindTile 是列表行首的圆角方块：这份拷贝是什么的图形，置于其种类的着色上。
func KindTile(c *ui.Context, mark *ui.SVG, Tile, Ink ui.Color) {
	ui.Box(c).Size(float32(28), float32(28)).Shrink(0).
		Radius(RadiusTile).
		Background(Tile).
		Center().
		Children(func() {
			ui.Icon(c, mark).Size(float32(14), float32(14)).Shrink(0).TextColor(Ink)
		})
}

// Hairline 是窗口在两个区域之间、以及一节的两部分之间画的线：1 点边框色。
//
// 它留在所画的区域内，如同页面上其它东西：一条跑到窗口边缘的线会越出页面其余部分
// 保持的边距，页面就会有两条边。
func Hairline(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(ColorBorder)
}

// RowDivider 是列表两项之间更淡的线。
func RowDivider(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(ColorHairline)
}
