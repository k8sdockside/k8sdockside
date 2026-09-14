package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A plugin installed into the plugins folder is uninstalled by deleting what
// it came in: the folder it was cloned or copied into, or -- for a file dropped
// straight into the plugins folder -- that file. Nothing else is ever deleted.
//
// A built-in has nothing on disk to delete, and a plugin read from a folder the
// user added is theirs -- typically the repository they are writing it in. That
// folder is only watched, so the app stops reading it rather than delete
// anything in it.

// Removal is what uninstalling a plugin deletes: one path in the plugins
// folder, and every plugin read from it. The rest of a pack goes with it, and
// so does every plugin file in the same folder.
type Removal struct {
	Path    string   `json:"path"`
	Plugins []string `json:"plugins"`
}

// Removable says what uninstalling p would delete, without deleting anything,
// so the question the user is asked can name exactly that.
func Removable(cat Catalogue, dir string, p Plugin) (Removal, error) {
	if p.Builtin() {
		return Removal{}, fmt.Errorf("%s is built into the app, so there is nothing to uninstall -- switch it off instead", uninstallName(p))
	}
	path, err := removalPath(dir, p.Origin)
	if err != nil {
		return Removal{}, fmt.Errorf("%s %w", uninstallName(p), err)
	}
	removal := Removal{Path: path, Plugins: []string{}}
	for _, other := range cat.Plugins {
		if !other.Builtin() && underPath(path, other.Origin) {
			removal.Plugins = append(removal.Plugins, other.ID)
		}
	}
	return removal, nil
}

// Uninstall deletes what Removable names, and returns it.
func Uninstall(cat Catalogue, dir string, p Plugin) (Removal, error) {
	removal, err := Removable(cat, dir, p)
	if err != nil {
		return removal, err
	}
	// RemoveAll unlinks a symlink rather than following it, so a folder linked
	// into the plugins folder by hand loses the link and keeps what it points at.
	if err := os.RemoveAll(removal.Path); err != nil {
		return removal, fmt.Errorf("could not delete %s: %w", removal.Path, err)
	}
	return removal, nil
}

// removalPath is the file or folder in dir that origin was read from: the file
// itself when it sits in dir, and otherwise the subfolder of dir it is in. The
// loader reads one level deep, so that subfolder is the plugin's own.
func removalPath(dir, origin string) (string, error) {
	if dir == "" || origin == "" {
		return "", errors.New("was not read from the plugins folder")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	file, err := filepath.Abs(origin)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("was read from %s, a folder the app only watches -- stop watching it in Settings, or delete the plugin there yourself", filepath.Dir(origin))
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return filepath.Join(root, first), nil
}

// underPath reports whether path is target or inside it.
func underPath(target, path string) bool {
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(target, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// InPluginsDir reports whether p was read from the plugins folder -- installed
// there by cloning or copying -- rather than being built in or read from a
// folder the user watches. Only such a plugin is the app's to update or
// uninstall: a watched folder is the user's own checkout.
func InPluginsDir(dir string, p Plugin) bool {
	if p.Builtin() {
		return false
	}
	_, err := removalPath(dir, p.Origin)
	return err == nil
}

// InstalledHere is the plugin with this id that is built in or installed in
// the plugins folder -- what installing it again would clash with. A copy read
// only from a watched folder does not count: installing puts one in the plugins
// folder, which then takes the id from it.
func (c Catalogue) InstalledHere(id string) (Plugin, bool) {
	for _, p := range c.Plugins {
		if p.ID == id && (p.Builtin() || InPluginsDir(c.Dir, p)) {
			return p, true
		}
	}
	return Plugin{}, false
}

func uninstallName(p Plugin) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}
