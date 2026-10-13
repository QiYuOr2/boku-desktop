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
	// read. The bytes themselves are not kept: the application records that
	// a picture was copied and how big it was, and copies back the file
	// when the platform also offered one.
	ImageBytes int
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
// keeps the poll from recording the same copy twice. A bitmap is compared by
// its size: a screenshot is the same screenshot while nobody takes another
// one.
func (c ClipContent) Unchanged(other ClipContent) bool {
	if c.Kind != other.Kind || c.HasImage != other.HasImage ||
		c.Display != other.Display || c.Text != other.Text ||
		c.ImageBytes != other.ImageBytes {
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
	// offered one. The bytes are not kept; the size says that there was a
	// picture, and how large it was.
	ImageBytes int
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
	// on the clipboard and was read: the bytes are not kept.
	ImageBytes int
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
	// so the shape matches docs/data-model.md §7.
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

// ToolSettings is the application's own configuration: the switches that are
// about Boku itself rather than about a panel or a provider.
//
// Both clipboard switches default to false. The clipboard is the most
// sensitive thing Boku can read, so reading it is something the user turns
// on rather than something they have to find and turn off.
type ToolSettings struct {
	// ClipboardRegionVisible shows the clipboard line in the quick window.
	ClipboardRegionVisible bool
	// ClipboardMonitoring reads the clipboard in the background. Off means
	// no reading at all, not a hidden reading.
	ClipboardMonitoring bool
	// ClipboardPollInterval is how often the clipboard is read while
	// monitoring. Values below ClipboardMinPollInterval are raised to it.
	ClipboardPollInterval time.Duration
}

// Bounds on the clipboard poll. The floor keeps a mistaken configuration
// from turning the poll into a busy loop; the default is the interval
// docs/tools.md §4.1 names.
const (
	ClipboardMinPollInterval     = 200 * time.Millisecond
	ClipboardDefaultPollInterval = 700 * time.Millisecond
	// MaxClipTextBytes is how much text of one copy is kept. Anything
	// longer is truncated: a clipboard holding a whole file must not end
	// up in the history.
	MaxClipTextBytes = 64 << 10
	// MaxClipImageBytes is how large a bitmap on the clipboard is read. A
	// larger one is not read at all: the entry still says that a picture
	// was copied, without the application holding it.
	MaxClipImageBytes = 16 << 20
	// ClipboardHistoryLimit is how many entries are kept. The history page
	// shows the ten most recent of them.
	ClipboardHistoryLimit = 100
	// ClipboardHistoryPageSize is how many entries the history page shows.
	ClipboardHistoryPageSize = 10
)

// Normalize fills in what a stored setting left out and brings the interval
// inside its bounds. It never fails: a hand-edited settings file should not
// keep the application from starting, it should just be made sane.
func (s ToolSettings) Normalize() ToolSettings {
	if s.ClipboardPollInterval < ClipboardMinPollInterval {
		s.ClipboardPollInterval = ClipboardDefaultPollInterval
	}
	return s
}

// Interval is the poll interval, inside its bounds.
func (s ToolSettings) Interval() time.Duration {
	if s.ClipboardPollInterval < ClipboardMinPollInterval {
		return ClipboardDefaultPollInterval
	}
	return s.ClipboardPollInterval
}

// ToolStore reads and writes the application's own settings. Its
// implementation is a file, and it is the only place they are written.
type ToolStore interface {
	Load(ctx context.Context) (ToolSettings, error)
	Save(ctx context.Context, settings ToolSettings) error
}

// ClipboardStore reads and writes clipboard history. Entries are stored
// whole, newest first: the list is short enough that reading it costs
// nothing, and replacing it is the whole write.
type ClipboardStore interface {
	LoadEntries(ctx context.Context) ([]ClipboardEntry, error)
	SaveEntries(ctx context.Context, entries []ClipboardEntry) error
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
}

// ErrClipboardChanged reports content that changed while it was being read,
// so the reading is dropped rather than recorded half-way.
var ErrClipboardChanged = errors.New("clipboard changed while reading")
