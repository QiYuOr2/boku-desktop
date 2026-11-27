package clipboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// 剪贴板工具针对测试自有的设备与存储而测：无平台剪贴板、无文件、无窗口。
// 这里检验的是工具承诺的行为——每次复制一条条目、被要求停止即停止的循环，
// 以及不会无限增长的历史。

func TestNewClipboardRequiresItsDependencies(t *testing.T) {
	if _, err := NewClipboard(ClipboardOptions{Store: &fakeStore{}}); err == nil {
		t.Error("NewClipboard accepted a service without a device")
	}
	if _, err := NewClipboard(ClipboardOptions{Device: &fakeDevice{}}); err == nil {
		t.Error("NewClipboard accepted a service without a store")
	}
}

// 两个开关在用户开启前都关闭：剪贴板是应用能读到的最敏感的东西。
// 合成复制——划词工具为读取某个应用不发布的文本而发起的复制——不是用户的复制，
// 历史不该收录它。
func TestASyntheticCopyIsNotRecorded(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	device.set(textReading("what the selection tool asked an application for"))
	clipboard.IgnoreTheNextCopy()
	clipboard.poll(context.Background())
	if got := len(clipboard.History(10)); got != 0 {
		t.Errorf("the history recorded %d entries of a synthetic copy", got)
	}

	// 窗口是时刻而非规则：此后用户自己做的复制照常记录。
	clock.advance(copyIgnoreWindow + time.Second)
	device.set(textReading("what the user copied themselves"))
	clipboard.poll(context.Background())
	history := clipboard.History(10)
	if len(history) != 1 || history[0].Content != "what the user copied themselves" {
		t.Errorf("the history holds %+v, want the copy the user made", history)
	}
}

func TestClipboardDefaultsToOff(t *testing.T) {
	clipboard, _, _, _ := newTestClipboard(t, ClipboardOptions{})
	settings := clipboard.Settings()
	if settings.RegionVisible || settings.Monitoring {
		t.Errorf("the clipboard starts enabled: %+v", settings)
	}
	if clipboard.Show() {
		t.Error("Show reports a region nothing asked to draw")
	}
}

func TestRecordShapesEveryKind(t *testing.T) {
	cases := []struct {
		name        string
		reading     domain.ClipContent
		wantKind    domain.ClipKind
		wantDisplay string
		wantContent string
	}{
		{
			name:        "plain text",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: "hello  world\n"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "hello world",
			wantContent: "hello  world\n",
		},
		{
			name:        "a url",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: "https://example.com/a"},
			wantKind:    domain.ClipKindURL,
			wantDisplay: "https://example.com/a",
			wantContent: "https://example.com/a",
		},
		{
			name:        "json",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: `{"a":1}`},
			wantKind:    domain.ClipKindJSON,
			wantDisplay: `{"a":1}`,
			wantContent: `{"a":1}`,
		},
		{
			name:        "one file",
			reading:     domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{`C:\dir\report.pdf`}},
			wantKind:    domain.ClipKindFile,
			wantDisplay: "[report.pdf]",
			wantContent: `C:\dir\report.pdf`,
		},
		{
			name:        "three files",
			reading:     domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"a.txt", "b.txt", "c.txt"}},
			wantKind:    domain.ClipKindFile,
			wantDisplay: "[a.txt 等 3 个文件]",
			wantContent: "a.txt\nb.txt\nc.txt",
		},
		{
			name:        "an image without a name",
			reading:     domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true},
			wantKind:    domain.ClipKindImage,
			wantDisplay: "[图片]",
			wantContent: "[图片]",
		},
		{
			name:        "an image with a name",
			reading:     domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true, Files: []string{"/tmp/shot.png"}},
			wantKind:    domain.ClipKindImage,
			wantDisplay: "[shot.png]",
			wantContent: "[shot.png]",
		},
		{
			name:        "rich text without a plain flavour",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, HTML: "<p>a <b>bold</b> word</p>"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "a bold word",
			wantContent: "a bold word",
		},
		{
			name:        "rich text with an empty plain flavour",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: "  ", HTML: "<div>from markup</div>"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "from markup",
			wantContent: "from markup",
		},
		{
			name:        "rich text with nothing readable",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, HTML: "<div></div>"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "",
			wantContent: "",
		},
	}
	for _, c := range cases {
		record := Record(c.reading)
		if record.Kind != c.wantKind {
			t.Errorf("%s: kind = %q, want %q", c.name, record.Kind, c.wantKind)
		}
		if record.Display != c.wantDisplay {
			t.Errorf("%s: display = %q, want %q", c.name, record.Display, c.wantDisplay)
		}
		if record.Content != c.wantContent {
			t.Errorf("%s: content = %q, want %q", c.name, record.Content, c.wantContent)
		}
		if record.Hash == "" {
			t.Errorf("%s: a record without a hash cannot be deduplicated", c.name)
		}
	}
}

