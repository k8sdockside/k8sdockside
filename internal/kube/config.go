// Package kube reads kubeconfig files from local disk and serves the cluster
// data the UI renders.
//
// Resource listings (pods, deployments, ...) are currently stubs: they are
// shaped exactly like the real thing and are deterministic per context, so the
// UI can be built and exercised before a live API client is wired in. Swapping
// in client-go means replacing the bodies in stub.go, not the types here.
package kube

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Where a kubeconfig file came from. Shown in the sidebar so the user can tell
// an auto-discovered file from one they added by hand.
const (
	SourceDefault = "default" // ~/.kube/config
	SourceEnv     = "env"     // listed in $KUBECONFIG
	SourceScan    = "scan"    // found by scanning ~/.kube
	SourceManual  = "manual"  // added by the user
	SourceFolder  = "folder"  // found in a folder the user asked us to watch
)

// maxConfigSize caps how much of a candidate file we are willing to read. A
// kubeconfig is a few KB; anything larger is not one and should not be slurped
// into memory just because it matched a glob.
const maxConfigSize = 4 << 20

// Context is one kubeconfig context flattened with the details the UI needs, so
// the frontend never has to cross-reference the clusters and users lists.
type Context struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
	File      string `json:"file"`
	Current   bool   `json:"current"`
}

// File is a kubeconfig file on disk together with the contexts parsed out of
// it. A file that failed to parse is still returned, with Error set, so the
// sidebar can show it as broken rather than silently dropping it.
type File struct {
	Path     string    `json:"path"`
	Source   string    `json:"source"`
	Contexts []Context `json:"contexts"`
	Error    string    `json:"error"`
}

// rawConfig is the subset of the kubeconfig schema we care about. Everything
// else in the file (credentials, extensions, proxy settings) is deliberately
// not parsed: we only need enough to list and label contexts.
type rawConfig struct {
	Kind           string `yaml:"kind"`
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server string `yaml:"server"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster   string `yaml:"cluster"`
			User      string `yaml:"user"`
			Namespace string `yaml:"namespace"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// ContextID is the stable key a context is known by across syncs, and the key
// its user settings (alias, colour) are stored under. Context names are only
// unique within a single file, so the file path has to be part of the identity.
func ContextID(path, name string) string {
	return path + "::" + name
}

// vacant is why a file yielded no contexts: true when there was nothing there
// to begin with, false when there was something we could not use. It is a named
// type rather than a bare bool so the two call sites cannot be read backwards.
type vacant bool

// ParseFile reads one kubeconfig and flattens its contexts. A read or parse
// failure is reported in the returned File rather than as an error, because the
// caller is building a list and one bad file should not sink the rest.
func ParseFile(path, source string) File {
	f, _ := parseFile(path, source)
	return f
}

// parseFile is ParseFile plus whether the file had nothing to say: either
// there is nothing at that path, or there is a file but it defines no contexts
// (including a zero-byte one, which parses cleanly to exactly that). Both mean
// "no clusters here", which is not a failure worth reporting for a path we
// offered unasked -- unlike a file that holds real content we could not make
// sense of. Discover needs the two apart, and asking here avoids a second stat.
func parseFile(path, source string) (File, vacant) {
	f := File{Path: path, Source: source, Contexts: []Context{}}

	info, err := os.Stat(path)
	if err != nil {
		f.Error = err.Error()
		return f, vacant(errors.Is(err, fs.ErrNotExist))
	}
	if info.IsDir() {
		f.Error = "not a file"
		return f, false
	}
	if info.Size() > maxConfigSize {
		f.Error = fmt.Sprintf("file is too large to be a kubeconfig (%d bytes)", info.Size())
		return f, false
	}

	data, err := os.ReadFile(path) // #nosec G304 -- a kubeconfig the user named, $KUBECONFIG, or ~/.kube
	if err != nil {
		f.Error = err.Error()
		return f, false
	}

	// A cheap guard before handing anything to the YAML parser. It matters most
	// for a folder the user pointed at, where every file is tried regardless of
	// its name: reporting "not a text file" beats a parse error full of binary.
	if !utf8.Valid(data) {
		f.Error = "not a text file"
		return f, false
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		f.Error = "not valid YAML: " + err.Error()
		return f, false
	}
	if raw.Kind != "" && raw.Kind != "Config" {
		f.Error = "not a kubeconfig (kind: " + raw.Kind + ")"
		return f, false
	}
	// Reported for a file the user named, so that adding an empty kubeconfig
	// does not look like it silently did nothing; dropped for ~/.kube/config,
	// which we look at whether or not they asked us to.
	if len(raw.Contexts) == 0 {
		f.Error = "no contexts defined"
		return f, true
	}

	servers := make(map[string]string, len(raw.Clusters))
	for _, c := range raw.Clusters {
		servers[c.Name] = c.Cluster.Server
	}

	for _, c := range raw.Contexts {
		if c.Name == "" {
			continue
		}
		f.Contexts = append(f.Contexts, Context{
			ID:        ContextID(path, c.Name),
			Name:      c.Name,
			Cluster:   c.Context.Cluster,
			User:      c.Context.User,
			Namespace: c.Context.Namespace,
			Server:    servers[c.Context.Cluster],
			File:      path,
			Current:   c.Name == raw.CurrentContext,
		})
	}
	return f, false
}

// candidate is a path we intend to try, tagged with how we found it.
type candidate struct {
	path   string
	source string
}

// Sources is what the user has told us about where their kubeconfigs are, and
// which of the ones we find they do not want to see.
type Sources struct {
	Files    []string // added by hand
	Folders  []string // scanned on every sync
	Excluded []string // found, but hidden at the user's request
}

// Discover finds every kubeconfig the app should offer: ~/.kube/config, the
// entries in $KUBECONFIG, the paths the user added by hand, everything in the
// folders they asked us to watch, and anything in ~/.kube that looks like a
// kubeconfig. Files are returned in that order, which is also the precedence
// used when the same file turns up twice.
func Discover(sources Sources) []File {
	manual, folders := sources.Files, sources.Folders

	excluded := make(map[string]bool, len(sources.Excluded))
	for _, p := range sources.Excluded {
		if resolved := resolve(p); resolved != "" {
			excluded[resolved] = true
		}
	}

	var candidates []candidate

	home, err := os.UserHomeDir()
	if err == nil {
		candidates = append(candidates, candidate{filepath.Join(home, ".kube", "config"), SourceDefault})
	}

	for _, p := range filepath.SplitList(os.Getenv("KUBECONFIG")) {
		if p = strings.TrimSpace(p); p != "" {
			candidates = append(candidates, candidate{p, SourceEnv})
		}
	}

	// User-added paths come before the directory scan so that a file the user
	// chose explicitly is labelled "manual" even if the scan would also find it.
	for _, p := range manual {
		if p = strings.TrimSpace(p); p != "" {
			candidates = append(candidates, candidate{p, SourceManual})
		}
	}

	// Folders the user chose are scanned before ~/.kube, so a config that is in
	// both is labelled with the folder they picked rather than as an incidental
	// scan hit they cannot remove.
	for _, dir := range folders {
		for _, p := range scanFolder(dir) {
			candidates = append(candidates, candidate{p, SourceFolder})
		}
	}

	if err == nil {
		for _, p := range scanKubeDir(filepath.Join(home, ".kube")) {
			candidates = append(candidates, candidate{p, SourceScan})
		}
	}

	files := make([]File, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		resolved := resolve(c.path)
		if resolved == "" || seen[resolved] {
			continue
		}
		seen[resolved] = true

		// Hidden at the user's request. Checked after dedup so that a file
		// reached by two routes is hidden by either of them.
		if excluded[resolved] {
			continue
		}

		parsed, nothingThere := parseFile(resolved, c.source)
		// A glob hit that turns out not to be a kubeconfig is not an error the
		// user needs to see -- they never asked for that file. Nor is a
		// ~/.kube/config that is absent or empty: plenty of people keep every
		// cluster in $KUBECONFIG or in files they add by hand, and we offer
		// that path unasked, so having nothing to show there is the ordinary
		// case rather than a fault. Greeting a new user with a red error over
		// a file they never chose is the worst version of this. Paths the user
		// named (or that $KUBECONFIG names) are reported even when broken, as
		// is a default config that holds content we could not read.
		if parsed.Error != "" && (c.source == SourceScan || c.source == SourceFolder ||
			(c.source == SourceDefault && nothingThere)) {
			continue
		}
		files = append(files, parsed)
	}
	return files
}

// scanFolder lists the files directly inside a folder the user chose.
//
// Unlike the ~/.kube scan it does not filter by name: the user pointed at this
// folder deliberately, and kubeconfigs are routinely saved with no extension at
// all. Every regular file is offered to ParseFile, which is what decides -- by
// reading it -- whether it is a kubeconfig. Sub-directories are not descended;
// one level is what "the files in this folder" means, and it keeps a folder
// chosen by mistake from walking a whole tree.
func scanFolder(dir string) []string {
	resolved := resolve(dir)
	if resolved == "" {
		return nil
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil
	}

	var found []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Sockets, devices and the like are not files we should be opening. A
		// symlink is followed rather than refused: a Kubernetes Secret mounted as
		// a volume is a folder of symlinks into a hidden, versioned directory, and
		// a kubeconfig linked in from elsewhere is still a kubeconfig. The path
		// kept is the link's, which stays put when the Secret is updated.
		path := filepath.Join(resolved, e.Name())
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		found = append(found, path)
	}
	sort.Strings(found)
	return found
}

