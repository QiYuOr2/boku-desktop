package ui

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"boku/internal/diary"
	"boku/internal/domain"
	"boku/internal/presentation"
)

type viewMode int

const (
	modeTimeline viewMode = iota
	modeDetail
	modeEditor
)

type saveState int

const (
	saveIdle saveState = iota
	saveSaving
	saveSaved
	saveFailed
)

type inlinePanel int

const (
	panelNone inlinePanel = iota
	panelDate
	panelTag
	panelLink
	panelEditLink
	panelLocation
	panelWeather
	panelStyle
)

const (
	defaultPageSize = 30

	defaultAutoSyncCooldown     = 30 * time.Second
	defaultAutoSyncPollInterval = 3 * time.Minute
	defaultAutoSyncStartupDelay = 3 * time.Second
	defaultAutoSyncQuitTimeout  = 4 * time.Second
)

// State 持有桌面端日记窗口与设置页日记区的全部交互状态。
// 所有字段仅在主 UI 线程上读写，后台 I/O 与网络结果都经 host.OnMain 回到主线程。
type State struct {
	svc          *diary.Service
	host         presentation.Host
	mark         *ui.SVG
	openSettings func()

	mode         viewMode
	shownMonth   diary.Month
	selectedDate diary.DateKey
	filterByDate bool
	dayCounts    map[diary.DateKey]int

	searchQuery     string
	lastSearchQuery string
	pageLimit       int
	entries         []diary.Entry
	hasMore         bool
	loadErr         error

	currentEntry     *diary.Entry
	confirmDeleteID  string
	lightboxFilename string

	mdOpts     markdown.Options
	mdDarkInit bool
	mdDark     bool

	bitmaps       map[string]*ui.Bitmap
	bitmapLoading map[string]bool
	bitmapFailed  map[string]bool

	ed   editorState
	sync syncUIState
}

type editorState struct {
	id                 string
	isNew              bool
	dateKey            diary.DateKey
	createdAt          time.Time
	title              string
	markdown           string
	lastTitle          string
	lastMarkdown       string
	tags               []string
	attachments        []diary.Attachment
	uploads            map[string]diary.AttachmentUpload
	removedAttachments map[string]bool
	linkCards          []diary.LinkCard
	location           *diary.LocationSnapshot
	weather            *diary.WeatherSnapshot

	tab            int // 0: 编辑, 1: 分栏, 2: 预览
	dirty          bool
	savedInSession bool
	saveState      saveState
	saveErr        string
	metaNotice     string
	autoSaveGen    uint64
	autoSaveTimer  *time.Timer

	panel            inlinePanel
	dateInput        string
	tagInput         string
	linkInput        string
	linkBusy         bool
	linkErr          string
	editLinkID       string
	editLinkTitle    string
	editLinkSummary  string
	editLinkPreview  bool
	locationInput    string
	weatherInput     string
	weatherBusy      bool
	weatherErr       string
	titleHandle      ui.Handle
	focusTitle       bool
	bodyHandle       ui.Handle
	panelInputHandle ui.Handle
	focusPanelInput  bool
}

type syncUIState struct {
	draftSettings diary.Settings
	settingsMsg   string
	settingsWarn  bool

	status          diary.SyncStatus
	emailInput      string
	codeInput       string
	deviceNameInput string
	awaitingCode    bool
	devices         []diary.Device
	conflicts       []diary.SyncConflict
	syncing         bool
	busy            bool
	message         string
	messageWarn     bool

	confirmLogout      bool
	confirmRevokeID    string
	confirmKeepCloudID string
	confirmKeepLocalID string

	lastSyncAt      time.Time
	pendingFollowUp bool
	windowOpen      bool
	stopped         bool
	pollTimer       *time.Timer
	startupTimer    *time.Timer
	cooldown        time.Duration
	pollInterval    time.Duration
	startupDelay    time.Duration
	quitTimeout     time.Duration
}

// New 构造日记 UI 状态。svc 可为空（例如启动初始化失败时），界面会安静降级。
func New(svc *diary.Service, host presentation.Host, mark *ui.SVG, openSettings func()) *State {
	today := diary.Today(time.Now())
	var settings diary.Settings
	if svc != nil {
		today = svc.Today()
		settings = svc.Settings()
	}
	s := &State{
		svc:           svc,
		host:          host,
		mark:          mark,
		openSettings:  openSettings,
		mode:          modeTimeline,
		shownMonth:    today.MonthValue(),
		selectedDate:  today,
		dayCounts:     map[diary.DateKey]int{},
		pageLimit:     defaultPageSize,
		bitmaps:       map[string]*ui.Bitmap{},
		bitmapLoading: map[string]bool{},
		bitmapFailed:  map[string]bool{},
		sync: syncUIState{
			draftSettings:   settings,
			deviceNameInput: diary.DefaultDeviceName(),
			cooldown:        defaultAutoSyncCooldown,
			pollInterval:    defaultAutoSyncPollInterval,
			startupDelay:    defaultAutoSyncStartupDelay,
			quitTimeout:     defaultAutoSyncQuitTimeout,
		},
	}
	s.mdOpts = markdown.Options{
		OnLink: openValidatedLink,
		LoadImage: func(rawURL string) (ui.ImageSource, error) {
			if s.svc == nil {
				return nil, domain.ErrProviderUnavailable
			}
			data, err := s.svc.LoadRemoteImage(context.Background(), rawURL)
			if err != nil {
				return nil, err
			}
			return ui.DecodeBitmap(data)
		},
	}
	return s
}

