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
//
// The translation switches are the other half of the same struct: one file
// holds everything the user chose about the application, and adding a switch
// never changes the file's version.
type ToolSettings struct {
	// ClipboardRegionVisible shows the clipboard line in the quick window.
	ClipboardRegionVisible bool
	// ClipboardMonitoring reads the clipboard in the background. Off means
	// no reading at all, not a hidden reading.
	ClipboardMonitoring bool
	// ClipboardPollInterval is how often the clipboard is read while
	// monitoring. Values below ClipboardMinPollInterval are raised to it.
	ClipboardPollInterval time.Duration
	// ClipboardHistorySize is how many of the most recent copies the
	// history keeps, and therefore how many the history page lists.
	// Anything that is not one of ClipboardHistorySizes is replaced by
	// ClipboardDefaultHistorySize.
	ClipboardHistorySize int

	// Panels holds what the user chose about the panels that can be turned
	// off, by panel id: false is off, true is on, and a panel whose id is not
	// here is on. An absent key is not a choice — a file written before a
	// panel existed must not read as somebody having turned it off, and the
	// switches in the settings window are the only thing that puts a key
	// here at all.
	Panels map[string]bool

	// Translation is the selection-translation tool's own configuration,
	// held in the same file for the same reason.
	Translation TranslationSettings
}

// PanelOn reports whether a panel is on. The default is on, because a panel
// is listed by the assembly and the user's switches are what take one away.
func (s ToolSettings) PanelOn(panelID string) bool {
	on, chosen := s.Panels[strings.TrimSpace(panelID)]
	return !chosen || on
}

// TranslationSettings is what the selection-translation tool was told to do.
//
// The default is on, with nothing else configured: pressing the shortcut
// should reach the tool, and the tool should then say where to put a key
// rather than be silent. A user who wants the shortcut to do nothing turns
// Enabled off, and the window it would have opened says so.
type TranslationSettings struct {
	// Enabled turns the tool on. Off means the shortcut opens a window that
	// says the tool is off, rather than a window that fails.
	Enabled bool
	// Accelerator is the global shortcut that starts a translation, in the
	// form the platform's own shortcut API takes, such as
	// "CmdOrCtrl+Shift+T". Changing it is a later task; it is stored now so
	// that the file is where the answer lives.
	Accelerator string
	// Provider names the translator to use, by the id it is registered
	// under. A name no translator answers to is replaced by the default.
	Provider string
	// Furigana asks the translator to annotate Japanese kanji with their
	// kana reading.
	Furigana bool
	// LoanwordGloss asks it to annotate katakana loanwords with the word
	// they came from.
	LoanwordGloss bool
	// Pinned is whether the window starts pinned: a pinned window stays
	// where it was put and does not close when the user clicks elsewhere.
	Pinned bool
	// CloseDelay is how long the window waits before closing itself once it
	// loses the keyboard. It is bounded by the constants below, and a stored
	// value outside them is brought back inside rather than kept.
	CloseDelay time.Duration
	// Custom is the address of the OpenAI-compatible service the "custom"
	// translator asks. It is not sensitive: a credential never goes in
	// here.
	Custom CustomTranslatorSettings
}

// CustomTranslatorSettings is the user's own translation service: where it
// is, and what to call it.
type CustomTranslatorSettings struct {
	// BaseURL is the service's address, such as
	// "https://api.example.com/v1". Empty means the translator is not
	// configured, and the window says so.
	BaseURL string
	// DisplayName is what the user calls the service. Empty means the
	// interface uses its own word for it.
	DisplayName string
}