// ScanFolder reports the kubeconfigs a folder contains, for the caller that
// wants to know before committing to watching it.
func ScanFolder(dir string) []File {
	var out []File
	for _, path := range scanFolder(dir) {
		if parsed := ParseFile(path, SourceFolder); parsed.Error == "" {
			out = append(out, parsed)
		}
	}
	return out
}

// scanKubeDir returns the entries of ~/.kube that are plausibly kubeconfigs.
// Sub-directories (notably ~/.kube/cache) are skipped, as is the well-known
// "config" file, which is always added ahead of the scan as SourceDefault.
func scanKubeDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var found []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "config" || strings.HasPrefix(name, ".") {
			continue
		}
		if !looksLikeKubeconfigName(name) {
			continue
		}
		found = append(found, filepath.Join(dir, name))
	}
	sort.Strings(found)
	return found
}

// looksLikeKubeconfigName is a cheap name filter run before opening a file. It
// only decides what is worth parsing; ParseFile has the final say.
func looksLikeKubeconfigName(name string) bool {
	lower := strings.ToLower(name)
	switch filepath.Ext(lower) {
	case ".yaml", ".yml", ".kubeconfig", ".conf":
		return true
	}
	return strings.HasPrefix(lower, "config") || strings.HasPrefix(lower, "kubeconfig")
}

// resolve turns a user-supplied path into the absolute, symlink-free form used
// to detect duplicates. A leading "~" is expanded because paths typed into the
// UI are not shell-expanded on the way in.
func resolve(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	// A symlink and its target are the same config; collapse them so the file
	// is not listed twice. Broken symlinks keep the abs path so the failure is
	// still reported to the user.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}
