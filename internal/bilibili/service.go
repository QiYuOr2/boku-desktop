// Package bilibili watches explicitly configured public creator profiles.
package bilibili

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"boku/internal/contract/bilibili"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

var apiBaseURL = bilibili.APIBaseURL
var liveBaseURL = bilibili.LiveBaseURL

const (
	// dynamicFeedPath is the space feed, the one endpoint that answers a
	// creator's latest post without a login. It is protected by WBI signing.
	dynamicFeedPath = "/x/polymer/web-dynamic/v1/feed/space"

	// riskControlCode is Bilibili's "signature rejected" code. It is retried
	// once with fresh keys, because the keys rotate roughly daily.
	riskControlCode = -352

	// feedAttempts bounds how many times an empty answer is retried before it
	// is believed. Bilibili answers a throttled anonymous request with an
	// empty list rather than an error, so one empty answer proves nothing.
	feedAttempts = 3

	// feedRetryPause is the pause between those attempts. It is short because
	// a refresh runs on a timer and several creators share it.
	feedRetryPause = 400 * time.Millisecond
)

type Service struct {
	client  *httpc.Client
	section *Section
	store   StoreSettings
	now     func() time.Time
	session *session
	// cache holds the last known row of each followed creator between runs.
	cache     *Cache
	mu        sync.RWMutex
	items     []domain.FollowedUpdate
	fetchedAt time.Time
	loading   bool
	err       error
	stop      context.CancelFunc
	emit      func(previous, current []domain.FollowedUpdate)
	// covers holds the fetched avatar of each creator, keyed by UID.
	covers map[int64][]byte
}

func New(client *httpc.Client, section *Section, store StoreSettings) (*Service, error) {
	if client == nil || section == nil {
		return nil, fmt.Errorf("%w: bilibili needs client and settings", domain.ErrInvalidArgument)
	}
	service := &Service{client: client, section: section, store: store, now: time.Now, covers: make(map[int64][]byte)}
	service.session = newSession(client, func() time.Time { return service.now() })
	return service, nil
}

// NewWithCache is New with the on-disk record of each creator's last row.
//
// The cache is restored as soon as the service exists, so a cold start shows
// the followed creators rather than an empty block while the first refresh is
// in flight. It is a cache: an unreadable file is a miss, never a failure.
func NewWithCache(client *httpc.Client, section *Section, store StoreSettings, cache *Cache) (*Service, error) {
	service, err := New(client, section, store)
	if err != nil {
		return nil, err
	}
	service.cache = cache
	service.restore()
	return service, nil
}

// restore puts the cached rows on screen before any network answer.
func (s *Service) restore() {
	settings := s.section.Settings()
	if len(settings.UIDs) == 0 {
		return
	}
	items, fetchedAt, ok := s.cache.Load(settings.UIDs)
	if !ok || len(items) == 0 {
		return
	}
	s.mu.Lock()
	s.items = items
	s.fetchedAt = fetchedAt
	s.mu.Unlock()
}

// persist writes the current rows, so this run's answers survive the next
// start. A failure is logged by the caller and never fails a refresh: the data
// is on screen either way.
func (s *Service) persist(items []domain.FollowedUpdate, at time.Time) error {
	return s.cache.Save(items, at)
}

// Avatar returns the fetched avatar bytes of one creator, or nil. The
// presentation turns these into bitmaps off the view path.
func (s *Service) Avatar(uid int64) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.covers[uid]...)
}
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.section.Settings()
}