func (s *State) markdownOptions(dark bool) *markdown.Options {
	if !s.mdDarkInit || s.mdDark != dark {
		s.mdDarkInit = true
		s.mdDark = dark
		s.mdOpts.Theme = diaryMarkdownTheme(dark)
		s.mdOpts.Block = func(c *ui.Context, d *parser.Document, i int) bool {
			return renderStyleTagBlock(c, d, i, dark)
		}
	}
	return &s.mdOpts
}

func (s *State) today() diary.DateKey {
	if s.svc == nil {
		return diary.Today(time.Now())
	}
	return s.svc.Today()
}

// Reload 从本地 SQLite 刷新日历计数、时间线列表与当前详情条目。
func (s *State) Reload() {
	if s.svc == nil {
		return
	}
	counts, err := s.svc.DayCounts()
	if err != nil {
		s.loadErr = err
		s.host.Refresh()
		return
	}
	s.loadErr = nil
	s.dayCounts = counts
	s.reloadList()
	if s.mode == modeDetail && s.currentEntry != nil {
		if latest, err := s.svc.Entry(s.currentEntry.ID); err == nil {
			if latest == nil {
				s.currentEntry = nil
				s.mode = modeTimeline
			} else {
				s.currentEntry = latest
			}
		}
	}
	s.ReloadSyncStatusLocal()
	s.host.Refresh()
}

func (s *State) reloadList() {
	if s.svc == nil {
		return
	}
	query := strings.TrimSpace(s.searchQuery)
	if query != "" {
		items, err := s.svc.SearchEntries(query, s.pageLimit)
		if err != nil {
			s.loadErr = err
			return
		}
		s.entries = items
		s.hasMore = len(items) >= s.pageLimit
		return
	}
	if s.filterByDate {
		items, err := s.svc.EntriesOn(s.selectedDate)
		if err != nil {
			s.loadErr = err
			return
		}
		s.entries = items
		s.hasMore = false
		return
	}
	items, err := s.svc.RecentEntries(s.pageLimit+1, 0)
	if err != nil {
		s.loadErr = err
		return
	}
	if len(items) > s.pageLimit {
		s.entries = items[:s.pageLimit]
		s.hasMore = true
	} else {
		s.entries = items
		s.hasMore = false
	}
}

// ReloadSyncStatusLocal 只读本地 SQLite 与 secrets.json 刷新同步汇总与冲突列表，不发起网络请求。
func (s *State) ReloadSyncStatusLocal() {
	if s.svc == nil {
		return
	}
	ctx := context.Background()
	if status, err := s.svc.SyncStatus(ctx); err == nil {
		s.sync.status = status
	}
	if conflicts, err := s.svc.Conflicts(ctx); err == nil {
		s.sync.conflicts = conflicts
	}
}

// ReloadSyncSettings 在打开设置页或云同步变更后刷新设置草稿、本地状态与设备列表。
func (s *State) ReloadSyncSettings() {
	if s.svc == nil {
		return
	}
	s.sync.draftSettings = s.svc.Settings()
	s.ReloadSyncStatusLocal()
	s.host.Refresh()
	if !s.sync.status.LoggedIn || !s.sync.status.Configured {
		s.sync.devices = nil
		return
	}
	go func() {
		devices, err := s.svc.ListDevices(context.Background())
		s.host.OnMain(func() {
			if err == nil {
				s.sync.devices = devices
			}
			s.host.Refresh()
		})
	}()
}

// StartAutoSync 在应用就绪后延迟触发一次自动同步，不阻塞托盘启动。
func (s *State) StartAutoSync() {
	if s.svc == nil || s.sync.stopped {
		return
	}
	if s.sync.startupTimer != nil {
		s.sync.startupTimer.Stop()
	}
	s.sync.startupTimer = time.AfterFunc(s.sync.startupDelay, func() {
		s.host.OnMain(func() {
			if s.sync.stopped {
				return
			}
			s.sync.startupTimer = nil
			s.ReloadSyncStatusLocal()
			s.TriggerAutoSync(false)
		})
	})
}

// OnWindowOpen 在日记窗口打开时刷新列表、触发冷却保护的自动拉取，并开启低频兜底轮询。
func (s *State) OnWindowOpen() {
	s.sync.windowOpen = true
	s.Reload()
	s.TriggerAutoSync(false)
	s.scheduleSyncPoll()
}

// OnWindowFocus 在日记窗口重新聚焦时触发冷却保护的自动拉取（编辑态跳过）。
func (s *State) OnWindowFocus() {
	if !s.sync.windowOpen {
		return
	}
	s.TriggerAutoSync(false)
}

// OnWindowClose 在日记窗口关闭时停止前台轮询、落盘未保存草稿，若有待同步改动则后台推送一次。
func (s *State) OnWindowClose() {
	s.sync.windowOpen = false
	s.stopSyncPoll()
	s.FlushOnClose()
	s.ReloadSyncStatusLocal()
	if s.sync.status.PendingCount > 0 {
		s.TriggerAutoSync(true)
	}
}

