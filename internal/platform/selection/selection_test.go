package selection

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

type fakeClipboard struct {
	content domain.ClipContent
	err     error
	// writes 是还原过的内容，最早在前。
	writes []string
	// files 是文件列表还原成的样子。
	files [][]string
	// images 是位图还原成的样子。
	images [][]byte
}

func (f *fakeClipboard) Read(context.Context) (domain.ClipContent, error) {
	return f.content, f.err
}

func (f *fakeClipboard) WriteText(_ context.Context, text string) error {
	f.writes = append(f.writes, text)
	f.content = domain.ClipContent{Kind: domain.ClipKindText, Text: text, Display: text}
	return nil
}

func (f *fakeClipboard) WriteFiles(_ context.Context, paths []string) error {
	f.files = append(f.files, paths)
	f.content = domain.ClipContent{Kind: domain.ClipKindFile, Files: paths, Display: paths[0]}
	return nil
}

func (f *fakeClipboard) WriteImage(_ context.Context, png []byte) error {
	f.images = append(f.images, append([]byte(nil), png...))
	f.content = domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true, Image: png, ImageBytes: len(png)}
	return nil
}

// TestACopyIsWhatIsReadAndTheClipboardIsPutBack 是回退方案的全部意义：不发布文本的应用
// 被要求复制，文本到达剪贴板，用户原有的内容被还原。
func TestACopyIsWhatIsReadAndTheClipboardIsPutBack(t *testing.T) {
	clipboard := &fakeClipboard{content: domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user had"}}
	var sequence uint32 = 7
	copied := 0
	text := readWithACopy(context.Background(), clipboard,
		func() { copied++ },
		func() uint32 { return sequence },
		func() {
			clipboard.content = domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user selected"}
			sequence++
		})
	if text != "what the user selected" {
		t.Errorf("read = %q, want what the application copied", text)
	}
	if copied != 1 {
		t.Errorf("the clipboard tool was told about the copy %d times, want once", copied)
	}
	if len(clipboard.writes) != 1 || clipboard.writes[0] != "what the user had" {
		t.Errorf("the clipboard was put back as %q", clipboard.writes)
	}
	if got := clipboard.content.Text; got != "what the user had" {
		t.Errorf("the clipboard holds %q, want what was there before", got)
	}
}

// 无选中的应用对复制返回空，这是「没有选中」而非空翻译：剪贴板自己的写计数器区分两者。
func TestACopyThatChangesNothingIsNoSelection(t *testing.T) {
	clipboard := &fakeClipboard{content: domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user had"}}
	text := readWithACopy(context.Background(), clipboard,
		nil,
		func() uint32 { return 12 },
		func() { /* the application copied nothing */ })
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if len(clipboard.writes) != 0 {
		t.Errorf("the clipboard was written %d times although nothing changed", len(clipboard.writes))
	}
}

// 剪贴板上有图片不是拒绝读取选中的理由：它被保存，复制覆盖后再还原。这正是用户遇到的
// 问题——剪贴板里的截图会让每次划词翻译都回答「剪贴板没有文本」。
func TestAPictureOnTheClipboardIsPutBack(t *testing.T) {
	picture := []byte("89504e470d0a1a0a-screenshot")
	clipboard := &fakeClipboard{content: domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, Image: picture, ImageBytes: len(picture),
	}}
	var sequence uint32 = 3
	text := readWithACopy(context.Background(), clipboard, nil,
		func() uint32 { return sequence },
		func() {
			clipboard.content = domain.ClipContent{Kind: domain.ClipKindText, Text: "the selection"}
			sequence++
		})
	if text != "the selection" {
		t.Errorf("read = %q, want the text the application copied over the picture", text)
	}
	if len(clipboard.images) != 1 || string(clipboard.images[0]) != string(picture) {
		t.Errorf("the picture was put back as %q, want the bytes that were there", clipboard.images)
	}
	if clipboard.content.Kind != domain.ClipKindImage {
		t.Errorf("the clipboard holds %q, want the picture back", clipboard.content.Kind)
	}
}

// 读不出字节的图片是唯一仍不动的东西：复制覆盖会丢失它，而本工具无法还原。
func TestAPictureWithoutItsBytesIsNotCopiedOver(t *testing.T) {
	clipboard := &fakeClipboard{content: domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, ImageBytes: 4096,
	}}
	pressed := false
	text := readWithACopy(context.Background(), clipboard, nil,
		func() uint32 { return 3 },
		func() { pressed = true })
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if pressed {
		t.Error("the application was asked to copy over a picture that could not be restored")
	}
}

