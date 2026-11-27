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

// cacheVersion 是关注 UP 缓存的磁盘格式版本。存储字段发生旧文件无法满足的变更时递增。
const cacheVersion = 1

// Cache 保存每个关注 UP 最近一次已知状态的磁盘记录。
//
// 它与设置刻意分离：UP 的最新动态是抓取来的数据，不是用户选择。它只保留每个 UP 的
// 最后一行动态（不留历史），让冷启动能显示已关注的 UP 而不是空块，也让久未发帖的 UP
// 仍显示上次做过什么。
//
// 不再关注的 UP 会从文件中删除：缓存只为关注列表作答，保留其余会在磁盘上留下用户已
// 移除对象的数据。
type Cache struct{ path string }

func NewCache(path string) *Cache { return &Cache{path: path} }

// cachedRow 是写入磁盘的单个 UP 记录。domain 类型没有自己的 JSON 名，磁盘字段名
// 由本文件决定。
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

// Load 读取给定 UID 存储的每个 UP 的最后一行。
//
// 无法读取或解码的文件视为缓存未命中而非错误：它只是抓取数据的记录，下次刷新会替换。
// 它绝不能导致功能失败。
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
		// 已不再配置的 UP 不予显示，即使文件里仍有其记录：关注关系以设置为准。
		if !followed[row.CreatorID] {
			continue
		}
		updates = append(updates, row.update())
	}
	sortRows(updates)
	return updates, saved.FetchedAt.UTC(), true
}

// Save 每个 UP 写一行，顺序与 flyout 显示一致。只保留 updates 中的 UP：
// Save 写的就是整份文件。
func (c *Cache) Save(updates []domain.FollowedUpdate, now time.Time) error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	rows := make([]cachedRow, 0, len(updates))
	seen := make(map[int64]bool, len(updates))
	for _, update := range updates {
		// 每个 UP 只留一行：同一 UP 后写的覆盖先写的，避免刷新返回两行时留下两条。
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

// Forget 从文件中删除不在 uids 里的 UP。
//
// 用于 UP 从设置中移除的场景：磁盘上的记录描述的是用户已不再关注的人，留着就是在对方
// 要求删除后继续保留其数据。删空后的文件直接移除，不留空文件。
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
		// 无法读取的文件不对应任何人：替换它是诚实的做法，没有值得保留的内容。
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

// remove 删除文件，已不存在视为已完成。
func (c *Cache) remove() error {
	if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove followed-UP cache: %w", err)
	}
	return nil
}

// sortRows 按 flyout 的显示顺序排序：直播在前，然后按时间倒序。
func sortRows(updates []domain.FollowedUpdate) {
	sort.SliceStable(updates, func(i, j int) bool {
		if updates[i].Live != updates[j].Live {
			return updates[i].Live
		}
		return updates[i].PublishedAt.After(updates[j].PublishedAt)
	})
}
