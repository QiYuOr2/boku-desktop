package ui

import (
	"boku/internal/presentation"
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"
)

// 快捷窗口的二维码动作：用与截图翻译相同的系统选区，在本地读取所选像素中的二维码，结果显示在同一张卡片。
//
// 数据不出本机。二维码常是登录码、支付码或个人链接，图片绝不发给翻译器——只在此本地解码，
// 仅在内存中保留文本，直到卡片关闭。
func (s *State) startQRRecognition() {
	if s.screenshot == nil {
		return
	}
	requestedAt := s.cursorPoint()
	startedAt := time.Now()
	slog.Info("QR recognition requested", "event", "qr_recognition_requested", "source", "quick_window")
	// 卡片钉住与否沿用翻译卡片的设置（本就是同一张卡片）。没有翻译工具也能扫码：识码不依赖服务。
	pinned := false
	if s.translate != nil {
		if settings, err := s.translate.Settings(); err == nil {
			pinned = settings.Pinned
		}
	}
	capture := s.screenshot
	decode := s.qrDecode
	if decode == nil {
		decode = kit.DecodeQRCodePNG
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), screenshotCaptureTimeout)
		image, err := capture.Capture(ctx)
		cancel()
		if err != nil {
			// 按 Esc、点出无效矩形、替换进行中的选区，都是退出应用自绘选区 UI 的正常方式，不留下卡片。
			if errors.Is(err, context.Canceled) {
				slog.Info("QR recognition capture cancelled", "event", "qr_recognition_cancelled", "duration", time.Since(startedAt))
				return
			}
			slog.Warn("QR recognition capture failed", "event", "qr_recognition_failed", "duration", time.Since(startedAt), "error", err)
			s.onMain(func() {
				s.showTranslation(requestedAt, translationState{
					status:  translationFailureStatus(err),
					qr:      true,
					failure: qrFailure(err),
					pinned:  pinned,
				})
			})
			return
		}
		slog.Info("QR recognition captured", "event", "qr_captured", "imageBytes", len(image), "duration", time.Since(startedAt))
		// 预览只给人看；无法生成预览不能连累文本，因此忽略其错误而非当作读取失败。
		preview, _ := ui.DecodeBitmap(image)
		text, decodeErr := decode(image)
		s.onMain(func() {
			// 选区耗时较久，卡片锚定在指针当前位置，与截图翻译一致。
			anchor := s.cursorPoint()
			if decodeErr != nil || strings.TrimSpace(text) == "" {
				failure := "没有识别到二维码。"
				switch {
				case decodeErr != nil && !errors.Is(decodeErr, kit.ErrQRCodeNotFound):
					slog.Warn("QR code could not be read", "event", "qr_recognition_failed", "duration", time.Since(startedAt), "error", decodeErr)
					failure = "没能读取这张截图。"
				case decodeErr != nil:
					slog.Info("QR code not found", "event", "qr_not_found", "duration", time.Since(startedAt))
				default:
					slog.Info("QR code holds no text", "event", "qr_empty", "duration", time.Since(startedAt))
					failure = "这个二维码里没有文本。"
				}
				s.showTranslation(anchor, translationState{
					status:  translateFailed,
					qr:      true,
					preview: preview,
					failure: failure,
					pinned:  pinned,
				})
				return
			}
			// 只记录长度。识别内容属于用户数据，只应出现在卡片里。
			slog.Info("QR code decoded", "event", "qr_recognition_completed", "chars", len([]rune(text)), "duration", time.Since(startedAt))
			s.showTranslation(anchor, translationState{
				status:  translateQRReady,
				qr:      true,
				preview: preview,
				source:  text,
				pinned:  pinned,
			})
		})
	}()
}

func qrFailure(err error) string {
	switch {
	case errors.Is(err, domain.ErrImageTranslateUnavailable):
		return "这台电脑还无法截取屏幕。"
	case errors.Is(err, context.DeadlineExceeded):
		return "选区等待超时，请重试。"
	default:
		return presentation.FriendlyError(err)
	}
}
