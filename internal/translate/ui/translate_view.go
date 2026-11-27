package ui

import (
	"boku/internal/presentation"
	"log/slog"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// translateView 绘制翻译卡片：原文、译文，以及两者的操作。
func (s *State) translateView(c *ui.Context) {
	s.useDesign(c)
	c.Root().Children(func() {
		// 卡片不裁剪：裁剪会去掉元素的自动最小高度，flex 就会把卡片挤进窗口，
		// 于是量到的是窗口而不是内容，而窗口正是按这个内容高度定尺寸的。
		//
		// 高度上限放在卡片自身而不是窗口：超过 MaxHeight 后卡片停止增长、正文滚动，
		// 这样一帧量到的仍是窗口应有的高度。若给窗口设上限，卡片会比窗口还高，
		// 底部的脚跑到下边缘之外无法触达。
		//
		// 正文滚动而脚不滚动：翻译可能比任何卡片都长，而脚是固定、拖拽、关闭卡片的位置，
		// 无论答案多长都应在窗口底部。
		card := ui.Column(c).FillWidth().
			MaxHeight(float32(translateMaxHeight)).
			Background(presentation.ColorSurface).
			Radius(presentation.RadiusCard).
			Border(1, presentation.ColorBorder)
		if c.Vibrancy() {
			card.Background(ui.Transparent)
		}
		card.Children(func() {
			body := ui.Scroll(c).FillWidth().Grow(1)
			body.Children(func() {
				// 仅在已有图片时显示预览：截图前的失败（缺屏幕录制权限）没有图片，
				// 空预览框配截图徽章会误导用户。
				if s.translation.preview != nil {
					s.imageTranslationPreview(c)
				}
				switch s.translation.status {
				case translateReady:
					s.translateSourceBlock(c)
					cardLine(c)
					s.translateTargetBlock(c)
					// 注音是独立区块，有独立分隔线：它由用户主动请求，并且单独花费一次请求。
					if s.readingsAvailable() {
						cardLine(c)
						s.translateReadings(c)
					}
				case translateQRReady:
					s.translateQRBlock(c)
				case translateLoading:
					s.translateSourceBlock(c)
					cardLine(c)
					s.translateLoadingBlock(c)
				default:
					s.translateNoticeBlock(c)
				}
			})
			cardLine(c)
			s.translateFoot(c)
		})
		// 量出刚构建内容的高度，窗口据此定尺寸。
		s.measureCard(c, card)
	})
}

func (s *State) imageTranslationPreview(c *ui.Context) {
	preview := ui.Box(c).FillWidth().Height(float32(160)).Shrink(0).Radius(presentation.RadiusField).Background(presentation.ColorCardFoot)
	preview.Children(func() {
		if s.translation.preview != nil {
			ui.Image(c, s.translation.preview).Absolute().Fill().Fit(ui.Contain).Radius(presentation.RadiusField)
		}
		ui.Row(c).Absolute().Top(float32(8)).Left(float32(8)).Shrink(0).
			Padding(float32(2), float32(6)).Radius(float32(4)).AlignItems(ui.Center).Gap(float32(4)).
			Background(ui.RGBA(0, 0, 0, 0.50)).Children(func() {
			ui.Icon(c, s.screenshotMark).Size(float32(10), float32(10)).TextColor(ui.RGBA(255, 255, 255, 0.92)).Shrink(0)
			ui.Text(c, "截图").FontSize(presentation.SizeTiny).TextColor(ui.RGBA(255, 255, 255, 0.92)).SingleLine()
		})
	})
}

func cardLine(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(presentation.ColorCardLine)
}

func (s *State) translateSourceBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(presentation.CardHeadTop), float32(presentation.CardEdge), float32(presentation.CardHeadEnd), float32(presentation.CardEdge)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
				presentation.Eyebrow(c, "原文")
				if s.translation.status == translateLoading {
					presentation.LangBadge(c, "检测中…", domain.LangUnknown)
				} else if s.translation.result.SourceLang != domain.LangUnknown {
					presentation.LangBadge(c, langName(s.translation.result.SourceLang), s.translation.result.SourceLang)
				}
				ui.Spacer(c)
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
					if s.translation.status != translateReady || s.translation.source == "" {
						return
					}
					if s.copiedLabel("source") {
						presentation.Notice(c, presentation.NoteSuccess, "已复制")
						return
					}
					if button := presentation.CardIconButton(c, s.copyMark, "复制原文"); button.Clicked() {
						s.copyTranslation("source")
					}
				})
			})
			ui.Spacer(c).Height(float32(8)).Shrink(0)
			s.translateSourceText(c)
		})
}