// SetEmit installs the observer called after a successful refresh. It is used
// by the presentation to decide whether a new live session or activity earns
// a notification; the service never imports a window or notification API.
func (s *Service) SetEmit(emit func(previous, current []domain.FollowedUpdate)) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}
func (s *Service) Enabled() bool       { return s.Settings().Enabled }
func (s *Service) UIDs() []int64       { return append([]int64(nil), s.Settings().UIDs...) }
func (s *Service) RefreshMinutes() int { return s.Settings().RefreshMinutes }
func (s *Service) DynamicCount() int   { return s.Settings().DynamicCount }
func (s *Service) Notifications() (bool, bool) {
	v := s.Settings()
	return v.LiveNotifications, v.DynamicNotifications
}
func (s *Service) Configure(ctx context.Context, enabled bool, ids []int64, minutes, dynamics int, liveNotice, dynamicNotice bool) error {
	return s.ConfigureSettings(ctx, Settings{Enabled: enabled, UIDs: ids, RefreshMinutes: minutes, DynamicCount: dynamics, LiveNotifications: liveNotice, DynamicNotifications: dynamicNotice})
}
func (s *Service) ConfigureSettings(ctx context.Context, settings Settings) error {
	var err error
	if s.store != nil {
		err = s.store.Update(ctx, sectionName, func() error {
			s.mu.Lock()
			s.section.Set(settings)
			s.mu.Unlock()
			return nil
		})
	} else {
		s.mu.Lock()
		s.section.Set(settings)
		s.mu.Unlock()
	}
	if err != nil {
		return err
	}
	s.applyFollowedList(settings.UIDs)
	return nil
}

// applyFollowedList drops what belongs to creators the user no longer follows.
//
// Removing a creator from the settings has to remove them from the block *and*
// from the record on disk: a row that describes someone the user dropped is
// data they asked to be gone, and leaving it behind means it comes back the
// next time the file is read.
func (s *Service) applyFollowedList(uids []int64) {
	followed := make(map[int64]bool, len(uids))
	for _, uid := range uids {
		followed[uid] = true
	}
	s.mu.Lock()
	kept := make([]domain.FollowedUpdate, 0, len(s.items))
	for _, item := range s.items {
		if followed[item.CreatorID] {
			kept = append(kept, item)
		}
	}
	s.items = kept
	for uid := range s.covers {
		if !followed[uid] {
			delete(s.covers, uid)
		}
	}
	fetchedAt := s.fetchedAt
	s.mu.Unlock()
	if err := s.cache.Forget(uids); err != nil {
		slog.Warn("the followed-UP record could not be pruned", "event", "bilibili_cache_unprunable", "error", err)
	}
	if len(kept) > 0 {
		_ = s.persist(kept, fetchedAt)
	}
}
func (s *Service) View() ([]domain.FollowedUpdate, time.Time, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.FollowedUpdate(nil), s.items...), s.fetchedAt, s.loading, s.err
}
func (s *Service) NeedsRefresh() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.section.Settings()
	return v.Enabled && len(v.UIDs) > 0 && !s.loading && (s.fetchedAt.IsZero() || s.now().Sub(s.fetchedAt) >= time.Duration(v.RefreshMinutes)*time.Minute || s.err != nil)
}
func (s *Service) RefreshIfNeeded(ctx context.Context) error {
	if !s.NeedsRefresh() {
		return nil
	}
	return s.Refresh(ctx)
}

// Start keeps the configured public profiles fresh while the tray app runs.
// A minute tick is intentionally the smallest supported refresh interval; the
// current setting is checked on every tick, so saving a new interval needs no
// second scheduler or process restart.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.stop != nil {
		s.mu.Unlock()
		return
	}
	ctx, s.stop = context.WithCancel(ctx)
	s.mu.Unlock()
	go func() {
		_ = s.RefreshIfNeeded(ctx)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_ = s.RefreshIfNeeded(ctx)
			}
		}
	}()
}

