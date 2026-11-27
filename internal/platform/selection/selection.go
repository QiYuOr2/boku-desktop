// Package selection 读取用户在另一个应用里选中的内容。
//
// 它存在是因为读取选中内容比读取剪贴板更好：剪贴板是上次复制的东西，而选中内容是用户正在
// 看的东西。Windows 上第一个答案来自 UI Automation，也就是屏幕阅读器读取控件的接口；macOS
// 上来自辅助功能 API。
//
// 两者都不覆盖全部：浏览器首次被询问时才开启辅助功能，某些应用根本不发布文本（Windows 11
// 记事本即是一例），聊天窗口无论怎么问都可能返回空。因此设备还有第二个答案，也是其他桌面
// 翻译工具的做法、让它们处处可用的原因：**请应用复制**。复制进入剪贴板，从中读出文本，再把
// 原内容还原——这里写明而非隐藏，因为这是本包唯一用户能察觉的操作。
//
// 剪贴板上无法还原的内容——平台不肯交出字节的位图——绝不复制覆盖：翻译不值得丢掉用户的截图。
// 控制台窗口里也不做复制：那里复制组合键是信号而非复制。macOS 上对安全输入框应用同样的规则。
//
// 完全没有该接口的系统、返回空的应用和空选中都是常见答案：都意味着调用方应回退到已有的
// 剪贴板，ErrSelectionUnavailable 表达这一点而非描述失败。
//
// 实现位于 selection_windows.go、selection_darwin.go 和 selection_other.go：前两者有两种
// 答案，其余返回与应用不发布文本时相同的「此处不可用」。
package selection

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/logonce"
)

// DefaultTimeout 限定一次读取选中内容的总时长（含所有尝试）。它很短：答案关乎用户此刻眼前的
// 东西，超过这个时长的请求用户早已转向别处——那时改由剪贴板回答。
const DefaultTimeout = 2 * time.Second

// DefaultAttempts 和 DefaultRetryGap 说明返回空的读取如何重试。
//
// 一次读取不够，原因是被读取应用的特性：从未被辅助功能客户端询问过的应用——通常是浏览器——
// 在首次被询问时才开启辅助功能、稍后才建树，所以第一次读返回空，下一次才有选中内容。以刚启动
// 的 Chrome 实测：25ms 时首次返回空，105ms 后第二次才有选中文本。四次尝试、间隔 150ms 能
// 应对该情况，并在半秒内走到下面的复制——无选中的应用也必须走到那里。
const (
	DefaultAttempts = 4
	DefaultRetryGap = 150 * time.Millisecond
)

// CopyWait 和 CopyStep 限定等待应用响应合成复制的时长。分步监视剪贴板自带的写计数器，
// 快的应用一步即答，慢的给足整个 CopyWait。
const (
	CopyWait = 200 * time.Millisecond
	CopyStep = 10 * time.Millisecond
)

// Options 是组装设备所需的参数。
type Options struct {
	// Clipboard 是平台剪贴板，复制方案读取并还原它。没有它设备只读取 UI Automation 发布的内容。
	Clipboard domain.ClipboardDevice
	// BeforeCopy 设置时在合成复制前立即调用。剪贴板工具用它把复制放进剪贴板的值排除在
	// 历史之外：那不是用户复制的，而且工具随后要还原原内容。
	BeforeCopy func()
	// Sequence 是剪贴板的写计数器（平台有时）。它区分「替换了剪贴板的复制」和「没有选中内容的
	// 复制」：无选中的应用对复制返回空，计数器不动。
	//
	// 用函数而非平台调用，使复制回退在有计数器的各平台上是同一套逻辑；没有计数器的平台留空，
	// 不使用回退。
	Sequence func() uint32
}

// attempt 是一次读取：文本，或平台在给定时限内无话可说时的 ""。
type attempt func(ctx context.Context, within time.Duration) string