// 复制回来的是文件列表或图片时不是可翻译文本，但剪贴板照样还原。
func TestACopyOfSomethingOtherThanTextIsNotATranslation(t *testing.T) {
	for _, arrived := range []domain.ClipContent{
		{Kind: domain.ClipKindFile, Files: []string{`C:\a.txt`}},
		{Kind: domain.ClipKindImage, ImageBytes: 16},
	} {
		clipboard := &fakeClipboard{content: domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user had"}}
		var sequence uint32 = 1
		text := readWithACopy(context.Background(), clipboard, nil,
			func() uint32 { return sequence },
			func() { clipboard.content = arrived; sequence++ })
		if text != "" {
			t.Errorf("kind %s read as %q, want nothing to translate", arrived.Kind, text)
		}
		if len(clipboard.writes) != 1 {
			t.Errorf("kind %s: the clipboard was put back %d times, want once", arrived.Kind, len(clipboard.writes))
		}
	}
}

// 本工具读不了的剪贴板不是拒绝的理由：复制照常在它上面进行，只是没有可还原的东西。
func TestACopyOverAnUnknownClipboardIsStillMade(t *testing.T) {
	clipboard := &fakeClipboard{err: domain.ErrUnsupportedClipboardFormat}
	var sequence uint32 = 5
	text := readWithACopy(context.Background(), clipboard, nil,
		func() uint32 { return sequence },
		func() {
			clipboard.err = nil
			clipboard.content = domain.ClipContent{Kind: domain.ClipKindText, Text: "the selection"}
			sequence++
		})
	if text != "the selection" {
		t.Errorf("read = %q, want the copied text", text)
	}
	if len(clipboard.writes) != 0 {
		t.Errorf("something was put back although nothing could be read: %q", clipboard.writes)
	}
}

// 等待受预算限制，且计数器一变化就停止：快的应用不必等慢应用的预算。
func TestTheWaitForTheCopyIsBounded(t *testing.T) {
	slept := time.Duration(0)
	steps := 0
	changed := waitForTheClipboardToChange(
		func() uint32 { return 1 },
		1,
		50*time.Millisecond,
		10*time.Millisecond,
		func(d time.Duration) { slept += d; steps++ },
	)
	if changed {
		t.Error("the wait reported a change that never happened")
	}
	if slept > 60*time.Millisecond {
		t.Errorf("the wait slept %v, want no more than the budget", slept)
	}

	// 计数器在第三次查看时变化：等待就此结束，而不是耗尽预算。
	looks := 0
	changed = waitForTheClipboardToChange(
		func() uint32 {
			looks++
			if looks > 3 {
				return 2
			}
			return 1
		},
		1,
		500*time.Millisecond,
		10*time.Millisecond,
		func(time.Duration) {},
	)
	if !changed {
		t.Error("the wait missed a change that happened inside its budget")
	}
	if looks > 5 {
		t.Errorf("the wait kept looking %d times after the counter moved", looks)
	}
}

// 有答案的读取就是答案：重试是为了需要询问两次的应用，不是为了让正常读取变慢。
func TestAReadingThatAnswersIsNotRepeated(t *testing.T) {
	asked := 0
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		return "the selection"
	}, 4, time.Millisecond, time.Now().Add(time.Second))
	if text != "the selection" {
		t.Errorf("read = %q, want the reading", text)
	}
	if asked != 1 {
		t.Errorf("the platform was asked %d times, want once", asked)
	}
}

// 写此测试的场景：从未被辅助功能客户端询问过的应用首次返回空，第二次读取才有选中内容
// （以刚启动的 Chrome 实测：25ms 时为空，105ms 后才是选中文本）。
func TestAReadingThatAnswersNothingIsTriedAgain(t *testing.T) {
	asked := 0
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		if asked < 2 {
			return ""
		}
		return "the selection"
	}, DefaultAttempts, time.Millisecond, time.Now().Add(time.Second))
	if text != "the selection" {
		t.Errorf("read = %q, want the selection the second reading had", text)
	}
	if asked != 2 {
		t.Errorf("the platform was asked %d times, want twice", asked)
	}
}

// 未选中任何内容是最常见的情况，剪贴板在它之后等着：尝试次数有限，等待也有限。
func TestNothingSelectedGivesUpAfterTheAttempts(t *testing.T) {
	asked := 0
	start := time.Now()
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		return "   "
	}, 3, time.Millisecond, time.Now().Add(time.Second))
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if asked != 3 {
		t.Errorf("the platform was asked %d times, want the three attempts", asked)
	}
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Errorf("giving up took %v, want the gaps only", waited)
	}
}

// 总时限覆盖整次读取（含重试）：已超时的读取不会再发起下一次尝试。
func TestTheDeadlineEndsTheRetrying(t *testing.T) {
	asked := 0
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		return ""
	}, 5, time.Millisecond, time.Now().Add(-time.Second))
	if text != "" || asked != 0 {
		t.Errorf("read = %q after %d attempts, want nothing and no attempt at all", text, asked)
	}
}

// 已放弃的调用方不会在间隔里继续被拖住。
func TestACancelledReadingStopsBetweenAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	asked := 0
	cancel()
	start := time.Now()
	text := readUntilAnswered(ctx, func(context.Context, time.Duration) string {
		asked++
		return ""
	}, 5, time.Second, time.Now().Add(time.Minute))
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if waited := time.Since(start); waited > 100*time.Millisecond {
		t.Errorf("a cancelled reading waited %v, want the first attempt only", waited)
	}
	if asked > 1 {
		t.Errorf("the platform was asked %d times after the caller gave up", asked)
	}
}
