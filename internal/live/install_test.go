package live

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateModelFile(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"model.gguf", "GGUFsynthetic-model", nil},
		{"wrong.gguf", "this is not a model", errNotGGUF},
		{"empty.gguf", "", errNotGGUF},
		{"short.gguf", "GG", errNotGGUF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ValidateModelFile(path); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if err := ValidateModelFile(filepath.Join(root, "missing.gguf")); !errors.Is(err, errMissingModel) {
		t.Fatalf("missing model: %v", err)
	}
}

func TestPlaceCheckedRejectsABadHash(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "model.bin")
	art := Artifact{URL: "http://invalid/model.bin", Size: 4, SHA256: hex.EncodeToString(sha256.New().Sum(nil))}
	prev := fetch
	fetch = func(ctx context.Context, endpoint, part string, _ func(int64)) error {
		return os.WriteFile(part, []byte("nope"), 0o600)
	}
	t.Cleanup(func() { fetch = prev })
	if err := placeChecked(context.Background(), art, dest, nil); !errors.Is(err, errChecksum) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("renamed a bad file")
	}
	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("kept the rejected partial")
	}
}

func TestPlaceCheckedCancelLeavesThePartial(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "model.bin")
	body := []byte("live")
	sum := sha256.Sum256(body)
	art := Artifact{URL: "http://invalid/model.bin", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	ctx, cancel := context.WithCancel(context.Background())
	prev := fetch
	fetch = func(ctx context.Context, endpoint, part string, _ func(int64)) error {
		if err := os.WriteFile(part, []byte("part"), 0o600); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	}
	t.Cleanup(func() { fetch = prev })
	if err := placeChecked(ctx, art, dest, nil); err == nil {
		t.Fatal("cancel returned nil")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancel renamed the file")
	}
	if _, err := os.Stat(dest + ".part"); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceCheckedRenamesAMatchingFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "model.bin")
	body := []byte("live")
	sum := sha256.Sum256(body)
	art := Artifact{URL: "http://invalid/model.bin", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	prev := fetch
	fetch = func(ctx context.Context, endpoint, part string, report func(int64)) error {
		if err := os.WriteFile(part, body, 0o600); err != nil {
			return err
		}
		if report != nil {
			report(int64(len(body)))
		}
		return nil
	}
	t.Cleanup(func() { fetch = prev })
	var got []int64
	if err := placeChecked(context.Background(), art, dest, func(n int64) { got = append(got, n) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(len(body)) {
		t.Fatalf("progress %v", got)
	}
	read, err := os.ReadFile(dest)
	if err != nil || string(read) != "live" {
		t.Fatalf("%q %v", read, err)
	}
}

func TestExtractRejectsAPathOutsideTheDirectory(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.zip")
	writeZip(t, archive, map[string]string{"../escape": "no"})
	dest := filepath.Join(dir, "out")
	if err := extractArchive(archive, dest); err == nil {
		t.Fatal("escaped path was extracted")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); err == nil {
		t.Fatal("wrote outside the destination")
	}
}

func TestCUDAStampIsSeparateFromTheRuntimeStamp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(stampPath(dir), []byte("runtime-sum\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stampMatches(stampPath(dir)+".cuda", "cuda-sum") {
		t.Fatal("a runtime stamp satisfied the cuda check")
	}
	if err := os.WriteFile(stampPath(dir)+".cuda", []byte("cuda-sum\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !stampMatches(stampPath(dir)+".cuda", "cuda-sum") {
		t.Fatal("cuda stamp did not match")
	}
	if !stampMatches(stampPath(dir), "runtime-sum") {
		t.Fatal("runtime stamp did not match")
	}
}

func TestExtractFindsANestedExecutable(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "asr.tar.gz")
	writeTarGz(t, archive, map[string]string{"nested/" + exeName("crispasr"): "bin"})
	dest := filepath.Join(dir, "out")
	if err := extractArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	found, err := findFile(dest, exeName("crispasr"))
	if err != nil || !strings.Contains(found, "nested") {
		t.Fatal(found, err)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	for name, body := range files {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadAllProgressIsTheWholeSet(t *testing.T) {
	arts, err := PlatformArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, art := range arts {
		total += art.Size
	}
	prev := fetch
	fetch = func(ctx context.Context, endpoint, part string, report func(int64)) error {
		report(100)
		report(200)
		report(1 << 62)
		return errors.New("stop")
	}
	t.Cleanup(func() { fetch = prev })

	var got []DownloadProgress
	err = DownloadAll(context.Background(), t.TempDir(), func(p DownloadProgress) { got = append(got, p) })
	if err == nil {
		t.Fatal("stopped download returned nil")
	}
	if len(got) != 3 {
		t.Fatalf("progress %v", got)
	}
	if got[0].Name != arts[0].Name || got[0].Done != 100 || got[0].Total != total {
		t.Fatalf("first report %+v, total %d", got[0], total)
	}
	if got[1].Done != 200 {
		t.Fatalf("second report %+v", got[1])
	}
	if got[2].Done != arts[0].Size {
		t.Fatalf("oversized report %+v, file is %d", got[2], arts[0].Size)
	}
	if err := DownloadAll(context.Background(), t.TempDir(), nil); err == nil {
		t.Fatal("nil progress still downloaded")
	}
}
