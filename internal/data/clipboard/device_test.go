package clipboard

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
