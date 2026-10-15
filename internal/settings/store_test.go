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
)

// The settings store is one file and knows nothing about what is in it. What
// it is tested for here is therefore its own job: reading the file into the
// sections that are registered, handing each of them every key and taking back
// the keys they own, writing the result, and never losing a change to one
// feature because another one was made at the same time.
//
// What a feature's own keys mean is that feature's business and is tested
// there; this file uses a stand-in section.
//
// A test works on its own directory, never on a real user's data.

// testSection is a Section that remembers what it decoded and answers with
// what the test tells it to write.
type testSection struct {
	name string
	// seen is what Decode was handed, for a test to look at.
	seen map[string]json.RawMessage
	// decodes counts the reads, so a test can tell one Load from two.
	decodes int
	// value is what Encode hands back.
	value map[string]any
	// decodeErr and encodeErr make the store's error paths reachable.
	decodeErr error
	encodeErr error
}

func newTestSection(name string) *testSection {
	return &testSection{name: name, value: map[string]any{}}
}

func (s *testSection) Name() string { return s.name }

func (s *testSection) Decode(values map[string]json.RawMessage) error {
	s.decodes++
	s.seen = values
	return s.decodeErr
}

func (s *testSection) Encode() (map[string]any, error) {
	if s.encodeErr != nil {
		return nil, s.encodeErr
	}
	return s.value, nil
}

func newStore(t *testing.T, sections ...Section) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := New(path, sections...)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return store, path
}

func TestNewRequiresAPathAndDistinctSections(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("New without a path was accepted")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := New(path, nil); err == nil {
		t.Error("New with a nil section was accepted")
	}
	if _, err := New(path, newTestSection("")); err == nil {
		t.Error("New with a nameless section was accepted")
	}
	if _, err := New(path, newTestSection("same"), newTestSection("same")); err == nil {
		t.Error("New with the same section twice was accepted")
	}
}

// A file that is not there is not an error: every section keeps its own
// defaults and writes on the first change.
func TestLoadWithoutAFileReadsNothing(t *testing.T) {
	section := newTestSection("clipboard")
	store, _ := newStore(t, section)
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if section.seen != nil {
		t.Errorf("a section was handed %v although there is no file", section.seen)
	}
}

// Every section is handed every key: the file is one flat object, and which
// keys belong to whom is the sections' own business.
func TestEverySectionSeesEveryKey(t *testing.T) {
	first, second := newTestSection("first"), newTestSection("second")
	store, path := newStore(t, first, second)
	written := `{"version":1,"tools":{"a":1,"b":"two","c":true}}`
	if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	for _, section := range []*testSection{first, second} {
		if len(section.seen) != 3 {
			t.Errorf("section %s was handed %d keys, want 3", section.name, len(section.seen))
		}
		if string(section.seen["b"]) != `"two"` {
			t.Errorf("section %s saw b=%s", section.name, section.seen["b"])
		}
	}
}

// What the sections hand back is what the file holds, under one flat object.
func TestWrittenFileIsTheSectionsOwnKeys(t *testing.T) {
	section := newTestSection("clipboard")
	section.value = map[string]any{"clipboardMonitoring": true, "clipboardHistorySize": 30}
	store, path := newStore(t, section)
	if err := store.Save(context.Background()); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the settings file is not the documented shape: %v", err)
	}
	if doc.Version != FileVersion {
		t.Errorf("version = %d, want %d", doc.Version, FileVersion)
	}
	if len(doc.Tools) != 2 {
		t.Fatalf("the file holds %d keys, want 2: %s", len(doc.Tools), data)
	}
	// The keys are what the section named, not the section's own name: the
	// file is one flat object, which is what makes an existing user's file
	// readable without a migration.
	if string(doc.Tools["clipboardMonitoring"]) != "true" {
		t.Errorf("clipboardMonitoring = %s", doc.Tools["clipboardMonitoring"])
	}
	if _, present := doc.Tools["clipboard"]; present {
		t.Errorf("the section's name was written as a key: %s", data)
	}
}

// Two features writing one key is a mistake in the assembly, not something to
// settle silently: whichever won would be decided by the order the sections
// happened to be registered in.
func TestTwoSectionsWritingOneKeyIsRefused(t *testing.T) {
	first, second := newTestSection("first"), newTestSection("second")
	first.value = map[string]any{"shared": 1}
	second.value = map[string]any{"shared": 2}
	store, _ := newStore(t, first, second)
	err := store.Save(context.Background())
	if err == nil {
		t.Fatal("two sections writing one key was accepted")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Errorf("the error does not name the key: %v", err)
	}
}

