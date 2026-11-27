package anime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
)

const cacheVersion = 2

// Cache 持有动画功能短期的磁盘缓存。它与设置刻意分离：剧集是取回的数据而非用户选择，
// 且只有文件中标注的上海日期可用。
type Cache struct{ path string }

func NewCache(path string) *Cache { return &Cache{path: path} }

type cachedToday struct {
	Version   int                     `json:"version"`
	Day       string                  `json:"day"`
	FetchedAt time.Time               `json:"fetchedAt"`
	Releases  []domain.EpisodeRelease `json:"releases"`
	Covers    map[int64][]byte        `json:"covers"`
}

// Load 只返回为 day 收集的结果。昨天的文件属于预期的缓存未命中而非错误：
// 绝不能把昨天的档期泄漏进今天的面板。
func (c *Cache) Load(day string) ([]domain.EpisodeRelease, map[int64][]byte, time.Time, bool, error) {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil, nil, time.Time{}, false, nil
	}
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, nil, time.Time{}, false, fmt.Errorf("read animation cache: %w", err)
	}
	var saved cachedToday
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, nil, time.Time{}, false, fmt.Errorf("decode animation cache: %w", err)
	}
	if saved.Version != cacheVersion {
		return nil, nil, time.Time{}, false, fmt.Errorf("decode animation cache: unsupported version %d", saved.Version)
	}
	if saved.Day != day {
		return nil, nil, time.Time{}, false, nil
	}
	if saved.FetchedAt.IsZero() {
		return nil, nil, time.Time{}, false, fmt.Errorf("decode animation cache: missing fetchedAt")
	}
	for _, release := range saved.Releases {
		if release.SubjectID <= 0 || release.EpisodeID <= 0 || strings.TrimSpace(release.SubjectName) == "" || release.AirDate.In(shanghai()).Format(time.DateOnly) != day {
			return nil, nil, time.Time{}, false, fmt.Errorf("decode animation cache: invalid release")
		}
	}
	covers := make(map[int64][]byte, len(saved.Covers))
	for subjectID, data := range saved.Covers {
		if subjectID <= 0 || len(data) == 0 {
			return nil, nil, time.Time{}, false, fmt.Errorf("decode animation cache: invalid cover")
		}
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return nil, nil, time.Time{}, false, fmt.Errorf("decode animation cache: invalid cover: %w", err)
		}
		covers[subjectID] = append([]byte(nil), data...)
	}
	return append([]domain.EpisodeRelease(nil), saved.Releases...), covers, saved.FetchedAt.UTC(), true, nil
}

func (c *Cache) Save(day string, fetchedAt time.Time, releases []domain.EpisodeRelease, covers map[int64][]byte) error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	data, err := json.Marshal(cachedToday{
		Version:   cacheVersion,
		Day:       day,
		FetchedAt: fetchedAt.UTC(),
		Releases:  releases,
		Covers:    cloneCovers(covers),
	})
	if err != nil {
		return fmt.Errorf("encode animation cache: %w", err)
	}
	if err := atomicfile.Write(c.path, append(data, '\n')); err != nil {
		return fmt.Errorf("write animation cache: %w", err)
	}
	return nil
}

func cloneCovers(covers map[int64][]byte) map[int64][]byte {
	if len(covers) == 0 {
		return nil
	}
	copy := make(map[int64][]byte, len(covers))
	for subjectID, data := range covers {
		copy[subjectID] = append([]byte(nil), data...)
	}
	return copy
}
