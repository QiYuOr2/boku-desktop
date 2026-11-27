package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// UpdateStatus 表示应用在查找和准备新版本过程中所处的阶段，是界面获知的全部状态：
// 由 internal/update 的服务决定，视图负责绘制。
type UpdateStatus int

const (
	// UpdateIdle 表示尚未检查更新，界面不绘制任何内容。
	UpdateIdle UpdateStatus = iota
	// UpdateChecking 表示检查进行中：正在请求 feed，或正在比较发布版本与运行版本。
	UpdateChecking
	// UpdateUpToDate 表示已是最新版本。
	UpdateUpToDate
	// UpdateDownloading 表示正在后台下载新版本，进度见 UpdateState.Downloaded/Total。
	UpdateDownloading
	// UpdateReady 表示新版本已下载并校验，就位并在下次启动时生效。
	UpdateReady
	// UpdateUnsupported 表示该构建无法自更新（开发构建，或由包管理器/管理员拥有的安装）。
	// 这不是失败，也不必提示用户：没有可修复的问题，每次启动都提示只会成为噪音。
	UpdateUnsupported
	// UpdateFailed 表示检查或下载未完成，是唯一需要用户处理的状态。
	UpdateFailed
)

// String 返回适合日志记录的状态名。
func (s UpdateStatus) String() string {
	switch s {
	case UpdateIdle:
		return "idle"
	case UpdateChecking:
		return "checking"
	case UpdateUpToDate:
		return "up_to_date"
	case UpdateDownloading:
		return "downloading"
	case UpdateReady:
		return "ready"
	case UpdateUnsupported:
		return "unsupported"
	case UpdateFailed:
		return "failed"
	}
	return fmt.Sprintf("update_status(%d)", int(s))
}

func (s UpdateStatus) Valid() bool {
	return s >= UpdateIdle && s <= UpdateFailed
}

// UpdateState 是更新服务报告的状态，按界面绘制所需：状态、相关版本，以及必要时的一句话。
//
// Detail 只放人类可读的句子，绝不含 URL、签名或响应片段：它会被画在设置页并写入日志，
// 携带这些内容的是服务的错误，这里只保留给用户看的答案。
type UpdateState struct {
	Status UpdateStatus
	// Version 是 Ready 时将安装的版本，或 Downloading 时正在下载的版本；
	// 与具体版本无关的状态下为空。
	Version string
	// Notes 是发布说明（Markdown），取自发布的原文，可为空。
	Notes string
	// Downloaded 是已下载字节数，Total 是需获取的总字节数。Total 未知时可能为零；
	// Percent 判断二者是否值得显示。
	Downloaded int64
	Total      int64
	// Detail 是 UpdateFailed 时检查或下载失败的一句话原因；其它状态为空。
	Detail string
}

// Percent 返回已获取进度（0..100）以及总量是否已知。总量为零表示未知：
// 若此时显示 0% 会让用户误以为下载未在进行。
func (s UpdateState) Percent() (int, bool) {
	if s.Total <= 0 || s.Downloaded < 0 {
		return 0, false
	}
	if s.Downloaded >= s.Total {
		return 100, true
	}
	return int(s.Downloaded * 100 / s.Total), true
}

// Settled 报告状态是否已结束：检查不再运行，且在下次检查前不会变化。
func (s UpdateState) Settled() bool {
	switch s.Status {
	case UpdateIdle, UpdateUpToDate, UpdateReady, UpdateUnsupported, UpdateFailed:
		return true
	}
	return false
}

// 更新错误。拆成两个名字是因为语义不同：“该构建无法自更新”是关于安装的事实，
// 界面对其保持安静；而 update 不可用是一次可重试的检查或下载失败。
var (
	// ErrUpdateUnsupported 在运行构建无法自更新时由检查返回；它不是失败，见 UpdateUnsupported。
	ErrUpdateUnsupported = errors.New("updates are not available for this build")
	// ErrUpdateUnavailable 是通往就绪更新途中一切错误的包装：下载失败、下载与发布承诺不符、
	// 文件无法就位、重启无法发起。
	ErrUpdateUnavailable = errors.New("update unavailable")
)

// maxUpdateReasonBytes 是保留的错误原因长度上限，等于 MaxSnippetBytes：
// 领域错误携带内容的统一规则，无论文本来自响应体还是 JSON 解码器。
const maxUpdateReasonBytes = MaxSnippetBytes

// UpdateReason 将原因裁剪到错误消息允许的长度，避免解析失败的 manifest 撑满日志；
// 同时确保文本是合法 UTF-8，因为截断的编码不是文本。
func UpdateReason(reason string) string {
	reason = strings.ToValidUTF8(strings.TrimSpace(reason), "\uFFFD")
	if len(reason) <= maxUpdateReasonBytes {
		return reason
	}
	cut := maxUpdateReasonBytes
	// 一个编码字符为一到四字节，只有跨过切点起始的那个字符会被截成半个。
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return strings.TrimSpace(reason[:cut])
}
