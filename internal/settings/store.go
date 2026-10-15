// Package settings stores the application's own configuration as one file,
// under the user data directory:
//
//	settings.json    what the user chose about Boku
//
// It knows nothing about what is in it. Each feature that has settings
// registers a Section: a name, and how that feature's own configuration is
// written down. This package reads the file, hands each section its own part
// of it, and writes back only the parts that changed.
//
// That ignorance is the point. A settings store that knew the fields would
// have to be edited whenever any feature gained one, and every feature would
// be able to reach every other feature's settings through it. Here a feature
// can only ever be handed its own section — the type it decodes into lives in
// that feature's package, and no other package has a reason to name it.
//
// The file is written through internal/platform/atomicfile, so a crash
// mid-write leaves the previous file rather than half of a new one. A file
// that cannot be read is an error: the window would otherwise show defaults
// the user never chose and then write them back.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"boku/internal/kit"
	"boku/internal/platform/atomicfile"
)

// FileVersion is the shape this build reads and writes. A file from another
// version is refused rather than partly understood.
const FileVersion = 1

// Section is one feature's configuration, as that feature knows it.
//
// The store never looks inside. Decode is handed the file's tool settings and
// takes the ones it owns; Encode hands back the ones it owns. The two are the
// only place the storage format and a feature's own type meet, which is why a
// feature can gain a setting without this package being touched.
//
// Decode is given every feature's keys rather than only its own — the file is
// one flat object, and that is what keeps a user's existing settings readable
// without a migration. Taking only its own keys is therefore each section's
// own discipline, and Encode's job is the other half of it: the map it returns
// is merged into the file, so a section that returned another feature's key
// would be writing settings it does not own.
type Section interface {
	// Name is the section's name, for errors. It is not a key in the file:
	// the settings of every feature share one flat object there, which is
	// the shape docs/database.md §8 documents and a person reads.
	Name() string
	// Decode reads this feature's settings out of values, which holds the
	// keys of every feature. A key that is absent leaves the setting as it
	// is, so a file written before a key existed gives that key's default
	// rather than a zero.
	Decode(values map[string]json.RawMessage) error
	// Encode returns this feature's settings as the keys it owns.
	Encode() (map[string]any, error)
}

// Store is settings.json. It is safe to use from several goroutines.
type Store struct {
	Path string

	// mu guards the file, the sections and every read-modify-write: a change
	// made while another one is in flight must not be lost, and the only way
	// to promise that is to hold one lock across the read and the write.
	mu       sync.Mutex
	sections []Section
	byName   map[string]Section
}

// document is what settings.json holds: the version, and the flat object the
// features' settings share.
//
// The settings of every feature sit in one "tools" object rather than in one
// entry each. That is the shape every file already in the wild has, and the
// shape docs/database.md §8 describes to a user; keeping it is what lets this
// package stop knowing the fields without any file needing a migration.
type document struct {
	Version int                        `json:"version"`
	Tools   map[string]json.RawMessage `json:"tools"`
}

// New returns the store over a path, with the sections it should read and
// write. A missing file is not an error: every section keeps its own defaults
// and writes on the first change.
func New(path string, sections ...Section) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("settings: the store has no path")
	}
	store := &Store{Path: path, byName: make(map[string]Section, len(sections))}
	for _, section := range sections {
		if section == nil {
			return nil, fmt.Errorf("settings: a section is nil")
		}
		name := section.Name()
		if name == "" {
			return nil, fmt.Errorf("settings: a section has no name")
		}
		if _, twice := store.byName[name]; twice {
			return nil, fmt.Errorf("settings: section %s is registered twice", name)
		}
		store.byName[name] = section
		store.sections = append(store.sections, section)
	}
	return store, nil
}

// Load reads the file into every registered section.
//
// A key the file does not hold is left as it is: the feature's default, which
// is what a user who has never saved anything should get. That is also what
// makes a new setting work without a migration — a file written before the key
// existed simply has no entry for it.
func (s *Store) Load(ctx context.Context) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Store) read() error {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settings: read %s: %w", s.Path, err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("settings: %s is not a readable settings file: %w", s.Path, err)
	}
	if doc.Version != FileVersion {
		return fmt.Errorf("settings: %s is version %d, this build reads version %d",
			s.Path, doc.Version, FileVersion)
	}
	for _, section := range s.sections {
		if err := section.Decode(doc.Tools); err != nil {
			return fmt.Errorf("settings: %s: %s: %w", s.Path, section.Name(), err)
		}
	}
	return nil
}

// Update reads the file, hands one section to change, and writes the whole
// document back.
//
// This is the only way a setting is changed. A caller that reads with Load,
// changes what it read and writes with Save is doing the same thing in three
// steps, and two such callers overwrite each other: both read the file as it
// was, both write back what they read plus their own change, and the second
// write undoes the first. Keeping the three steps in one critical section is
// what makes that impossible.
//
// The section named is the one whose change is run. The others are re-read and
// written back from the store's own copy of the file rather than from anything
// the caller holds, so a change to one feature's settings cannot put back
// another feature's stale values.
func (s *Store) Update(ctx context.Context, name string, change func() error) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	if _, known := s.byName[name]; !known {
		return fmt.Errorf("settings: no section is named %s", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.read(); err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	return s.write()
}

// Save writes every section as it stands.
func (s *Store) Save(ctx context.Context) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write()
}

func (s *Store) write() error {
	tools := make(map[string]json.RawMessage)
	for _, section := range s.sections {
		values, err := section.Encode()
		if err != nil {
			return fmt.Errorf("settings: encode %s: %w", section.Name(), err)
		}
		for key, value := range values {
			raw, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("settings: encode %s: %s: %w", section.Name(), key, err)
			}
			// Two features writing one key is a mistake in the assembly,
			// not something to resolve silently: whichever won would be
			// decided by the order the sections were registered in.
			if _, taken := tools[key]; taken {
				return fmt.Errorf("settings: %s and another section both write %q", section.Name(), key)
			}
			tools[key] = raw
		}
	}
	doc := document{Version: FileVersion, Tools: tools}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode %s: %w", s.Path, err)
	}
	if err := atomicfile.Write(s.Path, append(data, '\n')); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	return nil
}

// Into decodes one key of a section's settings into out.
//
// It is the answer to "the file does not have this key": out is left exactly
// as it was, which is how a feature keeps the default it started with. Passing
// the zero value would read as a choice the user made — every switch off,
// every interval zero — and a file written before a key existed would turn
// that feature off for everybody.
//
// It is here, next to the file format, so that every section answers that
// question the same way instead of each deciding for itself.
func Into(values map[string]json.RawMessage, key string, out any) error {
	raw, present := values[key]
	if !present {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}
