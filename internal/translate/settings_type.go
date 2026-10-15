// This file is the translation tool's own configuration: which translator to
// ask, how to ask, and how its window behaves.
//
// It lives with the feature rather than in internal/domain because the
// translation tool is the only thing that reads it. What a feature stores
// should look like what that feature is, and a type only one feature uses
// belongs to that feature — domain is for the entities and ports several
// features share.
//
// The file it is written to is still the application's one settings file, and
// the keys it is written under are still the ones docs/database.md §8
// describes. internal/settings owns that file and does not know this type.
package translate

import (
	"strings"
	"time"
)

// Settings is what the selection-translation tool was told to do.
//
// The default is on, with nothing else configured: pressing the shortcut
// should reach the tool, and the tool should then say where to put a key
// rather than be silent. A user who wants the shortcut to do nothing turns
// Enabled off, and the window it would have opened says so.
type Settings struct {
	// Enabled turns the tool on. Off means the shortcut opens a window that
	// says the tool is off, rather than a window that fails.
	Enabled bool
	// Accelerator is the global shortcut that starts a translation, in the
	// form the platform's own shortcut API takes, such as
	// "Ctrl+Shift+T". Changing it is a later task; it is stored now so that
	// the file is where the answer lives.
	Accelerator string
	// Provider names the translator to use, by the id it is registered under.
	// A name no translator answers to is replaced by the default.
	Provider string
	// Furigana asks the translator to annotate Japanese kanji with their kana
	// reading.
	Furigana bool
	// LoanwordGloss asks it to annotate katakana loanwords with the word they
	// came from.
	LoanwordGloss bool
	// Pinned is whether the window starts pinned: a pinned window stays where
	// it was put and does not close when the user clicks elsewhere.
	Pinned bool
	// CloseDelay is how long the window waits before closing itself once it
	// loses the keyboard. It is bounded by the constants below, and a stored
	// value outside them is brought back inside rather than kept.
	CloseDelay time.Duration
	// Custom is the address of the OpenAI-compatible service the "custom"
	// translator asks. It is not sensitive: a credential never goes in here.
	Custom CustomSettings
}

// CustomSettings is the user's own translation service: where it is, and what
// to call it.
type CustomSettings struct {
	// BaseURL is the service's address, such as "https://api.example.com/v1".
	// Empty means the translator is not configured, and the window says so.
	BaseURL string
	// DisplayName is what the user calls the service. Empty means the
	// interface uses its own word for it.
	DisplayName string
}

// Bounds on the translation window's own behavior.
const (
	// DefaultProvider is the translator a fresh installation uses. It is the
	// one whose credential the application already has a place for: the
	// panel's own key.
	DefaultProvider = "deepseek"
	// DefaultAccelerator is the shortcut that starts a translation: the one
	// the design's settings page shows, written the way the platform's own
	// parser reads it. "Ctrl" rather than "CmdOrCtrl" because the design
	// names Ctrl and this build's platforms use it; a Mac build writes Super.
	DefaultAccelerator = "Ctrl+Shift+T"
	// MinCloseDelay and MaxCloseDelay bound the wait before the window closes
	// itself. Zero is allowed and means "at once": a user who wants the
	// window gone when they look away can say so.
	MinCloseDelay     = 0
	MaxCloseDelay     = 5 * time.Second
	DefaultCloseDelay = 400 * time.Millisecond
	// MaxChars bounds what is sent to a translator. A selection is a sentence
	// or a paragraph; a whole document pasted into the clipboard is not what
	// this tool is for, and the window says what it left out.
	MaxChars = 4000
	// MaxNameChars bounds the user's own name for a service: it is drawn in a
	// line of a window, not stored as a document.
	MaxNameChars = 40
)

// Defaults is the configuration of a user who has never opened the settings:
// the tool on, the built-in translator, both Japanese annotations on, the
// window unpinned and closing soon after it loses the keyboard.
func Defaults() Settings {
	return Settings{
		Enabled:       true,
		Accelerator:   DefaultAccelerator,
		Provider:      DefaultProvider,
		Furigana:      true,
		LoanwordGloss: true,
		CloseDelay:    DefaultCloseDelay,
	}.Normalize()
}

// Normalize fills in what a stored setting left out and brings the values that
// are not free-form into their bounds. It never fails: a hand-edited file
// should not keep the application from starting, it should be made sane.
func (s Settings) Normalize() Settings {
	if strings.TrimSpace(s.Provider) == "" {
		s.Provider = DefaultProvider
	}
	if strings.TrimSpace(s.Accelerator) == "" {
		s.Accelerator = DefaultAccelerator
	}
	switch {
	case s.CloseDelay < MinCloseDelay:
		s.CloseDelay = DefaultCloseDelay
	case s.CloseDelay > MaxCloseDelay:
		s.CloseDelay = MaxCloseDelay
	}
	s.Custom = s.Custom.Normalize()
	return s
}

// Normalize cleans up the address and the name of a user's own service: an
// address that is not http or https is dropped rather than handed to the HTTP
// client, and a name is cut to what a line can show.
func (c CustomSettings) Normalize() CustomSettings {
	c.BaseURL = NormalizeBaseURL(c.BaseURL)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if runes := []rune(c.DisplayName); len(runes) > MaxNameChars {
		c.DisplayName = strings.TrimSpace(string(runes[:MaxNameChars]))
	}
	return c
}

// CloseDelayOr is the wait before the window closes itself, inside its bounds
// whatever the stored value said.
func (s Settings) CloseDelayOr() time.Duration {
	switch {
	case s.CloseDelay < MinCloseDelay:
		return DefaultCloseDelay
	case s.CloseDelay > MaxCloseDelay:
		return MaxCloseDelay
	}
	return s.CloseDelay
}

// ProviderOr is the translator to use, the default when nothing usable was
// named.
func (s Settings) ProviderOr() string {
	if strings.TrimSpace(s.Provider) == "" {
		return DefaultProvider
	}
	return s.Provider
}

// AcceleratorOr is the shortcut that starts a translation, the default when
// nothing was stored.
func (s Settings) AcceleratorOr() string {
	if strings.TrimSpace(s.Accelerator) == "" {
		return DefaultAccelerator
	}
	return s.Accelerator
}

// NormalizeBaseURL turns what a user typed into an address the application
// will ask, or into nothing.
//
// It accepts what people write — "api.example.com/v1", or a full URL — and
// refuses anything that is not plain HTTP: a credential is about to be sent
// there, and a scheme the transport does not speak is a mistake worth dropping
// rather than passing on. A protocol-relative address ("//host/v1") is dropped
// too: it is how a web page means "the same scheme as this page", which a
// desktop application has no answer for.
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
		// Something before the first slash that is not the host is a scheme,
		// however it is punctuated: "javascript:alert(1)" names a scheme and
		// a payload, not a host and a path. A port ("localhost:8080/v1")
		// names the host itself, which is the one colon that is allowed to
		// stand alone.
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

// isPort reports whether what follows a colon is a port number rather than the
// payload of a scheme: "8080/v1" is, "alert(1)" is not.
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
	// A port is a number, and the host it belongs to is a host: no scheme is
	// spelled with digits before it.
	return head != ""
}
