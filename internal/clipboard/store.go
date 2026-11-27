// 本文件是磁盘上的剪贴板历史：条目，以及与之相伴的图片。
//
// 它随剪贴板功能而居而非放进独立的存储包，因为它就是该功能的文件：没有别处
// 读取这段历史，日后迁到 clipboard_entries 表时也只会在这里、不会在别处改动。
//
// 条目形状不随这次迁移而变：存储设计 §3 描述的是同样的字段，而本文件每次复制都
// 整体重写，因为历史足够短，这是最简单也正确的做法。
package clipboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/atomicfile"
)

// historyFileVersion 是本构建读写的形状。其它版本的文件会被拒读，而非部分理解。
const historyFileVersion = 1

// 复制的图片存放在历史文件旁的独立目录，每张图片一个文件，以字节哈希命名。
// 它们作为文件而非历史的一部分，因为一张截图动辄数兆，而历史文件每次复制都
// 整体重写——也因为图片是历史中唯一可以干脆丢弃的东西。
const imageDirName = "clipboard_images"

// historyDocument 即 clipboard_history.json。
type historyDocument struct {
	Version int            `json:"version"`
	Entries []historyEntry `json:"entries"`
}

// historyEntry 是一条已记录的复制，以文件中的形式。
type historyEntry struct {
	ID          string          `json:"id"`
	Content     string          `json:"content"`
	ContentHash string          `json:"contentHash"`
	Kind        domain.ClipKind `json:"kind"`
	Pinned      bool            `json:"pinned"`
	CopiedAt    time.Time       `json:"copiedAt"`
	CreatedAt   time.Time       `json:"createdAt"`
	DeletedAt   *time.Time      `json:"deletedAt"`
	// Display 是界面为该条目显示的那一行。
	Display string `json:"display"`
	// Files 是复制持有的路径（当它是文件列表时）：它们是条目写回的内容，
	// 使粘贴文件复制得到的是文件。
	Files []string `json:"files"`
	// ImageBytes 是复制持有的位图大小。
	ImageBytes int `json:"imageBytes"`
	// ImageRef 指向存放在本文件旁图片目录中的该条目图片。字节放在那里而非此处，
	// 因为截图动辄数兆而本文件每次复制都整体重写；点击条目时加载它们并放回图片。
	ImageRef string `json:"imageRef,omitempty"`
	// Note 是读取时注意到的关于自身的信息，例如一张读不出的图片。
	Note string `json:"note"`
}

// History 是以文件为载体的剪贴板历史。它是一个 domain.ClipboardStore，
// 可安全地被多个 goroutine 使用。
//
// Path 为空表示历史仅保存在内存中，供测试或没有用户目录的装配使用。
type History struct {
	Path string

	mu      sync.Mutex
	entries []domain.ClipboardEntry
}

// NewHistory 返回指向某个路径的存储。
func NewHistory(path string) (*History, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: the clipboard history store has no path", domain.ErrInvalidArgument)
	}
	return &History{Path: path}, nil
}

// LoadEntries 读取剪贴板历史，最新在前。文件不存在视为空历史。
func (h *History) LoadEntries(ctx context.Context) ([]domain.ClipboardEntry, error) {
	if err := kit.ContextErr(ctx); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(h.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("clipboard history: read %s: %w", h.Path, err)
	}
	var doc historyDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("clipboard history: %s is not a readable history file: %w", h.Path, err)
	}
	if doc.Version != historyFileVersion {
		return nil, fmt.Errorf("clipboard history: %s is version %d, this build reads version %d",
			h.Path, doc.Version, historyFileVersion)
	}
	entries := make([]domain.ClipboardEntry, 0, len(doc.Entries))
	for _, stored := range doc.Entries {
		entries = append(entries, domain.ClipboardEntry{
			ID:          stored.ID,
			Content:     stored.Content,
			ContentHash: stored.ContentHash,
			Kind:        stored.Kind,
			Pinned:      stored.Pinned,
			CopiedAt:    stored.CopiedAt.UTC(),
			CreatedAt:   stored.CreatedAt.UTC(),
			DeletedAt:   stored.DeletedAt,
			Display:     stored.Display,
			Files:       append([]string(nil), stored.Files...),
			ImageBytes:  stored.ImageBytes,
			ImageRef:    stored.ImageRef,
			Note:        stored.Note,
		})
	}
	h.mu.Lock()
	h.entries = entries
	h.mu.Unlock()
	return entries, nil
}

