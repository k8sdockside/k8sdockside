package appconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A profile is a second, separate set of everything the app keeps for a user:
// its own settings file, and beside it its own installed plugins, themes and
// known-plugin list -- all of which already live next to the settings file, so
// a profile is simply another directory for that file to be in.
//
// What it is for is running two copies side by side without either touching
// the other's state: the installed app on the user's everyday settings, and a
// development build on a "dev" profile that loads plugins from working folders.
//
// Profiles live under the default directory, ~/.config/k8sdockside/profiles/
// <name>/, so the app's things stay in one place to back up or delete.

// ProfileEnv names the profile when no --profile flag does.
const ProfileEnv = "K8SDOCKSIDE_PROFILE"

// profileName is what a profile may be called: it becomes a directory name,
// so nothing that could climb out of the profiles folder or mean something to
// a shell.
var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ProfileFrom picks the profile out of the command line and the environment:
// --profile <name> or --profile=<name> first, then K8SDOCKSIDE_PROFILE. Empty
// means the default settings. Arguments it does not know are left alone --
// macOS passes its own to an app it launches.
func ProfileFrom(args []string, getenv func(string) string) string {
	for i, arg := range args {
		switch {
		case arg == "--profile" || arg == "-profile":
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
		case strings.HasPrefix(arg, "--profile="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "--profile="))
		case strings.HasPrefix(arg, "-profile="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "-profile="))
		}
	}
	return strings.TrimSpace(getenv(ProfileEnv))
}

// OpenProfile is Open for a named profile; an empty name is the default
// settings, exactly as Open.
//
// A profile used for the first time starts from a copy of the default
// settings file -- the same kubeconfig files, aliases and colours -- so a
// development build opens on the user's clusters rather than on an empty
// window. From then on the two files are separate; the default one is only
// ever read, here, and never written.
func OpenProfile(name string) (*Store, error) {
	if name == "" {
		return Open()
	}
	if err := ValidProfile(name); err != nil {
		return nil, err
	}
	base, err := defaultPath()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(base), "profiles", name, settingsFile)
	if err := seed(base, path); err != nil {
		return nil, fmt.Errorf("starting profile %q from %s: %w", name, base, err)
	}
	store, err := openAt(path)
	if err != nil {
		return nil, err
	}
	store.profile = name
	return store, nil
}

// seed copies the default settings to a profile that has none yet. Through the
// store rather than as bytes, as migrate does: the copy is normalised and
// written with the store's own atomic write and 0600 mode, and a default file
// that cannot be parsed is refused rather than carried across.
func seed(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	store, err := openAt(from)
	if err != nil {
		return err
	}
	// Not handed to anyone: it exists only to be written at its new home.
	store.path = to
	return store.flush()
}

// Profile is the profile this store is, empty for the default settings.
func (s *Store) Profile() string { return s.profile }

// ValidProfile reports why a name cannot be a profile, or nil if it can.
func ValidProfile(name string) error {
	if !profileName.MatchString(name) {
		return fmt.Errorf("profile %q: use lower-case letters, digits, '-' and '_', at most 32, starting with a letter or digit", name)
	}
	return nil
}

// Profiles lists the profiles that exist: the folders under profiles/ that a
// profile has been run in, sorted. The default settings are not among them.
func Profiles() ([]string, error) {
	base, err := defaultPath()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(base), "profiles"))
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() && profileName.MatchString(entry.Name()) {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}