func TestRecordShortensLongText(t *testing.T) {
	record := Record(textReading(strings.Repeat("x", 5000)))
	if len([]rune(record.Display)) != 121 { // 上限加上省略号
		t.Errorf("the display is %d runes, want 121", len([]rune(record.Display)))
	}
	if !strings.HasSuffix(record.Display, "…") {
		t.Error("a shortened display does not say it was shortened")
	}
	if len(record.Content) != 5000 {
		t.Error("the content itself was shortened as well as the display")
	}
}

// 重复历史已有内容的读取不成为第二条条目，且被记录的内容会在帧之后到达存储。
// （不阻塞地读取慢剪贴板由 blocking_test.go 保证；这里测的是读取在历史一侧的效果。）
func TestReadingTheSameValueTwiceKeepsOneEntry(t *testing.T) {
	clock := newTestClock()
	clipboard, device, store, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	read(clipboard, clock, device, "first")
	clipboard.Read(ctx)

	if got := len(clipboard.History(0)); got != 1 {
		t.Fatalf("the history has %d entries after reading one value twice, want 1", got)
	}
	if got := device.readCount(); got != 2 {
		t.Errorf("the device was read %d times, want 2", got)
	}
	// 历史在改动之后写入，因此存储稍后才追上，而不是拖住做出改动的那一帧。
	waitFor(t, "the history to be written", func() bool { return len(store.stored()) == 1 })
	latest, ok := clipboard.Latest()
	if !ok || latest.Content != "first" {
		t.Errorf("Latest = %+v, %v", latest, ok)
	}

	// 不同的值是第二条条目，最新在前。
	read(clipboard, clock, device, "second")
	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2", len(history))
	}
	if history[0].Content != "second" || history[1].Content != "first" {
		t.Errorf("the history is not newest first: %q, %q", history[0].Content, history[1].Content)
	}
}

func TestHistoryLimitAndOrder(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock, Keep: 3})
	for _, text := range []string{"one", "two", "three", "four"} {
		read(clipboard, clock, device, text)
	}
	history := clipboard.History(0)
	if len(history) != 3 {
		t.Fatalf("the history kept %d entries, want 3", len(history))
	}
	if history[0].Content != "four" || history[2].Content != "two" {
		t.Errorf("the oldest entry was not evicted: %q … %q", history[0].Content, history[2].Content)
	}
	// 页面请求的比存储保留的少。
	if got := len(clipboard.History(2)); got != 2 {
		t.Errorf("History(2) returned %d entries", got)
	}
}

// 相同内容再次复制会把其条目移回顶部，而非新增一条。
func TestSameContentCopiedAgainMovesToTheTop(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	for _, text := range []string{"alpha", "beta", "alpha"} {
		read(clipboard, clock, device, text)
	}

	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2", len(history))
	}
	if history[0].Content != "alpha" {
		t.Errorf("the repeated copy is not at the top: %q first", history[0].Content)
	}
}