// translateSourceText 只显示原文文本（把标注的分片拼回原句），不显示注音词：
// 注音放在译文下方独立的注音行里，由用户按需展开，常驻小字会更难读。
func (s *State) translateSourceText(c *ui.Context) {
	if s.translation.source == "" {
		ui.Text(c, "…").FontSize(float32(presentation.SizeCardSource)).TextColor(presentation.ColorEyebrow)
		return
	}
	ui.Text(c, s.translation.source).
		FontSize(float32(presentation.SizeCardSource)).
		FontWeight(500).
		TextColor(presentation.ColorSourceInk).
		Wrap().
		LineHeight(1.3)
}

func (s *State) translateTargetBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(presentation.CardBodyTop), float32(presentation.CardEdge), float32(presentation.CardBodyEnd), float32(presentation.CardEdge)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
				presentation.Eyebrow(c, "译文")
				presentation.LangBadge(c, langName(s.translation.result.TargetLang), s.translation.result.TargetLang)
				ui.Spacer(c)
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
					if s.copiedLabel("target") {
						presentation.Notice(c, presentation.NoteSuccess, "已复制")
						return
					}
					if button := presentation.CardIconButton(c, s.copyMark, "复制译文"); button.Clicked() {
						s.copyTranslation("target")
					}
				})
			})
			ui.Spacer(c).Height(float32(6)).Shrink(0)
			if note := s.translation.result.Note; note != "" {
				presentation.Notice(c, presentation.NotePlain, note)
			}
			ui.Text(c, s.translation.result.TranslatedText).
				FontSize(float32(presentation.SizeCardTarget)).
				TextColor(presentation.ColorBodyInk).
				Wrap().
				LineHeight(1.6)
		})
}

// translateQRBlock 是二维码卡片的全部内容：二维码识别出的文本本身，不是译文，因此只有一个区块。
func (s *State) translateQRBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(presentation.CardHeadTop), float32(presentation.CardEdge), float32(presentation.CardBodyEnd), float32(presentation.CardEdge)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
				presentation.Eyebrow(c, "识别结果")
				ui.Spacer(c)
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
					if s.copiedLabel("source") {
						presentation.Notice(c, presentation.NoteSuccess, "已复制")
						return
					}
					if button := presentation.CardIconButton(c, s.copyMark, "复制识别结果"); button.Clicked() {
						s.copyTranslation("source")
					}
				})
			})
			ui.Spacer(c).Height(float32(8)).Shrink(0)
			ui.Text(c, s.translation.source).
				FontSize(float32(presentation.SizeCardTarget)).
				TextColor(presentation.ColorBodyInk).
				Wrap().
				LineHeight(1.6)
		})
}

