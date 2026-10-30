package flash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveEditAndSoftDelete(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), FileName))
	created := time.Date(2026, 10, 30, 3, 55, 49, 123_000_000, time.UTC)
	saved, err := store.SaveText("  想法\n ", "", created)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Text != "想法" || saved.Source != SourceDesktop || saved.CreatedAt != created.Truncate(time.Second) {
		t.Fatalf("saved = %+v", saved)
	}
	edited, err := store.SaveText("修改", saved.ID, created.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if edited.CreatedAt != saved.CreatedAt || edited.Text != "修改" || edited.Source != SourceDesktop {
		t.Fatalf("edited = %+v", edited)
	}
	if err := store.Delete(saved.ID, created.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("list = %+v, want none", list)
	}
	body, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"deletedAt"`) || strings.Contains(text, ".123Z") || !strings.Contains(text, `"source":"desktop"`) {
		t.Fatalf("file = %s", text)
	}
}

func TestRejectsEmptyAndMissing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), FileName))
	if _, err := store.SaveText(" \n ", "", time.Now()); err != ErrEmpty {
		t.Fatalf("empty = %v", err)
	}
	if _, err := store.SaveText("text", "018f6c8e-6c3a-7000-8000-000000000001", time.Now()); err != ErrMissing {
		t.Fatalf("missing = %v", err)
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("file err = %v, want missing", err)
	}
}

func TestKeepsAppleSourceAndAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	original := `{"version":1,"entries":[{"id":"01950000-0000-7000-8000-000000000001","text":"","createdAt":"2026-10-30T03:55:49Z","updatedAt":"2026-10-30T03:55:49Z","audioFileName":"01950000-0000-7000-8000-000000000002.m4a"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Source != "" || list[0].AudioFileName == "" {
		t.Fatalf("list = %+v", list)
	}
	saved, err := store.SaveText("转写", list[0].ID, time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Source != "" || saved.AudioFileName != list[0].AudioFileName || saved.ID != list[0].ID {
		t.Fatalf("saved = %+v", saved)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"source"`) {
		t.Fatalf("missing source was written: %s", body)
	}
}

func TestPreservesDesktopSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	original := `{"version":1,"entries":[{"id":"01950000-0000-7000-8000-000000000001","text":"from desktop","createdAt":"2026-10-30T03:55:49Z","updatedAt":"2026-10-30T03:55:49Z","source":"desktop"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if _, err := store.SaveText("edited", "01950000-0000-7000-8000-000000000001", time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"source":"desktop"`) || !strings.Contains(string(body), "01950000-0000-7000-8000-000000000001") {
		t.Fatalf("file = %s", body)
	}
}

func TestCorruptFileIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	store := NewStore(path)
	for _, content := range []string{"broken", `{"version":2,"entries":[]}`, `{"version":1,"entries":[{"id":"01950000-0000-7000-8000-000000000001","text":"a","createdAt":"2026-10-30T03:55:49Z","updatedAt":"2026-10-30T03:55:49Z"},{"id":"01950000-0000-7000-8000-000000000001","text":"b","createdAt":"2026-10-30T03:55:50Z","updatedAt":"2026-10-30T03:55:50Z"}]}`, `{"version":1,"entries":[{"id":"not-a-uuid","text":"a","createdAt":"2026-10-30T03:55:49Z","updatedAt":"2026-10-30T03:55:49Z"}]}`, `{"version":1,"entries":[{"id":"01950000-0000-7000-8000-000000000001","text":"","createdAt":"2026-10-30T03:55:49Z","updatedAt":"2026-10-30T03:55:49Z","audioFileName":"nope.m4a"}]}`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.List(); err == nil || !strings.Contains(err.Error(), ErrUnreadable.Error()) {
			t.Fatalf("list %q = %v", content, err)
		}
		if _, err := store.SaveText("new", "", time.Now()); err == nil {
			t.Fatal("save overwrote")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != content {
			t.Fatalf("file changed to %s", body)
		}
	}
}

func TestCaseInsensitiveDuplicateIsUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `{"version":1,"entries":[{"id":"01950000-0000-7000-8000-00000000000A","text":"a","createdAt":"2026-10-30T03:55:49Z","updatedAt":"2026-10-30T03:55:49Z"},{"id":"01950000-0000-7000-8000-00000000000a","text":"b","createdAt":"2026-10-30T03:55:50Z","updatedAt":"2026-10-30T03:55:50Z"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).List(); err == nil {
		t.Fatal("duplicate cases were accepted")
	}
}
