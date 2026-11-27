package ui

import (
	"boku/internal/presentation"
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/translate"
)

// 图片翻译任务只保存在内存里：卡片关闭后任务仍可继续，私密截图不会落盘。
const (
	maxRunningImageTasks  = 2
	maxFinishedImageTasks = 4
)

type imageTaskStatus uint8

const (
	imageTaskRunning imageTaskStatus = iota
	imageTaskReady
	imageTaskFailed
)

type imageTranslationTask struct {
	id        uint64
	status    imageTaskStatus
	startedAt time.Time
	pinned    bool
	image     []byte
	preview   *ui.Bitmap
	result    translate.Result
	failure   string
	cancel    context.CancelFunc
	notified  bool
}

// 先登记任务再发起请求，避免关闭卡片时丢失结果。只在主线程调用，请求通过 onMain 回到主线程。
func (s *State) startImageTranslationTask(service *translate.Translate, image []byte, preview *ui.Bitmap, pinned bool) (uint64, bool) {
	if s.runningImageTranslationTasks() >= maxRunningImageTasks {
		return 0, false
	}
	s.trimFinishedImageTasks()
	s.nextImageTaskID++
	ctx, cancel := context.WithTimeout(context.Background(), screenshotTranslationTimeout)
	task := imageTranslationTask{
		id:        s.nextImageTaskID,
		status:    imageTaskRunning,
		startedAt: time.Now(),
		pinned:    pinned,
		image:     image,
		preview:   preview,
		cancel:    cancel,
	}
	s.imageTasks = append(s.imageTasks, task)
	slog.Info("image translation task started", "event", "image_translation_started", "task", task.id, "imageBytes", len(image))
	s.host.FitQuick()
	s.runImageTranslationTask(service, task.id, ctx, image)
	return task.id, true
}

func (s *State) runImageTranslationTask(service *translate.Translate, id uint64, ctx context.Context, image []byte) {
	go func() {
		result, err := service.TranslateImage(ctx, image)
		s.onMain(func() { s.finishImageTranslationTask(id, result, err) })
	}()
}

func (s *State) finishImageTranslationTask(id uint64, result translate.Result, err error) {
	task := s.imageTranslationTask(id)
	if task == nil || task.status != imageTaskRunning {
		return
	}
	if task.cancel != nil {
		task.cancel()
		task.cancel = nil
	}
	if err != nil {
		task.status = imageTaskFailed
		task.failure = presentation.FriendlyError(err)
		slog.Warn("image translation task failed", "event", "image_translation_failed", "task", id, "duration", time.Since(task.startedAt), "error", err)
	} else {
		task.status = imageTaskReady
		task.result = result
		slog.Info("image translation task completed", "event", "image_translation_completed", "task", id, "provider", result.ProviderID, "duration", time.Since(task.startedAt))
	}
	s.noteCompletedImageTranslation(task)
	// 完成的任务只更新它自己的卡片；已关闭的卡片保持关闭，结果从快捷窗口取，不抢焦点。
	if s.translateWindow != nil && s.translationVisible && s.translation.imageTaskID == id {
		s.translation = s.imageTranslationState(*task)
		s.cardChanged()
		s.fitTranslationWindow()
	}
	s.Refresh()
}

// 仅当任务的卡片不再打开时发送一次系统通知，通知不包含图片和识别文本，点击回到已保存的结果。
func (s *State) noteCompletedImageTranslation(task *imageTranslationTask) {
	if task.status != imageTaskReady || task.notified || (s.translationVisible && s.translation.imageTaskID == task.id) {
		return
	}
	task.notified = true
	if s.notifier == nil || !s.notifier.Supported() {
		return
	}
	id := task.id
	s.notifier.Notify(
		"boku-image-translation-"+strconv.FormatUint(id, 10),
		"截图翻译已完成",
		"点击查看翻译结果。",
		func() { s.onMain(func() { s.openImageTranslationTask(id) }) },
	)
}

func (s *State) imageTranslationTask(id uint64) *imageTranslationTask {
	for index := range s.imageTasks {
		if s.imageTasks[index].id == id {
			return &s.imageTasks[index]
		}
	}
	return nil
}

