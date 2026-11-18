// Package addons is the machinery the app's two extension points share: how a
// user's own files are found, how far into their folders we are willing to
// look, what happens when one of them will not parse, and how what loaded is
// merged over what ships with the app.
//
// Themes and solution plugins are different things with different schemas, but
// they are installed the same way and have to behave the same way when
// something is wrong -- one bad file never costs the user the files either side
// of it, and a file that fails is always named with a reason rather than
// silently missing. That behaviour is here, once, rather than in two loaders
// that would drift the first time either was fixed.
//
// Nothing in this package knows what a theme or a plugin is. It finds files and
// hands their bytes to a callback.
package addons

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Problem is a file that could not be used, and why. Problems are carried
// alongside what loaded rather than returned as an error: the answer to "this
// file is broken" is still "here is everything else", and a reason on screen is
// worth much more than a file that quietly does not appear.
type Problem struct {
	// Path of the offending file. Also used for an item inside a file that
	// parsed, where the file loaded and one of the things in it did not.
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Identified is anything loaded from an add-on file. The identity is what makes
// two of them the same thing: it is what a settings file stores, so it is also
// what lets a user's file deliberately replace one of ours.
type Identified interface {
	Identity() string
}

// Parse turns one file's bytes into the items it holds.
//
// The three results are deliberately distinct. `items` is what loaded; `refused`
// names things inside a file that did not, while others in the same file did --
// a pack of five themes with one typo should offer the four; and `err` is the
// file itself being unusable.
type Parse[T Identified] func(path string, raw []byte) (items []T, refused []string, err error)

// MaxFile is a sanity bound on an add-on file. A palette or a plugin manifest
// is a few hundred bytes to a few kilobytes; anything approaching this is not
// one, and reading it into memory to discover that is the thing worth avoiding.
const MaxFile = 1 << 20 // 1 MiB

// Load reads every add-on in the given folders and merges it over the built-in
// ones, in that order.
//
// Nothing here fails. A folder that has gone, a file that is not JSON, an item
// claiming an identity another already has: all of them are problems against
// the whole, because the useful answer to any of them is still the list of
// things that did load.
//
// An item whose identity matches a built-in one *replaces* it, in place. That
// is how someone retunes a theme or extends a plugin the app already ships
// without renaming it everywhere it is stored. Two of the user's own files
// claiming one identity is a mistake rather than a feature, so the first found
// wins and the second is named.
func Load[T Identified](builtin []T, folders []string, parse Parse[T]) ([]T, []Problem) {
	items := append([]T{}, builtin...)
	var problems []Problem

	// Where each identity came from, so a clash can say what it clashed with.
	// Built-ins start out claimed but yielding: they are meant to be replaceable.
	const fromBuiltin = ""
	origin := make(map[string]string, len(items))
	for _, item := range items {
		origin[item.Identity()] = fromBuiltin
	}

	for _, folder := range folders {
		if strings.TrimSpace(folder) == "" {
			continue
		}
		found, folderProblems := scan(folder, parse)
		problems = append(problems, folderProblems...)

		for _, item := range found {
			id := item.Identity()
			if where, taken := origin[id]; taken && where != fromBuiltin {
				problems = append(problems, Problem{
					Path:    Origin(item),
					Message: fmt.Sprintf("%q is already used by %s, so this one was skipped", id, where),
				})
				continue
			}

			replaced := false
			for i := range items {
				if items[i].Identity() == id {
					items[i] = item
					replaced = true
					break
				}
			}
			if !replaced {
				items = append(items, item)
			}
			origin[id] = Origin(item)
		}
	}
	return items, problems
}

// Sourced is an item that knows which file it came from. Implementing it is
// optional: it only improves the wording of a clash, which is why Load reaches
// for it through Origin rather than requiring it in Identified.
type Sourced interface {
	Source() string
}

// Origin is where an item came from, or its identity when it cannot say.
func Origin(item Identified) string {
	if s, ok := item.(Sourced); ok && s.Source() != "" {
		return s.Source()
	}
	return item.Identity()
}

// scan reads one folder's add-ons.
//
// It looks at the folder itself and one level into each subdirectory, which
// covers both ways an add-on arrives: a `.json` dropped straight in, and a pack
// cloned or unzipped into a folder of its own. It deliberately goes no deeper --
// this is not a source tree, and walking one would mean reading whatever else
// the user happens to keep in there.
func scan[T Identified](dir string, parse Parse[T]) ([]T, []Problem) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// A folder that was never created is the normal state of a fresh
		// install, and is not worth telling anyone about.
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []Problem{{Path: dir, Message: err.Error()}}
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		if entry.IsDir() {
			files = append(files, jsonFilesIn(path)...)
			continue
		}
		if isJSON(name) {
			files = append(files, path)
		}
	}
	// os.ReadDir already sorts, but subdirectory contents are appended in
	// blocks; sorting the whole makes the result independent of how the files
	// happen to be arranged on disk.
	sort.Strings(files)

	var items []T
	var problems []Problem
	for _, path := range files {
		found, refused, err := readFile(path, parse)
		if err != nil {
			problems = append(problems, Problem{Path: path, Message: err.Error()})
			continue
		}
		// Something refused from inside a file that otherwise loaded: the rest
		// is on offer, and this is still named rather than absent.
		for _, reason := range refused {
			problems = append(problems, Problem{Path: path, Message: reason})
		}
		items = append(items, found...)
	}
	return items, problems
}

