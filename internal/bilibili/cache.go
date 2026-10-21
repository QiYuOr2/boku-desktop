package bilibili

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
)

// cacheVersion is the on-disk shape of the followed-UP cache. It is raised when
// the stored fields change in a way an older file cannot satisfy.
const cacheVersion = 1

// Cache owns the on-disk record of what was last known about each followed
// creator.
//
// It is deliberately separate from settings: a creator's latest activity is
// fetched data, not a user choice. What it keeps is the *last* row per creator
// — no history — so that a cold start shows the creators the user follows
// instead of an empty block, and so that a creator who has not posted for a
// while still says what they last did.
//
// A creator that is no longer followed is dropped from the file: the cache
// answers for the followed list, and keeping the others would leave data on
// disk about someone the user removed.
type Cache struct{ path string }

func NewCache(path string) *Cache { return &Cache{path: path} }

// cachedRow is one creator's row as it is written to disk. The domain type has
// no JSON names of its own, and the stored names are this file's business.
type cachedRow struct {
	CreatorID    int64     `json:"creatorId"`
	CreatorName  string    `json:"creatorName"`
	AvatarURL    string    `json:"avatarUrl"`
	Kind         string    `json:"kind"`
	Summary      string    `json:"summary"`
	URL          string    `json:"url"`
	PublishedAt  time.Time `json:"publishedAt"`
	Live         bool      `json:"live"`
	LiveRoomID   int64     `json:"liveRoomId"`
	LiveTitle    string    `json:"liveTitle"`
	LiveAudience string    `json:"liveAudience"`
}

type cachedRows struct {
	Version   int         `json:"version"`
	FetchedAt time.Time   `json:"fetchedAt"`
	Creators  []cachedRow `json:"creators"`
}

func rowFromUpdate(update domain.FollowedUpdate) cachedRow {
	return cachedRow{
		CreatorID:    update.CreatorID,
		CreatorName:  update.CreatorName,
		AvatarURL:    update.AvatarURL,
		Kind:         update.Kind,
		Summary:      update.Summary,
		URL:          update.URL,
		PublishedAt:  update.PublishedAt.UTC(),
		Live:         update.Live,
		LiveRoomID:   update.LiveRoomID,
		LiveTitle:    update.LiveTitle,
		LiveAudience: update.LiveAudience,
	}
}

func (r cachedRow) update() domain.FollowedUpdate {
	return domain.FollowedUpdate{
		CreatorID:    r.CreatorID,
		CreatorName:  r.CreatorName,
		AvatarURL:    r.AvatarURL,
		Kind:         r.Kind,
		Summary:      r.Summary,
		URL:          r.URL,
		PublishedAt:  r.PublishedAt.UTC(),
		Live:         r.Live,
		LiveRoomID:   r.LiveRoomID,
		LiveTitle:    r.LiveTitle,
		LiveAudience: r.LiveAudience,
	}
}

// Load reads the last known row of every creator stored for the given UIDs.
//
// A file that cannot be read or decoded is a cache miss rather than an error:
// it is a record of fetched data, and the next refresh replaces it. What it
// must never do is fail the feature.
func (c *Cache) Load(uids []int64) ([]domain.FollowedUpdate, time.Time, bool) {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil, time.Time{}, false
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, time.Time{}, false
	}
	var saved cachedRows
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != cacheVersion {
		return nil, time.Time{}, false
	}
	followed := make(map[int64]bool, len(uids))
	for _, uid := range uids {
		followed[uid] = true
	}
	updates := make([]domain.FollowedUpdate, 0, len(saved.Creators))
	for _, row := range saved.Creators {
		if row.CreatorID <= 0 || strings.TrimSpace(row.CreatorName) == "" || strings.TrimSpace(row.Summary) == "" {
			continue
		}
		// A creator that is no longer configured is not shown, even if the
		// file still holds a row for it: the settings are the authority on
		// who is followed.
		if !followed[row.CreatorID] {
			continue
		}
		updates = append(updates, row.update())
	}
	sortRows(updates)
	return updates, saved.FetchedAt.UTC(), true
}

// Save writes one row per creator, in the order the flyout shows them. Only
// the creators in updates are kept: Save is the whole file.
func (c *Cache) Save(updates []domain.FollowedUpdate, now time.Time) error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	rows := make([]cachedRow, 0, len(updates))
	seen := make(map[int64]bool, len(updates))
	for _, update := range updates {
		// One row per creator: the last one written for a creator wins, so a
		// refresh that reported two rows cannot leave two behind.
		if seen[update.CreatorID] {
			for i := range rows {
				if rows[i].CreatorID == update.CreatorID {
					rows[i] = rowFromUpdate(update)
					break
				}
			}
			continue
		}
		seen[update.CreatorID] = true
		rows = append(rows, rowFromUpdate(update))
	}
	data, err := json.Marshal(cachedRows{Version: cacheVersion, FetchedAt: now.UTC(), Creators: rows})
	if err != nil {
		return fmt.Errorf("encode followed-UP cache: %w", err)
	}
	if err := atomicfile.Write(c.path, append(data, '\n')); err != nil {
		return fmt.Errorf("write followed-UP cache: %w", err)
	}
	return nil
}

// Forget removes the creators that are not in uids from the file.
//
// It exists for the case where a creator is removed from the settings: the row
// on disk describes someone the user no longer follows, and leaving it would
// keep data about them after they asked for it to be gone. A file with nothing
// left in it is removed rather than kept empty.
func (c *Cache) Forget(uids []int64) error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read followed-UP cache: %w", err)
	}
	var saved cachedRows
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != cacheVersion {
		// An unreadable file describes nobody: replacing it is the honest
		// answer, and there is nothing worth preserving.
		return c.remove()
	}
	followed := make(map[int64]bool, len(uids))
	for _, uid := range uids {
		followed[uid] = true
	}
	kept := make([]cachedRow, 0, len(saved.Creators))
	for _, row := range saved.Creators {
		if followed[row.CreatorID] {
			kept = append(kept, row)
		}
	}
	if len(kept) == 0 {
		return c.remove()
	}
	encoded, err := json.Marshal(cachedRows{Version: cacheVersion, FetchedAt: saved.FetchedAt.UTC(), Creators: kept})
	if err != nil {
		return fmt.Errorf("encode followed-UP cache: %w", err)
	}
	if err := atomicfile.Write(c.path, append(encoded, '\n')); err != nil {
		return fmt.Errorf("write followed-UP cache: %w", err)
	}
	return nil
}

// remove deletes the file, treating an already absent one as done.
func (c *Cache) remove() error {
	if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove followed-UP cache: %w", err)
	}
	return nil
}

// sortRows orders rows the way the flyout shows them: live first, then newest.
func sortRows(updates []domain.FollowedUpdate) {
	sort.SliceStable(updates, func(i, j int) bool {
		if updates[i].Live != updates[j].Live {
			return updates[i].Live
		}
		return updates[i].PublishedAt.After(updates[j].PublishedAt)
	})
}