func TestPinnedEntriesSurviveTheLimit(t *testing.T) {
	device := &fakeDevice{}
	store := &fakeStore{}
	// 一条置顶条目，完整剪贴板工具日后能够创建的形式。
	pinned := domain.ClipboardEntry{
		ID:          "pinned-1",
		Content:     "keep me",
		ContentHash: "sha256:pinned",
		Kind:        domain.ClipKindText,
		Pinned:      true,
		CopiedAt:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := store.SaveEntries(context.Background(), []domain.ClipboardEntry{pinned}); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:  device,
		Store:   store,
		Section: NewSection(),
		Clock:   newTestClock(),
		Keep:    2,
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)

	if got := len(clipboard.History(0)); got != 1 {
		t.Fatalf("the pinned entry was not loaded: %d entries", got)
	}
	for _, text := range []string{"a", "b", "c"} {
		device.set(textReading(text))
		clipboard.Read(context.Background())
	}
	history := clipboard.History(0)
	// 上限约束未置顶条目：置顶条目位于它们之上，而不在上限之内。
	if len(history) != 3 {
		t.Fatalf("the history has %d entries, want the limit plus the pinned one", len(history))
	}
	found := false
	for _, entry := range history {
		if entry.Content == "keep me" {
			found = true
		}
	}
	if !found {
		t.Errorf("the pinned entry was evicted: %v", history)
	}
}

func TestClipboardIgnoresWhatIsNotShowable(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()

	// 无类型的读取不算一次读取。
	device.set(domain.ClipContent{})
	clipboard.Read(ctx)
	if got := len(clipboard.History(0)); got != 0 {
		t.Errorf("an empty reading produced %d entries", got)
	}
	if _, ok := clipboard.Latest(); ok {
		t.Error("an empty reading became the latest content")
	}

	// 读取失败会被跳过，而非当作空剪贴板。
	device.fail(errors.New("the clipboard is busy"))
	clipboard.Read(ctx)
	if _, ok := clipboard.Latest(); ok {
		t.Error("a failed read became the latest content")
	}
	if got := len(clipboard.History(0)); got != 0 {
		t.Errorf("a failed read produced %d entries", got)
	}
}

// 本应用自己的写入不得作为新复制回来。
func TestCopySuppressesTheLoop(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	read(clipboard, clock, device, "from the user")

	clock.advance(time.Second)
	if err := clipboard.Copy(ctx, textEntry("from boku")); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	wroteAt := clock.Now()
	if got := device.written(); len(got) != 1 || got[0] != "from boku" {
		t.Fatalf("the device was written %v", got)
	}

	// 设备现在持有本应用写入的内容：下一次读取是它自己的写入回来。
	clock.advance(time.Second)
	clipboard.Read(ctx)
	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2: %v", len(history), history)
	}
	if history[0].Content != "from boku" {
		t.Errorf("the written value is not at the top: %q", history[0].Content)
	}
	for _, entry := range history {
		if entry.Content == "from boku" && !entry.CopiedAt.Equal(wroteAt) {
			t.Errorf("the written entry was re-recorded at %s, want %s", entry.CopiedAt, wroteAt)
		}
	}

	// 抑制窗口过后，真正的新复制会重新被记录。
	clock.advance(3 * DefaultPollInterval)
	read(clipboard, clock, device, "something else")
	if got := len(clipboard.History(0)); got != 3 {
		t.Errorf("a new value after the window produced %d entries, want 3", got)
	}
}

func TestCopyRejectsWhatItCannotWrite(t *testing.T) {
	clipboard, _, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()
	if err := clipboard.Copy(ctx, textEntry("   ")); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Copy of blank text = %v, want ErrInvalidArgument", err)
	}
	if err := clipboard.Copy(ctx, textEntry(strings.Repeat("x", maxCopiedTextBytes+1))); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Copy of an over-long value = %v, want ErrInvalidArgument", err)
	}
}

