package live

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"boku/internal/platform/httpc"
)

// ModelsDirName 是用户数据下存放固定模型与解压后运行时的目录，
// 不属于安装器。
const ModelsDirName = "models/live"

type Models struct {
	ASR    string
	VAD    string
	MT     string
	ASRExe string
	MTExe  string
}

// Resolve 检查本平台的运行时与所选模型文件是否在磁盘上。
// 自定义路径只替换模型：不做哈希校验，也不会替换可执行文件。
func Resolve(root string, settings Settings) (Models, error) {
	key, ok := runtimeKey()
	if !ok {
		return Models{}, errUnsupported
	}
	base := filepath.Join(root, "runtime", key)
	asrExe, err := findFile(filepath.Join(base, kindASR), exeName("crispasr"))
	if err != nil {
		return Models{}, errNeedDownload
	}
	mtExe, err := findFile(filepath.Join(base, kindMT), exeName("llama-server"))
	if err != nil {
		return Models{}, errNeedDownload
	}
	vad := filepath.Join(root, vadFile)
	if !sizeOf(vadFile, vad) {
		return Models{}, errNeedDownload
	}
	asr := filepath.Join(root, recognizerFile)
	if settings.ASRPath != "" {
		if _, err := os.Stat(settings.ASRPath); err != nil {
			return Models{}, errMissingASR
		}
		asr = settings.ASRPath
	} else if !sizeOf(recognizerFile, asr) {
		return Models{}, errNeedDownload
	}
	mt := filepath.Join(root, translatorFile)
	if settings.MTPath != "" {
		if err := ValidateModelFile(settings.MTPath); err != nil {
			return Models{}, err
		}
		mt = settings.MTPath
	} else if !sizeOf(translatorFile, mt) {
		return Models{}, errNeedDownload
	}
	return Models{ASR: asr, VAD: vad, MT: mt, ASRExe: asrExe, MTExe: mtExe}, nil
}

var (
	errNeedDownload = errString("先在设置里下载模型")
	errMissingASR   = errString("找不到识别文件")
	errMissingModel = errString("无法读取模型文件，请重新选择")
	errNotGGUF      = errString("请选择 GGUF 格式的模型文件（.gguf）")
	errChecksum     = errString("文件校验失败")
)

// ValidateModelFile 在替换模型前检查用户选中的文件。
// GGUF 头只能确认格式，不保证与模型后端兼容。
func ValidateModelFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return errMissingModel
	}
	defer file.Close()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(file, buf); err != nil || string(buf) != "GGUF" {
		return errNotGGUF
	}
	return nil
}

func sizeOf(name, path string) bool {
	art, ok := artifactNamed(name)
	return ok && fileReady(path, art.Size)
}

func artifactNamed(name string) (Artifact, bool) {
	list, err := PlatformArtifacts()
	if err != nil {
		return Artifact{}, false
	}
	for _, art := range list {
		if art.Name == name {
			return art, true
		}
	}
	return Artifact{}, false
}

// DownloadProgress 表示整组下载的进度。Done 把已完成文件按整份计入，
// 正在写入的文件按已落盘字节计入。Total 是各固定大小之和。
type DownloadProgress struct {
	Name  string
	Done  int64
	Total int64
}

// DownloadAll 拉取本进程的固定集合，逐个校验 SHA256，再解压运行时。
// 取消会保留部分文件供下次续传；校验失败则删除该部分文件、不重命名就位。
// progress 可以为 nil。
func DownloadAll(ctx context.Context, root string, progress func(DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	arts, err := PlatformArtifacts()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	var total int64
	for _, art := range arts {
		total += art.Size
	}
	var base int64
	send := func(name string, done int64) {
		if progress == nil {
			return
		}
		if done < 0 {
			done = 0
		}
		if total > 0 && done > total {
			done = total
		}
		progress(DownloadProgress{Name: name, Done: done, Total: total})
	}
	var mtExe string
	for _, art := range arts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		onFile := func(n int64) {
			if n < 0 {
				n = 0
			}
			if art.Size > 0 && n > art.Size {
				n = art.Size
			}
			send(art.Name, base+n)
		}
		var err error
		switch art.Kind {
		case kindModel:
			err = placeChecked(ctx, art, filepath.Join(root, art.Name), onFile)
		case kindASR, kindMT:
			var exe string
			exe, err = installRuntime(ctx, root, art, onFile)
			if err == nil && art.Kind == kindMT {
				mtExe = exe
			}
		case kindCUDA:
			if mtExe == "" {
				err = errString("翻译运行时还没有解压")
				break
			}
			err = installCUDA(ctx, root, art, filepath.Dir(mtExe), onFile)
		default:
			err = fmt.Errorf("未知的下载 %s", art.Kind)
		}
		if err != nil {
			return &fileError{name: art.Name, err: err}
		}
		base += art.Size
		send(art.Name, base)
	}
	return nil
}

