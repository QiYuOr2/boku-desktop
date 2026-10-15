package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ClipKind says what a clipboard entry holds. The first four describe text
// the user copied; the last two say that what was copied is a file list or
// an image, which Boku records by name and never by content.
type ClipKind string

const (
	ClipKindText  ClipKind = "text"
	ClipKindURL   ClipKind = "url"
	ClipKindJSON  ClipKind = "json"
	ClipKindCode  ClipKind = "code"
	ClipKindFile  ClipKind = "file"
	ClipKindImage ClipKind = "image"
)

// Valid reports whether the kind is one the application knows.
func (k ClipKind) Valid() bool {
	switch k {
	case ClipKindText, ClipKindURL, ClipKindJSON, ClipKindCode, ClipKindFile, ClipKindImage:
		return true
	}
	return false
}

// ClipContent is one reading of the system clipboard, as the platform found
// it: plain data, with no hint of which toolkit produced it.
//
// More than one of Text, Files, HasImage and Image can be set at once —
// copying a file from a file manager puts its path on the clipboard as text
// too — and Kind says which of them the user means.
type ClipContent struct {
	Kind ClipKind
	// Display is the one line the interface shows for this content.
	Display string
	// Text is the plain text on the clipboard, truncated to MaxClipTextBytes.
	Text string
	// Files are the paths on the clipboard, in the order the platform gave
	// them.
	Files []string
	// HasImage is set when an image is on the clipboard.
	HasImage bool
	// ImageBytes is how large the bitmap on the clipboard is, when one was
	// read.
	ImageBytes int
	// Image is the bitmap itself, as PNG bytes, up to MaxClipImageBytes. It
	// is kept — rather than only measured, which is what this did until
	// 2026-10-15 — because a picture has to be able to go back where it came
	// from: a click on an image in the history puts it there, and the
	// selection tool borrows the clipboard and gives it back.
	Image []byte
	// ImageRef identifies the bitmap: the hash of the bytes above, which is
	// what the history stores and compares. Two screenshots of the same
	// size are two pictures, and only the hash can say so.
	ImageRef string
	// HTML is the rich-text flavour of the copy, when the platform has one.
	// The application keeps no markup: it is read for the text it shows.
	HTML string
	// Observation is what the application noticed about this reading that
	// the user should know: something it could not read, or something it
	// had to make do without. It is in the application's own words, and it
	// is empty when the reading was complete.
	Observation string
	// CapturedAt is when the platform was read.
	CapturedAt time.Time
}

// WithObservation returns the reading with a note about what was noticed
// while making it.
func (c ClipContent) WithObservation(note string) ClipContent {
	c.Observation = note
	return c
}

// Valid reports whether the reading has a kind the application knows.
func (c ClipContent) Valid() bool { return c.Kind.Valid() }

// Unchanged reports whether two readings are the same content, which is what
// keeps the poll from recording the same copy twice.
//
// A bitmap is compared by the hash of its bytes when both readings carry one:
// comparing the size alone — which is what this did until 2026-10-15 — made
// two different screenshots of the same size look like one picture, so the
// second was never recorded and clicking the first put the wrong one back.
// Readings that carry no hash (a test's, or a picture whose bytes could not be
// read) fall back to the size.
func (c ClipContent) Unchanged(other ClipContent) bool {
	if c.Kind != other.Kind || c.HasImage != other.HasImage ||
		c.Display != other.Display || c.Text != other.Text {
		return false
	}
	if c.ImageRef != "" || other.ImageRef != "" {
		if c.ImageRef != other.ImageRef {
			return false
		}
	} else if c.ImageBytes != other.ImageBytes {
		return false
	}
	if len(c.Files) != len(other.Files) {
		return false
	}
	for i := range c.Files {
		if c.Files[i] != other.Files[i] {
			return false
		}
	}
	return true
}

// ClipRecord is one reading as the application keeps it: the content, the
// hash that identifies it, and the one line the interface draws for it.
//
// It is separate from ClipContent, which is what the platform answered: a
// record has already been classified, shortened and reduced to one line, so
// that drawing a frame never does that work.
type ClipRecord struct {
	Kind    ClipKind
	Content string
	Display string
	Hash    string
	// Files are the paths the copy holds, when it is a file list.
	Files []string
	// ImageBytes is the size of the bitmap the copy held, when the platform
	// offered one.
	ImageBytes int
	// ImageRef identifies that bitmap: the hash of its bytes, which is what
	// a click on the entry loads from the image store to put the picture
	// back. It is empty when the copy held no picture, or when its bytes
	// could not be read.
	ImageRef string
	// Note is what the reading noticed about itself, for the interface to
	// show: something it could not read, or something it had to make do
	// without. It is empty when the reading was complete.
	Note string
	// CapturedAt is when the platform was read.
	CapturedAt time.Time
}

