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
// the keys it is written under are still the ones §8 of the storage design
// describes. internal/settings owns that file and does not know this type.
package translate

import (
	"sort"
	"strconv"
	"strings"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/translate/openai"
)

// customServicePrefix is the id every one of the user's own services is stored
// under, numbered after the first: "custom", "custom-2", "custom-3" …
//
// It is spelled here, and in section.go as customID, because the two are the
// same decision seen from two sides: what the settings file calls a service,
// and what the tool calls the translator that asks it.
const customServicePrefix = "custom"

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
	// form the framework's shortcut API takes, such as
	// "CmdOrCtrl+Shift+T". Changing it is a later task; it is stored now so
	// that the file is where the answer lives.
	Accelerator string
	// ScreenshotAccelerator starts native region capture. Empty disables it.
	ScreenshotAccelerator string
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
	// Custom is the OpenAI-compatible services the user runs themselves, keyed
	// by the id each one is stored under ("custom", "custom-2", …).
	//
	// It is a map rather than one service because the settings window offers
	// adding another: a user may have a model on their own machine and a
	// gateway somewhere else, and both are theirs to keep. An entry's id is
	// what the provider setting names, so a file that remembers "custom-2"
	// still points at the service it did.
	//
	// It is not sensitive: a credential never goes in here. The addresses and
	// the names are in the clear, in the settings file, which is where §8 of
	// the storage design says they live.
	Custom map[string]CustomSettings
}

// CustomSettings is one of the user's own translation services: where it is,
// and what to call it.
type CustomSettings struct {
	// BaseURL is the service's address, such as "https://api.example.com/v1".
	// Empty means the service is not configured, and the window says so.
	BaseURL string
	// DisplayName is what the user calls the service. Empty means the
	// interface uses its own word for it.
	DisplayName string
	// Model is the OpenAI-compatible model name selected by the user.
	Model string
	// SupportsImage is set only after Boku successfully completes a real
	// multimodal request against this service. Missing means not yet verified.
	SupportsImage bool
}

// CustomServiceIDs returns the ids of the user's own services, sorted, which is
// the order the settings window draws them in.
func (s Settings) CustomServiceIDs() []string {
	ids := make([]string, 0, len(s.Custom))
	for id := range s.Custom {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// CustomService returns one of the user's own services, by id.
func (s Settings) CustomService(id string) (CustomSettings, bool) {
	service, ok := s.Custom[id]
	return service, ok
}

// NextCustomServiceID is the id a service being added is stored under: the
// first of "custom", "custom-2", "custom-3" … that is not taken.
//
// The bare "custom" is kept as the first id because a file written before there
// could be several services holds that name, and reading it as the first of
// them is what makes this change need no migration.
func (s Settings) NextCustomServiceID() string {
	if _, taken := s.Custom[customServicePrefix]; !taken {
		return customServicePrefix
	}
	for n := 2; ; n++ {
		id := customServicePrefix + "-" + strconv.Itoa(n)
		if _, taken := s.Custom[id]; !taken {
			return id
		}
	}
}

// IsCustomService reports whether an id names one of the user's own services
// rather than a built-in translator.
//
// The test is the prefix, not a fixed id: the ids are generated so that several
// services fit side by side, and every one of them is the same kind of thing to
// the window and to the backend that asks it.
func IsCustomService(id string) bool {
	return id == customServicePrefix || strings.HasPrefix(id, customServicePrefix+"-")
}

// Bounds on the translation window's own behavior.
const (
	// DefaultProvider is the translator a fresh installation uses: the
	// Microsoft Edge engine, which needs no key, no account and no
	// configuration.
	//
	// It is one of the free engines rather than a tier that picks between
	// them, because the settings page offers the engines as separate choices.
	// A user who configured a service of their own picks it, and the choice is
	// theirs from then on — a stored provider is never rewritten, which is
	// what keeps "I configured a key, why did the answer change" from being a
	// question nobody can answer (§4.5 of the free-translation design).
	DefaultProvider = contract.MicrosoftEdge
	// DefaultAccelerator is the shortcut that starts a translation: the one
	// the design's settings page shows, written the way the framework's
	// parser reads it. "CmdOrCtrl" is resolved by the platform parser:
	// Command on macOS, Ctrl on Windows and Linux. Writing "Ctrl" here would
	// bind Control+Shift+T on macOS, which is not the combination a Mac user
	// means.
	DefaultAccelerator           = "CmdOrCtrl+Shift+T"
	DefaultScreenshotAccelerator = "CmdOrCtrl+Alt+T"
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
		Enabled:               true,
		Accelerator:           DefaultAccelerator,
		ScreenshotAccelerator: DefaultScreenshotAccelerator,
		Provider:              DefaultProvider,
		Furigana:              true,
		LoanwordGloss:         true,
		CloseDelay:            DefaultCloseDelay,
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
	s.Custom = normalizeCustomServices(s.Custom)
	// A provider that names one of the user's own services has to still name
	// one. Removing a service leaves the setting pointing at an id that is no
	// longer there, and a tool whose provider does not exist cannot translate:
	// it falls back to the default rather than failing every press.
	//
	// Only the user's own services are checked here. Whether a built-in id is
	// one this build knows is the registry's question, and the registry is not
	// reachable from a value type — there the lookup fails and the window says
	// so, which is the right answer for a hand-edited file.
	if IsCustomService(s.Provider) {
		if _, exists := s.Custom[s.Provider]; !exists {
			s.Provider = DefaultProvider
		}
	}
	return s
}

// Normalize cleans up the address and the name of a user's own service: an
// address that is not http or https is dropped rather than handed to the HTTP
// client, and a name is cut to what a line can show.
func (c CustomSettings) Normalize() CustomSettings {
	c.BaseURL = NormalizeBaseURL(c.BaseURL)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	c.Model = strings.TrimSpace(c.Model)
	if runes := []rune(c.DisplayName); len(runes) > MaxNameChars {
		c.DisplayName = strings.TrimSpace(string(runes[:MaxNameChars]))
	}
	return c
}

// normalizeCustomServices cleans up every one of the user's own services.
//
// A service whose address does not survive the cleaning is dropped: an entry
// with nowhere to send anything is not a service, and keeping it would leave a
// row in the window that can never answer. A nil map becomes an empty one, so
// that a caller may add to it without checking.
func normalizeCustomServices(services map[string]CustomSettings) map[string]CustomSettings {
	cleaned := make(map[string]CustomSettings, len(services))
	for id, service := range services {
		if strings.TrimSpace(id) == "" {
			continue
		}
		normalized := service.Normalize()
		if normalized.BaseURL == "" {
			continue
		}
		cleaned[id] = normalized
	}
	return cleaned
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

// ScreenshotAcceleratorOr returns the capture shortcut. Empty is meaningful:
// the user explicitly cleared it.
func (s Settings) ScreenshotAcceleratorOr() string { return strings.TrimSpace(s.ScreenshotAccelerator) }

// NormalizeBaseURL turns what a user typed into an address the application
// will ask, or into nothing.
//
// It is the protocol package's own cleaning, re-exported rather than written
// again: the backend asks the address the settings stored, and two cleanings of
// the same string is two chances for them to differ — a stored "127.0.0.1:11434/v1"
// that the backend read as https would be a request to a port that speaks no
// TLS. The rule for what an address is lives with the protocol that speaks it;
// this feature only decides that it keeps one.
func NormalizeBaseURL(raw string) string { return openai.NormalizeBaseURL(raw) }