// 点击已记录的文件复制会粘贴文件，而非其路径文本：这次复制就是文件列表，
// 把它放回去正是剪贴板的用途。
func TestCopyWritesFilesBackAsFiles(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	paths := []string{"/tmp/report.pdf", "/tmp/notes.txt"}
	read(clipboard, clock, device, "placeholder")
	device.set(domain.ClipContent{Kind: domain.ClipKindFile, Files: paths})
	clipboard.Read(ctx)

	var fileEntry domain.ClipboardEntry
	for _, entry := range clipboard.History(0) {
		if entry.Kind == domain.ClipKindFile {
			fileEntry = entry
		}
	}
	if fileEntry.ID == "" {
		t.Fatalf("the file list was not recorded: %v", clipboard.History(0))
	}
	if len(fileEntry.Files) != 2 {
		t.Fatalf("the recorded entry holds %v, want the paths", fileEntry.Files)
	}

	if err := clipboard.Copy(ctx, fileEntry); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if got := device.written(); len(got) != 0 {
		t.Errorf("the file list was written as text: %v", got)
	}
	files := device.writtenFiles()
	if len(files) != 1 || len(files[0]) != 2 || files[0][0] != paths[0] || files[0][1] != paths[1] {
		t.Errorf("the file list written back is %v, want %v", files, paths)
	}

	// 且它不会作为新条目回来：这是本应用写的。
	clock.advance(time.Second)
	clipboard.Read(ctx)
	if got := len(clipboard.History(0)); got != 2 {
		t.Errorf("the history has %d entries after the write, want 2", got)
	}
}

// 图片被记录且其字节保留，点击它会放回图片。过去点击会把「[图片]」这一行
// 放到剪贴板，那是哪里都粘不了的文本。
func TestCopyWritesAPictureBackAsAPicture(t *testing.T) {
	clock := newTestClock()
	clipboard, device, store, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	png := []byte("89504e470d0a1a0a-a-screenshot")
	device.set(domain.ClipContent{
		Kind:       domain.ClipKindImage,
		HasImage:   true,
		ImageBytes: len(png),
		Image:      png,
		ImageRef:   kit.ClipImageRef(png),
	})
	clipboard.Read(ctx)

	history := clipboard.History(0)
	if len(history) != 1 {
		t.Fatalf("the image was not recorded: %v", history)
	}
	entry := history[0]
	if entry.Kind != domain.ClipKindImage {
		t.Fatalf("the entry is %q, want an image", entry.Kind)
	}
	if entry.ImageBytes != len(png) {
		t.Errorf("the recorded size is %d, want %d", entry.ImageBytes, len(png))
	}
	if entry.ImageRef != kit.ClipImageRef(png) {
		t.Errorf("the entry keeps the picture as %q, want its hash", entry.ImageRef)
	}
	if kept := store.keptImages(); len(kept) != 1 || string(kept[entry.ImageRef]) != string(png) {
		t.Errorf("the image store holds %d pictures, want the bytes that were copied", len(kept))
	}
	if entry.Content != "[图片]" {
		t.Errorf("the line the interface shows is %q", entry.Content)
	}

	if err := clipboard.Copy(ctx, entry); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if written := device.writtenImages(); len(written) != 1 || string(written[0]) != string(png) {
		t.Errorf("the click wrote %d pictures, want the one that was copied", len(written))
	}
	if written := device.written(); len(written) != 0 {
		t.Errorf("the click wrote the text %v as well", written)
	}
}

// 字节已丢失的图片会如实说明，而非粘贴自己的名称：界面展示的是条目，剪贴板拿到的是内容。
func TestAPictureWithNoBytesIsNotPastedAsItsName(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	entry := domain.ClipboardEntry{
		ID: "one", Kind: domain.ClipKindImage, Content: "[图片]", Display: "[图片]",
		ContentHash: kit.ClipContentHash("image:"), ImageBytes: 4096,
	}
	err := clipboard.Copy(context.Background(), entry)
	if err == nil {
		t.Fatal("copying a picture with no bytes kept succeeded")
	}
	if !errors.Is(err, domain.ErrUnsupportedClipboardFormat) {
		t.Errorf("the error is %v, want an unsupported format", err)
	}
	if written := device.written(); len(written) != 0 {
		t.Errorf("the name was written as text: %v", written)
	}
}