// Bounds on the translation window's own behavior.
const (
	// DefaultTranslationProvider is the translator a fresh installation
	// uses. It is the one whose credential the application already has a
	// place for: the panel's own key.
	DefaultTranslationProvider = "deepseek"
	// DefaultTranslationAccelerator is the shortcut that starts a
	// translation: the one the design's settings page shows, written the
	// way the platform's own parser reads it. "Ctrl" rather than
	// "CmdOrCtrl" because the design names Ctrl and this build's platforms
	// use it; a Mac build writes Super.
	DefaultTranslationAccelerator = "Ctrl+Shift+T"
	// TranslationMinCloseDelay and TranslationMaxCloseDelay bound the wait
	// before the window closes itself. Zero is allowed and means "at once":
	// a user who wants the window gone when they look away can say so.
	TranslationMinCloseDelay     = 0
	TranslationMaxCloseDelay     = 5 * time.Second
	DefaultTranslationCloseDelay = 400 * time.Millisecond
	// MaxTranslationChars bounds what is sent to a translator. A selection
	// is a sentence or a paragraph; a whole document pasted into the
	// clipboard is not what this tool is for, and the window says what it
	// left out.
	MaxTranslationChars = 4000
	// MaxTranslatorNameChars bounds the user's own name for a service: it
	// is drawn in a line of a window, not stored as a document.
	MaxTranslatorNameChars = 40
)

// TranslationDefaults is the configuration of a user who has never opened
// the settings: the tool on, the built-in translator, both Japanese
// annotations on, the window unpinned and closing soon after it loses the
// keyboard.
func TranslationDefaults() TranslationSettings {
	return TranslationSettings{
		Enabled:       true,
		Accelerator:   DefaultTranslationAccelerator,
		Provider:      DefaultTranslationProvider,
		Furigana:      true,
		LoanwordGloss: true,
		CloseDelay:    DefaultTranslationCloseDelay,
	}.Normalize()
}

// Normalize fills in what a stored setting left out and brings the values
// that are not free-form into their bounds. Like ToolSettings.Normalize it
// never fails: a hand-edited file should not keep the application from
// starting, it should be made sane.
func (s TranslationSettings) Normalize() TranslationSettings {
	if strings.TrimSpace(s.Provider) == "" {
		s.Provider = DefaultTranslationProvider
	}
	if strings.TrimSpace(s.Accelerator) == "" {
		s.Accelerator = DefaultTranslationAccelerator
	}
	switch {
	case s.CloseDelay < TranslationMinCloseDelay:
		s.CloseDelay = DefaultTranslationCloseDelay
	case s.CloseDelay > TranslationMaxCloseDelay:
		s.CloseDelay = TranslationMaxCloseDelay
	}
	s.Custom = s.Custom.Normalize()
	return s
}

// Normalize cleans up the address and the name of a user's own service: an
// address that is not http or https is dropped rather than handed to the
// HTTP client, and a name is cut to what a line can show.
func (c CustomTranslatorSettings) Normalize() CustomTranslatorSettings {
	c.BaseURL = NormalizeBaseURL(c.BaseURL)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if runes := []rune(c.DisplayName); len(runes) > MaxTranslatorNameChars {
		c.DisplayName = strings.TrimSpace(string(runes[:MaxTranslatorNameChars]))
	}
	return c
}

// NormalizeBaseURL turns what a user typed into an address the application
// will ask, or into nothing.
//
// It accepts what people write — "api.example.com/v1", or a full URL — and
// refuses anything that is not plain HTTP: a credential is about to be sent
// there, and a scheme the transport does not speak is a mistake worth
// dropping rather than passing on. A protocol-relative address ("//host/v1")
// is dropped too: it is how a web page means "the same scheme as this page",
// which a desktop application has no answer for.
func NormalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "//") {
		return ""
	}
	lower := strings.ToLower(value)
	scheme := "https"
	if index := strings.Index(lower, "://"); index >= 0 {
		scheme, value = lower[:index], value[index+3:]
	} else if index := strings.IndexAny(lower, ":/?#"); index >= 0 {
		// Something before the first slash that is not the host is a
		// scheme, however it is punctuated: "javascript:alert(1)" names a
		// scheme and a payload, not a host and a path. A port
		// ("localhost:8080/v1") names the host itself, which is the one
		// colon that is allowed to stand alone.
		head := lower[:index]
		if lower[index] == ':' && !strings.ContainsAny(head, "./") && !isPort(head, lower[index+1:]) {
			return ""
		}
	}
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.TrimLeft(value, "/")
	if host == "" {
		return ""
	}
	return scheme + "://" + strings.TrimRight(host, "/")
}