// Stop ends the monitor's ticker before the application exits.
func (s *Service) Stop() {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}
func (s *Service) Refresh(ctx context.Context) error {
	v := s.Settings()
	if !v.Enabled || len(v.UIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	if s.loading {
		s.mu.Unlock()
		return nil
	}
	s.loading = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.loading = false; s.mu.Unlock() }()
	items := make([]domain.FollowedUpdate, 0, len(v.UIDs))
	for _, uid := range v.UIDs {
		row, ok, err := s.fetchCreator(ctx, uid, v.DynamicCount)
		if err != nil {
			s.record(nil, err)
			return err
		}
		if ok {
			items = append(items, row)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Live != items[j].Live {
			return items[i].Live
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
	// The avatars are not part of the refresh's success: a row whose avatar
	// could not be fetched falls back to its mark.
	s.refreshAvatars(ctx, items)
	s.record(items, nil)
	// The record is the point of the refresh: keep the last row of each
	// creator so the next start has something to show. A write that fails is
	// worth a log line, not a failed refresh.
	if err := s.persist(items, s.now().UTC()); err != nil {
		slog.Warn("the followed-UP record could not be written", "event", "bilibili_cache_unwritable", "error", err)
	}
	return nil
}

// fetchCreator answers the one row that stands for a creator.
//
// The live status and the timeline are two independent questions, and both are
// always asked. Whether a creator is streaming has nothing to do with whether
// they posted recently: a creator who streams every day and never posts is not
// less live than one who posts between streams. Asking the live endpoint only
// when the timeline came back empty made a streaming creator look like a video
// one whenever a post happened to exist.
//
// The row is the live session when there is one, because that is what is
// happening now; otherwise it is the creator's newest post. It reports no row
// when they are neither streaming nor posting.
func (s *Service) fetchCreator(ctx context.Context, uid int64, dynamicCount int) (domain.FollowedUpdate, bool, error) {
	live, err := s.fetchLive(ctx, uid)
	if err != nil {
		return domain.FollowedUpdate{}, false, err
	}
	posts, err := s.fetchPosts(ctx, uid, dynamicCount)
	if err != nil {
		return domain.FollowedUpdate{}, false, err
	}
	if live.Live {
		return live, true, nil
	}
	if len(posts) == 0 {
		return domain.FollowedUpdate{}, false, nil
	}
	newest := posts[0]
	for _, post := range posts[1:] {
		if post.PublishedAt.After(newest.PublishedAt) {
			newest = post
		}
	}
	return newest, true, nil
}

// fetchLive asks whether the creator is streaming right now.
//
// It uses the endpoint that answers with the room and the creator's own name
// and portrait together, so a live row is complete even when the timeline is
// empty and there is no post to take a name from.
func (s *Service) fetchLive(ctx context.Context, uid int64) (domain.FollowedUpdate, error) {
	endpoint := fmt.Sprintf("%s/room/v1/Room/get_status_info_by_uids?uids[]=%d", liveBaseURL, uid)
	result, err := s.client.GetJSON(ctx, endpoint, browserHeaders())
	if err != nil {
		return domain.FollowedUpdate{}, err
	}
	s.absorbSession(result.Cookies)
	return decodeLiveStatus(uid, result.Payload, s.now())
}

// fetchPosts asks for the creator's newest posts, pinned ones excluded.
func (s *Service) fetchPosts(ctx context.Context, uid int64, dynamicCount int) ([]domain.FollowedUpdate, error) {
	payload, err := s.fetchFeed(ctx, uid)
	if err != nil {
		return nil, err
	}
	return decodeDynamics(uid, payload, dynamicCount)
}

// liveStatusResponse is the answer of get_status_info_by_uids. The rooms are
// keyed by UID rather than returned in an array.
type liveStatusResponse struct {
	Code int `json:"code"`
	Data map[string]struct {
		Title      string `json:"title"`
		RoomID     int64  `json:"room_id"`
		UID        int64  `json:"uid"`
		Online     int64  `json:"online"`
		LiveStatus int    `json:"live_status"`
		LiveTime   int64  `json:"live_time"`
		Uname      string `json:"uname"`
		Face       string `json:"face"`
	} `json:"data"`
}

func decodeLiveStatus(uid int64, payload []byte, now time.Time) (domain.FollowedUpdate, error) {
	var answer liveStatusResponse
	if err := json.Unmarshal(payload, &answer); err != nil {
		return domain.FollowedUpdate{}, bad("live status is not an object", payload)
	}
	if answer.Code != 0 {
		return domain.FollowedUpdate{}, bad(rejected("live status API", answer.Code, ""), payload)
	}
	room, ok := answer.Data[strconv.FormatInt(uid, 10)]
	if !ok || room.LiveStatus != 1 {
		return domain.FollowedUpdate{}, nil
	}
	if room.RoomID <= 0 {
		return domain.FollowedUpdate{}, bad("live room id is invalid", payload)
	}
	item := domain.FollowedUpdate{
		CreatorID:    uid,
		CreatorName:  strings.TrimSpace(room.Uname),
		AvatarURL:    strings.TrimSpace(room.Face),
		Kind:         "直播中",
		Live:         true,
		LiveRoomID:   room.RoomID,
		LiveTitle:    strings.TrimSpace(room.Title),
		LiveAudience: fmt.Sprint(room.Online),
		URL:          fmt.Sprintf("https://live.bilibili.com/%d", room.RoomID),
	}
	if item.LiveTitle != "" {
		item.Summary = item.LiveTitle
	} else {
		item.Summary = "正在直播"
	}
	// The session started at live_time, and is still going: a row about
	// something happening now sorts above any post.
	item.PublishedAt = now.UTC()
	if room.LiveTime > 0 {
		item.PublishedAt = time.Unix(room.LiveTime, 0).UTC()
	}
	return item, nil
}

// fetchFeed asks for one creator's feed, signing the query and retrying the
// ways the answer can come back without any items.
//
// Bilibili's risk control does not fail an anonymous request outright: it
// answers `code: 0` with an empty list. An empty answer and a genuine absence
// of activity look the same, so an empty list is retried a few times with a
// short pause before it is believed. Only after the retries does "no items"
// mean "this creator posted nothing".
func (s *Service) fetchFeed(ctx context.Context, uid int64) ([]byte, error) {
	s.session.ensureCookies(ctx)
	params := map[string]string{
		"host_mid": strconv.FormatInt(uid, 10),
		"platform": "web",
		"features": "itemOpusStyle",
	}
	var lastErr error
	// lastEmpty keeps the last well-formed answer that carried no items. Once
	// the retries are spent it is what the caller decodes: "this creator has
	// nothing" is an answer, and it has to reach the decoder as one.
	var lastEmpty []byte
	for attempt := 0; attempt < feedAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, feedRetryPause); err != nil {
				return nil, err
			}
		}
		endpoint, ok := s.session.signedURL(ctx, apiBaseURL, dynamicFeedPath, params)
		if !ok {
			// Without keys the query cannot be signed, and an unsigned query
			// is answered with -412 or -352 rather than data.
			return nil, badRejected("dynamic API", "the WBI keys could not be read", nil)
		}
		result, err := s.client.GetJSON(ctx, endpoint, s.session.headers())
		if err != nil {
			// A transport failure is worth one more attempt; a rejection is
			// not, because it repeats identically.
			lastErr = err
			if errors.Is(err, domain.ErrProviderUnauthorized) ||
				errors.Is(err, domain.ErrProviderBadResponse) {
				return nil, err
			}
			continue
		}
		s.session.absorbCookies(result.Cookies)
		code, message, empty := inspectFeed(result.Payload)
		if code == riskControlCode {
			// The keys rotated, or the signature was not accepted. Fetch a new
			// pair and sign again; that is what makes -352 self-heal.
			s.session.invalidateKeys()
			lastErr = badRejected("dynamic API", fmt.Sprintf("code %d", code), result.Payload)
			continue
		}
		if code != 0 {
			return nil, badRejected("dynamic API", feedRejection(code, message), result.Payload)
		}
		if empty {
			lastErr, lastEmpty = nil, result.Payload
			continue
		}
		return result.Payload, nil
	}
	if lastEmpty != nil {
		return lastEmpty, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

// absorbSession keeps the identity cookies a live answer handed out.
func (s *Service) absorbSession(cookies []string) {
	s.session.absorbCookies(cookies)
}

// inspectFeed reads the business code out of a feed answer and reports whether
// the list was empty.
func inspectFeed(payload []byte) (code int, message string, empty bool) {
	var page struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &page); err != nil {
		return 0, "", false
	}
	return page.Code, page.Message, len(page.Data.Items) == 0
}

// sleepContext waits for d, or returns early when the refresh is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (s *Service) record(items []domain.FollowedUpdate, err error) {
	s.mu.Lock()
	previous, emit := append([]domain.FollowedUpdate(nil), s.items...), s.emit
	if err == nil {
		s.items = append([]domain.FollowedUpdate(nil), items...)
		s.fetchedAt = s.now().UTC()
	}
	s.err = err
	s.mu.Unlock()
	if err == nil && emit != nil {
		emit(previous, append([]domain.FollowedUpdate(nil), items...))
	}
}

