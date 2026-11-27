// Package assets 提供应用启动所需的图片。
//
// 资源用 go:embed 在编译期嵌入：打包后的应用资源位于平台特定路径，
// 运行时从磁盘读取会在开发环境可用、打包后失败。
//
// 本包不依赖任何内部包，只用标准库解码与缩放。
package assets

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// trayIconPNG 是托盘图标，为 resources/icon.png 的副本。
//
// 需要副本是因为 go:embed 只能嵌入包自身目录下的文件，无法访问上级目录。
// resources/icon.png 是 `mygo build` 打包的应用图标；两者需同步修改。
//
//go:embed tray.png
var trayIconPNG []byte

// deepseekSVG 是 DeepSeek 鲸鱼标志，即同名厂商的图标。它是 SVG，
// 用 ui.Icon 展示：矢量在任意尺寸都清晰，且比位图只增加几 KB 而非几百 KB。
//
//go:embed deepseek.svg
var deepseekSVG []byte

// clipboardSVG 是剪贴板标志，画在快捷窗口的剪贴板行旁。用 SVG 的原因同上：
// 它按文字尺寸展示，需要保持清晰。
//
//go:embed clipboard.svg
var clipboardSVG []byte

// refreshSVG 是快捷窗口刷新动作的标志。用 SVG 是因为取数期间调用方会旋转它，
// 矢量在任意角度都清晰。
//
//go:embed refresh.svg
var refreshSVG []byte

// 快捷窗口底栏的动作：日记、闪念、两个截图工具、进入设置窗口与退出应用。
// 用标志而非文字，因为底栏是卡片底部的一行，文字在此读起来像内容，
// 标志则读起来是动作本身。
var (
	//go:embed diary.svg
	diarySVG []byte
	//go:embed lightbulb.svg
	lightbulbSVG []byte
	//go:embed settings.svg
	settingsSVG []byte
	//go:embed quit.svg
	quitSVG []byte
	//go:embed screenshot.svg
	screenshotSVG []byte
	//go:embed qr.svg
	qrSVG []byte
)

// 剪贴板历史各类型的标志：某条记录在行首图块里画成什么。
// 采用框架自带的绘制语言（24 单位画布、用 currentColor 描边），
// 因此会取所属类型的颜色。
var (
	//go:embed history-text.svg
	historyTextSVG []byte
	//go:embed history-image.svg
	historyImageSVG []byte
	//go:embed history-link.svg
	historyLinkSVG []byte
	//go:embed history-code.svg
	historyCodeSVG []byte
)

// 翻译窗口自身的标志：复制、保持打开与关闭方式。用标志而非文字，
// 因为该窗口只有一句话大小，脚部一排文字会压过它展示的内容。
var (
	//go:embed copy.svg
	copySVG []byte
	//go:embed pin.svg
	pinSVG []byte
	//go:embed close.svg
	closeSVG []byte
	//go:embed translate.svg
	translateSVG []byte
	//go:embed live.svg
	liveSVG []byte
	//go:embed command.svg
	commandSVG []byte
	//go:embed return.svg
	returnSVG []byte
)

// 设置窗口侧栏的标志：进入页面各分区的方式。与全应用一致的 24 单位描边图，
// 会取所在行文字的颜色。
var (
	//go:embed key.svg
	keySVG []byte
	//go:embed info.svg
	infoSVG []byte
	//go:embed layout.svg
	layoutSVG []byte
	//go:embed eye.svg
	eyeSVG []byte
	//go:embed eye-off.svg
	eyeOffSVG []byte
	//go:embed grip.svg
	gripSVG []byte
)

// gaugeSVG 是仓库里尚无自身绘图的 provider 所用块的占位标志：一个仪表，
// 表示「一种度量」而不假装是某个品牌。
//
//go:embed gauge.svg
var gaugeSVG []byte

//go:embed codex.svg
var codexSVG []byte

//go:embed grok.svg
var grokSVG []byte

//go:embed tv.svg
var tvSVG []byte

//go:embed tv-bilibili.svg
var tvBilibiliSVG []byte

//go:embed chevron-right.svg
var chevronRightSVG []byte

//go:embed steam.svg
var steamSVG []byte

// 游戏时长标题的手柄标志。三者都是平面彩色绘制，以图片形式展示以保留色调。
// Switch Pro 是黑色机身、浅色边框与 Super Famicom 按键配色。Xbox（含走
// Xbox 协议的第三方手柄）是黑色机身、绿色 guide 键与经典按键配色。
// 标题按约自身字高、同一中线绘制它们。
var (
	//go:embed dualsense.svg
	dualSenseSVG []byte
	//go:embed switchpro.svg
	switchProSVG []byte
	//go:embed xbox.svg
	xboxSVG []byte
)

// DeepSeek 返回 DeepSeek 标志的 SVG 文档。调用方解析一次并保留结果：
// 解析属于框架，本包只依赖标准库。
func DeepSeek() []byte { return deepseekSVG }

func Copy() []byte { return copySVG }

func Pin() []byte { return pinSVG }

func Close() []byte { return closeSVG }

func Command() []byte { return commandSVG }
func Return() []byte  { return returnSVG }

