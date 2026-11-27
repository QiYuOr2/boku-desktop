package diary

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
	"boku/internal/platform/httpc"
)

const (
	remoteImageDiskLimitBytes = 64 * 1024 * 1024
	remoteImageMemLimitCount  = 32
)

// ImageCache 为正文与链接卡片里的 HTTPS 外链图片提供按需加载与有上限的本地磁盘缓存。
// 绝不后台预抓取历史日记的外链图片。
type ImageCache struct {
	dir  string
	http RawRequester

	mu       sync.Mutex
	mem      map[string][]byte
	memOrder []string
}

func NewImageCache(dir string, http RawRequester) *ImageCache {
	return &ImageCache{
		dir:  dir,
		http: http,
		mem:  make(map[string][]byte),
	}
}

func (c *ImageCache) Load(ctx context.Context, rawURL string) ([]byte, error) {
	key, ok := ImageCacheKey(rawURL)
	if !ok {
		return nil, fmt.Errorf("%w: 外链图片只支持 HTTPS 地址", domain.ErrInvalidArgument)
	}
	hash := SHA256Hex([]byte(key))

	c.mu.Lock()
	if data, hit := c.mem[hash]; hit {
		c.mu.Unlock()
		return data, nil
	}
	c.mu.Unlock()

	cachePath := filepath.Join(c.dir, hash+".img")
	if c.dir != "" {
		if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 && len(data) <= MaxRemoteImageBytes {
			c.remember(hash, data)
			return data, nil
		}
	}

	if c.http == nil {
		return nil, domain.ErrProviderUnavailable
	}
	resp, err := c.http.Raw(ctx, "GET", key, map[string]string{
		"Accept": "image/*",
	}, nil, httpc.RawOptions{
		Timeout:      15 * time.Second,
		MaxBytes:     MaxRemoteImageBytes,
		MaxRedirects: 3,
	})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return nil, &domain.BadResponseError{
			ProviderID: "diary.image",
			HTTPStatus: resp.Status,
			Reason:     "外链图片加载失败",
		}
	}
	if resp.FinalURL != "" {
		if _, ok := ValidateImageURL(resp.FinalURL); !ok {
			return nil, &domain.BadResponseError{ProviderID: "diary.image", Reason: "外链图片跳转到非 HTTPS 地址"}
		}
	}
	ct := strings.ToLower(resp.Headers["content-type"])
	if ct != "" && !strings.HasPrefix(ct, "image/") && !strings.Contains(ct, "octet-stream") {
		return nil, &domain.BadResponseError{ProviderID: "diary.image", Reason: "外链地址返回的不是图片"}
	}
	if len(resp.Body) == 0 || len(resp.Body) > MaxRemoteImageBytes {
		return nil, &domain.BadResponseError{ProviderID: "diary.image", Reason: "外链图片大小超出限制"}
	}

	c.remember(hash, resp.Body)
	if c.dir != "" && os.MkdirAll(c.dir, 0o700) == nil {
		_ = atomicfile.Write(cachePath, resp.Body)
		c.pruneDisk()
	}
	return resp.Body, nil
}

func (c *ImageCache) remember(hash string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.mem[hash]; exists {
		return
	}
	if len(c.memOrder) >= remoteImageMemLimitCount {
		evict := c.memOrder[0]
		c.memOrder = c.memOrder[1:]
		delete(c.mem, evict)
	}
	c.mem[hash] = data
	c.memOrder = append(c.memOrder, hash)
}

func (c *ImageCache) pruneDisk() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type item struct {
		path    string
		size    int64
		modTime time.Time
	}
	var items []item
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, item{
			path:    filepath.Join(c.dir, entry.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		total += info.Size()
	}
	if total <= remoteImageDiskLimitBytes {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.Before(items[j].modTime)
	})
	for _, it := range items {
		if total <= remoteImageDiskLimitBytes {
			break
		}
		if os.Remove(it.path) == nil {
			total -= it.size
		}
	}
}