// dynamicPage is the space feed's answer. Its modules arrive as one object
// keyed by module name: the author under module_author, the post body under
// module_dynamic. This is the shape both the web and the desktop endpoint
// return; the two differ only in which modules they bother to fill in.
type dynamicPage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items []dynamicItem `json:"items"`
	} `json:"data"`
}

type dynamicItem struct {
	ID      string          `json:"id_str"`
	Type    string          `json:"type"`
	Modules dynamicItemBody `json:"modules"`
}

type dynamicItemBody struct {
	Author  dynamicAuthor  `json:"module_author"`
	Dynamic dynamicContent `json:"module_dynamic"`
	// Tag marks a pinned post. The object is absent (or null) on every other
	// item, and its text is "置顶" when it is present.
	Tag *struct {
		Text string `json:"text"`
	} `json:"module_tag"`
}

// pinned reports whether this item is a post the creator pinned to the top of
// their timeline.
//
// A pinned post is the old one they want noticed, not the new one they just
// made, and it arrives first in the feed. Taking the first item as "newest"
// would therefore show a post from months ago.
func (v dynamicItem) pinned() bool {
	return v.Modules.Tag != nil && strings.TrimSpace(v.Modules.Tag.Text) != ""
}

type dynamicAuthor struct {
	MID   flexInt64 `json:"mid"`
	Name  string    `json:"name"`
	Face  string    `json:"face"`
	PubTS flexInt64 `json:"pub_ts"`
}