// 丢弃条目会丢弃属于它的图片：只增不减的图片存储是一个堆满再也无法触及的截图的目录。
func TestEvictedEntriesTakeTheirPicturesWithThem(t *testing.T) {
	clipboard, device, store, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()
	// 比设置允许的最小历史多一张图片，使上限正好丢一张。
	copies := MinHistorySize + 1
	refs := make([]string, 0, copies)
	for i := 0; i < copies; i++ {
		png := []byte(fmt.Sprintf("89504e470d0a1a0a-picture-%d", i))
		ref := kit.ClipImageRef(png)
		refs = append(refs, ref)
		device.set(domain.ClipContent{
			Kind: domain.ClipKindImage, HasImage: true,
			Image: png, ImageBytes: len(png), ImageRef: ref,
		})
		clipboard.Read(ctx)
	}
	if kept := store.keptImages(); len(kept) != copies {
		t.Fatalf("the image store holds %d pictures, want all %d copies", len(kept), copies)
	}

	if err := clipboard.SetHistorySize(ctx, MinHistorySize); err != nil {
		t.Fatalf("SetHistorySize failed: %v", err)
	}
	// 条目先走、图片随后：删除发生在帧之后，因此测试等待它，而非假定它已完成。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(store.keptImages()) > MinHistorySize {
		time.Sleep(10 * time.Millisecond)
	}
	kept := store.keptImages()
	if len(kept) != MinHistorySize {
		t.Fatalf("the image store holds %d pictures, want the %d entries that are left",
			len(kept), MinHistorySize)
	}
	if _, ok := kept[refs[0]]; ok {
		t.Error("the picture of the entry that was dropped is still kept")
	}
	if _, ok := kept[refs[copies-1]]; !ok {
		t.Error("the picture of the newest entry was dropped with the oldest")
	}
}

// 同样大小的两张截图是两张图片：仅比较大小会让第二张看起来像第一张，
// 于是它从未被记录，而点击第一张会放回错误的图片。
func TestTwoScreenshotsOfTheSameSizeAreTwoEntries(t *testing.T) {
	one := Record(domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, ImageBytes: 4096, ImageRef: "sha256:one",
	})
	two := Record(domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, ImageBytes: 4096, ImageRef: "sha256:two",
	})
	if one.Hash == two.Hash {
		t.Errorf("two pictures of the same size are one entry: %q", one.Hash)
	}
	if one.Display != two.Display {
		t.Errorf("the two entries draw differently: %q and %q", one.Display, two.Display)
	}

	// 同一张图片复制两次是一条条目，无论以何种方式复制。
	again := Record(domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, ImageBytes: 4096, ImageRef: "sha256:one",
	})
	if again.Hash != one.Hash {
		t.Errorf("the same picture is two entries: %q and %q", one.Hash, again.Hash)
	}
}

// 平台拒绝的图片会被记录并附上说明，且该说明能历经历史往返而留存。
func TestAnUnreadablePictureIsRecordedWithItsReason(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	device.set(domain.ClipContent{
		Kind:        domain.ClipKindImage,
		HasImage:    true,
		Observation: "图片没能读出：unsupported clipboard format",
	})
	clipboard.Read(context.Background())

	history := clipboard.History(0)
	if len(history) != 1 {
		t.Fatalf("the picture was not recorded: %v", history)
	}
	if history[0].Note == "" {
		t.Error("the reason the picture could not be read was lost")
	}
	if history[0].ImageBytes != 0 {
		t.Errorf("a picture that was not read has a size: %d", history[0].ImageBytes)
	}
}

// 开关保存时的间隔就是构建抑制窗口所用的间隔：每两秒轮询一次的服务，
// 不能一度过后就误把自己的写入当作新内容。
func TestCopiedValueIsSuppressedForTheConfiguredInterval(t *testing.T) {
	clock := newTestClock()
	tools := newFakeTools()
	tools.section.Set(Settings{PollInterval: 2 * time.Second})
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock, Section: tools.section, Settings: tools})
	if got := clipboard.Settings().Interval(); got != 2*time.Second {
		t.Fatalf("the service reads an interval of %s, want the saved one", got)
	}
	if got := clipboard.interval; got != 2*time.Second {
		t.Errorf("the service polls every %s, want the saved interval", got)
	}

	read(clipboard, clock, device, "from the user")
	if err := clipboard.Copy(context.Background(), textEntry("from boku")); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	// 三秒后，在 2 × 2s 之内，却在 2 × 700ms 之外。
	clock.advance(3 * time.Second)
	clipboard.Read(context.Background())
	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2", len(history))
	}
	if history[0].Content != "from boku" {
		t.Errorf("the written value is not at the top: %q first", history[0].Content)
	}
}

