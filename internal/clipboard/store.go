// This file is the clipboard history on disk: the entries, and the pictures
// that go with them.
//
// It lives with the clipboard feature rather than in a storage package of its
// own because it is that feature's file: nothing else reads the history, and
// when Task 016 moves it to the clipboard_entries table it will move here and
// nowhere else.
//
// The shape of an entry does not change with that move: §3 of the storage design
// describes the same fields, and this file is written whole on every copy
// because the history is short enough for that to be the simplest thing that
// is also correct.
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

// historyFileVersion is the shape this build reads and writes. A file from
// another version is refused rather than partly understood.
const historyFileVersion = 1

// The pictures of recorded copies live in a directory of their own beside the
// history file, one file per picture, named after the hash of its bytes. They
// are files rather than part of the history file because a screenshot is
// megabytes and the history file is written whole on every copy — and because
// a picture is the one thing in the history that can also just be dropped.
const imageDirName = "clipboard_images"

// historyDocument is clipboard_history.json.
type historyDocument struct {
	Version int            `json:"version"`
	Entries []historyEntry `json:"entries"`
}

// historyEntry is one recorded copy, as the file holds it.
type historyEntry struct {
	ID          string          `json:"id"`
	Content     string          `json:"content"`
	ContentHash string          `json:"contentHash"`
	Kind        domain.ClipKind `json:"kind"`
	Pinned      bool            `json:"pinned"`
	CopiedAt    time.Time       `json:"copiedAt"`
	CreatedAt   time.Time       `json:"createdAt"`
	DeletedAt   *time.Time      `json:"deletedAt"`
	// Display is the one line the interface shows for the entry.
	Display string `json:"display"`
	// Files are the paths the copy holds, when it is a file list: they are
	// what the entry writes back, so that pasting a copy of files pastes
	// the files.
	Files []string `json:"files"`
	// ImageBytes is how large the bitmap the copy held was.
	ImageBytes int `json:"imageBytes"`
	// ImageRef names the picture kept for this entry in the image directory
	// beside this file. The bytes are there rather than here because a
	// screenshot is megabytes and this file is written whole on every copy;
	// a click on the entry loads them and puts the picture back.
	ImageRef string `json:"imageRef,omitempty"`
	// Note is what the reading noticed about itself, such as a picture it
	// could not read.
	Note string `json:"note"`
}

// History is the clipboard history as a file. It is a domain.ClipboardStore,
// and it is safe to use from several goroutines.
//
// An empty Path means the history is kept in memory only, which is how a test
// or an assembly without a user directory runs.
type History struct {
	Path string

	mu      sync.Mutex
	entries []domain.ClipboardEntry
}

// NewHistory returns the store over a path.
func NewHistory(path string) (*History, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: the clipboard history store has no path", domain.ErrInvalidArgument)
	}
	return &History{Path: path}, nil
}

// LoadEntries reads the clipboard history, newest first. A missing file is
// an empty history.
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

// SaveEntries replaces the clipboard history. The write happens under the
// lock, so two saves cannot race over one file.
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

// SaveImage keeps the bytes of a copied picture under ref.
//
// Saving the same ref twice is not a second copy: the same picture is the same
// file, and the poll may well record it twice over.
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

// LoadImage returns the picture kept under ref.
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

// DeleteImage forgets the picture kept under ref. A picture that is already
// gone is not an error: dropping an entry twice is not a failure.
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

// imagePath is where the picture with this ref is kept.
//
// The ref is a file name, and a file can be edited, so it is checked rather
// than trusted: a reference with a separator or a leading dot in it would name
// a file outside the directory. The colon in "sha256:…" becomes a dash, which
// is the one character a Windows file name cannot hold.
func (h *History) imagePath(ref string) (string, error) {
	if ref == "" || len(ref) > 200 || strings.ContainsAny(ref, `/\`) ||
		strings.HasPrefix(ref, ".") {
		return "", fmt.Errorf("%w: %q is not a picture reference", domain.ErrInvalidArgument, ref)
	}
	name := strings.ReplaceAll(ref, ":", "-") + ".png"
	return filepath.Join(filepath.Dir(h.Path), imageDirName, name), nil
}

// ensure the port is satisfied where it is defined, not at the call site.
var _ domain.ClipboardStore = (*History)(nil)