// ShutdownBeforeQuit 在应用退出前停掉定时器、落盘草稿，并在短超时内尽力推送未同步改动。
func (s *State) ShutdownBeforeQuit() {
	s.sync.stopped = true
	s.sync.windowOpen = false
	s.stopSyncPoll()
	if s.sync.startupTimer != nil {
		s.sync.startupTimer.Stop()
		s.sync.startupTimer = nil
	}
	s.FlushOnClose()
	if s.svc == nil {
		return
	}
	status, err := s.svc.SyncStatus(context.Background())
	if err != nil || !status.LoggedIn || !status.Configured || status.PendingCount == 0 {
		return
	}
	timeout := s.sync.quitTimeout
	if timeout <= 0 {
		timeout = defaultAutoSyncQuitTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, _ = s.svc.SyncNow(ctx)
}

func (s *State) scheduleSyncPoll() {
	s.stopSyncPoll()
	if s.svc == nil || s.sync.stopped || !s.sync.windowOpen || s.sync.pollInterval <= 0 {
		return
	}
	s.sync.pollTimer = time.AfterFunc(s.sync.pollInterval, func() {
		s.host.OnMain(func() {
			if s.sync.stopped || !s.sync.windowOpen {
				return
			}
			s.TriggerAutoSync(false)
			s.scheduleSyncPoll()
		})
	})
}

func (s *State) stopSyncPoll() {
	if s.sync.pollTimer != nil {
		s.sync.pollTimer.Stop()
		s.sync.pollTimer = nil
	}
}

// FlushOnClose 在窗口关闭或应用退出前落盘未保存的编辑草稿，避免丢失内容。
func (s *State) FlushOnClose() {
	if s.mode != modeEditor || s.svc == nil {
		return
	}
	s.stopAutoSave()
	if s.ed.dirty && !s.editorDraftBlank() {
		_ = s.SaveNow()
	}
}

// --- 日历与列表交互 ---

func (s *State) PrevMonth() {
	s.shownMonth = s.shownMonth.AddMonths(-1)
	s.host.Refresh()
}

func (s *State) NextMonth() {
	next := s.shownMonth.AddMonths(1)
	if next.Compare(s.today().MonthValue()) > 0 {
		return
	}
	s.shownMonth = next
	s.host.Refresh()
}

func (s *State) JumpToday() {
	today := s.today()
	s.shownMonth = today.MonthValue()
	s.selectedDate = today
	s.filterByDate = false
	s.searchQuery = ""
	s.lastSearchQuery = ""
	s.pageLimit = defaultPageSize
	s.reloadList()
	s.host.Refresh()
}

func (s *State) SelectDay(day diary.DateKey) {
	if day.IsFuture(s.now()) {
		return
	}
	if s.selectedDate == day && s.filterByDate {
		s.filterByDate = false
	} else {
		s.selectedDate = day
		s.shownMonth = day.MonthValue()
		s.filterByDate = true
	}
	s.searchQuery = ""
	s.lastSearchQuery = ""
	s.confirmDeleteID = ""
	if s.mode == modeDetail {
		s.mode = modeTimeline
		s.currentEntry = nil
	}
	s.reloadList()
	s.host.Refresh()
}

func (s *State) ClearDateFilter() {
	s.filterByDate = false
	s.reloadList()
	s.host.Refresh()
}

func (s *State) onSearchTick() {
	if s.searchQuery == s.lastSearchQuery {
		return
	}
	s.lastSearchQuery = s.searchQuery
	s.pageLimit = defaultPageSize
	s.reloadList()
}

func (s *State) ClearSearch() {
	s.searchQuery = ""
	s.lastSearchQuery = ""
	s.reloadList()
	s.host.Refresh()
}

func (s *State) LoadMore() {
	s.pageLimit += defaultPageSize
	s.reloadList()
	s.host.Refresh()
}

// --- 详情与删除 ---

func (s *State) OpenEntry(entry diary.Entry) {
	wasEditing := s.mode == modeEditor
	if wasEditing && s.ed.dirty && !s.editorDraftBlank() {
		_ = s.SaveNow()
	}
	shouldSync := wasEditing && s.ed.savedInSession
	s.stopAutoSave()
	copied := entry
	s.currentEntry = &copied
	s.confirmDeleteID = ""
	s.lightboxFilename = ""
	s.mode = modeDetail
	s.host.Refresh()
	if shouldSync {
		s.TriggerAutoSync(true)
	}
}

func (s *State) BackToTimeline() {
	s.confirmDeleteID = ""
	s.lightboxFilename = ""
	s.currentEntry = nil
	s.mode = modeTimeline
	s.Reload()
}

func (s *State) RequestDelete(id string) {
	if s.svc == nil {
		return
	}
	if s.confirmDeleteID != id {
		s.confirmDeleteID = id
		s.host.Refresh()
		return
	}
	s.confirmDeleteID = ""
	if err := s.svc.Delete(id); err != nil {
		s.loadErr = err
		s.host.Refresh()
		return
	}
	if s.currentEntry != nil && s.currentEntry.ID == id {
		s.currentEntry = nil
		s.mode = modeTimeline
	}
	s.Reload()
	s.TriggerAutoSync(true)
}

func (s *State) OpenLightbox(filename string) {
	s.lightboxFilename = filename
	s.host.Refresh()
}

func (s *State) CloseLightbox() {
	s.lightboxFilename = ""
	s.host.Refresh()
}

// --- 编辑器 ---

func (s *State) NewEntry(date diary.DateKey) {
	if s.svc == nil {
		return
	}
	if date.IsZero() || date.IsFuture(s.now()) {
		date = s.today()
	}
	id, err := domain.NewID()
	if err != nil {
		return
	}
	s.stopAutoSave()
	s.ed = editorState{
		id:                 id,
		isNew:              true,
		dateKey:            date,
		createdAt:          s.now().UTC(),
		uploads:            map[string]diary.AttachmentUpload{},
		removedAttachments: map[string]bool{},
		tab:                0,
		saveState:          saveIdle,
		focusTitle:         true,
	}
	s.mode = modeEditor
	s.confirmDeleteID = ""
	s.lightboxFilename = ""
	s.host.Refresh()

	// 新建日记时按设置后台采集默认地点 / 天气快照（两项都关闭时不启动后台任务）。
	cfg := s.svc.Settings()
	if !cfg.LocationEnabled && !cfg.WeatherEnabled {
		return
	}
	entryID := id
	go func() {
		meta := s.svc.CaptureNewEntryMetadata(context.Background())
		s.host.OnMain(func() {
			if s.mode != modeEditor || s.ed.id != entryID {
				return
			}
			if meta.Location != nil && s.ed.location == nil {
				s.ed.location = meta.Location
			}
			if meta.Weather != nil && s.ed.weather == nil {
				s.ed.weather = meta.Weather
			}
			var notes []string
			if meta.LocationMessage != "" {
				notes = append(notes, meta.LocationMessage)
			}
			if meta.WeatherMessage != "" {
				notes = append(notes, meta.WeatherMessage)
			}
			s.ed.metaNotice = strings.Join(notes, " ")
			s.host.Refresh()
		})
	}()
}

func (s *State) EditEntry(entry diary.Entry) {
	s.stopAutoSave()
	tags := append([]string(nil), entry.Tags...)
	atts := append([]diary.Attachment(nil), entry.Attachments...)
	cards := append([]diary.LinkCard(nil), entry.LinkCards...)
	var loc *diary.LocationSnapshot
	if entry.Location != nil {
		c := *entry.Location
		loc = &c
	}
	var weather *diary.WeatherSnapshot
	if entry.Weather != nil {
		c := *entry.Weather
		weather = &c
	}
	s.ed = editorState{
		id:                 entry.ID,
		isNew:              false,
		dateKey:            entry.DiaryDateKey,
		createdAt:          entry.CreatedAt,
		title:              entry.Title,
		markdown:           entry.Markdown,
		lastTitle:          entry.Title,
		lastMarkdown:       entry.Markdown,
		tags:               tags,
		attachments:        atts,
		uploads:            map[string]diary.AttachmentUpload{},
		removedAttachments: map[string]bool{},
		linkCards:          cards,
		location:           loc,
		weather:            weather,
		tab:                0,
		saveState:          saveSaved,
		focusTitle:         true,
	}
	s.mode = modeEditor
	s.confirmDeleteID = ""
	s.lightboxFilename = ""
	s.host.Refresh()
}

func (s *State) onEditorInputTick() {
	if s.ed.title == s.ed.lastTitle && s.ed.markdown == s.ed.lastMarkdown {
		return
	}
	s.ed.lastTitle = s.ed.title
	s.ed.lastMarkdown = s.ed.markdown
	s.markEditorDirty()
}

func (s *State) markEditorDirty() {
	s.ed.dirty = true
	s.ed.saveState = saveIdle
	s.ed.saveErr = ""
	s.scheduleAutoSave()
}

func (s *State) scheduleAutoSave() {
	s.stopAutoSave()
	s.ed.autoSaveGen++
	gen := s.ed.autoSaveGen
	s.ed.autoSaveTimer = time.AfterFunc(1200*time.Millisecond, func() {
		s.host.OnMain(func() {
			if s.mode != modeEditor || s.ed.autoSaveGen != gen || !s.ed.dirty {
				return
			}
			if s.editorDraftBlank() {
				return
			}
			_ = s.SaveNow()
		})
	})
}

func (s *State) stopAutoSave() {
	if s.ed.autoSaveTimer != nil {
		s.ed.autoSaveTimer.Stop()
		s.ed.autoSaveTimer = nil
	}
}

func (s *State) editorDraftBlank() bool {
	return strings.TrimSpace(s.ed.title) == "" &&
		strings.TrimSpace(s.ed.markdown) == "" &&
		len(s.ed.tags) == 0 &&
		len(s.ed.attachments) == 0 &&
		len(s.ed.linkCards) == 0
}

func (s *State) SaveNow() error {
	if s.svc == nil {
		return nil
	}
	if s.ed.isNew && s.editorDraftBlank() {
		s.ed.dirty = false
		return nil
	}
	s.ed.saveState = saveSaving
	s.ed.saveErr = ""

	uploads := make([]diary.AttachmentUpload, 0, len(s.ed.uploads))
	for _, up := range s.ed.uploads {
		uploads = append(uploads, up)
	}
	draft := diary.Entry{
		ID:           s.ed.id,
		Title:        s.ed.title,
		Markdown:     s.ed.markdown,
		DiaryDateKey: s.ed.dateKey,
		CreatedAt:    s.ed.createdAt,
		Tags:         s.ed.tags,
		Attachments:  s.ed.attachments,
		LinkCards:    s.ed.linkCards,
		Location:     s.ed.location,
		Weather:      s.ed.weather,
	}
	saved, err := s.svc.Save(draft, uploads, s.ed.removedAttachments)
	if err != nil {
		s.ed.saveState = saveFailed
		if errors.Is(err, diary.ErrFutureDate) {
			s.ed.saveErr = "不能把日记写到未来的日期。"
		} else {
			s.ed.saveErr = "保存失败，请重试。"
		}
		s.host.Refresh()
		return err
	}
	s.ed.isNew = false
	s.ed.createdAt = saved.CreatedAt
	s.ed.attachments = saved.Attachments
	s.ed.tags = saved.Tags
	s.ed.uploads = map[string]diary.AttachmentUpload{}
	s.ed.removedAttachments = map[string]bool{}
	s.ed.dirty = false
	s.ed.savedInSession = true
	s.ed.saveState = saveSaved
	s.ed.saveErr = ""
	s.currentEntry = &saved
	s.Reload()
	return nil
}

func (s *State) DoneEditing() {
	s.stopAutoSave()
	if s.ed.isNew && s.editorDraftBlank() {
		s.mode = modeTimeline
		s.currentEntry = nil
		s.Reload()
		return
	}
	if s.ed.dirty || s.ed.isNew {
		if err := s.SaveNow(); err != nil {
			return
		}
	}
	shouldSync := s.ed.savedInSession
	if s.currentEntry != nil {
		s.mode = modeDetail
	} else {
		s.mode = modeTimeline
	}
	s.Reload()
	if shouldSync {
		s.TriggerAutoSync(true)
	}
}

func (s *State) CancelEditing() {
	s.stopAutoSave()
	if s.ed.isNew && s.editorDraftBlank() {
		s.mode = modeTimeline
		s.currentEntry = nil
		s.host.Refresh()
		return
	}
	if s.ed.dirty && !s.editorDraftBlank() {
		_ = s.SaveNow()
	}
	shouldSync := s.ed.savedInSession
	if s.currentEntry != nil {
		s.mode = modeDetail
	} else {
		s.mode = modeTimeline
	}
	s.Reload()
	if shouldSync {
		s.TriggerAutoSync(true)
	}
}

func (s *State) ApplyFormat(cmd diary.FormatCommand) {
	n := utf8.RuneCountInString(s.ed.markdown)
	updated, _, _ := diary.ApplyFormat(cmd, s.ed.markdown, n, n)
	s.ed.markdown = updated
	s.ed.lastMarkdown = updated
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) OpenPanel(p inlinePanel) {
	if s.ed.panel == p {
		s.ed.panel = panelNone
		s.host.Refresh()
		return
	}
	s.ed.panel = p
	s.ed.linkErr = ""
	s.ed.weatherErr = ""
	s.ed.focusPanelInput = true
	switch p {
	case panelDate:
		s.ed.dateInput = s.ed.dateKey.String()
	case panelLocation:
		if s.ed.location != nil {
			s.ed.locationInput = s.ed.location.Place
		} else {
			s.ed.locationInput = s.sync.draftSettings.LocationPlace
		}
	case panelWeather:
		if s.ed.weather != nil && s.ed.weather.Place != "" {
			s.ed.weatherInput = s.ed.weather.Place
		} else {
			s.ed.weatherInput = s.sync.draftSettings.WeatherPlace
		}
	}
	s.host.Refresh()
}

func (s *State) ApplyDateInput() {
	parsed, err := diary.ParseDateKey(strings.TrimSpace(s.ed.dateInput))
	if err != nil {
		s.ed.saveErr = "日期格式应为 YYYY-MM-DD。"
		s.host.Refresh()
		return
	}
	if parsed.IsFuture(s.now()) {
		s.ed.saveErr = "不能选择未来的日期。"
		s.host.Refresh()
		return
	}
	s.ed.dateKey = parsed
	s.ed.saveErr = ""
	s.ed.panel = panelNone
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) AddTag() {
	tag := strings.TrimSpace(s.ed.tagInput)
	if tag == "" {
		return
	}
	if len(s.ed.tags) >= diary.MaxTags {
		s.ed.saveErr = "最多添加 20 个标签。"
		s.host.Refresh()
		return
	}
	s.ed.tags = diary.UniqueTags(append(s.ed.tags, tag))
	s.ed.tagInput = ""
	s.ed.saveErr = ""
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) RemoveTag(tag string) {
	out := make([]string, 0, len(s.ed.tags))
	for _, t := range s.ed.tags {
		if t != tag {
			out = append(out, t)
		}
	}
	s.ed.tags = out
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) PickImages() {
	if s.svc == nil {
		return
	}
	entryID := s.ed.id
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:    "添加图片",
			Multiple: true,
			Filters: []mygo.FileFilter{
				{Name: "图片", Extensions: []string{"jpg", "jpeg", "png", "heic"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		var uploads []diary.AttachmentUpload
		var firstErr error
		for _, p := range paths {
			up, prepErr := s.svc.PrepareAttachmentUpload(p)
			if prepErr != nil {
				if firstErr == nil {
					firstErr = prepErr
				}
				continue
			}
			uploads = append(uploads, up)
		}
		s.host.OnMain(func() {
			if s.mode != modeEditor || s.ed.id != entryID {
				return
			}
			for _, up := range uploads {
				s.ed.uploads[up.ID] = up
				s.ed.attachments = append(s.ed.attachments, up.Attachment())
				if bmp, decErr := ui.DecodeBitmap(up.Data); decErr == nil {
					s.bitmaps[up.Filename] = bmp
				}
			}
			if firstErr != nil {
				if errors.Is(firstErr, diary.ErrAttachmentTooLarge) {
					s.ed.saveErr = "单张图片不能超过 24 MB。"
				} else {
					s.ed.saveErr = "部分图片格式不受支持（仅支持 JPG、PNG、HEIC）。"
				}
			}
			if len(uploads) > 0 {
				s.markEditorDirty()
			}
			s.host.Refresh()
		})
	}()
}

func (s *State) RemoveAttachment(id string) {
	out := make([]diary.Attachment, 0, len(s.ed.attachments))
	for _, att := range s.ed.attachments {
		if att.ID == id {
			delete(s.ed.uploads, id)
			s.ed.removedAttachments[id] = true
			continue
		}
		out = append(out, att)
	}
	s.ed.attachments = out
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) AddLinkCard() {
	if s.svc == nil || s.ed.linkBusy {
		return
	}
	raw := strings.TrimSpace(s.ed.linkInput)
	cleanURL, ok := diary.ValidateWebURL(raw)
	if !ok {
		s.ed.linkErr = "请输入有效的 HTTP 或 HTTPS 链接。"
		s.host.Refresh()
		return
	}
	s.ed.linkBusy = true
	s.ed.linkErr = ""
	entryID := s.ed.id
	s.host.Refresh()

	go func() {
		card, err := s.svc.FetchLinkCard(context.Background(), cleanURL)
		if err != nil {
			// 抓取失败仍保存为纯链接卡片，与 Apple 端行为一致。
			id, _ := domain.NewID()
			card = diary.LinkCard{
				ID:           id,
				URL:          cleanURL,
				Title:        diary.DomainOfURL(cleanURL),
				Domain:       diary.DomainOfURL(cleanURL),
				ShowsPreview: false,
				FetchFailed:  true,
			}
		}
		s.host.OnMain(func() {
			if s.mode != modeEditor || s.ed.id != entryID {
				return
			}
			s.ed.linkBusy = false
			s.ed.linkInput = ""
			s.ed.panel = panelNone
			s.ed.linkCards = append(s.ed.linkCards, card)
			s.markEditorDirty()
			s.host.Refresh()
		})
	}()
}

func (s *State) BeginEditLinkCard(card diary.LinkCard) {
	s.ed.panel = panelEditLink
	s.ed.editLinkID = card.ID
	s.ed.editLinkTitle = card.Title
	s.ed.editLinkSummary = card.Summary
	s.ed.editLinkPreview = card.ShowsPreview
	s.ed.focusPanelInput = true
	s.host.Refresh()
}

func (s *State) SaveEditLinkCard() {
	for i := range s.ed.linkCards {
		if s.ed.linkCards[i].ID == s.ed.editLinkID {
			if t := strings.TrimSpace(s.ed.editLinkTitle); t != "" {
				s.ed.linkCards[i].Title = t
			}
			s.ed.linkCards[i].Summary = strings.TrimSpace(s.ed.editLinkSummary)
			s.ed.linkCards[i].ShowsPreview = s.ed.editLinkPreview
			break
		}
	}
	s.ed.panel = panelNone
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) RemoveLinkCard(id string) {
	out := make([]diary.LinkCard, 0, len(s.ed.linkCards))
	for _, c := range s.ed.linkCards {
		if c.ID != id {
			out = append(out, c)
		}
	}
	s.ed.linkCards = out
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) ApplyLocationInput() {
	place := strings.TrimSpace(s.ed.locationInput)
	if place == "" {
		s.ed.location = nil
	} else {
		s.ed.location = &diary.LocationSnapshot{Place: place, CapturedAt: s.now().UTC()}
	}
	s.ed.panel = panelNone
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) ClearLocation() {
	s.ed.location = nil
	s.markEditorDirty()
	s.host.Refresh()
}

func (s *State) FetchWeatherInput() {
	if s.svc == nil || s.ed.weatherBusy {
		return
	}
	place := strings.TrimSpace(s.ed.weatherInput)
	if place == "" {
		s.ed.weatherErr = "请输入城市或地区名称。"
		s.host.Refresh()
		return
	}
	s.ed.weatherBusy = true
	s.ed.weatherErr = ""
	entryID := s.ed.id
	s.host.Refresh()

	go func() {
		snap, err := s.svc.FetchWeather(context.Background(), place)
		s.host.OnMain(func() {
			if s.mode != modeEditor || s.ed.id != entryID {
				return
			}
			s.ed.weatherBusy = false
			if err != nil {
				s.ed.weatherErr = "未能获取该地点的天气，请换个城市名重试。"
			} else {
				s.ed.weather = &snap
				s.ed.panel = panelNone
				s.markEditorDirty()
			}
			s.host.Refresh()
		})
	}()
}

func (s *State) ClearWeather() {
	s.ed.weather = nil
	s.markEditorDirty()
	s.host.Refresh()
}

// attachmentBitmap 按需异步解码本地附件图片，绝不阻塞绘制帧。
func (s *State) attachmentBitmap(filename string) *ui.Bitmap {
	if bmp, ok := s.bitmaps[filename]; ok {
		return bmp
	}
	if s.bitmapLoading[filename] || s.bitmapFailed[filename] || s.svc == nil {
		return nil
	}
	if !s.svc.AttachmentExists(filename) {
		return nil
	}
	s.bitmapLoading[filename] = true
	path := s.svc.AttachmentPath(filename)
	go func() {
		data, err := os.ReadFile(path)
		var bmp *ui.Bitmap
		if err == nil {
			bmp, err = ui.DecodeBitmap(data)
		}
		s.host.OnMain(func() {
			delete(s.bitmapLoading, filename)
			if err != nil || bmp == nil {
				s.bitmapFailed[filename] = true
			} else {
				s.bitmaps[filename] = bmp
			}
			s.host.Refresh()
		})
	}()
	return nil
}

// --- 设置与云同步动作 ---

func (s *State) SaveDiarySettings() {
	if s.svc == nil {
		return
	}
	if err := s.svc.UpdateSettings(context.Background(), s.sync.draftSettings); err != nil {
		s.sync.settingsMsg = "设置无效，请检查服务地址（需 http:// 或 https://）与地点长度。"
		s.sync.settingsWarn = true
		s.host.Refresh()
		return
	}
	s.sync.draftSettings = s.svc.Settings()
	s.sync.settingsMsg = "已保存。"
	s.sync.settingsWarn = false
	s.ReloadSyncStatusLocal()
	s.host.Refresh()
}

func (s *State) SendLoginCode() {
	if s.svc == nil || s.sync.busy {
		return
	}
	email := strings.TrimSpace(s.sync.emailInput)
	if email == "" || !strings.Contains(email, "@") {
		s.sync.message = "请输入有效的邮箱地址。"
		s.sync.messageWarn = true
		s.host.Refresh()
		return
	}
	s.sync.busy = true
	s.sync.message = ""
	s.sync.messageWarn = false
	s.host.Refresh()

	go func() {
		err := s.svc.SendLoginCode(context.Background(), email)
		s.host.OnMain(func() {
			s.sync.busy = false
			if err != nil {
				s.sync.message = diary.SyncUserMessage(err)
				s.sync.messageWarn = true
			} else {
				s.sync.awaitingCode = true
				s.sync.message = "验证码已发送至邮箱。"
				s.sync.messageWarn = false
			}
			s.host.Refresh()
		})
	}()
}

func (s *State) CompleteLogin() {
	if s.svc == nil || s.sync.busy {
		return
	}
	email := strings.TrimSpace(s.sync.emailInput)
	code := strings.TrimSpace(s.sync.codeInput)
	deviceName := strings.TrimSpace(s.sync.deviceNameInput)
	if code == "" {
		s.sync.message = "请输入 6 位验证码。"
		s.sync.messageWarn = true
		s.host.Refresh()
		return
	}
	s.sync.busy = true
	s.sync.message = ""
	s.sync.messageWarn = false
	s.host.Refresh()

	go func() {
		err := s.svc.Login(context.Background(), email, code, deviceName)
		s.host.OnMain(func() {
			s.sync.busy = false
			if err != nil {
				s.sync.message = diary.SyncUserMessage(err)
				s.sync.messageWarn = true
			} else {
				s.sync.awaitingCode = false
				s.sync.codeInput = ""
				s.sync.message = "登录成功。"
				s.sync.messageWarn = false
				s.ReloadSyncSettings()
				s.TriggerAutoSync(true)
			}
			s.host.Refresh()
		})
	}()
}

func (s *State) CancelLogin() {
	s.sync.awaitingCode = false
	s.sync.codeInput = ""
	s.sync.message = ""
	s.host.Refresh()
}

func (s *State) SyncNow() {
	s.runSync(true, true)
}

// TriggerAutoSync 发起一次静默自动同步；窗口处于编辑态时跳过，避免写作期间高频同步。
func (s *State) TriggerAutoSync(force bool) {
	s.runSync(false, force)
}

func (s *State) runSync(manual, force bool) {
	if s.svc == nil || s.sync.stopped {
		return
	}
	if !manual {
		if s.mode == modeEditor && s.sync.windowOpen {
			return
		}
		if !s.sync.status.LoggedIn || !s.sync.status.Configured {
			s.ReloadSyncStatusLocal()
			if !s.sync.status.LoggedIn || !s.sync.status.Configured {
				return
			}
		}
		if !force && !s.sync.lastSyncAt.IsZero() && s.now().Sub(s.sync.lastSyncAt) < s.sync.cooldown {
			return
		}
	}
	if s.sync.syncing {
		if force || manual {
			s.sync.pendingFollowUp = true
		}
		return
	}
	s.sync.syncing = true
	if manual {
		s.sync.message = ""
		s.sync.messageWarn = false
	}
	s.host.Refresh()

	go func() {
		summary, err := s.svc.SyncNow(context.Background())
		s.host.OnMain(func() {
			s.sync.syncing = false
			s.sync.lastSyncAt = s.now()
			if manual {
				if err != nil {
					s.sync.message = diary.SyncUserMessage(err)
					s.sync.messageWarn = true
				} else if summary.Conflicts > 0 {
					s.sync.message = "同步完成，有冲突待处理。"
					s.sync.messageWarn = true
				} else {
					s.sync.message = "同步完成。"
					s.sync.messageWarn = false
				}
			}
			if manual || summary.Pulled > 0 || summary.DownloadedAttachments > 0 || summary.Pushed > 0 || summary.Conflicts > 0 {
				s.Reload()
			} else {
				s.ReloadSyncStatusLocal()
				s.host.Refresh()
			}
			if manual {
				s.ReloadSyncSettings()
			}
			if s.sync.pendingFollowUp {
				s.sync.pendingFollowUp = false
				if !s.sync.stopped && !(s.mode == modeEditor && s.sync.windowOpen) {
					s.TriggerAutoSync(true)
				}
			}
		})
	}()
}

func (s *State) RequestLogout() {
	if s.svc == nil || s.sync.busy {
		return
	}
	if !s.sync.confirmLogout {
		s.sync.confirmLogout = true
		s.host.Refresh()
		return
	}
	s.sync.confirmLogout = false
	s.sync.busy = true
	s.sync.message = ""
	s.host.Refresh()

	go func() {
		err := s.svc.Logout(context.Background())
		s.host.OnMain(func() {
			s.sync.busy = false
			if err != nil {
				s.sync.message = diary.SyncUserMessage(err)
				s.sync.messageWarn = true
			} else {
				s.sync.devices = nil
				s.sync.message = "已退出登录，本地日记已保留。"
				s.sync.messageWarn = false
			}
			s.ReloadSyncSettings()
		})
	}()
}

func (s *State) RequestRevokeDevice(deviceID string) {
	if s.svc == nil || s.sync.busy {
		return
	}
	if s.sync.confirmRevokeID != deviceID {
		s.sync.confirmRevokeID = deviceID
		s.host.Refresh()
		return
	}
	s.sync.confirmRevokeID = ""
	s.sync.busy = true
	s.sync.message = ""
	s.host.Refresh()

	go func() {
		err := s.svc.RevokeDevice(context.Background(), deviceID)
		s.host.OnMain(func() {
			s.sync.busy = false
			if err != nil {
				s.sync.message = diary.SyncUserMessage(err)
				s.sync.messageWarn = true
			} else {
				s.sync.message = "设备已撤销。"
				s.sync.messageWarn = false
			}
			s.ReloadSyncSettings()
		})
	}()
}

func (s *State) RequestResolveKeepCloud(entityID string) {
	if s.svc == nil || s.sync.busy {
		return
	}
	if s.sync.confirmKeepCloudID != entityID {
		s.sync.confirmKeepCloudID = entityID
		s.sync.confirmKeepLocalID = ""
		s.host.Refresh()
		return
	}
	s.sync.confirmKeepCloudID = ""
	s.sync.busy = true
	s.host.Refresh()

	go func() {
		err := s.svc.ResolveConflictKeepCloud(context.Background(), entityID)
		s.host.OnMain(func() {
			s.sync.busy = false
			if err != nil {
				s.sync.message = diary.SyncUserMessage(err)
				s.sync.messageWarn = true
			} else {
				s.sync.message = "已保留云端版本。"
				s.sync.messageWarn = false
			}
			s.Reload()
			s.ReloadSyncSettings()
			if err == nil {
				s.TriggerAutoSync(true)
			}
		})
	}()
}

func (s *State) RequestResolveKeepLocalAsNew(entityID string) {
	if s.svc == nil || s.sync.busy {
		return
	}
	if s.sync.confirmKeepLocalID != entityID {
		s.sync.confirmKeepLocalID = entityID
		s.sync.confirmKeepCloudID = ""
		s.host.Refresh()
		return
	}
	s.sync.confirmKeepLocalID = ""
	s.sync.busy = true
	s.host.Refresh()

	go func() {
		_, err := s.svc.ResolveConflictKeepLocalAsNew(context.Background(), entityID)
		s.host.OnMain(func() {
			s.sync.busy = false
			if err != nil {
				s.sync.message = diary.SyncUserMessage(err)
				s.sync.messageWarn = true
			} else {
				s.sync.message = "本地内容已另存为新日记，原条目已同步为云端版本。"
				s.sync.messageWarn = false
			}
			s.Reload()
			s.ReloadSyncSettings()
			if err == nil {
				s.TriggerAutoSync(true)
			}
		})
	}()
}

func (s *State) now() time.Time {
	if s.svc == nil {
		return time.Now()
	}
	return s.svc.Now()
}
