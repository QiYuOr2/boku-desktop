package clipboard

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// The history store is the clipboard feature's own file. A test works on its
// own directory, never on a real user's data.

func newHistory(t *testing.T) (*History, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clipboard_history.json")
	store, err := NewHistory(path)
	if err != nil {
		t.Fatalf("NewHistory failed: %v", err)
	}
	return store, path
}

func TestNewHistoryRequiresAPath(t *testing.T) {
	if _, err := NewHistory(""); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("NewHistory without a path = %v, want ErrInvalidArgument", err)
	}
	if _, err := NewHistory("  "); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("NewHistory with a blank path = %v, want ErrInvalidArgument", err)
	}
}

func TestLoadEntriesWithoutAFileIsEmpty(t *testing.T) {
	store, _ := newHistory(t)
	entries, err := store.LoadEntries(context.Background())
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("LoadEntries = %v, want nothing", entries)
	}
}

func TestEntriesRoundTrip(t *testing.T) {
	store, path := newHistory(t)
	copied := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	want := []domain.ClipboardEntry{
		{
			ID:          "0198a1b2-0000-7000-8000-000000000001",
			Content:     "https://example.com",
			ContentHash: "sha256:abc",
			Kind:        domain.ClipKindURL,
			CopiedAt:    copied,
			CreatedAt:   copied.Add(time.Second),
		},
		{
			ID:          "0198a1b2-0000-7000-8000-000000000002",
			Content:     "[a.txt 等 2 个文件]",
			ContentHash: "sha256:def",
			Kind:        domain.ClipKindFile,
			Pinned:      true,
			CopiedAt:    copied.Add(-time.Hour),
			CreatedAt:   copied.Add(-time.Hour),
		},
	}
	if err := store.SaveEntries(context.Background(), want); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the history file was not written: %v", err)
	}
	var doc historyDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the history file is not the documented shape: %v", err)
	}
	if doc.Version != historyFileVersion || len(doc.Entries) != 2 {
		t.Fatalf("the history file holds %d entries at version %d", len(doc.Entries), doc.Version)
	}

	reopened, err := NewHistory(path)
	if err != nil {
		t.Fatalf("NewHistory failed: %v", err)
	}
	got, err := reopened.LoadEntries(context.Background())
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("LoadEntries returned %d entries, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].ID != want[i].ID || got[i].Content != want[i].Content ||
			got[i].ContentHash != want[i].ContentHash || got[i].Kind != want[i].Kind ||
			got[i].Pinned != want[i].Pinned || !got[i].CopiedAt.Equal(want[i].CopiedAt) ||
			!got[i].CreatedAt.Equal(want[i].CreatedAt) {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A file list, and what a reading noticed about itself, survive the round
// trip: the paths are what the entry writes back to the clipboard, and the
// note is what the interface shows about it.
func TestEntriesKeepWhatTheyWriteBack(t *testing.T) {
	store, path := newHistory(t)
	copied := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	want := []domain.ClipboardEntry{
		{
			ID:          "files-1",
			Content:     "/tmp/a.txt\n/tmp/b.txt",
			Display:     "[a.txt 等 2 个文件]",
			ContentHash: "sha256:files",
			Kind:        domain.ClipKindFile,
			Files:       []string{"/tmp/a.txt", "/tmp/b.txt"},
			CopiedAt:    copied,
			CreatedAt:   copied,
		},
		{
			ID:          "image-1",
			Content:     "[图片]",
			Display:     "[图片]",
			ContentHash: "sha256:image",
			Kind:        domain.ClipKindImage,
			ImageBytes:  4096,
			Note:        "图片没能读出：unsupported clipboard format",
			CopiedAt:    copied.Add(time.Second),
			CreatedAt:   copied.Add(time.Second),
		},
	}
	if err := store.SaveEntries(context.Background(), want); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}

	reopened, err := NewHistory(path)
	if err != nil {
		t.Fatalf("NewHistory failed: %v", err)
	}
	got, err := reopened.LoadEntries(context.Background())
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadEntries returned %d entries, want 2", len(got))
	}
	if len(got[0].Files) != 2 || got[0].Files[0] != "/tmp/a.txt" || got[0].Files[1] != "/tmp/b.txt" {
		t.Errorf("the file list came back as %v", got[0].Files)
	}
	if len(got[0].Display) == 0 {
		t.Error("the display line was lost")
	}
	if got[1].ImageBytes != 4096 {
		t.Errorf("the image size came back as %d, want 4096", got[1].ImageBytes)
	}
	if got[1].Note == "" {
		t.Error("the note about the unreadable picture was lost")
	}
}