// flexInt64 accepts a number or a string holding one. Bilibili is not
// consistent about this: most items send pub_ts as a number, some send it as a
// string, and one item of the wrong kind would otherwise make the whole feed
// undecodable.
type flexInt64 int64

func (v *flexInt64) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		// A float or an unexpected shape is not worth failing the feed over.
		*v = 0
		return nil
	}
	*v = flexInt64(n)
	return nil
}

// dynamicContent is the post body.
//
// It is a fixed object in which every kind of entry has its own slot: only the
// one matching the item's type is filled in, and the rest are null. A post's
// own words live in desc; a picture post written in the newer editor carries
// them under opus.summary instead; a video, a column and a live room each carry
// a title.
type dynamicContent struct {
	Desc struct {
		Text string `json:"text"`
	} `json:"desc"`
	Major struct {
		Archive dynamicEntry `json:"archive"`
		// Opus is the newer picture-and-text entry, and the one that carries a
		// picture post's words.
		Opus dynamicEntry `json:"opus"`
		// Article is a column, Draw a set of pictures, Live a live card,
		// Common a forwarded post's reminder, PGC a series.
		Article dynamicEntry `json:"article"`
		Draw    dynamicEntry `json:"draw"`
		Live    dynamicEntry `json:"live"`
		Common  dynamicEntry `json:"common"`
		PGC     dynamicEntry `json:"pgc"`
	} `json:"major"`
}

// dynamicEntry is one slot of major. Every kind has a title and a summary, and
// a few carry a bvid or an address of their own.
type dynamicEntry struct {
	BVID    string `json:"bvid"`
	Title   string `json:"title"`
	JumpURL string `json:"jump_url"`
	Summary struct {
		Text string `json:"text"`
	} `json:"summary"`
	Desc string `json:"desc"`
}

// preview is the line a row shows under the creator's name: the post's own
// words when it has them, otherwise the title of whatever it points at.
//
// Every entry has to end up with something. A picture post written in the
// newer editor keeps its words in opus.summary and nowhere else, which is what
// used to leave rows reading "发布了新动态"; a video has only a title; a
// forwarded post has its own words, and a reminder of what it forwards.
func (c dynamicContent) preview() string {
	if text := oneLine(c.Desc.Text); text != "" {
		return text
	}
	entries := []dynamicEntry{c.Major.Opus, c.Major.Archive, c.Major.Article, c.Major.Draw, c.Major.Live, c.Major.PGC, c.Major.Common}
	for _, entry := range entries {
		if text := entry.preview(); text != "" {
			return text
		}
	}
	// Nothing but pictures and emotes: say what kind of post it is rather
	// than that it is a post.
	switch {
	case c.Major.Live.JumpURL != "":
		return "正在直播"
	case c.Major.Archive.BVID != "":
		return "发布了新视频"
	case c.Major.Article.JumpURL != "":
		return "发布了新专栏"
	case c.Major.Opus.JumpURL != "" || c.Major.Draw.JumpURL != "":
		return "发布了图片动态"
	}
	return ""
}