// translateReadings 是译文下方的注音区块：打开注音的行，以及打开后显示的有内容的词。
// 只有打开时才向翻译器请求，所以打开时会显示进行中的请求。
func (s *State) translateReadings(c *ui.Context) {
	if !s.readingsAvailable() {
		return
	}
	presentation.AnnotationBlock(c, s.translation.readings, s.readingsLabel(), s.toggleReadings)
	if !s.translation.readings {
		return
	}
	ui.Column(c).FillWidth().
		Padding(float32(presentation.CardBodyTop), float32(presentation.CardEdge), float32(presentation.CardBodyEnd), float32(presentation.CardEdge)).
		Children(func() {
			switch s.translation.annotation.status {
			case annotationLoading:
				ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Shrink(0).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "正在取注音…").
						FontSize(float32(presentation.SizeCardFoot)).
						TextColor(presentation.ColorEyebrow).
						SingleLine()
				})
			case annotationFailed:
				presentation.Notice(c, presentation.NotePlain, s.translation.annotation.failure)
			case annotationReady:
				words := s.annotatedWords()
				if len(words) == 0 {
					ui.Text(c, "这句没有需要注音的词。").
						FontSize(float32(presentation.SizeCardFoot)).
						TextColor(presentation.ColorEyebrow)
					return
				}
				showReading, showGloss := true, true
				if settings, err := s.translateSettings(); err == nil {
					showReading, showGloss = settings.Furigana, settings.LoanwordGloss
				}
				presentation.AnnotationChips(c, words, showReading, showGloss)
			}
		})
}

// readingsLabel 是打开注音那一行的文案：答案返回后附上词数，让这一行关着时也有信息量。
func (s *State) readingsLabel() string {
	label := "注音"
	if s.translation.annotation.status == annotationReady {
		if count := len(s.annotatedWords()); count > 0 {
			return label + " · " + strconv.Itoa(count) + " 个词"
		}
	}
	return label
}

func (s *State) translateLoadingBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(presentation.CardBodyTop), float32(presentation.CardEdge), float32(presentation.CardBodyEnd), float32(presentation.CardEdge)).
		Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(float32(10)).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在翻译…").FontSize(float32(presentation.SizeCardTarget)).TextColor(presentation.ColorEyebrow).SingleLine()
			})
			ui.Spacer(c).Height(float32(12)).Shrink(0)
			inner := float32(translateWidth - presentation.CardEdge*2)
			ui.Column(c).FillWidth().Gap(float32(8)).Children(func() {
				presentation.SkeletonBar(c, inner, 0.92)
				presentation.SkeletonBar(c, inner, 0.70)
			})
		})
}

func (s *State) translateNoticeBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(presentation.CardBodyTop), float32(presentation.CardEdge), float32(presentation.CardBodyEnd), float32(presentation.CardEdge)).
		Children(func() {
			ui.Column(c).FillWidth().Gap(float32(4)).Children(func() {
				switch s.translation.status {
				case translateOff:
					presentation.Notice(c, presentation.NotePlain, "划词翻译已关闭，快捷键不再响应。")
					presentation.Notice(c, presentation.NotePlain, "在设置的「划词翻译」一节里打开它。")
				case translateUnconfigured:
					// 设置指定的服务本版本没有，或缺少所需的 key；两种情况都在设置里修。
					presentation.Notice(c, presentation.NoteWarning, s.translation.failure)
					presentation.Notice(c, presentation.NotePlain, "在设置的「划词翻译」里重新选一个翻译服务。")
				case translateNoText:
					presentation.Notice(c, presentation.NoteWarning, "剪贴板里没有可翻译的文本。")
					presentation.Notice(c, presentation.NotePlain, "选中文本并复制，再按一次快捷键。")
				case translateNeedsScreenRecording:
					presentation.Notice(c, presentation.NoteWarning, s.translation.failure)
					presentation.Notice(c, presentation.NotePlain, "在「系统设置 > 隐私与安全性 > 屏幕与系统音频录制」里允许 Boku（没有就用 + 添加），再重试；系统要求时重启 Boku。")
					ui.Spacer(c).Height(float32(4)).Shrink(0)
					ui.Row(c).Children(func() {
						if presentation.CardButton(c, "打开系统设置", presentation.ButtonSecondary).Clicked() {
							s.openScreenRecordingSettings()
						}
					})
				case translateFailed:
					presentation.Notice(c, presentation.NoteDanger, s.translation.failure)
					switch {
					case s.translation.qr:
						presentation.Notice(c, presentation.NotePlain, "让二维码完整出现在选区里，再按二维码按钮试一次。")
					case s.translation.image:
						presentation.Notice(c, presentation.NotePlain, "按截图快捷键重新选取区域。")
					default:
						presentation.Notice(c, presentation.NotePlain, "选中文本并复制，再按一次快捷键重试。")
					}
				default:
					presentation.Notice(c, presentation.NotePlain, "选中文本并复制，再按快捷键。")
				}
			})
			if s.translation.source != "" {
				ui.Spacer(c).Height(float32(8)).Shrink(0)
				ui.Text(c, s.translation.source).
					FontSize(float32(presentation.SizeCardSource)).
					FontWeight(500).
					TextColor(presentation.ColorSourceInk).
					Wrap().
					LineHeight(1.3)
			}
		})
}

