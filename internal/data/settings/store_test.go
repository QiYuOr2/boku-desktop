package settings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// The settings store is two files: what the user switched on, and what was
// copied. A test works on its own directory, never on a real user's data.

func newStore(t *testing.T) (*Settings, string, string) {
	t.Helper()
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	historyPath := filepath.Join(dir, "clipboard_history.json")
	store, err := New(settingsPath, historyPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return store, settingsPath, historyPath
}

func TestNewRequiresAPath(t *testing.T) {
	if _, err := New("", ""); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("New without paths = %v, want ErrInvalidArgument", err)
	}
	if _, err := New("  ", ""); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("New with a blank path = %v, want ErrInvalidArgument", err)
	}
}

// The defaults are what a user who has never opened the settings gets: both
// clipboard switches off.
func TestLoadWithoutAFileGivesTheDefaults(t *testing.T) {
	store, _, _ := newStore(t)
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if settings.ClipboardRegionVisible || settings.ClipboardMonitoring {
		t.Errorf("the defaults enable the clipboard: %+v", settings)
	}
	if settings.Interval() != domain.ClipboardDefaultPollInterval {
		t.Errorf("interval = %s, want %s", settings.Interval(), domain.ClipboardDefaultPollInterval)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	want := domain.ToolSettings{
		ClipboardRegionVisible: true,
		ClipboardMonitoring:    true,
		ClipboardPollInterval:  1500 * time.Millisecond,
		ClipboardHistorySize:   domain.ClipboardMaxHistorySize,
	}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// The file is there, and it is what a person would read.
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("the settings file was not written: %v", err)
	}
	if !strings.Contains(string(data), `"clipboardMonitoring": true`) {
		t.Errorf("the settings file does not name the switch:\n%s", data)
	}
	if !strings.Contains(string(data), `"clipboardHistorySize": 50`) {
		t.Errorf("the settings file does not name the record range:\n%s", data)
	}

	reopened, err := New(settingsPath, settingsPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	got, err := reopened.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

// A file written before the record range existed, and one a person edited to
// a number that is not one of the ranges, both come back as the default:
// starting with a range nobody chose is better than refusing to start.
func TestLoadNormalizesTheRecordRange(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	for _, written := range []string{
		`{"version":1,"tools":{"clipboardMonitoring":true}}`,
		`{"version":1,"tools":{"clipboardHistorySize":7}}`,
		`{"version":1,"tools":{"clipboardHistorySize":1000}}`,
	} {
		if err := os.WriteFile(settingsPath, []byte(written), 0o600); err != nil {
			t.Fatalf("write the settings file: %v", err)
		}
		settings, err := store.Load(context.Background())
		if err != nil {
			t.Fatalf("Load(%s) failed: %v", written, err)
		}
		if got := settings.HistorySize(); got != domain.ClipboardDefaultHistorySize {
			t.Errorf("Load(%s) gave the record range %d, want the default %d",
				written, got, domain.ClipboardDefaultHistorySize)
		}
	}
}

// A hand-edited file with an interval below the floor is brought back to it
// rather than refused, and a file written by another version is refused.
func TestLoadNormalizesAndRefusesOtherVersions(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	if err := os.WriteFile(settingsPath, []byte(`{"version":1,"tools":{"clipboardPollIntervalMs":1}}`), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if settings.Interval() != domain.ClipboardDefaultPollInterval {
		t.Errorf("interval = %s, want the default after normalization", settings.Interval())
	}

	if err := os.WriteFile(settingsPath, []byte(`{"version":99,"tools":{}}`), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	if _, err := store.Load(context.Background()); err == nil {
		t.Error("Load accepted a settings file from another version")
	}

	if err := os.WriteFile(settingsPath, []byte(`not json`), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	if _, err := store.Load(context.Background()); err == nil {
		t.Error("Load accepted a settings file that is not JSON")
	}
}

func TestLoadEntriesWithoutAFileIsEmpty(t *testing.T) {
	store, _, _ := newStore(t)
	entries, err := store.LoadEntries(context.Background())
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("LoadEntries = %v, want nothing", entries)
	}
}

func TestEntriesRoundTrip(t *testing.T) {
	store, _, historyPath := newStore(t)
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
	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("the history file was not written: %v", err)
	}
	var doc historyDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the history file is not the documented shape: %v", err)
	}
	if doc.Version != fileVersion || len(doc.Entries) != 2 {
		t.Fatalf("the history file holds %d entries at version %d", len(doc.Entries), doc.Version)
	}

	reopened, err := New("", historyPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
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
	store, _, historyPath := newStore(t)
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

	reopened, err := New("", historyPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
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
	store, _, historyPath := newStore(t)
	old := `{"version":1,"entries":[{"id":"old","content":"hello","contentHash":"sha256:x",` +
		`"kind":"text","pinned":false,"copiedAt":"2026-02-03T09:00:00Z","createdAt":"2026-02-03T09:00:00Z"}]}`
	if err := os.WriteFile(historyPath, []byte(old), 0o600); err != nil {
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
	store, _, historyPath := newStore(t)
	if err := os.WriteFile(historyPath, []byte(`{"version":1,"entries":[`), 0o600); err != nil {
		t.Fatalf("write the history file: %v", err)
	}
	if _, err := store.LoadEntries(context.Background()); err == nil {
		t.Error("LoadEntries accepted half a document")
	}
	if err := os.WriteFile(historyPath, []byte(`{"version":7,"entries":[]}`), 0o600); err != nil {
		t.Fatalf("write the history file: %v", err)
	}
	if _, err := store.LoadEntries(context.Background()); err == nil {
		t.Error("LoadEntries accepted a history from another version")
	}
}

// A canceled context is answered with the context's error, like every other
// store in the application.
func TestContextIsHonoured(t *testing.T) {
	store, _, _ := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Load = %v, want context.Canceled", err)
	}
	if _, err := store.LoadEntries(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("LoadEntries = %v, want context.Canceled", err)
	}
	if err := store.Save(ctx, domain.ToolSettings{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Save = %v, want context.Canceled", err)
	}
	if err := store.SaveEntries(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("SaveEntries = %v, want context.Canceled", err)
	}
}

// Both parts have defaults, so a file that is not there is not an error; but
// a store with no file at all is an assembly mistake, and the two names are
// how the application is pointed at the user data directory.
func TestBothPathsAreRequiredToBeUsable(t *testing.T) {
	// One part may be absent: the settings file alone is a working store.
	store, err := New("", filepath.Join(t.TempDir(), "clipboard_history.json"))
	if err != nil {
		t.Fatalf("New failed with a history path alone: %v", err)
	}
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed without a settings file: %v", err)
	}
	if settings.ClipboardMonitoring || settings.ClipboardRegionVisible {
		t.Errorf("a store without a settings file reported a switch: %+v", settings)
	}
	entries := []domain.ClipboardEntry{{
		ID:          "one",
		Content:     "x",
		ContentHash: "sha256:x",
		Kind:        domain.ClipKindText,
		CopiedAt:    time.Now().UTC(),
	}}
	if err := store.SaveEntries(context.Background(), entries); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}
	got, err := store.LoadEntries(context.Background())
	if err != nil {
		t.Fatalf("LoadEntries failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("LoadEntries = %v, want the entry back", got)
	}
}

// Concurrent writes must not interleave into one file: the rename is what
// makes the file whole, and the lock is what keeps two writers from racing
// over the temporary file's name.
func TestConcurrentWrites(t *testing.T) {
	store, settingsPath, historyPath := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			settings := domain.ToolSettings{
				ClipboardRegionVisible: i%2 == 0,
				ClipboardMonitoring:    i%2 == 1,
			}
			if err := store.Save(context.Background(), settings); err != nil {
				t.Errorf("Save failed: %v", err)
			}
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

	// Whatever landed last, both files are readable documents.
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed after concurrent writes: %v", err)
	}
	if settings.Interval() == 0 {
		t.Error("the settings file did not survive the writes")
	}
	if _, err := store.LoadEntries(context.Background()); err != nil {
		t.Fatalf("LoadEntries failed after concurrent writes: %v", err)
	}
	for _, path := range []string{settingsPath, historyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s is missing: %v", path, err)
		}
	}
}

// The file is written through a temporary file, so a directory left behind
// holds no half-written document.
func TestWriteLeavesNoTemporaryFile(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	if err := store.Save(context.Background(), domain.ToolSettings{ClipboardRegionVisible: true}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(settingsPath))
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".settings-") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}