func TestCopyReportsAWriteFailure(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	failure := errors.New("the clipboard is owned by another application")
	device.writeEr = failure
	if err := clipboard.Copy(context.Background(), textEntry("text")); !errors.Is(err, failure) {
		t.Errorf("Copy = %v, want the device's failure", err)
	}
}

// 监控是轮询循环的开关，关闭即完全不读。
func TestMonitoringOnlyReadsWhileItIsOn(t *testing.T) {
	clipboard, device, _, tools := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()

	if err := clipboard.SetRegionVisible(ctx, true); err != nil {
		t.Fatalf("SetRegionVisible failed: %v", err)
	}
	if clipboard.Show() {
		t.Error("the region is shown while monitoring is off")
	}
	if err := clipboard.SetMonitoring(ctx, true); err != nil {
		t.Fatalf("SetMonitoring(true) failed: %v", err)
	}
	if !clipboard.Show() {
		t.Error("monitoring is on and the region is not shown")
	}
	waitFor(t, "the first poll", func() bool { return device.readCount() >= 1 })

	if err := clipboard.SetMonitoring(ctx, false); err != nil {
		t.Fatalf("SetMonitoring(false) failed: %v", err)
	}
	if clipboard.Show() {
		t.Error("the region is still shown after monitoring was turned off")
	}
	after := device.readCount()
	time.Sleep(4 * MinPollInterval)
	if got := device.readCount(); got != after {
		t.Errorf("the clipboard was read %d more times after monitoring was turned off", got-after)
	}

	if err := tools.Load(ctx); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if tools.section.Current().Monitoring {
		t.Error("the switch was not saved as off")
	}
	if tools.saves == 0 {
		t.Error("flipping the switch wrote nothing")
	}
}

