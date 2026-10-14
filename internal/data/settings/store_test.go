package settings

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

// A settings file from before the translation tool existed has none of its
// keys, and the two Japanese annotations default to on: reading an absent key
// as "false" is reading it as a choice the user never made, and it left the
// translation window with no readings at all.
//
// This is the file the user had (a clipboard-only settings.json) with the two
// switches still on, which is what the window needs to ask for annotations.
func TestAnOldSettingsFileKeepsTheAnnotationsOn(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	old := `{
  "version": 1,
  "tools": {
    "clipboardRegionVisible": true,
    "clipboardMonitoring": true,
    "clipboardPollIntervalMs": 700,
    "clipboardHistorySize": 30
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(old), 0o600); err != nil {
		t.Fatalf("writing the old settings file: %v", err)
	}
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !settings.Translation.Furigana {
		t.Error("an absent translateFurigana key turned the readings off")
	}
	if !settings.Translation.LoanwordGloss {
		t.Error("an absent translateLoanwordGloss key turned the loanword glosses off")
	}
	if !settings.Translation.Enabled {
		t.Error("an absent translateEnabled key turned the tool off")
	}
}

// A key that IS there is a choice, whichever way it goes.
func TestAStoredAnnotationKeyIsRespected(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	stored := `{
  "version": 1,
  "tools": {
    "translateEnabled": true,
    "translateFurigana": false,
    "translateLoanwordGloss": true
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(stored), 0o600); err != nil {
		t.Fatalf("writing the settings file: %v", err)
	}
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if settings.Translation.Furigana {
		t.Error("a stored translateFurigana: false was not respected")
	}
	if !settings.Translation.LoanwordGloss {
		t.Error("a stored translateLoanwordGloss: true was not respected")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	// want is what the store gives back, so it is normalized the way the
	// store normalizes what it reads: a stored setting is never a partly
	// filled struct.
	want := domain.ToolSettings{
		ClipboardRegionVisible: true,
		ClipboardMonitoring:    true,
		ClipboardPollInterval:  1500 * time.Millisecond,
		ClipboardHistorySize:   domain.ClipboardMaxHistorySize,
		Panels:                 map[string]bool{"0198a1b2-0000-7000-8000-000000000002": false},
		Translation: domain.TranslationSettings{
			Enabled:       true,
			Provider:      "custom",
			Furigana:      false,
			LoanwordGloss: true,
			Pinned:        true,
			CloseDelay:    900 * time.Millisecond,
			Custom: domain.CustomTranslatorSettings{
				BaseURL:     "https://api.example.com/v1",
				DisplayName: "本地 Ollama",
			},
		},
	}.Normalize()
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
	if !strings.Contains(string(data), `"translateProvider": "custom"`) {
		t.Errorf("the settings file does not name the translation provider:\n%s", data)
	}
	if !strings.Contains(string(data), `"baseURL": "https://api.example.com/v1"`) {
		t.Errorf("the settings file does not carry the custom address:\n%s", data)
	}
	if !strings.Contains(string(data), `"0198a1b2-0000-7000-8000-000000000002": false`) {
		t.Errorf("the settings file does not carry the panel the user turned off:\n%s", data)
	}
	// Nothing sensitive is written here: the key belongs to the credential
	// store, and a settings file is a file the user may put in a cloud
	// folder.
	if strings.Contains(string(data), "sk-") {
		t.Errorf("the settings file looks like it holds a credential:\n%s", data)
	}

	reopened, err := New(settingsPath, settingsPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	got, err := reopened.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
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

// A file written before the panel switches existed, or by a build that had
// none, says nothing about any panel — and saying nothing means every panel is
// on. Reading an absent key as a choice would turn a panel off for everybody
// who has never opened that page.
func TestAFileWithoutPanelChoicesTurnsNothingOff(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	for _, written := range []string{
		`{"version":1,"tools":{}}`,
		`{"version":1,"tools":{"clipboardMonitoring":true}}`,
		`{"version":1,"tools":{"panels":{}}}`,
	} {
		if err := os.WriteFile(settingsPath, []byte(written), 0o600); err != nil {
			t.Fatalf("write the settings file: %v", err)
		}
		settings, err := store.Load(context.Background())
		if err != nil {
			t.Fatalf("Load(%s) failed: %v", written, err)
		}
		if len(settings.Panels) != 0 {
			t.Errorf("Load(%s) invented the choices %v", written, settings.Panels)
		}
		if !settings.PanelOn("panel-1") {
			t.Errorf("Load(%s) turned a panel off", written)
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

// The translation switch defaults to on, and a settings file that predates
// the tool has no key for it: reading that file must not look like a user
// who turned translation off.
func TestLoadDefaultsTheTranslationSwitch(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	for _, written := range []string{
		`{"version":1,"tools":{}}`,
		`{"version":1,"tools":{"clipboardMonitoring":true}}`,
		// A key that is there and false is a choice, and it is kept.
		`{"version":1,"tools":{"translateEnabled":false}}`,
	} {
		if err := os.WriteFile(settingsPath, []byte(written), 0o600); err != nil {
			t.Fatalf("write the settings file: %v", err)
		}
		settings, err := store.Load(context.Background())
		if err != nil {
			t.Fatalf("Load(%s) failed: %v", written, err)
		}
		want := !strings.Contains(written, "false")
		if settings.Translation.Enabled != want {
			t.Errorf("Load(%s) gave enabled=%v, want %v", written, settings.Translation.Enabled, want)
		}
		if got := settings.Translation.ProviderOr(); got != domain.DefaultTranslationProvider {
			t.Errorf("Load(%s) gave provider %q, want the default", written, got)
		}
	}
}

// The values a hand-edited file may carry that are not free-form are brought
// into their bounds, like the record range is.
func TestLoadNormalizesTheTranslationSettings(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	written := `{"version":1,"tools":{
		"translateEnabled":true,
		"translateProvider":"nobody",
		"translateCloseDelayMs":99000,
		"translators":{"custom":{"baseURL":"api.example.com/v1/","displayName":"  中转站  "}}
	}}`
	if err := os.WriteFile(settingsPath, []byte(written), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	translation := settings.Translation
	if translation.CloseDelayOr() != domain.TranslationMaxCloseDelay {
		t.Errorf("close delay = %s, want it clamped to %s", translation.CloseDelayOr(), domain.TranslationMaxCloseDelay)
	}
	if got := translation.Custom.BaseURL; got != "https://api.example.com/v1" {
		t.Errorf("base URL = %q, want it cleaned up", got)
	}
	if got := translation.Custom.DisplayName; got != "中转站" {
		t.Errorf("display name = %q, want it trimmed", got)
	}
	// A provider nobody registered is kept in the file as written: which
	// ids have a translator is the assembly's business, not the store's,
	// and the use case falls back when it looks one up.
	if got := translation.ProviderOr(); got != "nobody" {
		t.Errorf("provider = %q, want it kept for the use case to judge", got)
	}
}

// A negative delay is a value nobody can mean, and it falls back rather than
// becoming "close at once".
func TestLoadTreatsANegativeCloseDelayAsUnset(t *testing.T) {
	store, settingsPath, _ := newStore(t)
	if err := os.WriteFile(settingsPath, []byte(`{"version":1,"tools":{"translateCloseDelayMs":-1}}`), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := settings.Translation.CloseDelayOr(); got != domain.DefaultTranslationCloseDelay {
		t.Errorf("close delay = %s, want the default %s", got, domain.DefaultTranslationCloseDelay)
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
