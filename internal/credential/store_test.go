package credential

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
)

func newStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), FileName)
}

func put(t *testing.T, store *Store, name, value string, meta domain.SecretMeta) {
	t.Helper()
	if err := store.Put(context.Background(), name, []byte(value), meta); err != nil {
		t.Fatalf("Put(%s) failed: %v", name, err)
	}
}

func TestOpenMissingFileStartsEmpty(t *testing.T) {
	store, err := Open(newStorePath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if _, err := store.Get(context.Background(), "deepseek.apiKey"); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Fatalf("Get error = %v, want ErrSecretNotFound", err)
	}
	if names, err := store.List(context.Background()); err != nil || len(names) != 0 {
		t.Fatalf("List = %v, %v; want no secrets", names, err)
	}
}

func TestPutGetListDelete(t *testing.T) {
	path := newStorePath(t)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	put(t, store, "deepseek.apiKey", "sk-test-0001", domain.SecretMeta{ProviderID: "deepseek", CreatedAt: created})
	put(t, store, "weread.apiKey", "wrk-test-0002", domain.SecretMeta{ProviderID: "weread"})

	secret, err := store.Get(ctx, "deepseek.apiKey")
	if err != nil || secret.Text() != "sk-test-0001" {
		t.Fatalf("Get = %q, %v", secret.Text(), err)
	}
	if secret.Name != "deepseek.apiKey" || secret.Meta.ProviderID != "deepseek" {
		t.Errorf("secret = %+v", secret)
	}

	names, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(names) != 2 || names[0] != "deepseek.apiKey" || names[1] != "weread.apiKey" {
		t.Fatalf("List = %v, want both names, sorted", names)
	}

	meta, err := store.Meta(ctx, "deepseek.apiKey")
	if err != nil {
		t.Fatalf("Meta failed: %v", err)
	}
	if !meta.CreatedAt.Equal(created) || meta.ProviderID != "deepseek" {
		t.Errorf("meta = %+v", meta)
	}
	if _, err := store.Meta(ctx, "nope"); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("Meta of an unknown name = %v", err)
	}

	if err := store.Delete(ctx, "weread.apiKey"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := store.Get(ctx, "weread.apiKey"); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("Get after Delete error = %v, want ErrSecretNotFound", err)
	}
	if err := store.Delete(ctx, "weread.apiKey"); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("second Delete error = %v, want ErrSecretNotFound", err)
	}
}

