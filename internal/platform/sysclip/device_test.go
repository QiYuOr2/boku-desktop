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

// The platform half of this package cannot be exercised without a session,
// so what is tested here is everything that decides around it: what happens
// when the main thread never answers, and what a dispatcher that has already
// gone away returns, and how a reading is classified.

// A dispatcher that never answers must not hold the poll: the reading is
// dropped and the next poll tries again.
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
	// A write is dropped the same way rather than blocking a window close.
	if err := device.WriteText(context.Background(), "text"); err != nil {
		t.Errorf("WriteText = %v, want nil", err)
	}
}

// A dispatcher that has already gone away — the application is quitting —
// runs nothing and says so.
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

// Without a dispatcher the calls run here, which is what a test and a
// single-threaded assembly need.
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

// A canceled context ends the wait rather than leaving the caller there.
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

// A picture the platform could not hand over is still recorded — the user
// copied something — and the reading says why, in the application's words.
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

	// A file list with an unreadable picture beside it says so as well: the
	// file is still what gets copied back.
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

	// A complete reading carries no note, and carries the bitmap itself: a
	// picture that was read can be put back, which is what a click on it in
	// the history does.
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

// A picture this device read is handed out again while it is still fresh, so
// that a screenshot sitting on the clipboard is not decoded and encoded on
// every poll.
func TestAPictureIsHandedOutAgainWhileItIsFresh(t *testing.T) {
	now := time.Now()
	device := New(nil)
	device.now = func() time.Time { return now }

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

	// Past the window it is read again rather than handed out forever.
	now = now.Add(pictureFresh)
	if device.pictureStillThere() {
		t.Error("a picture older than the window is still handed out")
	}
}

// Two runs of the same bytes are the same picture: the name a reading carries
// has to be the one the loop already knows, or every poll records a new copy.
func TestARememberedPictureKeepsItsName(t *testing.T) {
	png := []byte("89504e470d0a1a0a")
	now := time.Now()
	device := New(nil)
	device.now = func() time.Time { return now }
	device.rememberPicture(png, now)

	if got, want := device.pictureRef, kit.ClipImageRef(png); got != want {
		t.Errorf("pictureRef = %q, want %q", got, want)
	}
	// Remembering the same bytes again is the same name.
	device.rememberPicture(png, now)
	if got, want := device.pictureRef, kit.ClipImageRef(png); got != want {
		t.Errorf("pictureRef changed to %q, want %q", got, want)
	}
}

// A reading that found no picture, or could not read one, leaves nothing to
// hand out: a picture kept from earlier would have the next reading claim to
// hold a picture that is not there.
func TestForgettingAPicture(t *testing.T) {
	now := time.Now()
	device := New(nil)
	device.now = func() time.Time { return now }
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

// A reading that was handed a picture it did not read still has to name it:
// the loop identifies the reading by that name, and a reading that carried
// none would look like a different picture and be recorded a second time.
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

// A file list read from the platform's own file URLs: the paths that come
// out are what a copy back writes.
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