// A file from another version is refused rather than partly understood.
func TestLoadRefusesAnotherVersion(t *testing.T) {
	section := newTestSection("clipboard")
	store, path := newStore(t, section)
	for _, written := range []string{
		`{"version":99,"tools":{}}`,
		`{"version":1,"tools":`,
		`not json`,
	} {
		if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
			t.Fatalf("write the settings file: %v", err)
		}
		if err := store.Load(context.Background()); err == nil {
			t.Errorf("Load accepted %q", written)
		}
	}
}

// A section that cannot read its own keys stops the load: the file is the
// user's configuration, and starting with a different one silently would be
// worse than not starting.
func TestASectionThatCannotDecodeStopsTheLoad(t *testing.T) {
	section := newTestSection("clipboard")
	section.decodeErr = errors.New("that is not a number")
	store, path := newStore(t, section)
	if err := os.WriteFile(path, []byte(`{"version":1,"tools":{"a":1}}`), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	err := store.Load(context.Background())
	if err == nil {
		t.Fatal("Load accepted a section that could not decode")
	}
	if !strings.Contains(err.Error(), "clipboard") {
		t.Errorf("the error does not name the section: %v", err)
	}
}

// A canceled context is answered with the context's error, like every other
// store in the application.
func TestContextIsHonoured(t *testing.T) {
	store, _ := newStore(t, newTestSection("clipboard"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Load = %v, want context.Canceled", err)
	}
	if err := store.Save(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Save = %v, want context.Canceled", err)
	}
	if err := store.Update(ctx, "clipboard", func() error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("Update = %v, want context.Canceled", err)
	}
}

func TestUpdateRefusesAnUnknownSection(t *testing.T) {
	store, _ := newStore(t, newTestSection("clipboard"))
	if err := store.Update(context.Background(), "nobody", func() error { return nil }); err == nil {
		t.Error("Update accepted a section that is not registered")
	}
}

// A change made through Update cannot be lost by another one.
//
// The file holds every feature's settings, and each feature changes only its
// own. Both go through Update, which reads the file again, runs the change and
// writes it back as one step. Done as a load and a save instead, the two would
// read the same file, each write back what it read plus its own change, and the
// second write would undo the first — which is the bug this test exists to keep
// out.
func TestTwoChangesAtOnceBothSurvive(t *testing.T) {
	first, second := newTestSection("first"), newTestSection("second")
	store, _ := newStore(t, first, second)

	for round := 0; round < 50; round++ {
		first.value = map[string]any{"first": true}
		second.value = map[string]any{"second": true}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := store.Update(context.Background(), "first", func() error { return nil }); err != nil {
				t.Errorf("the first change failed: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := store.Update(context.Background(), "second", func() error { return nil }); err != nil {
				t.Errorf("the second change failed: %v", err)
			}
		}()
		wg.Wait()

		data, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatalf("read the settings file: %v", err)
		}
		var doc document
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("the settings file is not readable: %v", err)
		}
		if _, present := doc.Tools["first"]; !present {
			t.Fatalf("round %d lost the first section's key: %s", round, data)
		}
		if _, present := doc.Tools["second"]; !present {
			t.Fatalf("round %d lost the second section's key: %s", round, data)
		}
	}
}

// A change that fails leaves the file as it was.
func TestUpdateLeavesTheFileAloneWhenTheChangeFails(t *testing.T) {
	section := newTestSection("clipboard")
	section.value = map[string]any{"clipboardMonitoring": true}
	store, path := newStore(t, section)
	if err := store.Save(context.Background()); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}

	wanted := errors.New("the change refused")
	if err := store.Update(context.Background(), "clipboard", func() error { return wanted }); !errors.Is(err, wanted) {
		t.Fatalf("Update = %v, want the change's own error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings file again: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a change that failed still wrote the file")
	}
}

// Concurrent writes must not interleave into one file: the rename is what
// makes the file whole, and the lock is what keeps two writers from racing
// over the temporary file's name.
func TestConcurrentWrites(t *testing.T) {
	section := newTestSection("clipboard")
	store, path := newStore(t, section)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				section.value = map[string]any{"clipboardMonitoring": true}
			} else {
				section.value = map[string]any{"clipboardMonitoring": false}
			}
			if err := store.Save(context.Background()); err != nil {
				t.Errorf("Save failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s is missing: %v", path, err)
	}
	if err := store.Load(context.Background()); err != nil {
		t.Errorf("the file did not survive the writes: %v", err)
	}
}

// The file is written through a temporary file, so a directory left behind
// holds no half-written document.
func TestWriteLeavesNoTemporaryFile(t *testing.T) {
	section := newTestSection("clipboard")
	section.value = map[string]any{"clipboardMonitoring": true}
	store, path := newStore(t, section)
	if err := store.Save(context.Background()); err != nil {
		t.Fatalf("Save failed: %v", err)
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
