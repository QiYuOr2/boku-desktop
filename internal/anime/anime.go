// Package anime 监听公开 Bangumi 追番列表中今天更新的剧集。
package anime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"net/url"
	"sort"
	"sync"
	"time"

	"boku/internal/contract/bangumi"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	// 与 Apple 端 BangumiClient / BangumiService 保持一致：Bangumi /v0 接口偶发慢响应，
	// 放宽单请求超时、超时后短暂重试一次，并限制剧集拉取并发为 3。
	bangumiRequestTimeout = 30 * time.Second
	bangumiRetryDelay     = 500 * time.Millisecond
	bangumiConcurrency    = 3
)

// bangumiAPIBaseURL 声明为变量仅为让 HTTP 测试能用 httptest；生产值来自厂商常量，不是用户输入。
var bangumiAPIBaseURL = bangumi.APIBaseURL

// shanghaiZone 是读取厂商日历所用的固定偏移。厂商给出的是某一天而非某一时刻，
// 且该日为上海日历日；该偏移自 1991 年起恒定，固定时区与数据库结果一致且不会缺失。
var shanghaiZone = time.FixedZone("CST", 8*60*60)

// shanghai 是剧集日期的时区。先查时区数据库，查不到时回退到固定偏移。
// 打包版必须走回退：`mygo build` 带 -trimpath，会丢掉二进制里的 GOROOT 路径，
// 导致 time.LoadLocation 在 Windows 上既找不到 zoneinfo.zip 也找不到系统数据库，
// 返回 (nil, error)；用 nil 时区会在主线程的 Window.Update 回调里 panic，且无人 recover。
func shanghai() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return shanghaiZone
}

type Service struct {
	client         *httpc.Client
	section        *Section
	store          StoreSettings
	secrets        domain.SecretStore
	now            func() time.Time
	requestTimeout time.Duration
	retryDelay     time.Duration
	mu             sync.RWMutex
	releases       []domain.EpisodeRelease
	fetchedAt      time.Time
	err            error
	loading        bool
	covers         map[int64][]byte
	cache          *Cache
}

func New(client *httpc.Client, section *Section, store StoreSettings, secrets ...domain.SecretStore) (*Service, error) {
	return newService(client, section, store, nil, secrets...)
}

// NewWithCache 创建带本地每日缓存的动画服务。用户数据路径由 main 提供，
// 该功能包自身不决定全局文件系统位置。
func NewWithCache(client *httpc.Client, section *Section, store StoreSettings, cache *Cache, secrets ...domain.SecretStore) (*Service, error) {
	return newService(client, section, store, cache, secrets...)
}

func newService(client *httpc.Client, section *Section, store StoreSettings, cache *Cache, secrets ...domain.SecretStore) (*Service, error) {
	if client == nil || section == nil {
		return nil, fmt.Errorf("%w: anime service needs client and settings", domain.ErrInvalidArgument)
	}
	service := &Service{
		client:         client,
		section:        section,
		store:          store,
		now:            time.Now,
		requestTimeout: bangumiRequestTimeout,
		retryDelay:     bangumiRetryDelay,
		covers:         make(map[int64][]byte),
		cache:          cache,
	}
	if len(secrets) > 0 {
		service.secrets = secrets[0]
	}
	service.restoreToday()
	return service, nil
}
func (s *Service) Cover(subjectID int64) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.covers[subjectID]...)
}
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.section.Settings()
}
func (s *Service) Enabled() bool       { return s.Settings().Enabled }
func (s *Service) Username() string    { return s.Settings().Username }
func (s *Service) RefreshMinutes() int { return s.Settings().RefreshMinutes }
func (s *Service) Configure(ctx context.Context, enabled bool, username string, refreshMinutes int) error {
	return s.SetSettings(ctx, Settings{Enabled: enabled, Username: username, RefreshMinutes: refreshMinutes})
}
func (s *Service) SetSettings(ctx context.Context, settings Settings) error {
	s.mu.Lock()
	s.section.Set(settings)
	s.mu.Unlock()
	if s.store != nil {
		return s.store.Update(ctx, sectionName, func() error { return nil })
	}
	return nil
}
func (s *Service) View() (items []domain.EpisodeRelease, fetchedAt time.Time, loading bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.EpisodeRelease(nil), s.releases...), s.fetchedAt, s.loading, s.err
}
func (s *Service) NeedsRefresh() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	settings := s.section.Settings()
	return settings.Enabled && settings.Username != "" && (s.fetchedAt.IsZero() || s.now().Sub(s.fetchedAt) >= time.Duration(settings.RefreshMinutes)*time.Minute || s.err != nil) && !s.loading
}
func (s *Service) RefreshIfNeeded(ctx context.Context) error {
	if !s.NeedsRefresh() {
		return nil
	}
	return s.Refresh(ctx)
}
func (s *Service) Refresh(ctx context.Context) error {
	settings := s.Settings()
	if !settings.Enabled || settings.Username == "" {
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
	loc := shanghai()
	now := s.now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	endpoint := bangumiAPIBaseURL + "/v0/users/" + url.PathEscape(settings.Username) + "/collections?subject_type=2&type=3&limit=100"
	headers := s.headers(ctx)
	result, err := s.getJSON(ctx, endpoint, headers)
	if err != nil {
		s.record(nil, err)
		return err
	}
	page, err := collections(result.Payload)
	if err != nil {
		s.record(nil, err)
		return err
	}
	if page.Total > len(page.Data) {
		err = fmt.Errorf("%w: watching list has more than 100 items", domain.ErrProviderBadResponse)
		s.record(nil, err)
		return err
	}
	all, err := s.fetchEpisodes(ctx, page, result.Payload, headers, start, end)
	if err != nil {
		s.record(nil, err)
		return err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].SubjectName != all[j].SubjectName {
			return all[i].SubjectName < all[j].SubjectName
		}
		return all[i].EpisodeSort < all[j].EpisodeSort
	})
	s.refreshCovers(ctx, all)
	s.record(all, nil)
	return nil
}

