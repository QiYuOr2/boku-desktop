// Package anime listens for today's releases in a public Bangumi watching list.
package anime

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"sort"
	"sync"
	"time"

	"boku/internal/contract/bangumi"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

// bangumiAPIBaseURL is a variable only so HTTP tests can use httptest. The
// production value comes from the vendor contract and is never user input.
var bangumiAPIBaseURL = bangumi.APIBaseURL

// shanghaiZone is the fixed offset the provider's calendar is read in. The
// provider names a day rather than an instant, and that day is the Shanghai
// one; the offset has been constant since 1991, so a fixed zone gives the same
// answer the database does and it cannot be missing.
var shanghaiZone = time.FixedZone("CST", 8*60*60)

// shanghai is the location a release is dated in.
//
// It asks the time zone database first, and falls back to the fixed offset
// when the database is not there. That fallback is what a packaged build
// needs: `mygo build` passes -trimpath, which drops the GOROOT path baked into
// the binary, so time.LoadLocation finds neither $GOROOT/lib/time/zoneinfo.zip
// nor a system database on Windows and answers (nil, error). Using the nil
// location panicked with "time: missing Location in call to Time.In" inside
// the Window.Update callback that follows a refresh — the main thread, with no
// recover around it — and killed the process.
func shanghai() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return shanghaiZone
}

type Service struct {
	client    *httpc.Client
	section   *Section
	store     StoreSettings
	secrets   domain.SecretStore
	now       func() time.Time
	mu        sync.RWMutex
	releases  []domain.EpisodeRelease
	fetchedAt time.Time
	err       error
	loading   bool
	covers    map[int64][]byte
}

func New(client *httpc.Client, section *Section, store StoreSettings, secrets ...domain.SecretStore) (*Service, error) {
	if client == nil || section == nil {
		return nil, fmt.Errorf("%w: anime service needs client and settings", domain.ErrInvalidArgument)
	}
	service := &Service{client: client, section: section, store: store, now: time.Now, covers: make(map[int64][]byte)}
	if len(secrets) > 0 {
		service.secrets = secrets[0]
	}
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
	result, err := s.client.GetJSON(ctx, endpoint, headers)
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
	all := make([]domain.EpisodeRelease, 0)
	for _, collection := range page.Data {
		if collection.SubjectID <= 0 {
			err = bad("collection has invalid subject id", result.Payload)
			s.record(nil, err)
			return err
		}
		name := subjectName(page, collection.SubjectID)
		if name == "" {
			err = bad("collection has no subject name", result.Payload)
			s.record(nil, err)
			return err
		}
		eps, getErr := s.client.GetJSON(ctx, bangumiAPIBaseURL+"/v0/episodes?subject_id="+fmt.Sprint(collection.SubjectID)+"&type=0&limit=200", headers)
		if getErr != nil {
			s.record(nil, getErr)
			return getErr
		}
		today, parseErr := releases(collection.SubjectID, name, subjectCover(page, collection.SubjectID), eps.Payload, start, end)
		if parseErr != nil {
			s.record(nil, parseErr)
			return parseErr
		}
		all = append(all, today...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].SubjectName < all[j].SubjectName })
	s.refreshCovers(ctx, all)
	s.record(all, nil)
	return nil
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
		// The image URL is data returned by the public collection endpoint.
		// Do not forward the optional API token to it, even when the URL points
		// at a Bangumi image host.
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.releases = append([]domain.EpisodeRelease(nil), items...)
		s.fetchedAt = s.now().UTC()
	}
	s.err = err
}