func (s *State) runningImageTranslationTasks() int {
	count := 0
	for _, task := range s.imageTasks {
		if task.status == imageTaskRunning {
			count++
		}
	}
	return count
}

func (s *State) imageTranslationState(task imageTranslationTask) translationState {
	state := translationState{image: true, preview: task.preview, pinned: task.pinned, imageTaskID: task.id}
	switch task.status {
	case imageTaskReady:
		state.status = translateReady
		state.source = task.result.SourceText
		state.result = task.result
	case imageTaskFailed:
		state.status = translateFailed
		state.failure = task.failure
	default:
		state.status = translateLoading
	}
	return state
}

func (s *State) showImageTranslationTask(id uint64, at mygo.Point) {
	task := s.imageTranslationTask(id)
	if task == nil {
		return
	}
	s.showTranslation(at, s.imageTranslationState(*task))
}

func (s *State) openImageTranslationTask(id uint64) {
	slog.Info("image translation task opened", "event", "image_translation_opened", "task", id)
	s.showImageTranslationTask(id, s.cursorPoint())
}

// 只取消运行中的任务，并立即丢弃它的私密图片字节，不保留已取消的历史行。
func (s *State) cancelImageTranslationTask(id uint64) {
	for index := range s.imageTasks {
		task := &s.imageTasks[index]
		if task.id != id || task.status != imageTaskRunning {
			continue
		}
		if task.cancel != nil {
			task.cancel()
		}
		slog.Info("image translation task cancelled", "event", "image_translation_cancelled", "task", id, "duration", time.Since(task.startedAt))
		s.imageTasks = append(s.imageTasks[:index], s.imageTasks[index+1:]...)
		s.host.FitQuick()
		if s.translation.imageTaskID == id {
			s.closeTranslation()
			s.translation = translationState{}
		}
		s.Refresh()
		return
	}
}

// 重试是手动的：自动重试会把同一张图重复提交给付费模型。原始 PNG 只在该内存任务存活期间保留。
func (s *State) retryImageTranslationTask(id uint64) {
	task := s.imageTranslationTask(id)
	if task == nil || task.status != imageTaskFailed || s.translate == nil || s.runningImageTranslationTasks() >= maxRunningImageTasks {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), screenshotTranslationTimeout)
	task.status = imageTaskRunning
	task.startedAt = time.Now()
	task.result = translate.Result{}
	task.failure = ""
	task.cancel = cancel
	slog.Info("image translation task retried", "event", "image_translation_retried", "task", id)
	s.runImageTranslationTask(s.translate, task.id, ctx, task.image)
	s.host.FitQuick()
	s.Refresh()
}

func (s *State) clearFinishedImageTranslationTasks() {
	cleared := 0
	kept := s.imageTasks[:0]
	clearedCurrentCard := false
	for _, task := range s.imageTasks {
		if task.status == imageTaskRunning {
			kept = append(kept, task)
		} else if s.translation.imageTaskID == task.id {
			clearedCurrentCard = true
		}
		if task.status != imageTaskRunning {
			cleared++
		}
	}
	s.imageTasks = kept
	s.host.FitQuick()
	if clearedCurrentCard {
		s.closeTranslation()
		s.translation = translationState{}
	}
	s.Refresh()
	if cleared > 0 {
		slog.Info("image translation tasks cleared", "event", "image_translation_tasks_cleared", "count", cleared)
	}
}

func (s *State) trimFinishedImageTasks() {
	finished := 0
	for _, task := range s.imageTasks {
		if task.status != imageTaskRunning {
			finished++
		}
	}
	for finished >= maxFinishedImageTasks {
		for index, task := range s.imageTasks {
			if task.status == imageTaskRunning {
				continue
			}
			s.imageTasks = append(s.imageTasks[:index], s.imageTasks[index+1:]...)
			if s.translation.imageTaskID == task.id {
				s.closeTranslation()
				s.translation = translationState{}
			}
			finished--
			s.host.FitQuick()
			break
		}
	}
}
