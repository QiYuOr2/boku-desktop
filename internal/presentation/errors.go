package presentation

import (
	"boku/internal/domain"
	"errors"
)

// friendlyError says what went wrong in the interface's own words. The
// errors themselves are built so that they never carry a credential.
func FriendlyError(err error) string {
	var bad *domain.BadResponseError
	var imageFailure *domain.ImageTranslationError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrCredentialMissing):
		return "还没有配置凭据。"
	case errors.Is(err, domain.ErrProviderUnauthorized):
		return "凭据被拒绝，请检查是否仍然有效。"
	case errors.Is(err, domain.ErrProviderRateLimited):
		return "请求太频繁，请稍后再试。"
	case errors.Is(err, domain.ErrDeviceLoginExpired):
		return "登录验证已过期，请再试一次。"
	case errors.Is(err, domain.ErrProviderTimeout):
		return "请求超时，请检查网络。"
	case errors.Is(err, domain.ErrProviderUnavailable):
		return "服务暂时不可用，请稍后再试。"
	case errors.Is(err, domain.ErrProviderNotFound):
		return "找不到这个数据源。"
	case errors.Is(err, domain.ErrScreenRecordingDenied):
		return "需要屏幕录制权限才能读取截图。"
	case errors.As(err, &imageFailure):
		switch imageFailure.Code {
		case domain.ImageFailureNoReadableText:
			return "图片中没有可识别的文字。"
		case domain.ImageFailureUnsupported:
			return "模型无法处理这张图片。"
		case domain.ImageFailureRefused:
			return "模型拒绝处理这张图片。"
		}
		return "模型无法完成这张图片的翻译。"
	case errors.As(err, &bad):
		if bad.Reason != "" {
			return "服务返回了无法识别的数据：" + bad.Reason
		}
		return "服务返回了无法识别的数据。"
	case errors.Is(err, domain.ErrProviderBadResponse):
		return "服务返回了无法识别的数据。"
	default:
		return err.Error()
	}
}
