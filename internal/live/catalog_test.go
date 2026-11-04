package live

import (
	"strings"
	"testing"
)

func TestRecognizerArgsDoNotCaptureOrTranslate(t *testing.T) {
	args := RecognizerArgs("/models/ja.gguf", "/models/vad.gguf")
	want := []string{
		"--stream", "--stream-json", "--stream-realtime",
		"--stream-step", "500", "--stream-length", "10000",
		"--stream-final-on-silence-ms", "350", "--stream-vad-merge-gap-ms", "100", "--stream-final-mode", "redecode",
		"--stream-utterance-max-sec", "8", "-l", "ja",
		"--vad", "--vad-model", "/models/vad.gguf", "-m", "/models/ja.gguf",
	}
	if strings.Join(args, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%q", args)
	}
	for _, bad := range []string{"--mic", "--live", "--live-translate", "--backend", "--translate-model", "--tr-tl", "--punc-model", "auto", "firered"} {
		for _, arg := range args {
			if arg == bad {
				t.Fatalf("forbidden %s in %q", bad, args)
			}
		}
	}
}

func TestTranslatorArgsBindLoopback(t *testing.T) {
	args := TranslatorArgs(43111, "/models/mt.gguf")
	joined := strings.Join(args, "\n") + "\n"
	for _, part := range []string{"--host\n127.0.0.1\n", "--port\n43111\n", "-m\n/models/mt.gguf\n", "-c\n2048\n", "--jinja\n", "-ngl\n99\n"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("%q missing %q", args, part)
		}
	}
}

func TestPinnedModelsMatchTheSpec(t *testing.T) {
	arts, err := PlatformArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		recognizerFile: "5a61e6c7d956c3c72a76fafcd798cac0c9ea66d0e29b3910cd04865a1e42cc17",
		translatorFile: "dc5f44fcf1fa496ee7ad725982c0c8c553a4de00259b53af84c4b89fb0c06699",
		vadFile:        "72d37db1a5d2a9db386d1452b3f54d13f97bea79710105fc65acd050ea7db600",
	}
	for _, art := range arts {
		sum, ok := want[art.Name]
		if !ok {
			continue
		}
		if art.SHA256 != sum || art.Size <= 0 || !strings.Contains(art.URL, art.Name) {
			t.Fatalf("%+v", art)
		}
		delete(want, art.Name)
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
}

func TestDownloadListNamesTheModelsAndTheirSizes(t *testing.T) {
	arts, err := PlatformArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, art := range arts {
		if art.Kind != kindModel {
			continue
		}
		got[art.Title()+" "+art.Name] = FormatSize(art.Size)
	}
	want := map[string]string{
		"识别模型 " + recognizerFile: "642.4 MB",
		"翻译模型 " + translatorFile: "1.1 GB",
		"语音检测 " + vadFile:        "2.2 MB",
	}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for label, size := range want {
		if got[label] != size {
			t.Errorf("%s = %s, want %s", label, got[label], size)
		}
	}
}
