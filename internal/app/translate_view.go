package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// The translation card's view: what was selected, what it became, and the few
// things that can be done with either.
//
// It follows the design's own measurements rather than the settings window's:
// the card is 598 points wide, its two blocks are 14 points from the sides
// with a hairline between them, its labels and badges are 10-point type, and
// its actions are 14-point marks in 6 points of padding. The three line
// weights of the design are all here — the hairline between the two blocks
// (stone-100), the line above the foot (also stone-100), and the card's own
// 1-point outline (stone-200/80).
func (s *State) translateView(c *ui.Context) {
	s.useDesign(c)
	c.Root().Children(func() {
		// The card is a rounded sheet — the design's rounded-xl — and what is
		// inside it is kept inside its shape by the part that touches its
		// edge: the foot rounds its own two lower corners (translateFoot).
		//
		// The card itself is *not* clipped, and that is deliberate: clipping
		// an element takes away the automatic minimum height it would
		// otherwise keep, so the flex layout is free to squeeze the card into
		// whatever window it is in — and a card that is squeezed measures the
		// window instead of its content, which is the one number the window
		// is then sized to.
		//
		// The body scrolls and the foot does not: a translation can be longer
		// than any card, and the foot is where the card is pinned, dragged
		// and closed — it belongs at the bottom of the window whatever the
		// answer's length.
		//
		// The cap is the card's own rather than the window's, which is what
		// makes that work: past MaxHeight the card stops growing and its body
		// scrolls, so what a frame measures is still what the window should
		// be. Capping the window instead would leave the card taller than the
		// window it is drawn in, with its foot below the bottom edge where
		// nobody can reach it.
		card := ui.Column(c).FillWidth().
			MaxHeight(float32(translateMaxHeight)).
			Background(colorSurface).
			Radius(radiusCard).
			Border(1, colorBorder)
		if c.Vibrancy() {
			card.Background(ui.Transparent)
		}
		card.Children(func() {
			// The body takes what the foot leaves: what is in it scrolls
			// once it needs more room than that.
			body := ui.Scroll(c).FillWidth().Grow(1)
			body.Children(func() {
				switch s.translation.status {
				case translateReady:
					s.translateSourceBlock(c)
					cardLine(c)
					s.translateTargetBlock(c)
					// The annotation is its own block, with its own hairline:
					// it is something the user asks for, and it costs a
					// request of its own.
					if s.readingsAvailable() {
						cardLine(c)
						s.translateReadings(c)
					}
				case translateLoading:
					s.translateSourceBlock(c)
					cardLine(c)
					s.translateLoadingBlock(c)
				default:
					s.translateNoticeBlock(c)
				}
			})
			// The line and the foot are the card's own last two rows, not
			// part of what scrolls.
			cardLine(c)
			s.translateFoot(c)
		})
		// How tall what was just built is, which is what the window is then
		// sized to: a short answer gets a short card.
		s.measureCard(c, card)
	})
}

// cardLine is the hairline the design draws inside the card: stone-100,
// inside the card's own edge rather than across its width.
func cardLine(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(1).Shrink(0).Background(colorCardLine)
}

// translateSourceBlock is the design's first block: the label row at the top
// — 原文, the language's badge, and the actions that belong to the source —
// and the text under it, annotated word by word when the translator could say
// how each one is read.
func (s *State) translateSourceBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(cardHeadTop), float32(cardEdge), float32(cardHeadEnd), float32(cardEdge)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
				eyebrow(c, "原文")
				if s.translation.status == translateLoading {
					langBadge(c, "检测中…", domain.LangUnknown)
				} else if s.translation.result.SourceLang != domain.LangUnknown {
					langBadge(c, langName(s.translation.result.SourceLang), s.translation.result.SourceLang)
				}
				ui.Spacer(c)
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
					if s.translation.status != translateReady || s.translation.source == "" {
						return
					}
					if s.copiedLabel("source") {
						notice(c, noteSuccess, "已复制")
						return
					}
					if button := cardIconButton(c, s.copyMark, "复制原文"); button.Clicked() {
						s.copyTranslation("source")
					}
				})
			})
			ui.Spacer(c).Height(float32(8)).Shrink(0)
			s.translateSourceText(c)
		})
}

// translateSourceText is the source itself.
//
// It is the text — the pieces the translator annotated, joined back into what
// was sent — and never the annotated words: the readings live in the card's
// own row under the translation (translateReadings), which the user opens when
// they want them. A source that is always wearing a second line of small type
// is harder to read than the sentence it is meant to help with.
func (s *State) translateSourceText(c *ui.Context) {
	if s.translation.source == "" {
		ui.Text(c, "…").FontSize(float32(sizeCardSource)).TextColor(colorEyebrow)
		return
	}
	ui.Text(c, s.translation.source).
		FontSize(float32(sizeCardSource)).
		FontWeight(500).
		TextColor(colorSourceInk).
		Wrap().
		LineHeight(1.3)
}