// isPort reports whether what follows a colon is a port number rather than
// the payload of a scheme: "8080/v1" is, "alert(1)" is not.
func isPort(head, rest string) bool {
	digits := rest
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		digits = rest[:index]
	}
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	// A port is a number, and the host it belongs to is a host: no scheme
	// is spelled with digits before it.
	return head != ""
}

// CloseDelayOr is the wait before the window closes itself, inside its
// bounds whatever the stored value said.
func (s TranslationSettings) CloseDelayOr() time.Duration {
	switch {
	case s.CloseDelay < TranslationMinCloseDelay:
		return DefaultTranslationCloseDelay
	case s.CloseDelay > TranslationMaxCloseDelay:
		return TranslationMaxCloseDelay
	}
	return s.CloseDelay
}

// ProviderOr is the translator to use, the default when nothing usable was
// named.
func (s TranslationSettings) ProviderOr() string {
	if strings.TrimSpace(s.Provider) == "" {
		return DefaultTranslationProvider
	}
	return s.Provider
}

// AcceleratorOr is the shortcut that starts a translation, the default when
// nothing was stored.
func (s TranslationSettings) AcceleratorOr() string {
	if strings.TrimSpace(s.Accelerator) == "" {
		return DefaultTranslationAccelerator
	}
	return s.Accelerator
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
)

// The record range the user may choose, in entries: a short list that stays a
// glance, or a longer one for someone who keeps going back to it. It is a
// choice among three rather than a number, because a bound nobody can reason
// about is a bound nobody sets.
const (
	ClipboardMinHistorySize     = 10
	ClipboardDefaultHistorySize = 30
	ClipboardMaxHistorySize     = 50
)

// clipboardHistorySizes is the choice, in the order it is offered. It is an
// array so that nothing can change it, and so that answering "is this one of
// them" costs no allocation on a frame.
var clipboardHistorySizes = [...]int{
	ClipboardMinHistorySize,
	ClipboardDefaultHistorySize,
	ClipboardMaxHistorySize,
}

// ClipboardHistorySizes returns the record ranges the user may choose, from
// the shortest to the longest.
func ClipboardHistorySizes() []int {
	return append([]int(nil), clipboardHistorySizes[:]...)
}

// ValidClipboardHistorySize reports whether n is one of the record ranges.
func ValidClipboardHistorySize(n int) bool {
	for _, size := range clipboardHistorySizes {
		if size == n {
			return true
		}
	}
	return false
}

// Normalize fills in what a stored setting left out and brings the interval
// inside its bounds. It never fails: a hand-edited settings file should not
// keep the application from starting, it should just be made sane.
func (s ToolSettings) Normalize() ToolSettings {
	if s.ClipboardPollInterval < ClipboardMinPollInterval {
		s.ClipboardPollInterval = ClipboardDefaultPollInterval
	}
	if !ValidClipboardHistorySize(s.ClipboardHistorySize) {
		s.ClipboardHistorySize = ClipboardDefaultHistorySize
	}
	s.Translation = s.Translation.Normalize()
	return s
}

// Interval is the poll interval, inside its bounds.
func (s ToolSettings) Interval() time.Duration {
	if s.ClipboardPollInterval < ClipboardMinPollInterval {
		return ClipboardDefaultPollInterval
	}
	return s.ClipboardPollInterval
}

// HistorySize is how many copies the history keeps, one of the record ranges
// whatever the stored value said.
func (s ToolSettings) HistorySize() int {
	if !ValidClipboardHistorySize(s.ClipboardHistorySize) {
		return ClipboardDefaultHistorySize
	}
	return s.ClipboardHistorySize
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