// ClipboardEntry is one recorded copy: the unit of clipboard history.
type ClipboardEntry struct {
	ID string
	// Content is what the entry puts back on the clipboard: the text for a
	// copy, the paths for a file list, and the one line for an image.
	Content string
	// Display is the one line the interface shows for the entry. It is
	// stored rather than recomputed so that drawing a list of copies is
	// reading, not work.
	Display string
	// Files are the paths the copy holds, when it is a file list: what a
	// click on the entry writes back, so that pasting a copy of files
	// pastes the files.
	Files []string
	// ImageBytes is the size of the bitmap the copy held, when an image was
	// on the clipboard and was read.
	ImageBytes int
	// ImageRef is the picture this copy holds, kept in the image store
	// rather than in the history file: a click on the entry loads it and
	// puts the picture back on the clipboard. Empty when the copy held no
	// picture, or when its bytes could not be read.
	ImageRef string
	// Note is what the reading noticed about itself, for the interface to
	// show. It is empty when the reading was complete.
	Note string
	// ContentHash identifies the content for deduplication. Entries with
	// the same hash are the same entry.
	ContentHash string
	Kind        ClipKind
	// Pinned entries stay while unpinned ones are evicted.
	Pinned bool
	// CopiedAt is when the platform was read; CreatedAt is when the entry
	// was written. They are separate so that pinning a copy back to the
	// clipboard can move CopiedAt without rewriting history.
	CopiedAt  time.Time
	CreatedAt time.Time
	// DeletedAt is the soft delete of the documented model. This
	// application's store keeps no deleted entries yet; the field exists
	// so the shape matches §7 of the domain model.
	DeletedAt *time.Time
}

// Line is what a list shows for the entry. A stored display is used when
// there is one; an entry from an older file is shown as its first line.
func (e ClipboardEntry) Line() string {
	if e.Display != "" {
		return e.Display
	}
	if index := strings.IndexByte(e.Content, '\n'); index >= 0 {
		return e.Content[:index] + " …"
	}
	return e.Content
}

// Validate reports what is wrong with the entry.
func (e ClipboardEntry) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("%w: clipboard entry has no id", ErrInvalidArgument)
	}
	if strings.TrimSpace(e.ContentHash) == "" {
		return fmt.Errorf("%w: clipboard entry %s has no content hash", ErrInvalidArgument, e.ID)
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: clipboard entry %s has kind %q", ErrInvalidArgument, e.ID, e.Kind)
	}
	if e.Content == "" {
		return fmt.Errorf("%w: clipboard entry %s has no content", ErrInvalidArgument, e.ID)
	}
	if e.CopiedAt.IsZero() {
		return fmt.Errorf("%w: clipboard entry %s has no copiedAt", ErrInvalidArgument, e.ID)
	}
	return nil
}

// MaxClipTextBytes is how much text of one copy is kept. Anything longer is
// truncated: a clipboard holding a whole file must not end up in the history.
//
// It is a bound on the entity rather than a setting, which is why it lives
// here and not with the clipboard feature's own configuration: what a recorded
// copy may hold is what a ClipboardEntry is, and internal/kit — which does the
// truncating — may only depend on this package.
const MaxClipTextBytes = 64 << 10

// MaxClipImageBytes is how large a bitmap on the clipboard is read. A larger
// one is not read at all: the entry still says that a picture was copied,
// without the application holding it.
const MaxClipImageBytes = 16 << 20

// ClipboardStore reads and writes clipboard history. Entries are stored
// whole, newest first: the list is short enough that reading it costs
// nothing, and replacing it is the whole write.
type ClipboardStore interface {
	LoadEntries(ctx context.Context) ([]ClipboardEntry, error)
	SaveEntries(ctx context.Context, entries []ClipboardEntry) error
	// SaveImage keeps the bytes of a copied picture under ref, so that a
	// click on its entry can put the picture back. Saving the same ref twice
	// is not an error and not a second copy.
	SaveImage(ctx context.Context, ref string, png []byte) error
	// LoadImage returns what SaveImage kept.
	LoadImage(ctx context.Context, ref string) ([]byte, error)
	// DeleteImage forgets it, which is what dropping its entry does.
	DeleteImage(ctx context.Context, ref string) error
}

// ClipboardDevice is the platform clipboard. Read runs on the thread the
// platform requires; an implementation is responsible for that, so callers
// may read from a goroutine.
type ClipboardDevice interface {
	// Read returns what is on the clipboard now. ErrUnsupportedClipboardFormat
	// means it held nothing this application can show.
	Read(ctx context.Context) (ClipContent, error)
	// WriteText puts text back on the clipboard.
	WriteText(ctx context.Context, text string) error
	// WriteFiles puts a file list back on the clipboard: what a click on a
	// recorded copy of files should do is copy the files again, not the
	// text of their paths.
	WriteFiles(ctx context.Context, paths []string) error
	// WriteImage puts a bitmap back on the clipboard, as the PNG bytes it
	// was read as. A click on an image in the history is what uses it, and
	// so is the selection tool putting back what it borrowed.
	WriteImage(ctx context.Context, png []byte) error
}

// ErrClipboardChanged reports content that changed while it was being read,
// so the reading is dropped rather than recorded half-way.
var ErrClipboardChanged = errors.New("clipboard changed while reading")
