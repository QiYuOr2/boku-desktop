package ui

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"boku/internal/diary"
	"boku/internal/presentation"
)

type diaryPalette struct {
	paper      ui.Color
	raised     ui.Color
	sidebar    ui.Color
	rule       ui.Color
	ink        ui.Color
	secondary  ui.Color
	faint      ui.Color
	chipFill   ui.Color
	hover      ui.Color
	selectedBg ui.Color
	accent     ui.Color
	onAccent   ui.Color
}

func paletteFor(dark bool) diaryPalette {
	if dark {
		return diaryPalette{
			paper:      ui.Hex("#141416"),
			raised:     ui.Hex("#1C1C1F"),
			sidebar:    ui.Hex("#18181B"),
			rule:       ui.Hex("#2C2C30"),
			ink:        ui.Hex("#EDEDEE"),
			secondary:  ui.Hex("#A1A1AA"),
			faint:      ui.Hex("#71717A"),
			chipFill:   ui.Hex("#242428"),
			hover:      ui.Hex("#242428"),
			selectedBg: presentation.ColorBrandTintStrong,
			accent:     presentation.ColorBrand,
			onAccent:   presentation.ColorOnBrand,
		}
	}
	return diaryPalette{
		paper:      ui.Hex("#FAF9F7"),
		raised:     ui.Hex("#FFFFFF"),
		sidebar:    ui.Hex("#F5F4F1"),
		rule:       ui.Hex("#E7E5E4"),
		ink:        ui.Hex("#292524"),
		secondary:  ui.Hex("#78716C"),
		faint:      ui.Hex("#A8A29E"),
		chipFill:   ui.Hex("#F5F5F4"),
		hover:      ui.Hex("#F0EFEA"),
		selectedBg: presentation.ColorBrandTintStrong,
		accent:     presentation.ColorBrand,
		onAccent:   presentation.ColorOnBrand,
	}
}

func isDarkTheme(c *ui.Context) bool {
	bg := c.Theme().Background
	luma := 0.299*float32(bg.R) + 0.587*float32(bg.G) + 0.114*float32(bg.B)
	return luma < 128
}

func styleTagColor(name string, dark bool) ui.Color {
	if dark {
		switch name {
		case "gray":
			return ui.Hex("#A1A1AA")
		case "red":
			return ui.Hex("#F87171")
		case "blue":
			return ui.Hex("#60A5FA")
		case "green":
			return ui.Hex("#4ADE80")
		case "orange":
			return ui.Hex("#FB923C")
		case "purple":
			return ui.Hex("#C084FC")
		}
		return ui.Color{}
	}
	switch name {
	case "gray":
		return ui.Hex("#78716C")
	case "red":
		return ui.Hex("#DC2626")
	case "blue":
		return ui.Hex("#2563EB")
	case "green":
		return ui.Hex("#15803D")
	case "orange":
		return ui.Hex("#EA580C")
	case "purple":
		return ui.Hex("#7C3AED")
	}
	return ui.Color{}
}

func diaryMarkdownTheme(dark bool) *markdown.Theme {
	p := paletteFor(dark)
	return &markdown.Theme{
		FontSize:            14.5,
		LineHeight:          1.7,
		Text:                p.ink,
		Muted:               p.secondary,
		Link:                p.accent,
		Strong:              p.ink,
		StrongWeight:        600,
		Code:                p.ink,
		CodeBackground:      p.chipFill,
		BlockSpacing:        0.8,
		ItemSpacing:         0.3,
		QuoteBar:            p.accent.Alpha(0.5),
		QuoteText:           p.secondary,
		QuoteBarWidth:       3,
		Rule:                p.rule,
		CodeBlockBackground: p.chipFill,
		CodeBlockBorder:     p.rule,
		TableBorder:         p.rule,
		TableHeader:         p.chipFill,
	}
}

// renderStyleTagBlock 在官方 mygo/ui/markdown 的 Block 钩子中接管包含 Apple 日记
// <c color> / <s size> 标签的段落或标题；其它块全部返回 false 交由官方组件渲染。
func renderStyleTagBlock(c *ui.Context, d *parser.Document, i int, dark bool) bool {
	b := d.Block(i)
	if b.Kind != parser.Paragraph && b.Kind != parser.Heading && b.Kind != parser.HTMLBlock {
		return false
	}
	src := d.Source()
	if b.Start < 0 || b.End > len(src) || b.Start >= b.End {
		return false
	}
	raw := strings.TrimSpace(src[b.Start:b.End])
	if !diary.HasStyleTags(raw) {
		return false
	}

	p := paletteFor(dark)
	baseSize := float32(14.5)
	baseWeight := 400
	if b.Kind == parser.Heading {
		raw = strings.TrimSpace(strings.TrimLeft(raw, "#"))
		baseWeight = 600
		switch b.Level {
		case 1:
			baseSize = 21
		case 2:
			baseSize = 18
		default:
			baseSize = 16
		}
	}

	runs := diary.ParseInline(raw)
	rt := ui.RichText(c).
		FillWidth().
		FontSize(baseSize).
		FontWeight(baseWeight).
		LineHeight(1.7).
		TextColor(p.ink)

	rt.Children(func() {
		for _, r := range runs {
			sz := baseSize
			if r.Size == "small" {
				sz = baseSize * 0.85
			} else if r.Size == "large" {
				sz = baseSize * 1.25
			}
			weight := baseWeight
			if r.Bold {
				weight = 600
			}
			col := p.ink
			if r.Color != "" {
				if custom := styleTagColor(r.Color, dark); custom.A > 0 {
					col = custom
				}
			}
			span := ui.Span{
				Text:          r.Text,
				Size:          sz,
				Weight:        weight,
				Italic:        r.Italic,
				Strikethrough: r.Strike,
				Color:         col,
			}
			if r.Code {
				span.Font = "monospace"
				span.Background = p.chipFill
				span.Radius = 3
			}
			if r.LinkURL != "" {
				span.Color = p.accent
				span.Underline = true
				ui.Link(c, "", r.LinkURL).Children(func() {
					ui.RichText(c, span)
				})
				continue
			}
			ui.RichText(c, span)
		}
	})
	return true
}

func openValidatedLink(rawURL string) {
	if clean, ok := diary.ValidateWebURL(rawURL); ok {
		_ = mygo.Shell.OpenExternal(clean)
	}
}