func installRuntime(ctx context.Context, root string, art Artifact, report func(int64)) (string, error) {
	key, _ := runtimeKey()
	dest := filepath.Join(root, "runtime", key, art.Kind)
	name := exeName("crispasr")
	if art.Kind == kindMT {
		name = exeName("llama-server")
	}
	if exe, err := findFile(dest, name); err == nil && stampMatches(stampPath(dest), art.SHA256) {
		return exe, nil
	}
	archive := filepath.Join(root, "runtime", key, art.Name)
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		return "", err
	}
	if err := placeChecked(ctx, art, archive, report); err != nil {
		return "", err
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := extractArchive(archive, dest); err != nil {
		return "", err
	}
	_ = os.Remove(archive)
	exe, err := findFile(dest, name)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(exe, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(stampPath(dest), []byte(art.SHA256), 0o600); err != nil {
		return "", err
	}
	return exe, nil
}

func installCUDA(ctx context.Context, root string, art Artifact, beside string, report func(int64)) error {
	if stampMatches(stampPath(beside)+".cuda", art.SHA256) {
		return nil
	}
	key, _ := runtimeKey()
	archive := filepath.Join(root, "runtime", key, art.Name)
	if err := placeChecked(ctx, art, archive, report); err != nil {
		return err
	}
	if err := extractArchive(archive, beside); err != nil {
		return err
	}
	_ = os.Remove(archive)
	if err := copyDLLs(beside); err != nil {
		return err
	}
	return os.WriteFile(stampPath(beside)+".cuda", []byte(art.SHA256), 0o600)
}

type fileError struct {
	name string
	err  error
}

func (e *fileError) Error() string { return e.name + ": " + e.err.Error() }

func (e *fileError) Unwrap() error { return e.err }

func stampMatches(path, sum string) bool {
	text, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(text)) == sum
}

func stampPath(dir string) string { return dir + ".sha" }

// fetch 是 httpc 提供的下载函数，只有测试会替换它。
// report 拿到 dest 当前持有的字节数，包括续传的部分文件。
var fetch = httpc.DownloadProgress

func placeChecked(ctx context.Context, art Artifact, dest string, report func(int64)) error {
	if fileReady(dest, art.Size) {
		sum, err := fileSHA(dest)
		if err == nil && sum == art.SHA256 {
			return nil
		}
		_ = os.Remove(dest)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	part := dest + ".part"
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := fetch(ctx, art.URL, part, report); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	info, err := os.Stat(part)
	if err != nil {
		return err
	}
	sum, err := fileSHA(part)
	if err != nil {
		return err
	}
	if info.Size() != art.Size || sum != art.SHA256 {
		_ = os.Remove(part)
		return errChecksum
	}
	_ = os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		return err
	}
	return nil
}

func extractArchive(path, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(path, ".zip") {
		return extractZip(path, dest)
	}
	return extractTarGz(path, dest)
}

func extractZip(path, dest string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		target, err := safeJoin(dest, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		err = writeNew(target, in, file.Mode())
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(path, dest string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeNew(target, tr, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}
}

func writeNew(path string, r io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode&0o777)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, r)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("压缩包路径不对")
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("压缩包路径不对")
	}
	return target, nil
}

func copyDLLs(root string) error {
	exe, err := findFile(root, exeName("llama-server"))
	if err != nil {
		return nil
	}
	beside := filepath.Dir(exe)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".dll") {
			return nil
		}
		dest := filepath.Join(beside, entry.Name())
		if path == dest {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		return writeNew(dest, in, 0o755)
	})
}
