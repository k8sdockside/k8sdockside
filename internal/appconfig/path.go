package appconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// settingsFile is the name the settings file has under whichever directory we
// settle on.
const settingsFile = "settings.json"

// defaultPath is where the settings live.
//
// It deliberately does not use os.UserConfigDir, which on macOS hardcodes
// $HOME/Library/Application Support and never consults XDG_CONFIG_HOME.
// k8sdockside sits alongside kubectl, helm and k9s, whose config all lives
// under ~/.config, and being the one tool in that set that hides its file in
// the Library is not worth the Apple convention -- the file is a dotfile the
// user is expected to manage, not opaque application state.
//
// Windows keeps %AppData%: there is no ~/.config convention there to join.
func defaultPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "k8sdockside", settingsFile), nil
}

// ThemesDir is where a user's own theme files live: a folder beside the
// settings file, so that everything this app reads or writes for a user is in
// one place they can back up, sync or delete as a unit.
//
// It is a function on the store rather than a constant because the settings
// path is resolved at runtime, and the two must not be able to disagree about
// which directory that is.
func (s *Store) ThemesDir() string {
	return filepath.Join(filepath.Dir(s.path), "themes")
}

// PluginsDir is where a user's own solution plugins live, beside the settings
// file and the themes folder for the same reason: everything this app reads or
// writes for a user is in one place they can back up, sync or delete as a unit.
func (s *Store) PluginsDir() string {
	return filepath.Join(filepath.Dir(s.path), "plugins")
}

// legacyPath is where releases before the move wrote the file: whatever
// os.UserConfigDir picks for the platform. On Windows and on a Linux box with
// no XDG_CONFIG_HOME set it is the same path defaultPath returns, which is why
// the migration has to tolerate the two being equal.
func legacyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config directory: %w", err)
	}
	return filepath.Join(dir, "k8sdockside", settingsFile), nil
}

// configDir resolves the base directory, following the XDG base directory
// spec on every platform but Windows.
func configDir() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		// A relative XDG_CONFIG_HOME would resolve against the working
		// directory, which for a launched .app bundle is "/". Refusing is
		// better than scattering a settings file somewhere unfindable, and it
		// is what os.UserConfigDir does with the same input.
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("XDG_CONFIG_HOME must be an absolute path, got %q", dir)
		}
		return dir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

// migrate moves a settings file left at the legacy location to path, so that
// the move to ~/.config does not read as "all my aliases and colours are
// gone". It goes through the store rather than copying bytes: that reuses the
// atomic temp-file write and the 0600 mode from flush, normalises anything an
// older version left out of the file, and refuses to move a file it cannot
// parse instead of carrying a broken one across.
//
// It does nothing when there is nothing to move, when the two paths are the
// same (Windows, and Linux with no XDG_CONFIG_HOME), or when a file already
// exists at path: a settings file the user is already using is never
// overwritten, and a legacy file that was not moved is left where it is
// rather than deleted.
func migrate(legacy, path string) error {
	if legacy == path {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if _, err := os.Stat(legacy); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("checking %s: %w", legacy, err)
	}

	store, err := openAt(legacy)
	if err != nil {
		return err
	}
	// Nothing else can reach this store yet, so flushing it without taking the
	// lock is safe -- it is not handed to the caller, only used to rewrite the
	// file at its new home.
	store.path = path
	if err := store.flush(); err != nil {
		return err
	}

	if err := os.Remove(legacy); err != nil {
		return fmt.Errorf("removing %s: %w", legacy, err)
	}
	// Best effort: os.Remove only succeeds on an empty directory, so this
	// tidies away the abandoned one and leaves anything else alone.
	_ = os.Remove(filepath.Dir(legacy))
	return nil
}

// KnownListPath is where the last list of known plugins fetched from the
// repository is kept, beside the settings file, so a launch without a network
// still offers what the last one with one found.
func (s *Store) KnownListPath() string {
	return filepath.Join(filepath.Dir(s.path), "known-plugins.json")
}
