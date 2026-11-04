package live

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func TestDownloadFailureIsExplainedInSettings(t *testing.T) {
	prev := fetch
	fetch = func(context.Context, string, string, func(int64)) error {
		return errors.New(`Get "https://example.test/model": dial tcp 1.2.3.4:443: i/o timeout`)
	}
	t.Cleanup(func() { fetch = prev })

	svc := openLive(t)
	svc.Download()
	view := waitDownload(t, svc)
	if view.Downloading {
		t.Fatal("still downloading")
	}
	arts, err := PlatformArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if view.DownloadNotice != arts[0].Name+"：下载超时" {
		t.Fatalf("notice %q", view.DownloadNotice)
	}
	if strings.Contains(view.DownloadNotice, "example.test") {
		t.Fatal(view.DownloadNotice)
	}
	if view.Notice != "" {
		t.Fatalf("subtitle %q", view.Notice)
	}
}

func TestDownloadStatusIsExplained(t *testing.T) {
	prev := fetch
	fetch = func(context.Context, string, string, func(int64)) error {
		return &httpc.StatusError{Code: 503}
	}
	t.Cleanup(func() { fetch = prev })

	svc := openLive(t)
	svc.Download()
	view := waitDownload(t, svc)
	arts, err := PlatformArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if view.DownloadNotice != arts[0].Name+"：服务器返回 503" {
		t.Fatalf("notice %q", view.DownloadNotice)
	}
}

func TestDownloadCancelStaysQuiet(t *testing.T) {
	started := make(chan struct{})
	prev := fetch
	fetch = func(ctx context.Context, _, _ string, _ func(int64)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { fetch = prev })

	svc := openLive(t)
	svc.Download()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start")
	}
	svc.CancelDownload()
	view := svc.Snapshot()
	if view.Downloading || view.DownloadNotice != "" || view.Notice != "" {
		t.Fatalf("downloading=%v notice=%q subtitle=%q", view.Downloading, view.DownloadNotice, view.Notice)
	}
}

func TestDownloadBadHashIsExplained(t *testing.T) {
	prev := fetch
	fetch = func(_ context.Context, _, part string, report func(int64)) error {
		if err := os.WriteFile(part, []byte("nope"), 0o600); err != nil {
			return err
		}
		if report != nil {
			report(4)
		}
		return nil
	}
	t.Cleanup(func() { fetch = prev })

	svc := openLive(t)
	svc.Download()
	view := waitDownload(t, svc)
	arts, err := PlatformArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if view.DownloadNotice != arts[0].Name+"：文件校验失败" {
		t.Fatalf("notice %q", view.DownloadNotice)
	}
	if view.Notice != "" {
		t.Fatalf("subtitle %q", view.Notice)
	}
}

func openLive(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	svc, err := Open(filepath.Join(dir, "models"), filepath.Join(dir, "glossary.json"), NewSection(), nil, domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func waitDownload(t *testing.T, svc *Service) View {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		view := svc.Snapshot()
		if !view.Downloading {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatal("download did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCaptionsKeepTheRecentLines(t *testing.T) {
	svc := openLive(t)
	svc.applyLine(lineState{commit: true, commitID: 1, commitSource: "一"})
	svc.applyLine(lineState{setTranslation: true, translationID: 1, translation: "1"})
	svc.applyLine(lineState{setDraft: true, draft: "二の途中"})
	view := svc.Snapshot()
	if len(view.Lines) != 1 || view.Lines[0].Source != "一" || view.Lines[0].Target != "1" || view.Draft != "二の途中" {
		t.Fatalf("%+v draft %q", view.Lines, view.Draft)
	}
	svc.applyLine(lineState{commit: true, commitID: 2, commitSource: "二", setDraft: true, draft: ""})
	svc.applyLine(lineState{setTranslation: true, translationID: 1, translation: "1b"})
	svc.applyLine(lineState{setTranslation: true, translationID: 2, translation: "2"})
	view = svc.Snapshot()
	if view.Draft != "" || len(view.Lines) != 2 || view.Lines[0].Target != "1b" || view.Lines[1].Target != "2" {
		t.Fatalf("%+v draft %q", view.Lines, view.Draft)
	}
	for id := uint64(3); id <= 4; id++ {
		svc.applyLine(lineState{commit: true, commitID: id, commitSource: string(rune('0' + id))})
	}
	view = svc.Snapshot()
	if len(view.Lines) != 4 || view.Lines[0].Source != "一" {
		t.Fatalf("%+v", view.Lines)
	}
	if view.Lines[len(view.Lines)-1].Source != "4" {
		t.Fatalf("%+v", view.Lines)
	}
	for id := uint64(5); id <= captionBufferLimit+1; id++ {
		svc.applyLine(lineState{commit: true, commitID: id, commitSource: "短句"})
	}
	view = svc.Snapshot()
	if len(view.Lines) != captionBufferLimit || view.Lines[0].Source != "二" || view.Lines[0].Target != "2" || view.Lines[len(view.Lines)-1].id != captionBufferLimit+1 {
		t.Fatalf("%+v", view.Lines)
	}
}