// 关闭监控会等待循环：开关报告时轮询 goroutine 已退出。
func TestMonitoringStopEndsTheLoop(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()
	if err := clipboard.SetMonitoring(ctx, true); err != nil {
		t.Fatalf("SetMonitoring(true) failed: %v", err)
	}
	waitFor(t, "the first poll", func() bool { return device.readCount() >= 1 })

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := clipboard.SetMonitoring(ctx, false); err != nil {
			t.Errorf("SetMonitoring(false) failed: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("turning monitoring off did not return")
	}

	// 再次启动可行：循环不会停在半关闭状态。
	if err := clipboard.SetMonitoring(ctx, true); err != nil {
		t.Fatalf("SetMonitoring(true) after a stop failed: %v", err)
	}
	before := device.readCount()
	waitFor(t, "a poll after restarting", func() bool { return device.readCount() > before })
}

func TestSetRegionVisibleIsIndependentOfMonitoring(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()

	if err := clipboard.SetRegionVisible(ctx, true); err != nil {
		t.Fatalf("SetRegionVisible failed: %v", err)
	}
	if clipboard.Settings().Monitoring {
		t.Error("showing the region turned monitoring on")
	}
	time.Sleep(2 * MinPollInterval)
	if got := device.readCount(); got != 0 {
		t.Errorf("the clipboard was read %d times while monitoring is off", got)
	}
	if !clipboard.Settings().RegionVisible {
		t.Error("the visibility switch was not saved")
	}
}

// 无法写入的设置文件不得连累开关。
func TestSwitchesSurviveASaveFailure(t *testing.T) {
	tools := newFakeTools()
	tools.saveErr = errors.New("the settings file is read-only")
	clipboard, _, _, _ := newTestClipboard(t, ClipboardOptions{Section: tools.section, Settings: tools})
	if err := clipboard.SetMonitoring(context.Background(), true); err == nil {
		t.Error("SetMonitoring reported no error for a failed save")
	}
	if !clipboard.Settings().Monitoring {
		t.Error("the switch the user flipped was dropped")
	}
}

// 记录数量范围约束历史：它是用户的选择，缩短会立刻丢弃不再容得下的条目，
// 并被写下来供下次启动使用。
func TestRecordRangeBoundsTheHistory(t *testing.T) {
	tools := newFakeTools()
	tools.section.Set(Settings{
		RegionVisible: true,
		Monitoring:    true,
		HistorySize:   MaxHistorySize,
	})
	clipboard, device, store, _ := newTestClipboard(t, ClipboardOptions{Section: tools.section, Settings: tools})
	if got := clipboard.Settings().KeepsHistory(); got != MaxHistorySize {
		t.Fatalf("the range starts at %d, want %d", got, MaxHistorySize)
	}
	ctx := context.Background()
	total := MaxHistorySize + 2
	for i := 0; i < total; i++ {
		device.set(textReading(fmt.Sprintf("copy %d", i)))
		clipboard.Read(ctx)
	}
	if got := len(clipboard.History(0)); got != MaxHistorySize {
		t.Fatalf("the history holds %d entries, want the range %d", got, MaxHistorySize)
	}

	if err := clipboard.SetHistorySize(ctx, MinHistorySize); err != nil {
		t.Fatalf("SetHistorySize failed: %v", err)
	}
	if got := len(clipboard.History(0)); got != MinHistorySize {
		t.Errorf("the history holds %d entries after the range was shortened, want %d",
			got, MinHistorySize)
	}
	waitFor(t, "the pruned history to be written", func() bool {
		return len(store.stored()) == MinHistorySize
	})
	if got := tools.section.Current().KeepsHistory(); got != MinHistorySize {
		t.Errorf("the saved range is %d, want %d", got, MinHistorySize)
	}

	// 设置表单给不出的数字是默认值，而非它自己的边界。
	if err := clipboard.SetHistorySize(ctx, 7); err != nil {
		t.Fatalf("SetHistorySize(7) failed: %v", err)
	}
	if got := clipboard.Settings().KeepsHistory(); got != DefaultHistorySize {
		t.Errorf("a range of 7 became %d, want the default %d", got, DefaultHistorySize)
	}
}

// 再次启动会遵循上次会话保存的范围，并在展示任何内容前裁剪读到的历史。
func TestRecordRangeIsReadAtStart(t *testing.T) {
	base := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	entries := make([]domain.ClipboardEntry, 0, MaxHistorySize)
	for i := 0; i < MaxHistorySize; i++ {
		entries = append(entries, domain.ClipboardEntry{
			ID:          fmt.Sprintf("entry-%02d", i),
			Content:     fmt.Sprintf("copy %d", i),
			ContentHash: fmt.Sprintf("sha256:%d", i),
			Kind:        domain.ClipKindText,
			CopiedAt:    base.Add(time.Duration(i) * time.Minute),
			CreatedAt:   base,
		})
	}
	tools := newFakeTools()
	tools.section.Set(Settings{HistorySize: MinHistorySize})
	clipboard, err := NewClipboard(ClipboardOptions{
		Device:   &fakeDevice{},
		Store:    &fakeStore{entries: entries},
		Section:  tools.section,
		Settings: tools,
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	history := clipboard.History(0)
	if len(history) != MinHistorySize {
		t.Fatalf("the history starts with %d entries, want the saved range %d",
			len(history), MinHistorySize)
	}
	// 保留的是条目中最新的那些。
	if want := fmt.Sprintf("copy %d", MaxHistorySize-1); history[0].Content != want {
		t.Errorf("the history starts with %q, want the newest copy %q", history[0].Content, want)
	}
}

// 读不出的历史文件不得妨碍工具正常工作。
func TestCorruptHistoryDoesNotStopTheTool(t *testing.T) {
	store := &fakeStore{loadErr: errors.New("the history file is not json")}
	device := &fakeDevice{}
	clipboard, err := NewClipboard(ClipboardOptions{Device: device, Store: store, Section: NewSection()})
	if err != nil {
		t.Fatalf("NewClipboard failed over an unreadable history: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	if clipboard.HistoryError() == nil {
		t.Error("the unreadable history was not reported")
	}
	device.set(textReading("still works"))
	clipboard.Read(context.Background())
	if got := len(clipboard.History(0)); got != 1 {
		t.Errorf("the tool recorded %d entries after an unreadable history, want 1", got)
	}
}

// 读不出的设置文件意味着用户的配置不可用，值得直接失败，而非猜测。
func TestUnreadableSettingsStopTheTool(t *testing.T) {
	store := &fakeStore{}
	device := &fakeDevice{}
	tools := newFakeTools()
	tools.loadErr = errors.New("the settings file is unreadable")
	if _, err := NewClipboard(ClipboardOptions{
		Device:   device,
		Store:    store,
		Section:  tools.section,
		Settings: tools,
	}); err == nil {
		t.Error("NewClipboard started over an unreadable settings file")
	}
}

// prune 是历史边界的所在，因此单独测试它，用测试决定的时间戳，
// 而非某次读取碰巧打上的。
func TestPruneOrdersAndBoundsTheHistory(t *testing.T) {
	base := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	entry := func(name string, ago time.Duration, pinned bool) domain.ClipboardEntry {
		return domain.ClipboardEntry{
			ID:          name,
			Content:     name,
			ContentHash: "sha256:" + name,
			Kind:        domain.ClipKindText,
			Pinned:      pinned,
			CopiedAt:    base.Add(-ago),
			CreatedAt:   base.Add(-ago),
		}
	}
	names := func(entries []domain.ClipboardEntry) []string {
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Content)
		}
		return out
	}

	cases := []struct {
		name    string
		keep    int
		entries []domain.ClipboardEntry
		want    []string
	}{
		{
			name:    "newest first",
			keep:    5,
			entries: []domain.ClipboardEntry{entry("old", 2*time.Hour, false), entry("new", time.Hour, false)},
			want:    []string{"new", "old"},
		},
		{
			name:    "over the limit drops the oldest",
			keep:    3,
			entries: []domain.ClipboardEntry{entry("one", 3*time.Hour, false), entry("two", 2*time.Hour, false), entry("three", time.Hour, false), entry("four", 0, false)},
			want:    []string{"four", "three", "two"},
		},
		{
			name:    "a pinned entry past the limit stays",
			keep:    2,
			entries: []domain.ClipboardEntry{entry("pinned", 5*time.Hour, true), entry("two", 2*time.Hour, false), entry("three", time.Hour, false), entry("four", 0, false)},
			want:    []string{"four", "three", "pinned"},
		},
		{
			name: "entries that cannot be shown go",
			keep: 5,
			entries: []domain.ClipboardEntry{
				entry("keep", time.Hour, false),
				{ID: "blank", ContentHash: "sha256:blank", Kind: domain.ClipKindText, CopiedAt: base},
				{ID: "nohash", Content: "x", Kind: domain.ClipKindText, CopiedAt: base},
			},
			want: []string{"keep"},
		},
		{
			name:    "an empty history stays empty",
			keep:    3,
			entries: nil,
			want:    []string{},
		},
	}
	for _, c := range cases {
		clipboard := &Clipboard{keep: c.keep}
		got := names(clipboard.prune(c.entries))
		if len(got) != len(c.want) {
			t.Errorf("%s: pruned to %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: pruned to %v, want %v", c.name, got, c.want)
				break
			}
		}
	}

	// 无论多少未置顶条目到来，置顶条目从不被驱逐。
	clipboard := &Clipboard{keep: 2}
	entries := []domain.ClipboardEntry{entry("pinned", 9*time.Hour, true)}
	for i := 0; i < 6; i++ {
		entries = append(entries, entry(string(rune('a'+i)), time.Duration(6-i)*time.Minute, false))
	}
	kept := clipboard.prune(entries)
	found := false
	for _, e := range kept {
		if e.Content == "pinned" {
			found = true
		}
	}
	if !found {
		t.Errorf("the pinned entry was evicted: %v", names(kept))
	}
	if len(kept) != 3 {
		t.Errorf("prune kept %d entries, want the limit plus the pinned one: %v", len(kept), names(kept))
	}
}

// 再次启动工具会遵循上次会话保存的开关。
func TestClipboardStartRespectsTheSavedSwitch(t *testing.T) {
	tools := newFakeTools()
	tools.section.Set(Settings{
		Monitoring:    true,
		RegionVisible: true,
	})
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Section: tools.section, Settings: tools})
	clipboard.Start(context.Background())
	waitFor(t, "the first poll from Start", func() bool { return device.readCount() >= 1 })
	if !clipboard.Show() {
		t.Error("a saved switch did not bring the region back")
	}
}