// preview is one entry's own line: its summary's text, then its title, then
// its description — whichever is filled in first.
func (e dynamicEntry) preview() string {
	if text := oneLine(e.Summary.Text); text != "" {
		return text
	}
	if text := oneLine(e.Title); text != "" {
		return text
	}
	return oneLine(e.Desc)
}

func decodeDynamic(uid int64, payload []byte) (domain.FollowedUpdate, error) {
	items, err := decodeDynamics(uid, payload, 1)
	if err != nil {
		return domain.FollowedUpdate{}, err
	}
	return items[0], nil
}

func decodeDynamics(uid int64, payload []byte, count int) ([]domain.FollowedUpdate, error) {
	var page dynamicPage
	if err := json.Unmarshal(payload, &page); err != nil {
		return nil, bad("dynamic is not an object", payload)
	}
	if page.Code != 0 {
		return nil, badRejected("dynamic API", feedRejection(page.Code, page.Message), payload)
	}
	if len(page.Data.Items) == 0 {
		// fetchFeed has already retried an empty answer, so this creator
		// really has nothing to show. The caller shows the creator with no
		// activity rather than an error.
		return nil, nil
	}
	// Pinned posts are dropped before the count is taken, so that asking for
	// two posts yields two posts the creator actually wrote recently rather
	// than a pinned one from months ago plus one real post.
	posts := make([]dynamicItem, 0, len(page.Data.Items))
	for _, item := range page.Data.Items {
		if item.pinned() {
			continue
		}
		posts = append(posts, item)
	}
	if len(posts) == 0 {
		// Everything on the timeline is pinned: there is still no news.
		return nil, nil
	}
	if count < 1 {
		count = 1
	}
	if count > len(posts) {
		count = len(posts)
	}
	items := make([]domain.FollowedUpdate, 0, count)
	for _, v := range posts[:count] {
		a := v.Modules.Author
		if int64(a.MID) != uid || strings.TrimSpace(a.Name) == "" || a.PubTS <= 0 || strings.TrimSpace(v.ID) == "" {
			return nil, bad("dynamic has no valid author", payload)
		}
		content := v.Modules.Dynamic
		summary := content.preview()
		if summary == "" {
			summary = "发布了新动态"
		}
		// The row opens whatever the post points at when it points at
		// something; the timeline entry is the fallback, not the default.
		url := "https://t.bilibili.com/" + v.ID
		for _, jump := range []string{content.Major.Archive.JumpURL, content.Major.Opus.JumpURL, content.Major.Article.JumpURL, content.Major.Live.JumpURL} {
			if jump = strings.TrimSpace(jump); jump != "" {
				url = absoluteURL(jump)
				break
			}
		}
		if url == "https://t.bilibili.com/"+v.ID {
			if bvid := strings.TrimSpace(content.Major.Archive.BVID); bvid != "" {
				url = "https://www.bilibili.com/video/" + bvid
			}
		}
		items = append(items, domain.FollowedUpdate{CreatorID: uid, CreatorName: strings.TrimSpace(a.Name), AvatarURL: strings.TrimSpace(a.Face), Kind: dynamicKind(v.Type, content), Summary: summary, URL: url, PublishedAt: time.Unix(int64(a.PubTS), 0).UTC()})
	}
	return items, nil
}

// dynamicKind is the short label the row wears beside the creator's name.
func dynamicKind(rawType string, content dynamicContent) string {
	switch {
	case strings.Contains(rawType, "AV") || content.Major.Archive.BVID != "":
		return "视频"
	case content.Major.Live.JumpURL != "":
		return "直播"
	case strings.Contains(rawType, "ARTICLE") || content.Major.Article.JumpURL != "":
		return "专栏"
	case content.Major.Opus.JumpURL != "" || content.Major.Draw.JumpURL != "":
		return "图文"
	case strings.Contains(rawType, "FORWARD"):
		return "转发"
	}
	return "动态"
}