// translateTargetBlock is the design's second block: the label row — 译文,
// the language it went into, and the way to copy it — and the translation
// under it.
func (s *State) translateTargetBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(cardBodyTop), float32(cardEdge), float32(cardBodyEnd), float32(cardEdge)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
				eyebrow(c, "译文")
				langBadge(c, langName(s.translation.result.TargetLang), s.translation.result.TargetLang)
				ui.Spacer(c)
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
					if s.copiedLabel("target") {
						notice(c, noteSuccess, "已复制")
						return
					}
					if button := cardIconButton(c, s.copyMark, "复制译文"); button.Clicked() {
						s.copyTranslation("target")
					}
				})
			})
			ui.Spacer(c).Height(float32(6)).Shrink(0)
			if note := s.translation.result.Note; note != "" {
				notice(c, notePlain, note)
			}
			ui.Text(c, s.translation.result.TranslatedText).
				FontSize(float32(sizeCardTarget)).
				TextColor(colorBodyInk).
				Wrap().
				LineHeight(1.6)
		})
}

// translateReadings is the card's third block, under the translation: the row
// that opens the annotation, and — once it is open — the words that carry
// something to say.
//
// It is here rather than over the source because that is where a reader looks
// for it: the card's job is the translation, the source is there to be
// recognised, and the words a sentence is made of are what someone asks for
// afterwards. It is asked of the translator only when it is opened, which is
// why opening it shows a request in flight.
//
// What it shows is the words, not the sentence: a reading over a piece of kana
// says nothing, and the sentence is already above it.
func (s *State) translateReadings(c *ui.Context) {
	if !s.readingsAvailable() {
		return
	}
	// The row is the card's own width; what it opens is inset in the usual
	// block padding, like the other two blocks' content.
	annotationBlock(c, s.translation.readings, s.readingsLabel(), s.toggleReadings)
	if !s.translation.readings {
		return
	}
	ui.Column(c).FillWidth().
		Padding(float32(cardBodyTop), float32(cardEdge), float32(cardBodyEnd), float32(cardEdge)).
		Children(func() {
			switch s.translation.annotation.status {
			case annotationLoading:
				ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Shrink(0).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "正在取注音…").
						FontSize(float32(sizeCardFoot)).
						TextColor(colorEyebrow).
						SingleLine()
				})
			case annotationFailed:
				notice(c, notePlain, s.translation.annotation.failure)
			case annotationReady:
				words := s.annotatedWords()
				if len(words) == 0 {
					ui.Text(c, "这句没有需要注音的词。").
						FontSize(float32(sizeCardFoot)).
						TextColor(colorEyebrow)
					return
				}
				showReading, showGloss := true, true
				if settings, err := s.translateSettings(); err == nil {
					showReading, showGloss = settings.Furigana, settings.LoanwordGloss
				}
				annotationChips(c, words, showReading, showGloss)
			}
		})
}

// readingsLabel is what the row that opens the annotation says: what is behind
// it, and — once the answer is in — how much of it there is. The count is
// there so that the row is worth reading on its own, closed: four words behind
// a door is an invitation, and a door with nothing said about it is not.
func (s *State) readingsLabel() string {
	label := "注音"
	if s.translation.annotation.status == annotationReady {
		if count := len(s.annotatedWords()); count > 0 {
			return label + " · " + strconv.Itoa(count) + " 个词"
		}
	}
	return label
}

// translateLoadingBlock is what the card shows while the translator is being
// asked: a spinner, a word, and the shape of the paragraph that is coming.
func (s *State) translateLoadingBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(cardBodyTop), float32(cardEdge), float32(cardBodyEnd), float32(cardEdge)).
		Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(float32(10)).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在翻译…").FontSize(float32(sizeCardTarget)).TextColor(colorEyebrow).SingleLine()
			})
			ui.Spacer(c).Height(float32(12)).Shrink(0)
			inner := float32(translateWidth - cardEdge*2)
			ui.Column(c).FillWidth().Gap(float32(8)).Children(func() {
				skeletonBar(c, inner, 0.92)
				skeletonBar(c, inner, 0.70)
			})
		})
}

