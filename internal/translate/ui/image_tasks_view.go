package ui

import (
	"boku/internal/presentation"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

// imageTranslationTasks is omitted entirely until there is a screenshot task;
// an empty queue must not make the quick window taller.
func (s *State) imageTranslationTasks(c *ui.Context) {
	if len(s.imageTasks) == 0 {
		return
	}
	presentation.QuickDivider(c)
	presentation.QuickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(7)).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
					ui.Text(c, "翻译任务").FontSize(presentation.SizeQuickFine).FontWeight(500).TextColor(presentation.PanelBody).SingleLine()
					presentation.KindBadge(c, presentation.ColorBrandTintStrong, presentation.ColorBrand, strconv.Itoa(len(s.imageTasks)))
				})
				ui.Spacer(c)
				if s.hasFinishedImageTranslationTasks() {
					if clear := presentation.TextAction(c, "清空", presentation.PanelFaint); clear.Clicked() {
						s.clearFinishedImageTranslationTasks()
					}
				}
			})
			for _, task := range s.imageTasks {
				s.imageTranslationTaskRow(c, task)
			}
		})
	})
}

func (s *State) hasFinishedImageTranslationTasks() bool {
	for _, task := range s.imageTasks {
		if task.status != imageTaskRunning {
			return true
		}
	}
	return false
}

func (s *State) imageTranslationTaskRow(c *ui.Context, task imageTranslationTask) {
	if task.status == imageTaskReady {
		row := ui.ButtonBase(c).Label("打开截图翻译结果").FillWidth().
			Padding(float32(6), float32(8)).Radius(presentation.RadiusField).
			AlignItems(ui.Center).Gap(float32(8)).Cursor(ui.CursorPointer).
			Background(presentation.ColorBrandTint)
		if row.Hovered() || row.Focused() {
			row = row.Background(presentation.ColorBrandTintStrong)
		}
		row.Children(func() { s.imageTranslationTaskContents(c, task, true) })
		if row.Clicked() {
			s.openImageTranslationTask(task.id)
		}
		return
	}

	row := ui.Row(c).FillWidth().Padding(float32(6), float32(8)).
		Radius(presentation.RadiusField).AlignItems(ui.Center).Gap(float32(8)).
		Background(presentation.ColorRowAlt).Border(1, presentation.ColorCardLine)
	if task.status == imageTaskFailed {
		row = row.Background(presentation.ColorDanger.Alpha(0.08))
	}
	row.Children(func() {
		s.imageTranslationTaskContents(c, task, false)
		if task.status == imageTaskRunning {
			if cancel := presentation.CardIconButton(c, s.closeMark, "取消图片翻译"); cancel.Clicked() {
				s.cancelImageTranslationTask(task.id)
			}
			return
		}
		if retry := presentation.TextAction(c, "重试", presentation.ColorBrand); retry.Clicked() {
			s.retryImageTranslationTask(task.id)
		}
	})
}

func (s *State) imageTranslationTaskContents(c *ui.Context, task imageTranslationTask, disclosure bool) {
	imageTranslationTaskThumbnail(c, task)
	ui.Column(c).Grow(1).MinWidth(0).Gap(float32(2)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(float32(5)).Children(func() {
			ui.Text(c, "截图翻译").FontSize(presentation.SizeQuickFine).FontWeight(500).TextColor(presentation.PanelInk).SingleLine().MinWidth(0)
			switch task.status {
			case imageTaskRunning:
				ui.Spinner(c).Size(float32(10), float32(10)).Shrink(0)
				ui.Text(c, "进行中").FontSize(presentation.SizeQuickTiny).TextColor(presentation.ColorBrand).SingleLine()
			case imageTaskReady:
				ui.Text(c, "已完成").FontSize(presentation.SizeQuickTiny).TextColor(presentation.ColorSuccess).SingleLine()
			case imageTaskFailed:
				ui.Text(c, "失败").FontSize(presentation.SizeQuickTiny).TextColor(presentation.ColorDanger).SingleLine()
			}
		})
		ui.Text(c, imageTranslationTaskDetail(task)).FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine().MinWidth(0)
	})
	if disclosure {
		ui.Text(c, "›").FontSize(float32(18)).TextColor(presentation.ColorBrand).SingleLine().Shrink(0)
	}
}

func imageTranslationTaskThumbnail(c *ui.Context, task imageTranslationTask) {
	box := ui.Box(c).Size(float32(28), float32(28)).Shrink(0).
		Radius(presentation.RadiusTile).Background(presentation.ColorCardFoot).Border(1, presentation.ColorBorder).Center()
	box.Children(func() {
		if task.preview != nil {
			ui.Image(c, task.preview).Fill().Fit(ui.Cover).Radius(presentation.RadiusTile)
			return
		}
		ui.Text(c, "截图").FontSize(presentation.SizeTiny).TextColor(presentation.PanelFaint).SingleLine()
	})
}

func imageTranslationTaskDetail(task imageTranslationTask) string {
	switch task.status {
	case imageTaskReady:
		if task.result.TranslatedText != "" {
			return task.result.TranslatedText
		}
		return "点击查看翻译结果"
	case imageTaskFailed:
		return task.failure + " · 可重试"
	default:
		return "约 " + strconv.Itoa(max(1, int(time.Since(task.startedAt).Round(time.Second).Seconds()))) + " 秒前 · 可关闭浮窗"
	}
}
