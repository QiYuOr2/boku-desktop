package ui

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/live"
	"boku/internal/presentation"
)

func (s *State) modelRow(c *ui.Context, view live.View) {
	arts, err := live.PlatformArtifacts()
	if err != nil {
		presentation.Notice(c, presentation.NoteWarning, err.Error())
		return
	}
	var total int64
	for _, art := range arts {
		total += art.Size
	}
	presentation.Stacked(c, presentation.RowGap, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.RowGap).Children(func() {
			ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
				presentation.SectionLabel(c, "本地模型")
				status := "尚未就绪"
				if view.Ready {
					status = "已就绪"
				}
				if view.Downloading {
					status = "正在准备下载"
					for _, art := range arts {
						if art.Name == view.DownloadFile {
							status = "正在下载" + art.Title()
						}
					}
				}
				presentation.Notice(c, presentation.NotePlain, status)
			})
			if view.Downloading {
				if presentation.CardButton(c, "取消下载", presentation.ButtonSecondary).Shrink(0).Clicked() {
					go s.live.CancelDownload()
				}
			} else if !view.Ready {
				if presentation.CardButton(c, "下载默认文件", presentation.ButtonPrimary).Shrink(0).Clicked() {
					s.live.Download()
				}
			}
		})
		if view.DownloadNotice != "" {
			presentation.Notice(c, presentation.NoteWarning, view.DownloadNotice)
		}
		if view.Downloading && view.DownloadTotal > 0 {
			presentation.Stacked(c, presentation.FieldGap, func() {
				presentation.Notice(c, presentation.NotePlain, live.FormatSize(view.DownloadDone)+" / "+live.FormatSize(view.DownloadTotal))
				presentation.ProgressBar(c, view.DownloadDone*100/view.DownloadTotal, presentation.ColorBrand, "", "模型下载")
			})
		}
		s.disclosureRow(c, "下载清单 · "+strconv.Itoa(len(arts))+" 个文件 · "+live.FormatSize(total), &s.modelDetailsOpen)
		if s.modelDetailsOpen {
			s.downloadList(c, arts)
		}
		s.modelFileRow(c, "语音识别", "Parakeet 日语 · 默认", view.Settings.ASRPath, true)
		s.modelFileRow(c, "文字翻译", "Hy-MT2 1.8B · 默认", view.Settings.MTPath, false)
		presentation.Notice(c, presentation.NotePlain, "本地模型须为 .gguf 文件：识别需支持日语，翻译需兼容 Hy-MT2。")
		if s.modelNote != "" {
			presentation.Notice(c, s.modelNoteKind, s.modelNote)
		}
	})
}

func (s *State) disclosureRow(c *ui.Context, label string, open *bool) {
	b := ui.ButtonBase(c).Label(label).Expanded(*open).FillWidth().
		Justify(ui.Start).PaddingY(float32(2)).Gap(float32(4)).Cursor(ui.CursorPointer)
	b.Children(func() {
		angle := float32(0)
		if *open {
			angle = 90
		}
		presentation.DisclosureArrow(c, angle, presentation.ColorTextMuted)
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
	})
	if b.Clicked() {
		*open = !*open
		s.host.Refresh()
	}
}

func (s *State) downloadList(c *ui.Context, arts []live.Artifact) {
	presentation.Stacked(c, presentation.FieldGap, func() {
		for _, art := range arts {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(presentation.FieldGap).Children(func() {
				ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
					ui.Text(c, art.Title()).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextBody).SingleLine()
					ui.Text(c, art.Name).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).Wrap()
				})
				ui.Text(c, live.FormatSize(art.Size)).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine().Shrink(0)
			})
		}
	})
}

func (s *State) modelFileRow(c *ui.Context, title, defaultName, path string, recognition bool) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
		ui.Column(c).Grow(1).Gap(float32(2)).Children(func() {
			ui.Text(c, title).FontSize(presentation.SizeBody).TextColor(presentation.ColorTextBody).SingleLine()
			name := defaultName
			if path != "" {
				name = filepath.Base(path)
			}
			ui.Text(c, name).Tooltip(path).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).Wrap()
		})
		if presentation.CardButton(c, "更换文件…", presentation.ButtonSecondary).
			Label("更换" + title + "模型文件").Disabled(s.choosingModel).Shrink(0).Clicked() {
			s.chooseModel(title, path, recognition)
		}
		if path != "" {
			if presentation.TextAction(c, "恢复默认", presentation.ColorTextMuted).
				Label("恢复默认" + title + "模型").Disabled(s.choosingModel).Shrink(0).Clicked() {
				s.useModelPath(recognition, "")
			}
		}
	})
}

func (s *State) chooseModel(title, path string, recognition bool) {
	s.choosingModel = true
	s.modelNote = ""
	s.host.Refresh()
	parent := mygo.FocusedWindow()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: parent, Title: "选择" + title + "模型", DefaultPath: path,
			Filters: []mygo.FileFilter{{Name: "GGUF 模型", Extensions: []string{"gguf"}}},
		})
		if err == nil && len(paths) > 0 {
			err = live.ValidateModelFile(paths[0])
		}
		s.host.OnMain(func() {
			s.choosingModel = false
			if err != nil {
				s.modelNote = "选择" + title + "模型失败：" + err.Error()
				s.modelNoteKind = presentation.NoteWarning
			} else if len(paths) > 0 {
				s.useModelPath(recognition, paths[0])
			}
			s.host.Refresh()
		})
	}()
}

func (s *State) useModelPath(recognition bool, path string) {
	view := s.live.Snapshot()
	asr, mt := view.Settings.ASRPath, view.Settings.MTPath
	if recognition {
		asr = path
	} else {
		mt = path
	}
	if err := s.live.SetPaths(context.Background(), asr, mt); err != nil {
		s.modelNote = "没有保存模型设置，请重试"
		s.modelNoteKind = presentation.NoteWarning
	} else {
		s.modelNote = ""
		if view.Listening {
			s.modelNote = "新模型将在下次开始时使用"
			s.modelNoteKind = presentation.NotePlain
		}
	}
	s.host.Refresh()
}
