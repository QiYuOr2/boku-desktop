package diary

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"boku/internal/domain"
)

// SettingsStore 是应用设置存储的最小写入接口。
type SettingsStore interface {
	Update(ctx context.Context, section string, change func() error) error
}

// Service 是桌面端日记模块的业务入口，统一协调本地存储、设置、云同步、外链卡片、
// 天气快照与外链图片按需缓存。
type Service struct {
	store    *Store
	section  *Section
	settings SettingsStore
	sync     *SyncEngine
	metadata *MetadataFetcher
	weather  *WeatherService
	images   *ImageCache
	now      func() time.Time
}

// Open 打开 dir 下的日记 SQLite 数据库并组装所有依赖。
func OpenService(
	dir string,
	section *Section,
	settings SettingsStore,
	secrets domain.SecretStore,
	http RawRequester,
	clock domain.Clock,
) (*Service, error) {
	if strings.TrimSpace(dir) == "" || section == nil || settings == nil {
		return nil, fmt.Errorf("diary: incomplete setup")
	}
	if clock == nil {
		clock = domain.SystemClock{}
	}
	store, err := Open(dir)
	if err != nil {
		return nil, err
	}
	getCfg := func() (SyncConfiguration, bool) {
		return section.Current().SyncConfig()
	}
	return &Service{
		store:    store,
		section:  section,
		settings: settings,
		sync:     NewSyncEngine(store, getCfg, http, secrets, clock),
		metadata: NewMetadataFetcher(http, clock),
		weather:  NewWeatherService(http, clock),
		images:   NewImageCache(filepath.Join(dir, ImageCacheDirName), http),
		now:      clock.Now,
	}, nil
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *Service) Now() time.Time { return s.now() }

func (s *Service) Today() DateKey { return Today(s.now()) }

func (s *Service) Settings() Settings { return s.section.Current() }

func (s *Service) UpdateSettings(ctx context.Context, next Settings) error {
	if err := validateSettings(next); err != nil {
		return err
	}
	return s.settings.Update(ctx, sectionName, func() error {
		s.section.Set(next)
		return nil
	})
}

func validateSettings(next Settings) error {
	trimmed := strings.TrimSpace(next.SyncEndpoint)
	if trimmed != "" && normalizeBaseURL(trimmed) == "" {
		return fmt.Errorf("%w: 无效的服务地址", domain.ErrInvalidArgument)
	}
	if len([]rune(strings.TrimSpace(next.LocationPlace))) > 60 || len([]rune(strings.TrimSpace(next.WeatherPlace))) > 60 {
		return fmt.Errorf("%w: 地点名称过长", domain.ErrInvalidArgument)
	}
	return nil
}

// 日记条目读写。

func (s *Service) Entry(id string) (*Entry, error) { return s.store.Entry(id, false) }

func (s *Service) EntriesOn(key DateKey) ([]Entry, error) { return s.store.EntriesOn(key) }

func (s *Service) RecentEntries(limit, offset int) ([]Entry, error) {
	return s.store.RecentEntries(limit, offset)
}

func (s *Service) SearchEntries(query string, limit int) ([]Entry, error) {
	return s.store.SearchEntries(query, limit)
}

func (s *Service) DayCounts() (map[DateKey]int, error) { return s.store.DayCounts() }

func (s *Service) Save(entry Entry, uploads []AttachmentUpload, removedAttachmentIDs map[string]bool) (Entry, error) {
	return s.store.Save(entry, uploads, removedAttachmentIDs, s.now())
}

func (s *Service) Delete(id string) error {
	return s.store.Delete(id, s.now())
}

func (s *Service) AttachmentPath(filename string) string {
	return s.store.AttachmentPath(filename)
}

func (s *Service) AttachmentExists(filename string) bool {
	return s.store.AttachmentExists(filename)
}