func TestUpdateKeepsTheCreationTime(t *testing.T) {
	store, err := Open(newStorePath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	put(t, store, "deepseek.apiKey", "old", domain.SecretMeta{CreatedAt: created, UpdatedAt: created})
	put(t, store, "deepseek.apiKey", "new", domain.SecretMeta{UpdatedAt: later})

	meta, err := store.Meta(ctx, "deepseek.apiKey")
	if err != nil {
		t.Fatalf("Meta failed: %v", err)
	}
	if !meta.CreatedAt.Equal(created) {
		t.Errorf("createdAt = %s, want %s", meta.CreatedAt, created)
	}
	if !meta.UpdatedAt.Equal(later) {
		t.Errorf("updatedAt = %s, want %s", meta.UpdatedAt, later)
	}
	if secret, _ := store.Get(ctx, "deepseek.apiKey"); secret.Text() != "new" {
		t.Errorf("value = %q, want new", secret.Text())
	}
}

// 明文文件没有可解锁的东西，存储如实呈现而不是假装受保护。
func TestNothingToUnlock(t *testing.T) {
	store, err := Open(newStorePath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	if !store.Unlocked() {
		t.Error("a plain-text store reports itself locked")
	}
	if err := store.Unlock(ctx, ""); err != nil {
		t.Errorf("Unlock failed: %v", err)
	}
	store.Lock()
	if !store.Unlocked() {
		t.Error("Lock made a plain-text store unreadable, which it cannot be")
	}
	put(t, store, "deepseek.apiKey", "sk-test-0001", domain.SecretMeta{})
	if secret, err := store.Get(ctx, "deepseek.apiKey"); err != nil || secret.Text() != "sk-test-0001" {
		t.Errorf("Get after Lock = %q, %v", secret.Text(), err)
	}
}

// 文件是明文所以可读，这是设计决定；测试显式断言，避免被"意外发现"。
func TestTheFileIsPlainTextAndRestricted(t *testing.T) {
	path := newStorePath(t)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	put(t, store, "deepseek.apiKey", "sk-test-0001", domain.SecretMeta{ProviderID: "deepseek"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file back: %v", err)
	}
	// 值以明文在文件中。一旦这一点改变，设置窗口的告知必须同步修改。
	if !strings.Contains(string(data), "sk-test-0001") {
		t.Error("the value is not in the file in the clear, which the notice claims it is")
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if doc.Version != fileVersion {
		t.Errorf("version = %d, want %d", doc.Version, fileVersion)
	}
	if doc.Secrets["deepseek.apiKey"].Value != "sk-test-0001" {
		t.Errorf("the file does not hold the value: %+v", doc.Secrets)
	}

	// 权限位只在有该位的文件系统上是真实边界：Windows 由 ACL 决定访问，此位无意义。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != atomicfile.FileMode {
			t.Errorf("file mode = %o, want %o", perm, atomicfile.FileMode)
		}
	}

	// 重新打开后看到相同的凭据：文件是事实来源。
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if secret, err := reopened.Get(ctx, "deepseek.apiKey"); err != nil || secret.Text() != "sk-test-0001" {
		t.Errorf("after reopening, Get = %q, %v", secret.Text(), err)
	}
}

func TestOpenRefusesAnotherVersion(t *testing.T) {
	path := newStorePath(t)
	if err := os.WriteFile(path, []byte(`{"version":2,"secrets":{}}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted a file from another version")
	} else if !strings.Contains(err.Error(), "version 2") {
		t.Errorf("the error does not say what it found: %v", err)
	}
}

func TestOpenRefusesSomethingElse(t *testing.T) {
	path := newStorePath(t)
	if err := os.WriteFile(path, []byte(`not json at all`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted a file that is not JSON")
	}
}

func TestOpenRefusesAnEmptyPath(t *testing.T) {
	if _, err := Open("   "); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Open(\"   \") = %v, want ErrInvalidArgument", err)
	}
}

func TestEmptyNamesAndValuesAreRefused(t *testing.T) {
	store, err := Open(newStorePath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "  ", []byte("v"), domain.SecretMeta{}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty name: error = %v", err)
	}
	if err := store.Put(ctx, "deepseek.apiKey", []byte("   "), domain.SecretMeta{}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty value: error = %v", err)
	}
	put(t, store, "deepseek.apiKey", "v", domain.SecretMeta{})
	if _, err := store.Get(ctx, " "); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("Get with an empty name: error = %v", err)
	}
	if err := store.Delete(ctx, ""); !errors.Is(err, domain.ErrSecretNotFound) {
		t.Errorf("Delete with an empty name: error = %v", err)
	}
}

// 写不到磁盘时，存储不能声称写成功。
func TestAFailedWriteKeepsTheStoreAsItWas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	put(t, store, "deepseek.apiKey", "first", domain.SecretMeta{})

	// 把目标变成目录，使其不可写。
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := store.Put(ctx, "deepseek.apiKey", []byte("second"), domain.SecretMeta{}); err == nil {
		t.Fatal("Put reported success although the file could not be written")
	}
	secret, err := store.Get(ctx, "deepseek.apiKey")
	if err != nil || secret.Text() != "first" {
		t.Errorf("Get after a failed write = %q, %v; want the previous value", secret.Text(), err)
	}
	if err := store.Put(ctx, "new.name", []byte("value"), domain.SecretMeta{}); err == nil {
		t.Fatal("Put of a new name reported success")
	}
	if names, err := store.List(ctx); err == nil && len(names) != 1 {
		t.Errorf("List = %v, want only the name that was stored", names)
	}
}

func TestConcurrentUse(t *testing.T) {
	store, err := Open(newStorePath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := string(rune('a'+i)) + ".apiKey"
			if err := store.Put(ctx, name, []byte("value"), domain.SecretMeta{ProviderID: "test"}); err != nil {
				t.Errorf("Put(%s) failed: %v", name, err)
				return
			}
			if _, err := store.Get(ctx, name); err != nil {
				t.Errorf("Get(%s) failed: %v", name, err)
			}
			if _, err := store.List(ctx); err != nil {
				t.Errorf("List failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
	names, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(names) != 16 {
		t.Errorf("List returned %d names, want 16", len(names))
	}
}

func TestCanceledContextStopsWork(t *testing.T) {
	store, err := Open(newStorePath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Get(ctx, "deepseek.apiKey"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get error = %v, want context.Canceled", err)
	}
	if err := store.Put(ctx, "deepseek.apiKey", []byte("v"), domain.SecretMeta{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Put error = %v, want context.Canceled", err)
	}
	if err := store.Delete(ctx, "deepseek.apiKey"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete error = %v, want context.Canceled", err)
	}
	if _, err := store.List(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("List error = %v, want context.Canceled", err)
	}
	if _, err := store.Meta(ctx, "deepseek.apiKey"); !errors.Is(err, context.Canceled) {
		t.Errorf("Meta error = %v, want context.Canceled", err)
	}
	if err := store.Unlock(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("Unlock error = %v, want context.Canceled", err)
	}
}