// translateNoticeBlock is what the card shows when there is nothing to
// translate: one block that says what happened, in the same measurements as
// the blocks of a real translation.
func (s *State) translateNoticeBlock(c *ui.Context) {
	ui.Column(c).FillWidth().
		Padding(float32(cardBodyTop), float32(cardEdge), float32(cardBodyEnd), float32(cardEdge)).
		Children(func() {
			ui.Column(c).FillWidth().Gap(float32(4)).Children(func() {
				switch s.translation.status {
				case translateOff:
					notice(c, notePlain, "划词翻译已关闭，快捷键不再响应。")
					notice(c, notePlain, "在设置的「划词翻译」一节里打开它。")
				case translateUnconfigured:
					// The service the settings name is not one this build has,
					// or it needs a key that is not there. Either way the fix
					// is in the settings, and the card says which service.
					notice(c, noteWarning, s.translation.failure)
					notice(c, notePlain, "在设置的「划词翻译」里重新选一个翻译服务。")
				case translateNoText:
					notice(c, noteWarning, "剪贴板里没有可翻译的文本。")
					notice(c, notePlain, "选中文本并复制，再按一次快捷键。")
				case translateFailed:
					notice(c, noteDanger, s.translation.failure)
					notice(c, notePlain, "选中文本并复制，再按一次快捷键重试。")
				default:
					notice(c, notePlain, "选中文本并复制，再按快捷键。")
				}
			})
			if s.translation.source != "" {
				ui.Spacer(c).Height(float32(8)).Shrink(0)
				ui.Text(c, s.translation.source).
					FontSize(float32(sizeCardSource)).
					FontWeight(500).
					TextColor(colorSourceInk).
					Wrap().
					LineHeight(1.3)
			}
		})
}

// translateFoot is the card's last line: where the translation came from on
// the left, and what can be done with the card on the right. It is the part
// the pointer drags the card by, and the design gives it the faint stone
// face, 10 points above and below and 12 at the sides.
//
// Its two lower corners are round, because it is the row that reaches the
// card's bottom edge: a square row there paints over the card's own rounded
// corners and leaves the sheet looking round on top and square underneath.
// The radius is the card's less its 1-point outline, which is where the
// inside of that outline is.
//
// The room above and below is 10 rather than the design's 6: the user found
// the line sitting too close to the card's bottom edge, and a footer that
// carries the card's actions reads better with the same room on both sides
// (which is what the settings window's own footer uses).
//
// The line takes every pointer event that lands on it, which is what makes it
// a handle; the actions beside it are their own elements and keep their own
// clicks.
func (s *State) translateFoot(c *ui.Context) {
	corner := float32(radiusCard - 1)
	foot := ui.Row(c).FillWidth().Shrink(0).
		Background(colorCardFoot).
		Radius(0, 0, corner, corner).
		Padding(float32(cardFootTop), float32(12), float32(cardFootEnd), float32(12)).
		AlignItems(ui.Center).
		Gap(float32(2)).
		HandleInput(s.dragCard)
	foot.Children(func() {
		ui.Text(c, s.translationOrigin()).
			FontSize(float32(sizeCardFoot)).
			TextColor(colorFooterInk).
			SingleLine()
		ui.Spacer(c)
		if button := cardIconToggle(c, s.pinMark, pinLabel(s.translation.pinned), s.translation.pinned); button.Clicked() {
			s.toggleTranslationPin()
		}
		if button := cardIconButton(c, s.closeMark, "关闭"); button.Clicked() {
			s.closeTranslation()
		}
	})
}

// translationOrigin is what the foot says about where the translation came
// from: the service, and nothing more when there is none to name.
//
// The free tier is one entry in the settings and several endpoints behind it,
// so what it answers with is which engine actually served this translation —
// "免费（微软）" — and not the id of the tier. A footer that said only "free"
// would leave the user unable to tell where their text went, which §12 of the
// product design does not allow.
func (s *State) translationOrigin() string {
	switch s.translation.status {
	case translateReady:
		if name := strings.TrimSpace(s.translation.result.ProviderName); name != "" {
			return "AI 翻译 · " + name
		}
		if id := strings.TrimSpace(s.translation.result.ProviderID); id != "" {
			return "AI 翻译 · " + id
		}
		return "AI 翻译"
	case translateLoading:
		return "AI 翻译 · 正在请求"
	default:
		return "划词翻译"
	}
}

// pinLabel is what the pin action is called, which is also what assistive
// technology reads: a mark that only says "pin" leaves the state unsaid.
func pinLabel(pinned bool) string {
	if pinned {
		return "取消固定"
	}
	return "固定浮窗"
}

// langName is what the card calls a language.
func langName(code domain.Language) string {
	if name := code.Name(); name != "" {
		return name
	}
	return "未知"
}