// translateFoot 是卡片最后一行：左侧来源、右侧操作，也是拖动卡片的把手。
//
// 下方两个角要圆角：这一行贴着卡片下边缘，方角会盖掉卡片自身的圆角，
// 让卡片上圆下方。圆角半径为卡片半径减 1 点描边。
//
// 整行接收落在其上的指针事件以充当把手；旁边的操作是独立元素，保留各自的点击。
func (s *State) translateFoot(c *ui.Context) {
	corner := float32(presentation.RadiusCard - 1)
	foot := ui.Row(c).FillWidth().Shrink(0).
		Background(presentation.ColorCardFoot).
		Radius(0, 0, corner, corner).
		Padding(float32(presentation.CardFootTop), float32(12), float32(presentation.CardFootEnd), float32(12)).
		AlignItems(ui.Center).
		Gap(float32(2)).
		HandleInput(s.dragCard)
	foot.Children(func() {
		ui.Text(c, s.translationOrigin()).
			FontSize(float32(presentation.SizeCardFoot)).
			TextColor(presentation.ColorFooterInk).
			SingleLine()
		ui.Spacer(c)
		if s.translation.imageTaskID != 0 {
			ui.Text(c, "已加入后台任务，可关闭浮窗").
				FontSize(float32(presentation.SizeTiny)).
				TextColor(presentation.ColorFooterInk).
				SingleLine().
				MinWidth(0)
		}
		if button := presentation.CardIconToggle(c, s.pinMark, pinLabel(s.translation.pinned), s.translation.pinned); button.Clicked() {
			s.toggleTranslationPin()
		}
		if button := presentation.CardIconButton(c, s.closeMark, "关闭"); button.Clicked() {
			slog.Info("translation card closed", "event", "translation_card_closed", "image", s.translation.image)
			s.closeTranslation()
		}
	})
}

// translationOrigin 是脚部显示的翻译来源。
// 免费档在设置里是一个条目、背后有多个端点，所以要显示实际服务的引擎（如「免费（微软）」）而非档位 id，
// 否则用户无法知道文本去了哪里（产品设计 §12 要求）。
func (s *State) translationOrigin() string {
	switch s.translation.status {
	case translateQRReady:
		return "二维码识别"
	case translateReady:
		if s.translation.image {
			return "AI 图片翻译 · " + s.translation.result.ProviderName
		}
		if name := strings.TrimSpace(s.translation.result.ProviderName); name != "" {
			return "AI 翻译 · " + name
		}
		if id := strings.TrimSpace(s.translation.result.ProviderID); id != "" {
			return "AI 翻译 · " + id
		}
		return "AI 翻译"
	case translateLoading:
		if s.translation.image {
			return "AI 图片翻译 · 正在请求"
		}
		return "AI 翻译 · 正在请求"
	default:
		if s.translation.qr {
			return "二维码识别"
		}
		if s.translation.image {
			return "截图翻译"
		}
		return "划词翻译"
	}
}

// pinLabel 是固定操作的名称，也是辅助技术朗读的内容：只说「固定」会漏掉当前状态。
func pinLabel(pinned bool) string {
	if pinned {
		return "取消固定"
	}
	return "固定浮窗"
}

func langName(code domain.Language) string {
	if name := code.Name(); name != "" {
		return name
	}
	return "未知"
}