// SaveEntries 替换剪贴板历史。写入在锁内进行，两个保存不会在同一文件上竞争。
func (h *History) SaveEntries(ctx context.Context, entries []domain.ClipboardEntry) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	doc := historyDocument{Version: historyFileVersion, Entries: make([]historyEntry, 0, len(entries))}
	for _, entry := range entries {
		doc.Entries = append(doc.Entries, historyEntry{
			ID:          entry.ID,
			Content:     entry.Content,
			ContentHash: entry.ContentHash,
			Kind:        entry.Kind,
			Pinned:      entry.Pinned,
			CopiedAt:    entry.CopiedAt.UTC(),
			CreatedAt:   entry.CreatedAt.UTC(),
			DeletedAt:   entry.DeletedAt,
			Display:     entry.Display,
			Files:       append([]string(nil), entry.Files...),
			ImageBytes:  entry.ImageBytes,
			ImageRef:    entry.ImageRef,
			Note:        entry.Note,
		})
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("clipboard history: encode %s: %w", h.Path, err)
	}
	if err := atomicfile.Write(h.Path, append(data, '\n')); err != nil {
		return fmt.Errorf("clipboard history: %w", err)
	}
	h.entries = append([]domain.ClipboardEntry(nil), entries...)
	return nil
}

// SaveImage 把复制图片的字节保存在 ref 下。
//
// 同一 ref 保存两次不算第二份：同一张图片就是同一个文件，而轮询很可能把它记录两次。
func (h *History) SaveImage(ctx context.Context, ref string, png []byte) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	if len(png) == 0 {
		return fmt.Errorf("%w: there are no image bytes to keep", domain.ErrInvalidArgument)
	}
	path, err := h.imagePath(ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := atomicfile.Write(path, png); err != nil {
		return fmt.Errorf("clipboard history: %w", err)
	}
	return nil
}

// LoadImage 返回保存在 ref 下的图片。
func (h *History) LoadImage(ctx context.Context, ref string) ([]byte, error) {
	if err := kit.ContextErr(ctx); err != nil {
		return nil, err
	}
	path, err := h.imagePath(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read the picture: %v", domain.ErrUnsupportedClipboardFormat, err)
	}
	return data, nil
}

// DeleteImage 忘记保存在 ref 下的图片。图片本已不在不算错误：丢弃条目两次不是失败。
func (h *History) DeleteImage(ctx context.Context, ref string) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	path, err := h.imagePath(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clipboard history: delete the picture: %w", err)
	}
	return nil
}

// imagePath 是此 ref 的图片存放位置。
//
// ref 会成为文件名，而文件是可被编辑的，因此校验而非信任它：含分隔符或以点开头的
// 引用会指向目录之外。「sha256:…」中的冒号会换成破折号，那是 Windows 文件名唯一
// 不能包含的字符。
func (h *History) imagePath(ref string) (string, error) {
	if ref == "" || len(ref) > 200 || strings.ContainsAny(ref, `/\`) ||
		strings.HasPrefix(ref, ".") {
		return "", fmt.Errorf("%w: %q is not a picture reference", domain.ErrInvalidArgument, ref)
	}
	name := strings.ReplaceAll(ref, ":", "-") + ".png"
	return filepath.Join(filepath.Dir(h.Path), imageDirName, name), nil
}

// 在端口定义处确认它被满足，而非在调用处。
var _ domain.ClipboardStore = (*History)(nil)
