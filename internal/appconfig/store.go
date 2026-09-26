// Package appconfig persists the user's own choices -- which kubeconfig files
// they added, what they renamed each context to, the colour they gave it, and
// where they docked the detail panel. None of this can be derived from the
// kubeconfig files themselves, so it lives in its own file under the user's
// config directory -- ~/.config/k8sdockside, alongside the other Kubernetes
// tooling, rather than wherever the platform would hide application state.
package appconfig

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Store is the settings file plus the lock guarding it. Wails calls service
// methods from multiple goroutines, so every read and write goes through the
// mutex and hands back a copy.
type Store struct {
	mu   sync.Mutex
	path string
	data Settings
}

// Open loads the settings file, creating nothing on disk. A missing file is not
// an error -- it just means the user has not customised anything yet. A file
// that exists but cannot be parsed *is* an error, because silently replacing it
// with defaults would throw away the user's colours and aliases.
func Open() (*Store, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	legacy, err := legacyPath()
	if err != nil {
		return nil, err
	}
	// A failed migration is fatal rather than ignored: carrying on would open
	// an empty file at the new path and present the user with a store that has
	// lost every alias, colour and tab they had.
	if err := migrate(legacy, path); err != nil {
		return nil, fmt.Errorf("migrating settings from %s: %w", legacy, err)
	}
	return openAt(path)
}

