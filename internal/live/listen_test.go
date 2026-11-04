package live

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("BOKU_LIVE_HELPER") {
	case "asr":
		helperASR()
		return
	case "crash":
		os.Exit(1)
	case "sleep":
		time.Sleep(time.Hour)
		return
	}
	os.Exit(m.Run())
}

func helperASR() {
	fmt.Fprintln(os.Stderr, "loaded")
	time.Sleep(500 * time.Millisecond)
	fmt.Println(`{"type":"partial","text":"部室"}`)
	fmt.Println(`{"type":"silence","t":1}`)
	fmt.Println("not-json")
	fmt.Println(`{"type":"final","text":""}`)
	fmt.Println(`{"type":"final","text":"部室に行く"}`)
	_, _ = io.Copy(io.Discard, os.Stdin)
	time.Sleep(time.Hour)
}

type stubTranslator struct {
	mu      sync.Mutex
	prompt  string
	prompts []string
}

func (s *stubTranslator) Ready(context.Context) error { return nil }

func (s *stubTranslator) Translate(ctx context.Context, prompt string) (string, error) {
	s.mu.Lock()
	s.prompt = prompt
	s.prompts = append(s.prompts, prompt)
	s.mu.Unlock()
	return "社团活动室", nil
}

type fakeAudio struct{ pcm chan []byte }

func (f *fakeAudio) PCM() <-chan []byte { return f.pcm }
func (f *fakeAudio) Close() error       { return nil }

func TestFinalUsesTheTerminologyPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	tr := &stubTranslator{}
	got := make(chan string, 1)
	var draft string
	var mu sync.Mutex
	audio := &fakeAudio{pcm: make(chan []byte)}
	errc := make(chan error, 1)
	go func() {
		errc <- runListen(ctx,
			processSpec{name: "crispasr", label: "识别", path: os.Args[0], stdin: true, extraEnv: []string{"BOKU_LIVE_HELPER=asr"}},
			processSpec{name: "llama-server", label: "翻译", path: os.Args[0], extraEnv: []string{"BOKU_LIVE_HELPER=sleep"}},
			audio, tr,
			func() []Entry { return []Entry{{Source: "部室", Target: "社团活动室"}} },
			func(state lineState) {
				if state.commit {
					mu.Lock()
					draft = state.commitSource
					mu.Unlock()
				} else if state.setDraft && state.draft != "" {
					mu.Lock()
					draft = state.draft
					mu.Unlock()
				}
				if state.setTranslation {
					got <- state.translation
				}
			})
	}()
	select {
	case text := <-got:
		if text != "社团活动室" {
			t.Fatalf("translation %q", text)
		}
	case err := <-errc:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("timed out")
	}
	cancel()
	tr.mu.Lock()
	prompt := tr.prompt
	tr.mu.Unlock()
	mu.Lock()
	seen := draft
	mu.Unlock()
	if !strings.Contains(prompt, "参考下面的翻译：") || !strings.Contains(prompt, "部室 翻译成 社团活动室") || !strings.Contains(prompt, "部室に行く") {
		t.Fatalf("prompt %q", prompt)
	}
	if strings.Contains(prompt, "上一句") {
		t.Fatalf("prompt carried context %q", prompt)
	}
	if seen != "部室に行く" && seen != "部室" {
		t.Fatalf("draft %q", seen)
	}
}

func TestRecognizerIsRestartedOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	audio := &fakeAudio{pcm: make(chan []byte)}
	err := runListen(ctx,
		processSpec{name: "crispasr", label: "识别", path: os.Args[0], extraEnv: []string{"BOKU_LIVE_HELPER=crash"}},
		processSpec{name: "llama-server", label: "翻译", path: os.Args[0], extraEnv: []string{"BOKU_LIVE_HELPER=sleep"}},
		audio, &stubTranslator{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "识别进程退出了") {
		t.Fatal(err)
	}
}

func TestFinalSentencesAreTranslatedSeparately(t *testing.T) {
	ctx, cancel := context.WithTimeout(withSeq(context.Background()), time.Second)
	defer cancel()
	tr := &stubTranslator{}
	svc := openLive(t)
	done := make(chan struct{}, 2)
	publish := func(state lineState) {
		svc.applyLine(state)
		if state.setTranslation {
			done <- struct{}{}
		}
	}
	entries := func() []Entry { return []Entry{{Source: "部室", Target: "社团活动室"}} }
	handleLine(ctx, `{"type":"partial","text":"今日は晴れです。部室に行きます！"}`, tr, entries, publish)
	if view := svc.Snapshot(); len(view.Lines) != 0 || view.Draft != "今日は晴れです。部室に行きます！" {
		t.Fatalf("partial was committed: %+v", view)
	}
	handleLine(ctx, `{"type":"final","text":"今日は晴れです。部室に行きます！"}`, tr, entries, publish)
	for range 2 {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("sentences were not translated")
		}
	}
	view := svc.Snapshot()
	if len(view.Lines) != 2 || view.Lines[0].Source != "今日は晴れです。" || view.Lines[1].Source != "部室に行きます！" || view.Lines[0].id == view.Lines[1].id || view.Draft != "" {
		t.Fatalf("%+v", view)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.prompts) != 2 {
		t.Fatalf("prompts: %q", tr.prompts)
	}
	for _, prompt := range tr.prompts {
		if strings.Contains(prompt, "今日は晴れです。部室") {
			t.Fatalf("whole paragraph translated: %q", prompt)
		}
		if strings.Contains(prompt, "今日は晴れです。") && strings.Contains(prompt, "参考下面的翻译") {
			t.Fatalf("other sentence's terminology leaked: %q", prompt)
		}
		if strings.Contains(prompt, "部室に行きます！") && !strings.Contains(prompt, "部室 翻译成 社团活动室") {
			t.Fatalf("terminology missing: %q", prompt)
		}
	}
}
