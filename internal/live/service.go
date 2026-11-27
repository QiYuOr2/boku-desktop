package live

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/platform/loopback"
	"boku/internal/platform/screenrecording"
)

// UI 决定一屏能放下多少完整配对，这里界定会话缓冲的上限。
const captionBufferLimit = 32

// Caption 是一个已完结的句子。正在识别中的 partial 是 Draft，不是 Caption。
type Caption struct {
	Source string
	Target string
	id     uint64
}

type View struct {
	Settings       Settings
	Listening      bool
	Status         string
	Draft          string
	Translation    string
	Lines          []Caption
	Slow           bool
	Notice         string
	SettingsURL    string
	Ready          bool
	Downloading    bool
	DownloadFile   string
	DownloadDone   int64
	DownloadTotal  int64
	DownloadNotice string
	GlossaryBroken bool
	Entries        []Entry
}

type Service struct {
	modelsDir string
	section   *Section
	store     StoreSettings
	glossary  *Glossary
	client    *httpc.Client

	mu             sync.Mutex
	view           View
	changed        func()
	listening      bool
	cancel         context.CancelFunc
	done           chan struct{}
	downloading    bool
	downloadCancel context.CancelFunc
	downloadDone   chan struct{}
	readyKnown     bool
	ready          bool
	readySettings  Settings
}

// Open 读取术语文件。无法解析的文件仍保留在磁盘上，在用户替换它之前表现为空表。
func Open(modelsDir, glossaryPath string, section *Section, store StoreSettings, clock domain.Clock) (*Service, error) {
	if section == nil {
		section = NewSection()
	}
	glossary, err := LoadGlossary(glossaryPath, clock)
	if err != nil {
		return nil, err
	}
	return &Service{
		modelsDir: modelsDir,
		section:   section,
		store:     store,
		glossary:  glossary,
		client:    httpc.NewDirect(httpc.Options{Timeout: 30 * time.Second, UserAgent: "Boku"}),
	}, nil
}

// SetChanged 用于告知窗口需要重绘。可从管线的 goroutine 调用，重绘由宿主投递。
func (s *Service) SetChanged(fn func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.changed = fn
	s.mu.Unlock()
}