// FilesIn lists the add-on files directly inside dir: what is read from a
// folder cloned or copied into an add-on folder, and so what a check of such a
// folder on its own should read.
func FilesIn(dir string) []string { return jsonFilesIn(dir) }

// IsFile reports whether a file of this name, in a folder that is read, is
// read as an add-on: a JSON file that is not one of a repository's tooling
// files.
func IsFile(name string) bool { return isJSON(name) }

// jsonFilesIn lists the add-on files directly inside a subdirectory.
func jsonFilesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if isJSON(entry.Name()) {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	return out
}

func isJSON(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".json") && !toolingFile(name)
}

// toolingFile is a JSON file a repository keeps for its tools rather than for
// us. An add-on kept in a repository of its own -- cloned straight into the
// folder -- has these beside its manifest, and reading them as add-ons would
// only fill the settings view with complaints about files nobody meant.
func toolingFile(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "package.json", "package-lock.json", "jsconfig.json", "deno.json", "renovate.json", "composer.json":
		return true
	}
	return strings.HasPrefix(lower, "tsconfig") || strings.HasPrefix(lower, ".")
}

// readFile hands one file's bytes to the parser, having first checked it is
// small enough to be worth reading at all.
func readFile[T Identified](path string, parse Parse[T]) ([]T, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Size() > MaxFile {
		return nil, nil, fmt.Errorf("file is %d bytes, which is too large to be one of these", info.Size())
	}

	raw, err := os.ReadFile(path) // #nosec G304 -- the user chose this folder
	if err != nil {
		return nil, nil, err
	}
	return parse(path, raw)
}

// ValidID keeps an identity to what can go in an attribute selector and a file
// name without quoting, which is what makes `[data-theme='nord']` safe to write
// and a plugin id safe to put in a tab's kind string.
func ValidID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// EnsureDir creates an add-on folder, so that "open the folder" has something
// to open on a machine where nothing has ever been installed.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	return nil
}

// WriteExample writes a starter file into dir without overwriting one already
// there, and returns the path it chose. `stem` is the file name without its
// extension, e.g. "my-theme".
func WriteExample(dir, stem string, body []byte) (string, error) {
	if err := EnsureDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, stem+".json")
	for n := 2; ; n++ {
		if _, err := os.Stat(path); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
			break
		}
		if n > 50 {
			return "", fmt.Errorf("too many starter files already in %s", dir)
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.json", stem, n))
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Sort orders items by identity, so a catalogue reads the same way twice.
func Sort(names []string) { sort.Strings(names) }