// Translate 返回翻译分区标题所用标志：应用自绘，而非品牌。
func Translate() []byte { return translateSVG }

func Live() []byte { return liveSVG }

// Clipboard 返回剪贴板标志的 SVG 文档。
func Clipboard() []byte { return clipboardSVG }

func Key() []byte { return keySVG }

// Info 返回应用自身的标志，画在「关于」旁。
func Info() []byte { return infoSVG }

func Layout() []byte { return layoutSVG }
func Eye() []byte    { return eyeSVG }
func EyeOff() []byte { return eyeOffSVG }
func Grip() []byte   { return gripSVG }

// Gauge 返回无自身绘图的 provider 块的占位标志：表示度量而非品牌。
func Gauge() []byte { return gaugeSVG }

// Codex 返回 Codex 产品标志，取调用方当前颜色。
func Codex() []byte { return codexSVG }

// Grok 返回 Grok 标志，取调用方当前颜色。
func Grok() []byte { return grokSVG }

// TV 返回用于定时动画更新块的小电视标志。
func TV() []byte { return tvSVG }

func Steam() []byte { return steamSVG }

// DualSense、SwitchPro 与 Xbox 是画在游戏时长标题旁的手柄标志。
// 不是 DualSense 或 Switch Pro 的一律用 Xbox。DualSense 与 Switch Pro
// 保留自身颜色，Xbox 取调用方颜色。
func DualSense() []byte { return dualSenseSVG }
func SwitchPro() []byte { return switchProSVG }
func Xbox() []byte      { return xboxSVG }

// TVBilibili 返回追更 UP 主块标题所用的小电视标志。与 TV 是不同的绘制，
// 以便同属一个色系的两种块仍能一眼区分。
func TVBilibili() []byte { return tvBilibiliSVG }

// ChevronRight 返回可点击行使用的紧凑展开标志。
func ChevronRight() []byte { return chevronRightSVG }

// Refresh 返回刷新标志的 SVG 文档。
func Refresh() []byte { return refreshSVG }

// Settings 返回设置标志的 SVG 文档。
func Settings() []byte { return settingsSVG }

// Quit 返回退出标志的 SVG 文档。
func Quit() []byte { return quitSVG }

// Diary 返回打开日记的标志。
func Diary() []byte { return diarySVG }

// Lightbulb 返回打开闪念的标志。
func Lightbulb() []byte { return lightbulbSVG }

// Screenshot 返回设置旁使用的区域截图标志。
func Screenshot() []byte { return screenshotSVG }

// QR 返回启动屏幕二维码识别的标志。
func QR() []byte { return qrSVG }

// HistoryText 返回内容为文本的记录所用标志。
func HistoryText() []byte { return historyTextSVG }

// HistoryImage 返回内容为图片的记录所用标志。
func HistoryImage() []byte { return historyImageSVG }

// HistoryLink 返回内容为链接的记录所用标志。
func HistoryLink() []byte { return historyLinkSVG }

// HistoryCode 返回内容读起来像代码的记录所用标志。
func HistoryCode() []byte { return historyCodeSVG }

// Icon 返回缩放到 size×size 像素的应用图标 PNG。
//
// 同一张图用在两处：MyGo 托盘约需 16×16 点（Retina 下 32×32 像素），
// 视图中会更大；嵌入文件比两者都大，以保证在任何位置都清晰。
func Icon(size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("assets: icon size %d is not positive", size)
	}
	src, err := png.Decode(bytes.NewReader(trayIconPNG))
	if err != nil {
		return nil, fmt.Errorf("assets: decode icon: %w", err)
	}
	dst := scale(src, size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("assets: encode icon: %w", err)
	}
	return buf.Bytes(), nil
}

// scale 用盒式滤波把 src 缩放到 size×size 像素：每个目标像素取它所覆盖源像素
// 的平均值，使图标大幅缩小时仍清晰。
func scale(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	b := src.Bounds()
	for y := 0; y < size; y++ {
		y0 := b.Min.Y + y*b.Dy()/size
		y1 := b.Min.Y + (y+1)*b.Dy()/size
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0 := b.Min.X + x*b.Dx()/size
			x1 := b.Min.X + (x+1)*b.Dx()/size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			dst.Set(x, y, average(src, x0, y0, x1, y1))
		}
	}
	return dst
}

// average 返回 [x0,x1)×[y0,y1) 内源像素的均值，按 alpha 加权，
// 以免透明像素把边缘压暗。
func average(src image.Image, x0, y0, x1, y1 int) color.NRGBA {
	var r, g, b, a, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			pr, pg, pb, pa := src.At(x, y).RGBA()
			w := uint64(pa >> 8)
			r += uint64(pr>>8) * w
			g += uint64(pg>>8) * w
			b += uint64(pb>>8) * w
			a += w
			n++
		}
	}
	if n == 0 || a == 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{
		R: uint8(clamp(r/a, 255)),
		G: uint8(clamp(g/a, 255)),
		B: uint8(clamp(b/a, 255)),
		A: uint8(clamp(a/n, 255)),
	}
}

func clamp(v, max uint64) uint64 {
	if v > max {
		return max
	}
	return v
}
