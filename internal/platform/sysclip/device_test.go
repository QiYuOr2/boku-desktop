package sysclip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"

	"boku/internal/domain"
	"boku/internal/kit"
)

// 本包与平台相关的那一半没有会话无法执行，所以这里测的是围绕它的全部决策：
// 主线程永不答复时如何、已消失的 dispatcher 返回什么、以及一次读取如何归类。

// 永不答复的 dispatcher 不得卡住轮询：读取被丢弃，下一次轮询重试。
func TestUnansweredDispatchIsDropped(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	device := New(func(func()) bool {
		<-blocked
		return true
	})
	device.timeout = 50 * time.Millisecond

	start := time.Now()
	_, err := device.Read(context.Background())
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Read waited %s for a main thread that never answered", elapsed)
	}
	if !errors.Is(err, domain.ErrUnsupportedClipboardFormat) {
		t.Errorf("Read = %v, want ErrUnsupportedClipboardFormat", err)
	}
	// 一次写入也同样被丢弃，而不是阻塞窗口关闭。
	if err := device.WriteText(context.Background(), "text"); err != nil {
		t.Errorf("WriteText = %v, want nil", err)
	}
}

// 已经消失的 dispatcher（应用正在退出）什么都不执行并如实返回。
func TestDispatchAfterShutdown(t *testing.T) {
	device := New(func(func()) bool { return false })
	device.timeout = time.Second

	if _, err := device.Read(context.Background()); !errors.Is(err, domain.ErrUnsupportedClipboardFormat) {
		t.Errorf("Read = %v, want ErrUnsupportedClipboardFormat", err)
	}
	if err := device.WriteText(context.Background(), "text"); err != nil {
		t.Errorf("WriteText = %v, want nil", err)
	}
}

// 没有 dispatcher 时调用就地运行，这是测试和单线程装配所需的。
func TestNilDispatcherRunsInPlace(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	device := New(nil)
	if device.call(context.Background(), func() {
		mu.Lock()
		runs++
		mu.Unlock()
	}) != true {
		t.Fatal("call reported that it did not run")
	}
	if runs != 1 {
		t.Errorf("the call ran %d times, want 1", runs)
	}
}

// 被取消的 context 结束等待，而不是把调用方留在那里。
func TestCanceledContextEndsTheWait(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	device := New(func(func()) bool {
		<-blocked
		return true
	})
	device.timeout = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := device.Read(ctx); !errors.Is(err, domain.ErrUnsupportedClipboardFormat) {
		t.Errorf("Read = %v, want ErrUnsupportedClipboardFormat", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Read waited %s after its context was canceled", elapsed)
	}
}

func TestNewUsesTheDefaultTimeout(t *testing.T) {
	device := New(nil)
	if device.timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want %s", device.timeout, DefaultTimeout)
	}
	if device.now == nil {
		t.Error("the device has no clock")
	}
}

// 平台无法交出的图片仍被记录——用户确实复制了东西——并由读取用应用自己的话说明原因。
func TestAPictureThatCouldNotBeReadIsNoted(t *testing.T) {
	content := Convert(platformFacts{
		formats:  []transfer.Format{transfer.PNG},
		hasImage: true,
		imageErr: domain.ErrUnsupportedClipboardFormat,
	})
	if content.Kind != domain.ClipKindImage {
		t.Fatalf("kind = %q, want an image", content.Kind)
	}
	if content.ImageBytes != 0 {
		t.Errorf("imageBytes = %d, want none", content.ImageBytes)
	}
	if content.Observation == "" {
		t.Fatal("a picture that could not be read was not noted")
	}
	if !strings.Contains(content.Observation, "图片") {
		t.Errorf("the note says %q", content.Observation)
	}

	// 旁边有读不出的图片的文件列表也会说明：文件仍是被复制回去的内容。
	withFiles := Convert(platformFacts{
		formats:  []transfer.Format{transfer.FileList, transfer.PNG},
		files:    []string{"/tmp/a.png"},
		hasImage: true,
		imageErr: domain.ErrUnsupportedClipboardFormat,
	})
	if withFiles.Kind != domain.ClipKindFile {
		t.Fatalf("kind = %q, want a file list", withFiles.Kind)
	}
	if withFiles.Observation == "" {
		t.Error("a file list with an unreadable picture was not noted")
	}

	// 完整的读取不带说明，且携带位图本身：读到的图片可以被放回，这正是历史中
	// 点击它时所做的。
	complete := Convert(platformFacts{
		formats:  []transfer.Format{transfer.PNG},
		hasImage: true,
		image:    []byte("89504e470d0a1a0a"),
	})
	if complete.Observation != "" {
		t.Errorf("a complete reading was noted: %q", complete.Observation)
	}
	if complete.ImageBytes != len("89504e470d0a1a0a") {
		t.Errorf("imageBytes = %d, want the size of the bitmap that was read", complete.ImageBytes)
	}
	if string(complete.Image) != "89504e470d0a1a0a" {
		t.Errorf("image = %q, want the bytes the platform handed over", complete.Image)
	}
}

