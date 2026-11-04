package live

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	recognizerFile = "parakeet-tdt-0.6b-ja-q8_0.gguf"
	translatorFile = "Hy-MT2-1.8B-Q4_K_M.gguf"
	vadFile        = "firered-vad.gguf"
)

// Artifact is one pinned download. The bytes and hash are the files checked
// on 2026-11-02. They are not "latest".
type Artifact struct {
	Name   string
	URL    string
	Size   int64
	SHA256 string
	Kind   string
}

const (
	kindModel = "model"
	kindASR   = "asr"
	kindMT    = "mt"
	kindCUDA  = "cuda"
)

// PlatformArtifacts is the set the current process needs. A universal Mac
// build still picks the slice for GOARCH, not for the installer.
func PlatformArtifacts() ([]Artifact, error) {
	common := []Artifact{
		{Name: recognizerFile, URL: "https://huggingface.co/cstr/parakeet-tdt-0.6b-ja-GGUF/resolve/d9e3ba65a6579796389ea89e5939509ed257f972/" + recognizerFile, Size: 673554880, SHA256: "5a61e6c7d956c3c72a76fafcd798cac0c9ea66d0e29b3910cd04865a1e42cc17", Kind: kindModel},
		{Name: translatorFile, URL: "https://huggingface.co/tencent/Hy-MT2-1.8B-GGUF/resolve/a0c709d9fac510f2c807aa3af52872340dc37a4a/" + translatorFile, Size: 1133080448, SHA256: "dc5f44fcf1fa496ee7ad725982c0c8c553a4de00259b53af84c4b89fb0c06699", Kind: kindModel},
		{Name: vadFile, URL: "https://huggingface.co/cstr/firered-vad-GGUF/resolve/f965baa5daf11f6dbad5fd10068455666980c717/" + vadFile, Size: 2357952, SHA256: "72d37db1a5d2a9db386d1452b3f54d13f97bea79710105fc65acd050ea7db600", Kind: kindModel},
	}
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return append(common,
			pinned("crispasr-macos-arm64.tar.gz", "https://github.com/CrispStrobe/CrispASR/releases/download/v0.8.49/crispasr-macos-arm64.tar.gz", 18165415, "53fa0b1addeacaafd05f8b0cd10cad6e8f44cbe302be3bd07fdf7fce2eff27ec", kindASR),
			pinned("llama-b11960-bin-macos-arm64.tar.gz", "https://github.com/ggml-org/llama.cpp/releases/download/b11960/llama-b11960-bin-macos-arm64.tar.gz", 16465074, "58e5a5e1d7c2bf66cedd9c29e0323f3e570660a70a1e2156d32eed08f4cc1e3c", kindMT),
		), nil
	case "darwin/amd64":
		return append(common,
			pinned("crispasr-macos-x86_64.tar.gz", "https://github.com/CrispStrobe/CrispASR/releases/download/v0.8.49/crispasr-macos-x86_64.tar.gz", 18668950, "06511a96c1d292dadb5b3cfb2b331f8155fd14c7da21a7f5935c940d941d8ea4", kindASR),
			pinned("llama-b11960-bin-macos-x64.tar.gz", "https://github.com/ggml-org/llama.cpp/releases/download/b11960/llama-b11960-bin-macos-x64.tar.gz", 12205764, "d1e6c9ddcaed9a3b1bb8f3d946bdb3481c7b919357a4029dcb747762397f1da5", kindMT),
		), nil
	case "windows/amd64":
		return append(common,
			pinned("crispasr-windows-x86_64-cuda13.zip", "https://github.com/CrispStrobe/CrispASR/releases/download/v0.8.49/crispasr-windows-x86_64-cuda13.zip", 569033716, "b4f48eb8e86a0cf97b70b862ca9e47b19234e534d3f298293183020065fcd9ad", kindASR),
			pinned("llama-b11960-bin-win-cuda-13.4-x64.zip", "https://github.com/ggml-org/llama.cpp/releases/download/b11960/llama-b11960-bin-win-cuda-13.4-x64.zip", 164986387, "31c75ea62b80f0816c855ea783ef68757bc2d48028edaa57d904e4982bddea4a", kindMT),
			pinned("cudart-llama-bin-win-cuda-13.4-x64.zip", "https://github.com/ggml-org/llama.cpp/releases/download/b11960/cudart-llama-bin-win-cuda-13.4-x64.zip", 423535356, "738f8c251ac22b70c3ae6f83a10cf222725df0395246a2cf58f32bdb85fbe668", kindCUDA),
		), nil
	default:
		return nil, errUnsupported
	}
}

func pinned(name, url string, size int64, sum, kind string) Artifact {
	return Artifact{Name: name, URL: url, Size: size, SHA256: sum, Kind: kind}
}

// Title is the short name shown next to a download: what the file is for,
// not the archive it arrives in.
func (a Artifact) Title() string {
	switch a.Name {
	case recognizerFile:
		return "识别模型"
	case translatorFile:
		return "翻译模型"
	case vadFile:
		return "语音检测"
	}
	switch a.Kind {
	case kindASR:
		return "识别运行时"
	case kindMT:
		return "翻译运行时"
	case kindCUDA:
		return "CUDA 运行库"
	default:
		return "文件"
	}
}

// FormatSize writes a byte count the way a download list reads it.
// 1024-based, one decimal, and the trailing ".0" is dropped.
func FormatSize(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	value, unit := float64(n), "B"
	switch {
	case n >= gb:
		value, unit = float64(n)/float64(gb), "GB"
	case n >= mb:
		value, unit = float64(n)/float64(mb), "MB"
	case n >= kb:
		value, unit = float64(n)/float64(kb), "KB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
	text := strconv.FormatFloat(value, 'f', 1, 64)
	text = strings.TrimSuffix(text, ".0")
	return text + " " + unit
}

var errUnsupported = errString("这个平台没有实时翻译的运行时")

type errString string

func (e errString) Error() string { return string(e) }

func runtimeKey() (string, bool) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64", "darwin/amd64", "windows/amd64":
		return runtime.GOOS + "-" + runtime.GOARCH, true
	default:
		return "", false
	}
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// RecognizerArgs is the fixed CrispASR invocation. It does not ask the
// process to capture, translate, or download a model.
func RecognizerArgs(model, vad string) []string {
	return []string{
		"--stream", "--stream-json", "--stream-realtime",
		"--stream-step", "500",
		"--stream-length", "10000",
		"--stream-final-on-silence-ms", "350",
		"--stream-vad-merge-gap-ms", "100",
		"--stream-final-mode", "redecode",
		"--stream-utterance-max-sec", "8",
		"-l", "ja",
		"--vad", "--vad-model", vad,
		"-m", model,
	}
}

// TranslatorArgs is the fixed llama-server invocation. The port is chosen
// here rather than assumed to be 8080.
func TranslatorArgs(port int, model string) []string {
	return []string{
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-m", model,
		"-c", "2048",
		"--jinja",
		"-ngl", "99",
	}
}

func fileSHA(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fileReady(path string, size int64) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() == size && info.Mode().IsRegular()
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found != "" {
			return err
		}
		if entry.Name() == name {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}
