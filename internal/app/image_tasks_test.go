package app

import (
	"context"
	"errors"
	"testing"

	"boku/internal/domain"
	"boku/internal/translate"
)

func TestACompletedImageTaskKeepsItsResultForTheQuickWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	state := &State{imageTasks: []imageTranslationTask{{id: 7, status: imageTaskRunning, image: []byte("private screenshot"), cancel: cancel}}}
	result := translate.Result{SourceText: "原文", TranslatedText: "translation", ProviderName: "Index-Translate"}

	state.finishImageTranslationTask(7, result, nil)

	task := state.imageTranslationTask(7)
	if task == nil || task.status != imageTaskReady {
		t.Fatalf("task = %#v, want a ready task", task)
	}
	if got := task.result.TranslatedText; got != "translation" {
		t.Errorf("translated text = %q, want translation", got)
	}
	select {
	case <-ctx.Done():
	default:
		t.Error("the request context was not released after completion")
	}
}

func TestAFailedImageTaskIsRetainedForManualRetry(t *testing.T) {
	state := &State{imageTasks: []imageTranslationTask{{id: 8, status: imageTaskRunning, image: []byte("private screenshot")}}}

	state.finishImageTranslationTask(8, translate.Result{}, domain.ErrProviderTimeout)

	task := state.imageTranslationTask(8)
	if task == nil || task.status != imageTaskFailed {
		t.Fatalf("task = %#v, want a failed task", task)
	}
	if task.failure == "" {
		t.Error("failed task has no user-facing retry reason")
	}
}

func TestCompletedHiddenImageTaskSendsOnePrivateCompletionNotice(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := &State{
		notifier: notifier,
		imageTasks: []imageTranslationTask{{
			id:     10,
			status: imageTaskRunning,
		}},
	}

	state.finishImageTranslationTask(10, translate.Result{SourceText: "private source", TranslatedText: "private result"}, nil)
	state.noteCompletedImageTranslation(state.imageTranslationTask(10))

	if got := len(notifier.notices); got != 1 {
		t.Fatalf("notices = %d, want 1", got)
	}
	notice := notifier.notices[0]
	if notice.title != "截图翻译已完成" || notice.body != "点击查看翻译结果。" {
		t.Errorf("notice = %#v, want completion text without image content", notice)
	}
}

func TestCompletedVisibleImageTaskDoesNotInterruptWithANotification(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := &State{
		notifier:           notifier,
		translationVisible: true,
		translation:        translationState{imageTaskID: 11},
		imageTasks:         []imageTranslationTask{{id: 11, status: imageTaskRunning}},
	}

	state.finishImageTranslationTask(11, translate.Result{SourceText: "source", TranslatedText: "result"}, nil)

	if got := len(notifier.notices); got != 0 {
		t.Errorf("notices = %d, want none while the result card is visible", got)
	}
}

func TestCancellingAnImageTaskDropsItsPrivateBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	state := &State{imageTasks: []imageTranslationTask{{id: 9, status: imageTaskRunning, image: []byte("private screenshot"), cancel: cancel}}}

	state.cancelImageTranslationTask(9)

	if task := state.imageTranslationTask(9); task != nil {
		t.Errorf("task = %#v, want removed", task)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("context error = %v, want canceled", ctx.Err())
	}
}
