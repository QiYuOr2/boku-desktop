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

// 测的是存储自身：读文件、把全部键交给每个 Section 并收回它们拥有的键、写出结果，
// 以及并发改动时不丢失任一功能的改动。各功能键的含义由各功能自己测，本文件用替身 Section。
// 每个测试只用自己的临时目录，绝不碰真实用户数据。

// testSection 是一个记住 Decode 内容、按测试指定内容输出的 Section。
type testSection struct {
	name string
	// seen 是 Decode 拿到的内容，供测试查看。
	seen map[string]json.RawMessage
	// decodes 统计读取次数，便于区分一次与两次 Load。
	decodes int
	// value 是 Encode 交回的内容。
	value map[string]any
	// decodeErr 与 encodeErr 让存储的错误路径可达。
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

// 文件不存在不算错误：各 Section 保留默认值，首次改动时再写入。
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

// 每个 Section 都拿到全部键：文件是扁平对象，哪些键属于谁由各 Section 自行判断。
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

// 文件内容就是各 Section 交回的键，放在同一个扁平对象里。
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
	// 写的是 Section 声明的键，而非 Section 的名字：扁平对象正是已有用户文件无需迁移的原因。
	if string(doc.Tools["clipboardMonitoring"]) != "true" {
		t.Errorf("clipboardMonitoring = %s", doc.Tools["clipboardMonitoring"])
	}
	if _, present := doc.Tools["clipboard"]; present {
		t.Errorf("the section's name was written as a key: %s", data)
	}
}

// 无人认领的键在保存后仍保留（可能是手写或旧版本遗留）。改别的设置不能悄悄缩小
// 用户的文件，这是「缺失的键保持默认」从另一侧的承诺。
func TestAnUnclaimedKeySurvivesASave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"version":1,"tools":{"handWritten":"kept","clipboardMonitoring":true}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	section := newTestSection("clipboard")
	section.value = map[string]any{"clipboardMonitoring": true}
	store, err := New(path, section)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// 用户改的是完全不相干的另一项。
	section.value = map[string]any{"clipboardMonitoring": false}
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
	if got := string(doc.Tools["handWritten"]); got != `"kept"` {
		t.Errorf("handWritten = %s, want it kept as it was: %s", got, data)
	}
	// 用户改动的那一项确实变了。
	if got := string(doc.Tools["clipboardMonitoring"]); got != "false" {
		t.Errorf("clipboardMonitoring = %s, want false", got)
	}
}

// Update 会重读并整份写回，因此同样必须保留无人认领的键——它是每次设置改动的必经路径。
func TestUpdateKeepsAnUnclaimedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"version":1,"tools":{"futureBuildKey":{"nested":[1,2]}}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	section := newTestSection("clipboard")
	store, err := New(path, section)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = store.Update(context.Background(), "clipboard", func() error {
		section.value = map[string]any{"clipboardMonitoring": true}
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the settings file is not the documented shape: %v", err)
	}
	if got := string(doc.Tools["futureBuildKey"]); !strings.Contains(got, `"nested"`) {
		t.Errorf("futureBuildKey = %s, want it kept as it was: %s", got, data)
	}
	if got := string(doc.Tools["clipboardMonitoring"]); got != "true" {
		t.Errorf("clipboardMonitoring = %s, want true", got)
	}
}

// Section 拥有的键归该 Section 写：存储不会把旧值覆盖回新值之上，否则改动会自我撤销。
func TestAClaimedKeyIsWrittenByItsSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"version":1,"tools":{"clipboardMonitoring":true}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write the settings file: %v", err)
	}
	section := newTestSection("clipboard")
	store, err := New(path, section)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = store.Update(context.Background(), "clipboard", func() error {
		section.value = map[string]any{"clipboardMonitoring": false}
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the settings file is not the documented shape: %v", err)
	}
	if got := string(doc.Tools["clipboardMonitoring"]); got != "false" {
		t.Errorf("clipboardMonitoring = %s, want the section's false", got)
	}
}

// 两个功能写同一个键属于装配错误，不能静默取舍：谁赢只会取决于注册顺序。
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

// 版本不符或无法解析的文件会被拒绝，而不是被部分解读。
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

// 读不出自身键的 Section 会中止加载：文件是用户的配置，静默改用另一份比不启动更糟。
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

// 取消的 context 返回 context 的错误，与应用里其他存储一致。
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

// 经 Update 做出的改动不会被另一处改动弄丢。
//
// 两个功能都走 Update：它在同一步内重读、改动并写回。若改用 Load + Save，双方读到同一份
// 文件、各写回「读到的 + 自己的改动」，后写的撤销先写的——本测试守住这个 bug 不复现。
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

// 失败的改动保持文件原样。
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

// 并发写入不能交错进同一个文件：rename 保证文件完整，锁保证两个写者不争抢临时文件名。
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

// 文件经临时文件写入，目录里不应留下写了一半的残留文件。
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
