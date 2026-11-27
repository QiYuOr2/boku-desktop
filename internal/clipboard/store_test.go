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

// 历史存储是剪贴板功能自己的文件。测试在自己的目录上工作，绝不触碰真实用户数据。

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

// 文件列表，以及读取时注意到的关于自身的信息，能历经往返而留存：
// 路径是条目写回剪贴板的内容，说明是界面展示的信息。
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

// 在这些字段存在之前写成的历史仍能加载：缺失的就只是缺失。
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

// 读不出的历史在这里是错误；由用例层决定它可被容忍，那一层才懂得区分。
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

// 被取消的 context 以 context 的错误回应，与应用中其它存储一致。
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

// 并发写入不得交织进同一个文件：重命名使文件完整，锁使两个写入者不会在同一
// 临时文件名上竞争。
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

	// 无论最后落地的是哪个，文件都是可读的文档。
	if _, err := store.LoadEntries(context.Background()); err != nil {
		t.Fatalf("LoadEntries failed after concurrent writes: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s is missing: %v", path, err)
	}
}

// 文件经临时文件写入，因此留下的目录里不会有半写的文档。
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

// 复制的图片保存在历史旁、以其字节哈希为名，并逐字节返回：剪贴板移开后它是
// 唯一的副本，点击条目就是索取它。
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

	// 它是一个独立文件，以引用命名：冒号是 Windows 文件名唯一不能包含的字符。
	dir := filepath.Join(filepath.Dir(path), imageDirName)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the picture was not kept in its own directory: %v", err)
	}
	if len(files) != 1 || files[0].Name() != "sha256-abc123.png" {
		t.Errorf("the image directory holds %v", fileNames(files))
	}

	// 保存同一张图片两次不是第二个文件。
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
	// 删除两次不算失败：条目可能只被丢弃一次。
	if err := store.DeleteImage(ctx, "sha256:abc123"); err != nil {
		t.Errorf("deleting a picture that is already gone: %v", err)
	}
}

// 引用会成为文件名，而文件是可被编辑的：会指向图片目录之外的引用会被拒绝，而非拼接。
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

// 图片随其条目一起丢弃，这也是存储停止增长的方式：只增不减的图片目录就是一个
// 堆满再也看不到的截图的目录。
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

// 图片引用能历经历史文件留存：它是一个短名称，字节放在它旁边。
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
	// 历史文件本身不含字节：截图动辄数兆，而本文件每次复制都整体重写。
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

// fileNames 是目录列表中的名字，供测试消息使用。
func fileNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