// absoluteURL completes the protocol-relative addresses the feed uses.
func absoluteURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// oneLine folds a post's text into the single line a floater row and a system
// notification both show. A post routinely carries newlines and blank lines,
// which would otherwise stretch the row or break the notification's layout.
//
// Emote placeholders are dropped first. A post can be nothing but a row of
// them — picture posts often are — and "[名字]" repeated six times says
// nothing about what the creator posted, so a line with nothing left is what
// tells the caller to fall back to the entry's title.
func oneLine(text string) string {
	return strings.Join(strings.Fields(stripEmotes(text)), " ")
}

// stripEmotes removes Bilibili's bracketed emote placeholders, which stand in
// for an image the text cannot show.
func stripEmotes(text string) string {
	if !strings.Contains(text, "[") {
		return text
	}
	var out strings.Builder
	for {
		open := strings.IndexByte(text, '[')
		if open < 0 {
			out.WriteString(text)
			break
		}
		end := strings.IndexByte(text[open:], ']')
		if end < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:open])
		text = text[open+end+1:]
	}
	return out.String()
}

// feedRejection turns Bilibili's business code into something a card can show.
// The code itself always survives: -412 is a block, -352 a signature the
// service did not accept, and they must not read the same to whoever is
// looking at the error.
func feedRejection(code int, message string) string {
	switch code {
	case -412:
		return fmt.Sprintf("code %d (the request was blocked)", code)
	case riskControlCode:
		return fmt.Sprintf("code %d (the signature was not accepted)", code)
	}
	if message = strings.TrimSpace(message); message != "" && message != "0" {
		return fmt.Sprintf("code %d: %s", code, message)
	}
	return fmt.Sprintf("code %d", code)
}

// rejected names the upstream's own verdict. Bilibili answers a refused query
// with code 0 only when it worked; anything else carries the reason, and a
// card that shows it saves the reader from guessing at a status code.
func rejected(what string, code int, message string) string {
	if message = strings.TrimSpace(message); message != "" && message != "0" {
		return fmt.Sprintf("%s returned code %d: %s", what, code, message)
	}
	return fmt.Sprintf("%s returned code %d", what, code)
}

func bad(reason string, payload []byte) error {
	return &domain.BadResponseError{ProviderID: bilibili.ProviderID, Reason: reason, Snippet: domain.Snippet(payload)}
}

// badRejected reports the service refusing the query, naming the reason it
// gave rather than only the status code.
func badRejected(what, reason string, payload []byte) error {
	return bad(what+" returned "+reason, payload)
}

// avatarURL turns the avatar address the feed reports into one to fetch. A
// host-relative address is completed against the API host; an absolute http://
// address is asked for over TLS, because plain HTTP is worth avoiding on a
// network that inspects or rewrites traffic and the image hosts answer over
// both.
func avatarURL(raw string) string {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return ""
	case strings.HasPrefix(raw, "//"):
		return "https:" + raw
	case strings.HasPrefix(raw, "/"):
		return apiBaseURL + raw
	case strings.HasPrefix(raw, "http://"):
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

// refreshAvatars fetches the avatar of each creator once. An avatar that cannot
// be fetched or is not an image is not a failed refresh: the row falls back to
// its mark, and the next refresh tries again.
func (s *Service) refreshAvatars(ctx context.Context, items []domain.FollowedUpdate) {
	wanted := make(map[int64]string)
	for _, item := range items {
		if item.AvatarURL == "" {
			continue
		}
		if _, seen := wanted[item.CreatorID]; !seen {
			wanted[item.CreatorID] = item.AvatarURL
		}
	}
	for uid, endpoint := range wanted {
		s.mu.RLock()
		_, loaded := s.covers[uid]
		s.mu.RUnlock()
		if loaded {
			continue
		}
		result, err := s.client.GetBytes(ctx, avatarURL(endpoint), browserHeaders())
		if err != nil {
			continue
		}
		if _, _, err := image.DecodeConfig(bytes.NewReader(result.Payload)); err != nil {
			continue
		}
		s.mu.Lock()
		s.covers[uid] = append([]byte(nil), result.Payload...)
		s.mu.Unlock()
	}
}