func TestHasFormat(t *testing.T) {
	offered := []transfer.Format{transfer.Text, transfer.FileList}
	if !hasFormat(offered, transfer.Text) {
		t.Error("text was offered and not seen")
	}
	if hasFormat(offered, transfer.PNG) {
		t.Error("a bitmap that was not offered was reported")
	}
}

// 本设备读过的图片在仍新鲜期间再次交出，使留在剪贴板上的截图不必每次轮询都
// 解码再编码。
func TestAPictureIsHandedOutAgainWhileItIsFresh(t *testing.T) {
	now := time.Now()
	device := New(nil)
	device.now = func() time.Time { return now }
	device.changeCount = func() uint32 { return 7 }

	if device.pictureStillThere() {
		t.Fatal("a device that has read nothing claims to hold a picture")
	}

	device.rememberPicture([]byte("89504e470d0a1a0a"), now)
	if !device.pictureStillThere() {
		t.Fatal("a picture that was just read is not handed out again")
	}
	if device.pictureRef == "" {
		t.Error("the picture that was remembered has no name")
	}

	// 超出窗口就重新读取，而不是永远交出。
	now = now.Add(pictureFresh)
	if device.pictureStillThere() {
		t.Error("a picture older than the window is still handed out")
	}
}

// 窗口仍开着时发生的一次复制是新的图片：仅凭时间看不出，需靠剪贴板自身的写
// 计数器。没有它，窗口内截的第二张会被当作第一张交出、永不记录。
func TestAClipboardWriteInvalidatesThePicture(t *testing.T) {
	now := time.Now()
	count := uint32(7)
	device := New(nil)
	device.now = func() time.Time { return now }
	device.changeCount = func() uint32 { return count }

	device.rememberPicture([]byte("89504e470d0a1a0a"), now)
	if !device.pictureStillThere() {
		t.Fatal("a picture that was just read is not handed out again")
	}

	count++
	if device.pictureStillThere() {
		t.Error("a picture from before a clipboard write is still handed out")
	}
}

// 同样字节的两次运行是同一张图片：读取携带的名字必须是循环已认识的那个，否则
// 每次轮询都会记录一份新副本。
func TestARememberedPictureKeepsItsName(t *testing.T) {
	png := []byte("89504e470d0a1a0a")
	now := time.Now()
	device := New(nil)
	device.now = func() time.Time { return now }
	device.changeCount = func() uint32 { return 7 }
	device.rememberPicture(png, now)

	if got, want := device.pictureRef, kit.ClipImageRef(png); got != want {
		t.Errorf("pictureRef = %q, want %q", got, want)
	}
	// 再次记住同样的字节仍是同一个名字。
	device.rememberPicture(png, now)
	if got, want := device.pictureRef, kit.ClipImageRef(png); got != want {
		t.Errorf("pictureRef changed to %q, want %q", got, want)
	}
}

// 未发现图片、或读不出图片的读取不留下可交出的东西：否则从早前留存的图片会让
// 下一次读取声称持有并不存在的图片。
func TestForgettingAPicture(t *testing.T) {
	now := time.Now()
	device := New(nil)
	device.now = func() time.Time { return now }
	device.changeCount = func() uint32 { return 7 }
	device.rememberPicture([]byte("89504e470d0a1a0a"), now)

	device.forgetPicture()
	if device.pictureStillThere() {
		t.Error("a forgotten picture is still handed out")
	}
	if device.picture != nil || device.pictureRef != "" || !device.pictureStamp.IsZero() {
		t.Errorf("forgetting left something behind: %v %q %v",
			device.picture, device.pictureRef, device.pictureStamp)
	}
}

// 被直接交出、并非本次读到的图片仍须命名：循环用该名字识别这次读取，若不带名字
// 会被当成另一张图片而被记录两次。
func TestAReusedPictureKeepsItsNameInTheReading(t *testing.T) {
	const ref = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	content := Convert(platformFacts{
		formats:  []transfer.Format{"System.Drawing.Bitmap", transfer.PNG},
		hasImage: true,
		image:    []byte("89504e470d0a1a0a"),
		imageRef: ref,
	})
	if content.ImageRef != ref {
		t.Errorf("imageRef = %q, want the name the device gave it", content.ImageRef)
	}
	if content.Kind != domain.ClipKindImage {
		t.Errorf("kind = %q, want an image", content.Kind)
	}
	if content.ImageBytes != len("89504e470d0a1a0a") {
		t.Errorf("imageBytes = %d, want the size of the picture", content.ImageBytes)
	}
	if content.Observation != "" {
		t.Errorf("a picture that was read was noted: %q", content.Observation)
	}
}

// 从平台自身的文件 URL 读到的文件列表：输出的路径正是复制回去时所写的内容。
func TestFilePathsFromAReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("create the fixture file: %v", err)
	}
	data, err := transfer.FileData(path)
	if err != nil {
		t.Fatalf("FileData: %v", err)
	}
	files, err := data.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 || files[0] != path {
		t.Errorf("Files = %v, want [%s]", files, path)
	}
}