// A history written before these fields existed still loads: what is missing
// is simply missing.
func TestAnOlderHistoryLoads(t *testing.T) {
	store, path := newHistory(t)
	old := `{"version":1,"entries":[{"id":"old","content":"hello","contentHash":"sha256:x",` +
		`"kind":"text","pinned":false,"copiedAt":"2026-02-03T09:00:00Z","createdAt":"2026-02-03T09:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("write the history file: %v", err)
	}
	got, err := store.LoadEntries(context.Background())
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("LoadEntries = %+v", got)
	}
	if got[0].Display != "" || got[0].ImageBytes != 0 || got[0].Note != "" {
		t.Errorf("an entry from an older file grew fields: %+v", got[0])
	}
}

// A history that cannot be read is an error here; the use case decides that
// it is survivable, which is the layer that knows the difference.
func TestUnreadableHistoryIsAnError(t *testing.T) {
	store, path := newHistory(t)
	if err := os.WriteFile(path, []byte(`{"version":1,"entries":[`), 0o600); err != nil {
		t.Fatalf("write the history file: %v", err)
	}
	if _, err := store.LoadEntries(context.Background()); err == nil {
		t.Error("LoadEntries accepted half a document")
	}
	if err := os.WriteFile(path, []byte(`{"version":7,"entries":[]}`), 0o600); err != nil {
		t.Fatalf("write the history file: %v", err)
	}
	if _, err := store.LoadEntries(context.Background()); err == nil {
		t.Error("LoadEntries accepted a history from another version")
	}
}

// A canceled context is answered with the context's error, like every other
// store in the application.
func TestContextIsHonoured(t *testing.T) {
	store, _ := newHistory(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.LoadEntries(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("LoadEntries = %v, want context.Canceled", err)
	}
	if err := store.SaveEntries(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("SaveEntries = %v, want context.Canceled", err)
	}
}

// Concurrent writes must not interleave into one file: the rename is what
// makes the file whole, and the lock is what keeps two writers from racing
// over the temporary file's name.
func TestConcurrentEntryWrites(t *testing.T) {
	store, path := newHistory(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entries := []domain.ClipboardEntry{{
				ID:          "id",
				Content:     strings.Repeat("x", 100+i),
				ContentHash: "sha256:x",
				Kind:        domain.ClipKindText,
				CopiedAt:    time.Now().UTC(),
			}}
			if err := store.SaveEntries(context.Background(), entries); err != nil {
				t.Errorf("SaveEntries failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Whatever landed last, the file is a readable document.
	if _, err := store.LoadEntries(context.Background()); err != nil {
		t.Fatalf("LoadEntries failed after concurrent writes: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s is missing: %v", path, err)
	}
}

// The file is written through a temporary file, so a directory left behind
// holds no half-written document.
func TestHistoryWriteLeavesNoTemporaryFile(t *testing.T) {
	store, path := newHistory(t)
	if err := store.SaveEntries(context.Background(), []domain.ClipboardEntry{{
		ID: "one", Content: "x", ContentHash: "sha256:x", Kind: domain.ClipKindText,
		CopiedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".atomic-") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

// A copied picture is kept beside the history, under the hash of its bytes, and
// comes back byte for byte: it is the only copy left once the clipboard moves
// on, and a click on the entry is what asks for it.
func TestAPictureIsKeptBesideTheHistory(t *testing.T) {
	store, path := newHistory(t)
	ctx := context.Background()
	png := []byte("89504e470d0a1a0a-a-screenshot")

	if err := store.SaveImage(ctx, "sha256:abc123", png); err != nil {
		t.Fatalf("SaveImage failed: %v", err)
	}
	kept, err := store.LoadImage(ctx, "sha256:abc123")
	if err != nil {
		t.Fatalf("LoadImage failed: %v", err)
	}
	if !reflect.DeepEqual(kept, png) {
		t.Errorf("the picture came back as %q, want what was kept", kept)
	}

	// It is a file of its own, named after the reference: a colon is the one
	// character a Windows file name cannot hold.
	dir := filepath.Join(filepath.Dir(path), imageDirName)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the picture was not kept in its own directory: %v", err)
	}
	if len(files) != 1 || files[0].Name() != "sha256-abc123.png" {
		t.Errorf("the image directory holds %v", fileNames(files))
	}

	// Saving the same picture twice is not a second file.
	if err := store.SaveImage(ctx, "sha256:abc123", png); err != nil {
		t.Fatalf("SaveImage failed the second time: %v", err)
	}
	files, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the directory again: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("the same picture was kept %d times", len(files))
	}

	if err := store.DeleteImage(ctx, "sha256:abc123"); err != nil {
		t.Fatalf("DeleteImage failed: %v", err)
	}
	if _, err := store.LoadImage(ctx, "sha256:abc123"); err == nil {
		t.Error("a deleted picture was read back")
	}
	// Deleting it twice is not a failure: an entry can be dropped once.
	if err := store.DeleteImage(ctx, "sha256:abc123"); err != nil {
		t.Errorf("deleting a picture that is already gone: %v", err)
	}
}

// A reference is a file name, and a file can be edited: one that would name a
// file outside the image directory is refused rather than joined.
func TestAPictureReferenceCannotLeaveItsDirectory(t *testing.T) {
	store, _ := newHistory(t)
	ctx := context.Background()
	for _, ref := range []string{"", "..", "../secrets", `..\secrets`, "/etc/passwd", ".hidden", strings.Repeat("a", 201)} {
		if err := store.SaveImage(ctx, ref, []byte("x")); err == nil {
			t.Errorf("a picture was kept under the reference %q", ref)
		}
		if _, err := store.LoadImage(ctx, ref); err == nil {
			t.Errorf("a picture was read under the reference %q", ref)
		}
	}
	if err := store.SaveImage(ctx, "sha256:abc", nil); err == nil {
		t.Error("a picture with no bytes was kept")
	}
}

// A picture is dropped with its entry, which is also how the store stops
// growing: an image directory that only grows is a directory of screenshots
// nobody will see again.
func TestDroppedEntriesTakeTheirPicturesWithThem(t *testing.T) {
	store, path := newHistory(t)
	dir := filepath.Join(filepath.Dir(path), imageDirName)
	if err := store.SaveImage(context.Background(), "sha256:one", []byte("a")); err != nil {
		t.Fatalf("SaveImage failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sha256-one.png")); err != nil {
		t.Fatalf("the picture was not written: %v", err)
	}
	if err := store.DeleteImage(context.Background(), "sha256:one"); err != nil {
		t.Fatalf("DeleteImage failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sha256-one.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the picture is still there: %v", err)
	}
}

// A picture reference survives the history file: it is a short name, and the
// bytes are beside it.
func TestAPictureReferenceSurvivesTheHistoryFile(t *testing.T) {
	store, path := newHistory(t)
	ctx := context.Background()
	entry := domain.ClipboardEntry{
		ID: "one", Content: "[图片]", Display: "[图片]", Kind: domain.ClipKindImage,
		ContentHash: "sha256:abc", ImageBytes: 4, ImageRef: "sha256:abcdef",
		CopiedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveEntries(ctx, []domain.ClipboardEntry{entry}); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the history file: %v", err)
	}
	if !strings.Contains(string(data), `"imageRef": "sha256:abcdef"`) {
		t.Errorf("the history file does not name the picture: %s", data)
	}
	// No bytes in the history file itself: a screenshot is megabytes and this
	// file is written whole on every copy.
	if strings.Contains(string(data), "image\":") {
		t.Errorf("the history file holds image bytes: %s", data)
	}

	reloaded, err := NewHistory(path)
	if err != nil {
		t.Fatalf("NewHistory failed: %v", err)
	}
	entries, err := reloaded.LoadEntries(ctx)
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(entries) != 1 || entries[0].ImageRef != "sha256:abcdef" {
		t.Errorf("the reference did not survive the file: %+v", entries)
	}
}

// fileNames is what a directory listing holds, for a test's message.
func fileNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