// readUntilAnswered 反复尝试读取，直到某次有答案、尝试次数用尽、超时或调用方放弃。
//
// 与所运行的平台分开，以便不带桌面会话也能测试重试——微妙之处正在于此。
func readUntilAnswered(ctx context.Context, read attempt, attempts int, gap time.Duration, deadline time.Time) string {
	for round := 0; round < attempts; round++ {
		if round > 0 {
			select {
			case <-ctx.Done():
				return ""
			case <-time.After(gap):
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ""
		}
		if text := read(ctx, remaining); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// readWithACopy 请应用自己提供选中内容：press 是复制组合键，sequence 是剪贴板自带的写
// 计数器，复制放进剪贴板的内容被读出，然后被原内容替换。
//
// 写计数器区分「没有选中」和「空选中」：无选中的应用对复制返回空，其计数器不动。等待有界，
// 损害也有界——本工具无法还原的剪贴板不被复制覆盖。
//
// 放在这里而非平台文件，是为了用假剪贴板和假复制测试整套决策。
func readWithACopy(ctx context.Context, clipboard domain.ClipboardDevice, beforeCopy func(), sequence func() uint32, press func()) string {
	if clipboard == nil {
		return ""
	}
	saved, savedErr := clipboard.Read(ctx)
	if savedErr == nil && !putBackable(saved) {
		// 剪贴板上是本工具无法还原的东西。覆盖它会导致丢失，翻译不值得这样。
		return ""
	}
	was := sequence()
	if beforeCopy != nil {
		beforeCopy()
	}
	press()
	if !waitForTheClipboardToChange(sequence, was, CopyWait, CopyStep, time.Sleep) {
		// 应用对复制返回空，这是无选中的应用的表现。
		return ""
	}
	got, gotErr := clipboard.Read(ctx)
	putBack(ctx, clipboard, saved, savedErr)
	if gotErr != nil || got.Kind != domain.ClipKindText {
		// 到达的是文件列表或图片：都不是可翻译文本，且无论哪种剪贴板都已还原。
		return ""
	}
	return got.Text
}

// putBackable 报告剪贴板上的内容能否在合成复制替换它之后还原：文本、文件列表，以及读取时
// 连字节一起拿到的位图。拿不到字节的位图不行——不能破坏无法还原的东西，那正是用户的截图。
func putBackable(content domain.ClipContent) bool {
	switch content.Kind {
	case domain.ClipKindText, domain.ClipKindFile:
		return true
	case domain.ClipKindImage:
		return len(content.Image) > 0
	}
	return false
}

// putBack 把剪贴板还原成复制发现时的样子。
//
// 写失败只记录不返回：调用方要的是选中内容，而它做的复制无论如何已经发生——它没有别的选择，
// 因为剪贴板不肯接回旧值而让读取失败，会把一次翻译变成用户没问过的错误。但仍值得记一行：
// 此时用户之前的剪贴板已丢失，这是唯一的记录。
func putBack(ctx context.Context, clipboard domain.ClipboardDevice, saved domain.ClipContent, savedErr error) {
	if savedErr != nil {
		// 什么都读不到，也就没有可还原的：复制发生在本工具无法描述的剪贴板上。
		return
	}
	var err error
	switch saved.Kind {
	case domain.ClipKindFile:
		err = clipboard.WriteFiles(ctx, saved.Files)
	case domain.ClipKindImage:
		err = clipboard.WriteImage(ctx, saved.Image)
	default:
		err = clipboard.WriteText(ctx, saved.Text)
	}
	// 去重：每次读取选中内容都会执行（每次快捷键或截图），用户因没反应而再次按键时否则会
	// 每次写同一行，而剪贴板不可用过了第一分钟就不再是新闻。
	if err != nil && logonce.Should("selection-clipboard-restore", err.Error()) {
		slog.Warn("the clipboard could not be put back after reading the selection", "event", "clipboard_restore_failed", "kind", saved.Kind, "error", err)
	}
	if err == nil {
		logonce.Forget("selection-clipboard-restore")
	}
}

// waitForTheClipboardToChange 等待剪贴板被写入，以系统自己的计数方式判断，返回是否变化。
//
// 计数器与时钟是参数，以便不带剪贴板、不实际消耗等待时间地测试。
func waitForTheClipboardToChange(sequence func() uint32, was uint32, budget, step time.Duration, sleep func(time.Duration)) bool {
	for waited := time.Duration(0); waited < budget; waited += step {
		if sequence() != was {
			return true
		}
		sleep(step)
	}
	return sequence() != was
}