type watchingSubject struct {
	id    int64
	name  string
	cover string
}

func (s *Service) fetchEpisodes(ctx context.Context, page collectionPage, rawPayload []byte, headers map[string]string, start, end time.Time) ([]domain.EpisodeRelease, error) {
	subjects := make([]watchingSubject, 0, len(page.Data))
	for _, collection := range page.Data {
		if collection.SubjectID <= 0 {
			return nil, bad("collection has invalid subject id", rawPayload)
		}
		name := subjectName(page, collection.SubjectID)
		if name == "" {
			return nil, bad("collection has no subject name", rawPayload)
		}
		subjects = append(subjects, watchingSubject{
			id:    collection.SubjectID,
			name:  name,
			cover: subjectCover(page, collection.SubjectID),
		})
	}
	if len(subjects) == 0 {
		return []domain.EpisodeRelease{}, nil
	}

	workers := min(bangumiConcurrency, len(subjects))
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan watchingSubject, len(subjects))
	for _, subject := range subjects {
		jobs <- subject
	}
	close(jobs)

	var (
		mu       sync.Mutex
		all      []domain.EpisodeRelease
		firstErr error
		wg       sync.WaitGroup
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for subject := range jobs {
				if workCtx.Err() != nil {
					return
				}
				endpoint := bangumiAPIBaseURL + "/v0/episodes?subject_id=" + fmt.Sprint(subject.id) + "&type=0&limit=200"
				eps, err := s.getJSON(workCtx, endpoint, headers)
				if err == nil {
					var today []domain.EpisodeRelease
					today, err = releases(subject.id, subject.name, subject.cover, eps.Payload, start, end)
					if err == nil {
						mu.Lock()
						all = append(all, today...)
						mu.Unlock()
						continue
					}
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func (s *Service) getJSON(ctx context.Context, endpoint string, headers map[string]string) (domain.FetchResult, error) {
	opts := httpc.RequestOptions{Timeout: s.requestTimeout}
	result, err := s.client.GetJSONWithOptions(ctx, endpoint, headers, opts)
	if !errors.Is(err, domain.ErrProviderTimeout) || ctx.Err() != nil {
		return result, err
	}
	if s.retryDelay > 0 {
		timer := time.NewTimer(s.retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, err
		case <-timer.C:
		}
	}
	return s.client.GetJSONWithOptions(ctx, endpoint, headers, opts)
}

func (s *Service) today() string {
	return s.now().In(shanghai()).Format(time.DateOnly)
}

func (s *Service) restoreToday() {
	if s.cache == nil {
		return
	}
	items, covers, fetchedAt, ok, err := s.cache.Load(s.today())
	if err != nil {
		slog.Warn("animation cache could not be read", "event", "anime_cache_unreadable", "error", err)
		return
	}
	if !ok {
		return
	}
	s.mu.Lock()
	s.releases = items
	s.covers = covers
	s.fetchedAt = fetchedAt
	s.mu.Unlock()
}

func (s *Service) refreshCovers(ctx context.Context, releases []domain.EpisodeRelease) {
	urls := make(map[int64]string)
	for _, release := range releases {
		if release.CoverURL != "" {
			urls[release.SubjectID] = release.CoverURL
		}
	}
	for subjectID, endpoint := range urls {
		s.mu.RLock()
		_, loaded := s.covers[subjectID]
		s.mu.RUnlock()
		if loaded {
			continue
		}
		// 图片 URL 是公开收藏接口返回的数据，不要把可选的 API token 转发给它，
		// 即使该 URL 指向 Bangumi 图床。
		result, err := s.client.GetBytes(ctx, endpoint, nil)
		if err != nil {
			continue
		}
		_, _, err = image.Decode(bytes.NewReader(result.Payload))
		if err != nil {
			continue
		}
		s.mu.Lock()
		s.covers[subjectID] = append([]byte(nil), result.Payload...)
		s.mu.Unlock()
	}
}

func (s *Service) headers(ctx context.Context) map[string]string {
	if s.secrets == nil {
		return nil
	}
	secret, err := s.secrets.Get(ctx, domain.CredentialKey(bangumi.ProviderID, "accessToken"))
	if err != nil || string(secret.Value) == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + string(secret.Value)}
}
func (s *Service) record(items []domain.EpisodeRelease, err error) {
	day := s.today()
	var (
		cache     *Cache
		fetchedAt time.Time
		toSave    []domain.EpisodeRelease
		covers    map[int64][]byte
	)
	s.mu.Lock()
	if err == nil {
		s.releases = append([]domain.EpisodeRelease(nil), items...)
		s.fetchedAt = s.now().UTC()
		cache = s.cache
		fetchedAt = s.fetchedAt
		toSave = append([]domain.EpisodeRelease(nil), s.releases...)
		covers = cloneCovers(s.covers)
	} else if s.fetchedAt.IsZero() || s.fetchedAt.In(shanghai()).Format(time.DateOnly) != day {
		// 其他日期的成功结果不能代表今天。
		s.releases = nil
		s.fetchedAt = time.Time{}
	}
	s.err = err
	s.mu.Unlock()
	if cache != nil {
		if saveErr := cache.Save(day, fetchedAt, toSave, covers); saveErr != nil {
			slog.Warn("animation cache could not be saved", "event", "anime_cache_unwritable", "error", saveErr)
		}
	}
}