func (s *Service) Snapshot() View {
	if s == nil {
		return View{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.view
	if len(s.view.Lines) > 0 {
		view.Lines = append([]Caption(nil), s.view.Lines...)
	}
	view.Settings = s.section.Current()
	if !s.readyKnown || view.Settings != s.readySettings {
		_, err := Resolve(s.modelsDir, view.Settings)
		s.ready = err == nil
		s.readyKnown = true
		s.readySettings = view.Settings
	}
	view.Ready = s.ready
	view.GlossaryBroken = s.glossary.Broken()
	view.Entries = s.glossary.Entries()
	return view
}

func (s *Service) notify() {
	s.mu.Lock()
	fn := s.changed
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Toggle 开始或停止监听。它不等待模型加载完成就返回；调用方是快捷键 goroutine，
// 不是主线程。
func (s *Service) Toggle() {
	if s == nil {
		return
	}
	s.mu.Lock()
	on := s.listening
	s.mu.Unlock()
	if on {
		s.Stop()
		return
	}
	s.start()
}

func (s *Service) start() {
	s.mu.Lock()
	if s.listening {
		s.mu.Unlock()
		return
	}
	settings := s.section.Current()
	if _, err := Resolve(s.modelsDir, settings); err != nil {
		s.view.Notice = err.Error()
		s.view.SettingsURL = ""
		s.ready = false
		s.readyKnown = true
		s.readySettings = settings
		s.mu.Unlock()
		s.notify()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.listening = true
	s.cancel = cancel
	s.done = done
	s.view.Listening = true
	s.view.Notice = ""
	s.view.SettingsURL = ""
	s.view.Status = "正在载入模型"
	s.view.Slow = false
	s.view.Draft = ""
	s.view.Translation = ""
	s.view.Lines = nil
	s.mu.Unlock()
	s.notify()
	go s.loop(ctx, done, settings)
}

func (s *Service) loop(ctx context.Context, done chan struct{}, settings Settings) {
	defer func() {
		s.finish()
		close(done)
	}()
	s.run(ctx, settings)
}

func (s *Service) finish() {
	s.mu.Lock()
	s.listening = false
	s.view.Listening = false
	s.view.Status = ""
	s.cancel = nil
	s.mu.Unlock()
	s.notify()
}

// Stop 结束会话并等待两个进程都退出。Quit 走同一条路径。等待有上限，避免忽略
// 终止信号的进程一直拖着应用。
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(4 * time.Second):
		}
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.CancelDownload()
	s.Stop()
}

func (s *Service) run(ctx context.Context, settings Settings) {
	if settings.Input != InputMicrophone && !screenrecording.Granted() {
		s.setNotice(loopbackDenied("screen"), screenrecording.SettingsURL())
		return
	}
	models, err := Resolve(s.modelsDir, settings)
	if err != nil {
		s.setNotice(err.Error(), "")
		return
	}
	slog.Info("live model", "event", "live_model", "file", filepath.Base(models.ASR))
	slog.Info("live model", "event", "live_model", "file", filepath.Base(models.MT))
	stream, err := loopback.Start(ctx, settings.Input)
	if err != nil {
		var denied *loopback.DeniedError
		if errors.As(err, &denied) {
			s.setNotice(denied.Error(), denied.SettingsURL)
			return
		}
		s.setNotice("没有开始采集", "")
		return
	}
	defer stream.Close()
	port, err := httpc.LocalPort()
	if err != nil {
		s.setNotice("没有可用的本机端口", "")
		return
	}
	client := llamaClient{client: s.client, base: fmt.Sprintf("http://127.0.0.1:%d", port)}
	err = runListen(ctx,
		processSpec{name: "crispasr", label: "识别", path: models.ASRExe, args: RecognizerArgs(models.ASR, models.VAD), dir: filepath.Dir(models.ASRExe), stdin: true},
		processSpec{name: "llama-server", label: "翻译", path: models.MTExe, args: TranslatorArgs(port, models.MT), dir: filepath.Dir(models.MTExe)},
		stream, client, s.entries, s.applyLine)
	if err != nil && ctx.Err() == nil {
		s.setNotice(err.Error(), "")
	}
}

func loopbackDenied(kind string) string {
	return (&loopback.DeniedError{Kind: kind}).Error()
}

func (s *Service) setNotice(text, url string) {
	s.mu.Lock()
	s.view.Notice = text
	s.view.SettingsURL = url
	s.view.Status = ""
	s.mu.Unlock()
	s.notify()
}

func (s *Service) applyLine(state lineState) {
	s.mu.Lock()
	if state.commit {
		s.view.Lines = append(s.view.Lines, Caption{Source: state.commitSource, id: state.commitID})
		if len(s.view.Lines) > captionBufferLimit {
			s.view.Lines = append([]Caption(nil), s.view.Lines[len(s.view.Lines)-captionBufferLimit:]...)
		}
		s.view.Draft = ""
		s.view.Translation = ""
	}
	if state.setDraft {
		s.view.Draft = state.draft
	}
	if state.setTranslation {
		for i := range s.view.Lines {
			if s.view.Lines[i].id == state.translationID {
				s.view.Lines[i].Target = state.translation
			}
		}
		if n := len(s.view.Lines); n > 0 && s.view.Lines[n-1].id == state.translationID {
			s.view.Translation = state.translation
		}
	}
	if state.setSlow {
		s.view.Slow = state.slow
	}
	if state.setReady && s.view.Listening {
		if state.ready {
			s.view.Status = "正在听"
		} else {
			s.view.Status = "正在载入模型"
		}
	}
	s.mu.Unlock()
	s.notify()
}

func (s *Service) entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.glossary.Entries()
}

// Download 拉取固定的模型集合。由 CancelDownload 和退出流程取消。
func (s *Service) Download() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.downloading {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.downloading = true
	s.downloadCancel = cancel
	s.downloadDone = done
	s.view.Downloading = true
	s.view.DownloadFile = ""
	s.view.DownloadDone = 0
	s.view.DownloadTotal = 0
	s.view.DownloadNotice = ""
	s.view.Notice = ""
	s.mu.Unlock()
	s.notify()
	go func() {
		defer close(done)
		file := ""
		var noted time.Time
		err := DownloadAll(ctx, s.modelsDir, func(p DownloadProgress) {
			file = p.Name
			s.mu.Lock()
			s.view.DownloadFile = p.Name
			s.view.DownloadDone = p.Done
			s.view.DownloadTotal = p.Total
			s.mu.Unlock()
			// 刷新会重绘整个窗口；每兆字节汇报一次就会让多吉字节的下载全程重绘。
			now := time.Now()
			if !noted.IsZero() && now.Sub(noted) < 200*time.Millisecond && (p.Total <= 0 || p.Done < p.Total) {
				return
			}
			noted = now
			s.notify()
		})
		s.mu.Lock()
		s.downloading = false
		s.view.Downloading = false
		s.downloadCancel = nil
		s.readyKnown = false
		if err != nil && ctx.Err() == nil {
			slog.Warn("live download failed", "event", "live_download_failed", "file", file, "error", err)
			s.view.DownloadNotice = downloadNotice(err)
		}
		s.mu.Unlock()
		s.notify()
	}()
}

// CancelDownload 停止拉取并等待它释放临时文件。
func (s *Service) CancelDownload() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.downloadCancel
	done := s.downloadDone
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
	return s.section.SetEnabled(ctx, s.store, enabled)
}

func (s *Service) SetInput(ctx context.Context, input string) error {
	s.mu.Lock()
	s.readyKnown = false
	s.mu.Unlock()
	return s.section.SetInput(ctx, s.store, input)
}

func (s *Service) SetPaths(ctx context.Context, asr, mt string) error {
	s.mu.Lock()
	s.readyKnown = false
	s.mu.Unlock()
	return s.section.SetPaths(ctx, s.store, asr, mt)
}

func (s *Service) SetOpacity(ctx context.Context, opacity int) error {
	return s.section.SetOpacity(ctx, s.store, opacity)
}

func (s *Service) AddTerm(source, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.glossary.Add(source, target)
}

func (s *Service) UpdateTerm(id, source, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.glossary.Update(id, source, target)
}

func (s *Service) DeleteTerm(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.glossary.Delete(id)
}

func (s *Service) ImportTerms(text string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.glossary.Import(text)
}

type llamaClient struct {
	client *httpc.Client
	base   string
}

func (c llamaClient) Ready(ctx context.Context) error {
	_, err := c.client.GetJSON(ctx, c.base+"/health", nil)
	return err
}

func (c llamaClient) Translate(ctx context.Context, prompt string) (string, error) {
	body, err := completionBody(prompt)
	if err != nil {
		return "", err
	}
	result, err := c.client.PostJSONWithOptions(ctx, c.base+"/v1/chat/completions", nil, body, httpc.RequestOptions{Timeout: 30 * time.Second})
	if err != nil {
		return "", err
	}
	return completionText(result.Payload)
}