// PrepareAttachmentUpload 读取用户选中的本地图片文件并构造待保存的 AttachmentUpload。
func (s *Service) PrepareAttachmentUpload(path string) (AttachmentUpload, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "jpg", "jpeg", "png", "heic":
	default:
		return AttachmentUpload{}, fmt.Errorf("%w: 仅支持 JPG、PNG、HEIC 图片", ErrInvalidAttachment)
	}
	info, err := os.Stat(path)
	if err != nil {
		return AttachmentUpload{}, err
	}
	if info.Size() <= 0 {
		return AttachmentUpload{}, ErrInvalidAttachment
	}
	if info.Size() > MaxAttachmentBytes {
		return AttachmentUpload{}, ErrAttachmentTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AttachmentUpload{}, err
	}
	if len(data) > MaxAttachmentBytes {
		return AttachmentUpload{}, ErrAttachmentTooLarge
	}
	id, err := domain.NewID()
	if err != nil {
		return AttachmentUpload{}, err
	}
	orig := filepath.Base(path)
	return AttachmentUpload{
		ID:           id,
		Filename:     id + "." + ext,
		Data:         data,
		OriginalName: &orig,
		AddedAt:      s.now().UTC(),
	}, nil
}

// 新建日记上下文（地点 / 天气）与外链 / 远程图片。

// CaptureNewEntryMetadata 根据当前日记设置，为新建日记采集默认地点与天气快照。
// 开关关闭时不会触发任何网络请求；采集失败只返回友好提示，不阻塞写作。
func (s *Service) CaptureNewEntryMetadata(ctx context.Context) MetadataResult {
	cfg := s.section.Current()
	var res MetadataResult
	now := s.now().UTC()
	if cfg.LocationEnabled {
		place := strings.TrimSpace(cfg.LocationPlace)
		if place != "" {
			res.Location = &LocationSnapshot{Place: place, CapturedAt: now}
		} else {
			res.LocationMessage = "未填写默认地点，可在设置里填写或手动添加。"
		}
	}
	if cfg.WeatherEnabled {
		place := strings.TrimSpace(cfg.WeatherPlace)
		if place == "" {
			place = strings.TrimSpace(cfg.LocationPlace)
		}
		if place == "" {
			res.WeatherMessage = "未填写天气城市，可在设置里填写或手动添加。"
		} else {
			snap, err := s.weather.SnapshotForPlace(ctx, place)
			if err != nil {
				res.WeatherMessage = "天气获取失败，可稍后手动添加。"
			} else {
				res.Weather = &snap
			}
		}
	}
	return res
}

func (s *Service) FetchWeather(ctx context.Context, place string) (WeatherSnapshot, error) {
	return s.weather.SnapshotForPlace(ctx, place)
}

func (s *Service) FetchLinkCard(ctx context.Context, rawURL string) (LinkCard, error) {
	return s.metadata.Fetch(ctx, rawURL)
}

func (s *Service) LoadRemoteImage(ctx context.Context, rawURL string) ([]byte, error) {
	return s.images.Load(ctx, rawURL)
}

// 云同步透传。

func (s *Service) SyncStatus(ctx context.Context) (SyncStatus, error) { return s.sync.Status(ctx) }

func (s *Service) SendLoginCode(ctx context.Context, email string) error {
	return s.sync.SendLoginCode(ctx, email)
}

func (s *Service) Login(ctx context.Context, email, code, deviceName string) error {
	return s.sync.Login(ctx, email, code, deviceName)
}

func (s *Service) Logout(ctx context.Context) error { return s.sync.Logout(ctx) }

func (s *Service) SyncNow(ctx context.Context) (SyncSummary, error) { return s.sync.SyncNow(ctx) }

func (s *Service) ListDevices(ctx context.Context) ([]Device, error) {
	return s.sync.ListDevices(ctx)
}

func (s *Service) RevokeDevice(ctx context.Context, deviceID string) error {
	return s.sync.RevokeDevice(ctx, deviceID)
}

func (s *Service) Conflicts(ctx context.Context) ([]SyncConflict, error) {
	return s.sync.Conflicts(ctx)
}

func (s *Service) ResolveConflictKeepCloud(ctx context.Context, entityID string) error {
	return s.sync.ResolveConflictKeepCloud(ctx, entityID)
}

func (s *Service) ResolveConflictKeepLocalAsNew(ctx context.Context, entityID string) (string, error) {
	return s.sync.ResolveConflictKeepLocalAsNew(ctx, entityID)
}