// openAt is Open against a named file. It exists so the tests can run against a
// temporary directory without going near the real settings file, and so that
// the loading rules can be exercised without also exercising the migration.
func openAt(path string) (*Store, error) {
	s := &Store{path: path, data: normalise(Defaults())}

	raw, err := os.ReadFile(path) // #nosec G304 -- the app's own settings file
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	loaded := Defaults()
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	// What this file predates, which is what decides which sessions have to be
	// brought across: panes from TabOrder and Dock, the describe tab's pane
	// from the edge the panel used to dock to. Unmarshalling over Defaults
	// cannot answer either question -- the defaults have already filled the
	// fields in, so an absent key and an empty one look identical afterwards --
	// so both are put to the file itself, and the answer is written back as the
	// zero value the migrations look for.
	var probe struct {
		Panes  *Panes `json:"panes"`
		Layout *struct {
			DetailPane *string `json:"detailPane"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		if probe.Panes == nil {
			loaded.Panes = nil
		}
		if probe.Layout == nil || probe.Layout.DetailPane == nil {
			loaded.Layout.DetailPane = ""
		}
	}
	s.data = normalise(loaded)
	return s, nil
}

// Path is where the settings are stored, surfaced in the UI so the user can
// find (or delete) the file.
func (s *Store) Path() string { return s.path }

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.data)
}

// ManualFiles returns the kubeconfig paths the user added by hand.
func (s *Store) ManualFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ManualFiles)
}

// ManualFolders returns the directories the user asked us to scan.
func (s *Store) ManualFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ManualFolders)
}

// update applies a change under the lock and flushes to disk. If the write
// fails the in-memory state is rolled back, so what the UI shows after an error
// still matches what is on disk.
func (s *Store) update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := clone(s.data)
	fn(&s.data)
	s.data = normalise(s.data)

	if err := s.flush(); err != nil {
		s.data = before
		return clone(s.data), err
	}
	return clone(s.data), nil
}

// SetLayout records the sidebar width and detail-panel dock and size.
func (s *Store) SetLayout(l Layout) (Settings, error) {
	return s.update(func(d *Settings) { d.Layout = l })
}

// SetWindow records where the main window is and how big.
func (s *Store) SetWindow(w Window) (Settings, error) {
	return s.update(func(d *Settings) { d.Window = w })
}

// SetPreferences records the app-wide preferences. It replaces the block
// wholesale rather than patching one field, matching SetLayout: the settings
// view edits them together and hands back the whole thing, so a partial
// mutator would only add a way for the two to disagree.
func (s *Store) SetPreferences(p Preferences) (Settings, error) {
	return s.update(func(d *Settings) {
		// Copied on the way in for the same reason clone copies it on the way
		// out: the caller's pointer must not become the store's.
		if p.RestoreTabs != nil {
			restore := *p.RestoreTabs
			p.RestoreTabs = &restore
		}
		if p.ShowLineNumbers != nil {
			numbers := *p.ShowLineNumbers
			p.ShowLineNumbers = &numbers
		}
		if p.CheckForUpdates != nil {
			check := *p.CheckForUpdates
			p.CheckForUpdates = &check
		}
		if p.DesktopNotifications != nil {
			notify := *p.DesktopNotifications
			p.DesktopNotifications = &notify
		}
		p.DateTime = normaliseDateTime(p.DateTime)
		p = normaliseAlerts(p)
		d.Preferences = p
	})
}

// CheckForUpdates is whether the app should look for new releases on its own,
// with "never chosen" resolved to yes. It is read here, on the service's side,
// rather than passed from the window, so that the loop that asks and the
// toggle that governs it can never disagree about what was chosen.
func (s *Store) CheckForUpdates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Preferences.CheckForUpdates == nil || *s.data.Preferences.CheckForUpdates
}

// MarkUpdateRead records that the user has seen the notice about one release.
func (s *Store) MarkUpdateRead(version string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.Updates.ReadVersion = version
	})
}

// SetPanes records where every open view sits: which pane holds it, in what
// order, and how much room each pane takes.
//
// Every pane is written together rather than one at a time, for the reason the
// dock it replaces was written whole. One gesture moves a tab out of a pane and
// into another, filling and opening the second; two writers over that would
// each answer with the whole settings, and the slower would carry the other's
// half of the move back.
func (s *Store) SetPanes(panes Panes) (Settings, error) {
	return s.update(func(d *Settings) {
		// Cloned on the way in for the same reason clone copies it on the way
		// out: the caller's slices must not become the store's.
		panes.Left.Tabs = slices.Clone(panes.Left.Tabs)
		panes.Main.Tabs = slices.Clone(panes.Main.Tabs)
		panes.Right.Tabs = slices.Clone(panes.Right.Tabs)
		panes.Bottom.Tabs = slices.Clone(panes.Bottom.Tabs)
		d.Panes = &panes
	})
}

// settingsFormat pins the file format to what encoding/json v1 wrote, so moving
// to v2 does not rewrite every existing settings file. Two of these are load
// bearing rather than cosmetic:
//
//   - Deterministic keeps the contexts map in a stable key order. v2 otherwise
//     follows Go's randomised map iteration, which would reshuffle the file on
//     every save and make it useless to diff or eyeball.
//   - FormatNilSliceAsNull keeps nil distinct from empty, which Layout and
//     ContextPrefs both depend on -- see CollapsedGroups. v2 would write [] for
//     a nil slice, turning "never chosen" into the explicit choice "show me
//     everything" the first time a fresh install saved anything.
//
// EscapeForHTML and FormatNilMapAsNull only keep the bytes identical to what
// earlier releases produced; nothing reads the file that would care either way.
var settingsFormat = json.JoinOptions(
	jsontext.WithIndent("  "),
	jsontext.EscapeForHTML(true),
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)

// flush writes the settings via a temp file and a rename, so an interrupted
// write cannot leave a half-written config behind. The caller holds the lock.
func (s *Store) flush() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	raw, err := json.Marshal(s.data, settingsFormat)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// no-op once the rename below has succeeded
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// normalise fills in anything a hand-edited or older settings file left out, so
// the rest of the app never has to check for zero values.
// PortForwards returns the forwards the user has set up.
func (s *Store) PortForwards() []PortForward {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.PortForwards)
}

// SetPortForwards replaces the remembered list.
//
// The whole list rather than one entry: the service that owns them holds the
// live state and writes what it has, and an add-one/remove-one pair would give
// two writers to a list with one owner.
func (s *Store) SetPortForwards(forwards []PortForward) (Settings, error) {
	return s.update(func(d *Settings) {
		d.PortForwards = slices.Clone(forwards)
	})
}

// BackgroundFolder is the directory the start page's own images are read
// from, or empty when there is none.
func (s *Store) BackgroundFolder() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.BackgroundFolder
}

// SetBackgroundFolder records the directory to read start page images from.
// Empty clears it.
func (s *Store) SetBackgroundFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.BackgroundFolder = path
	})
}
