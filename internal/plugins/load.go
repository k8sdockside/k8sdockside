package plugins

import (
	"embed"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/k8sdockside/k8sdockside/internal/addons"
)

// builtinFS holds the built-in manifests, builtin/<id>.json, and the pages of
// the built-ins that have any, builtin/ui/<id>/. A built-in with pages is the
// same as a plugin read from a folder with its ui/ beside it, except that its
// folder is in the binary: it cannot go missing, and it updates with the app.
//
//go:embed builtin/*.json builtin/ui
var builtinFS embed.FS

// builtinUIDir is where a built-in's pages are embedded: builtinUIDir/<id>.
const builtinUIDir = "builtin/ui"

// jsonIndent matches the indentation the built-in plugin files are written in,
// so a starter written by the app and one copied out of the repository look the
// same in an editor.
var jsonIndent = jsontext.WithIndent("    ")

// builtinOrder is the order the built-in plugins are offered in: alphabetical
// would be an accident, and this is a judgement about what a Kubernetes user is
// most likely to be looking for.
//
// Only what most clusters worth connecting to run is built in. A plugin for a
// product fewer clusters have -- cert-manager, MetalLB, KubeVirt -- lives in a
// repository of its own, follows that product's releases rather than the
// app's, and is offered from the list in known.json.
var builtinOrder = []string{
	"argocd",
	"flux",
	"prometheus",
}

// Builtin returns the plugins that ship with the app, in the order they are
// offered. A failure is a mistake in our own data, which a test catches, so it
// is fatal rather than something every caller has to handle.
var Builtin = sync.OnceValue(func() []Plugin {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		panic(fmt.Sprintf("plugins: reading embedded builtins: %v", err))
	}

	byID := make(map[string]Plugin, len(entries))
	for _, entry := range entries {
		// builtin/ui holds pages, not manifests.
		if entry.IsDir() {
			continue
		}
		name := "builtin/" + entry.Name()
		raw, err := builtinFS.ReadFile(name)
		if err != nil {
			panic(fmt.Sprintf("plugins: reading %s: %v", name, err))
		}
		// Held to the same strict reading as a user's file, so the built-ins are
		// proof the rules can be met rather than an exception to them.
		var plugin Plugin
		if err := decodeStrict(raw, &plugin); err != nil {
			panic(fmt.Sprintf("plugins: %s: %v", name, describe(raw, err, "", true)))
		}
		plugin, err = validate(plugin)
		if err != nil {
			panic(fmt.Sprintf("plugins: %s: %v", name, err))
		}
		plugin.Origin = BuiltinOrigin
		// A built-in with pages serves them from its embedded folder; one that
		// declares pages without shipping them is a mistake in our own data.
		if plugin.UI != nil {
			files, done, ok := plugin.UIFiles()
			if !ok {
				panic(fmt.Sprintf("plugins: %s: ships views of its own but has no %s/%s folder", name, builtinUIDir, plugin.ID))
			}
			err := checkEntries(plugin, files, builtinUIDir+"/"+plugin.ID)
			done()
			if err != nil {
				panic(fmt.Sprintf("plugins: %s: %v", name, err))
			}
		}
		byID[plugin.ID] = plugin
	}

	out := make([]Plugin, 0, len(byID))
	for _, id := range builtinOrder {
		plugin, ok := byID[id]
		if !ok {
			panic(fmt.Sprintf("plugins: builtinOrder names %q, which has no file", id))
		}
		out = append(out, plugin)
		delete(byID, id)
	}
	// A file added without being listed would otherwise vanish silently.
	if len(byID) > 0 {
		missing := make([]string, 0, len(byID))
		for id := range byID {
			missing = append(missing, id)
		}
		addons.Sort(missing)
		panic(fmt.Sprintf("plugins: builtin plugins missing from builtinOrder: %s", strings.Join(missing, ", ")))
	}
	return out
})

// Load builds the catalogue: the built-in plugins, plus everything readable in
// dir and in each of the extra folders the user has added.
//
// The finding, the size limits, the one-level-deep rule and what happens to a
// file that will not parse are all in internal/addons, shared with the theme
// loader. What is left here is only what a *plugin* file is.
//
// disabled are the ids the user has switched off. They are marked rather than
// filtered out, and an id naming nothing is ignored: a plugin switched off and
// then deleted leaves its id behind in settings, and that is not an error worth
// showing anyone.
//
// It reads as a development build, which every plugin's minAppVersion admits;
// the app itself goes through LoadAt with the release it is.
func Load(dir string, extra []string, disabled []string) Catalogue {
	return LoadAt("", dir, extra, disabled)
}

// LoadAt is Load for a given release of the app, which is what each plugin's
// minAppVersion is checked against. A plugin asking for a newer release is
// refused with that said. appVersion that is not a release -- empty, or
// "development build" -- admits everything; see Plugin.NeedsNewerApp.
func LoadAt(appVersion, dir string, extra []string, disabled []string) Catalogue {
	// The default folder first, so a plugin in one the user added later cannot
	// quietly take an id from the folder we told them to use.
	folders := append([]string{dir}, extra...)
	parse := func(path string, raw []byte) ([]Plugin, []string, error) {
		return parseFile(path, raw, appVersion)
	}
	loaded, problems := addons.Load(Builtin(), folders, parse)
	problems = append(problems, emptyClones(dir)...)

	off := make(map[string]bool, len(disabled))
	for _, id := range disabled {
		off[id] = true
	}
	for i := range loaded {
		loaded[i].Disabled = off[loaded[i].ID]
		loaded[i].Repo, _ = RepoOf(loaded[i])
		// Always decided here, whatever the manifest wrote.
		loaded[i].Official = officialClone(loaded[i])
		loaded[i].Category = knownCategory(loaded[i])
	}

	return Catalogue{
		Plugins:  loaded,
		Dir:      dir,
		Folders:  append([]string{}, extra...),
		Problems: problems,
	}
}

// emptyClones names the repositories in the plugins folder with no plugin
// file at their root. Nothing loads from one, and nothing else would say so:
// the folder reader looks for files, and there are none to report on. The
// usual cause is a repository cloned before its plugin was pushed to it, and
// the plugin it was meant to be is then offered for installing again -- which
// updates the clone, and is what the message says to do.
func emptyClones(dir string) []Problem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Problem
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		folder := filepath.Join(dir, entry.Name())
		if info, err := os.Stat(filepath.Join(folder, ".git")); err != nil || !info.IsDir() {
			continue
		}
		if len(addons.FilesIn(folder)) > 0 {
			continue
		}
		out = append(out, Problem{
			Path:    folder,
			Message: "this repository has no plugin.json at its root, so nothing in it loads -- it may have been cloned before its plugin was pushed. Installing the plugin again updates it; so does git pull in the folder",
		})
	}
	return out
}

// packFile is a pack as it is first read: its plugins kept as they are
// written, so each is read -- and refused -- on its own.
type packFile struct {
	Schema string `json:"$schema,omitzero"`
	Name   string `json:"name,omitzero"`
	Author string `json:"author,omitzero"`
	// AuthorURL is where to find the pack's author. With Author, it is given
	// to every plugin in the pack that does not name an author of its own.
	AuthorURL string           `json:"authorUrl,omitzero"`
	Version   string           `json:"version,omitzero"`
	Plugins   []jsontext.Value `json:"plugins"`
}

// parseFile reads one plugin file, which may hold a single plugin or a pack of
// them. The two are told apart by whether `plugins` is present, so the simplest
// contribution is still one object.
func parseFile(path string, raw []byte, appVersion string) (loaded []Plugin, refused []string, err error) {
	// Read loosely first, to find out which of the two it is. This is also
	// where a file that is not JSON at all is caught, with a line to look at.
	var top map[string]jsontext.Value
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil, describe(raw, err, "", true)
	}
	if top == nil {
		return nil, nil, errors.New("the file holds null rather than a plugin")
	}

	type entry struct {
		raw []byte
		at  jsontext.Pointer
	}
	entries := []entry{{raw: raw}}
	packName := ""
	// The pack's author, checked as a plugin's would be, for the plugins in it
	// that do not name one.
	var packAuthor Plugin
	if _, isPack := top["plugins"]; isPack {
		var pack packFile
		if err := decodeStrict(raw, &pack); err != nil {
			return nil, nil, describe(raw, err, "", true)
		}
		if len(pack.Plugins) == 0 {
			return nil, nil, errors.New("the file is a pack, but its plugins list is empty")
		}
		packAuthor = Plugin{ID: pack.Name, Author: pack.Author, AuthorURL: pack.AuthorURL}
		if err := validateAuthor(&packAuthor); err != nil {
			return nil, nil, err
		}
		packName = pack.Name
		entries = entries[:0]
		for i, value := range pack.Plugins {
			entries = append(entries, entry{raw: value, at: jsontext.Pointer("/plugins/" + strconv.Itoa(i))})
		}
	}

	for _, e := range entries {
		plugin, err := readPlugin(e.raw, e.at, appVersion)
		if plugin.ID == "" {
			// Not read far enough to have pages worth looking for.
			refused = append(refused, err.Error())
			continue
		}
		plugin.Origin = path
		plugin.Pack = packName
		if plugin.Author == "" && plugin.AuthorURL == "" {
			plugin.Author, plugin.AuthorURL = packAuthor.Author, packAuthor.AuthorURL
		}
		// The pages are looked for even when the manifest has already failed,
		// so the one list says everything that is wrong with the plugin.
		err = errors.Join(err, checkPages(plugin))
		if err != nil {
			refused = append(refused, err.Error())
			continue
		}
		loaded = append(loaded, plugin)
	}
	if len(loaded) == 0 && len(refused) > 0 {
		return nil, nil, errors.New(strings.Join(refused, "\n"))
	}
	return loaded, refused, nil
}

// readPlugin reads and checks one plugin. at is where it sits in its file:
// empty for a file holding just the one, whose offsets are then the file's
// own and can be given as lines.
func readPlugin(raw []byte, at jsontext.Pointer, appVersion string) (Plugin, error) {
	// Whether this app is new enough is asked first, from a loose read: a
	// plugin for a newer app may well use fields this one has never heard of,
	// and the answer to that is "update the app", not "unknown field".
	var first asked
	_ = json.Unmarshal(raw, &first) // anything wrong is reported by the strict read
	probe := Plugin{ID: strings.TrimSpace(first.ID), MinAppVersion: strings.TrimSpace(first.MinAppVersion)}
	if msg, newer := probe.NeedsNewerApp(appVersion); newer {
		return Plugin{}, errors.New(msg)
	}

	var plugin Plugin
	if err := decodeStrict(raw, &plugin); err != nil {
		described := describe(raw, err, at, at == "")
		if errors.Is(err, json.ErrUnknownName) && probe.MinAppVersion == "" && !strings.Contains(described.Error(), "did you mean") {
			described = fmt.Errorf("%w; if the plugin is written for a newer version, set minAppVersion to it", described)
		}
		if probe.ID != "" {
			described = fmt.Errorf("plugin %q: %w", probe.ID, described)
		}
		return Plugin{}, described
	}
	return validate(plugin)
}

// checkPages refuses a plugin whose own views have no folder to be served
// from, or open pages that folder does not have.
func checkPages(p Plugin) error {
	if err := checkUIDir(p); err != nil {
		return err
	}
	files, done, ok := p.UIFiles()
	if !ok {
		return nil
	}
	defer done()
	root, _ := p.UIRoot()
	return checkEntries(p, files, root)
}

// checkEntries refuses a plugin whose views, panels or overview open a page
// its ui folder does not have, when the file is read, rather than leaving
// each to open onto "not found" in a frame. dir is the folder as it should be
// named in the message.
func checkEntries(p Plugin, files fs.FS, dir string) error {
	var errs []error
	check := func(what, entry string) {
		if ext := strings.ToLower(path.Ext(entry)); ext != ".html" && ext != ".htm" {
			errs = append(errs, fmt.Errorf("plugin %q: %s opens %s, which is not an HTML page", p.ID, what, entry))
			return
		}
		info, err := fs.Stat(files, entry)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("plugin %q: %s opens %s, which is not in %s", p.ID, what, entry, dir))
		case info.IsDir():
			errs = append(errs, fmt.Errorf("plugin %q: %s opens %s, which is a folder, not a page", p.ID, what, entry))
		}
	}
	for _, v := range p.Views {
		if v.Type == ViewCustom {
			check(fmt.Sprintf("view %q", v.ID), v.Entry)
		}
	}
	for _, s := range p.Sections {
		check(fmt.Sprintf("section %q", s.ID), s.Entry)
	}
	if p.Overview != nil {
		check("its overview", p.Overview.Entry)
	}
	if p.Logo != "" {
		info, err := fs.Stat(files, p.Logo)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("plugin %q: its logo %s is not in %s", p.ID, p.Logo, dir))
		case info.IsDir():
			errs = append(errs, fmt.Errorf("plugin %q: its logo %s is a folder, not an image", p.ID, p.Logo))
		}
	}
	return errors.Join(errs...)
}

// checkUIDir refuses a plugin whose own views have no folder to be served
// from, when the file is read, rather than leaving it to open onto a blank
// frame.
func checkUIDir(p Plugin) error {
	root, ok := p.UIRoot()
	if !ok {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("plugin %q ships views of its own, but its ui folder %s cannot be read: %w", p.ID, root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin %q ships views of its own, but %s is a file, not a folder", p.ID, root)
	}
	return nil
}

// SchemaURL is where the plugin manifest's JSON Schema is published, for an
// editor to check a file against while it is written. See
// docs/plugin.schema.json.
const SchemaURL = "https://raw.githubusercontent.com/k8sdockside/k8sdockside/main/docs/plugin.schema.json"

// Example is a starter plugin, written into the plugins folder on request. It
// is a real, working plugin for something almost every cluster has -- rather
// than a skeleton of placeholders -- so the first edit is changing something
// that already appears in the sidebar.
func Example() []byte {
	example := Plugin{
		Schema:  SchemaURL,
		ID:      "my-solution",
		Name:    "My Solution",
		Version: "0.1.0",
		Tagline: "a starting point",
		Icon:    "puzzle",
		Docs:    "https://example.com",
		Links: []Link{
			{Label: "Home page", URL: "https://example.com"},
		},
		Description: "Replace the kinds below with the ones your solution installs. Every view lists a kind this app already knows how to open: a built-in name like \"deployments\", or \"crd:<plural>.<group>\" for a custom resource.",
		Requires: []Requirement{
			{Kind: "deployments", Label: "Deployments"},
		},
		Views: []View{
			{
				ID:       "workloads",
				Label:    "Workloads",
				Icon:     "rocket",
				Kind:     "deployments",
				Selector: "app.kubernetes.io/part-of=my-solution",
			},
		},
		Cards: []Card{
			{
				Label:    "Workloads",
				Kind:     "deployments",
				Selector: "app.kubernetes.io/part-of=my-solution",
			},
		},
	}

	raw, err := json.Marshal(example, json.Deterministic(true), jsonIndent)
	if err != nil {
		// Marshalling a literal we wrote cannot fail; a panic here would be a
		// bug in this function rather than anything the user did.
		panic(fmt.Sprintf("plugins: rendering the example: %v", err))
	}
	return append(raw, '\n')
}

// WriteExample writes the starter plugin into dir without overwriting one that
// is already there, and returns the path it chose.
func WriteExample(dir string) (string, error) {
	return addons.WriteExample(dir, "my-solution", Example())
}

// EnsureDir creates the plugins folder, so that "open the folder" has something
// to open on a machine where nothing has ever been installed.
func EnsureDir(dir string) error { return addons.EnsureDir(dir) }

// CheckFolder reads the plugin files directly inside dir exactly as they would
// be read once that folder is cloned or copied into the plugins folder, and
// reports what loaded and what did not. It is for checking a plugin's own
// repository -- see cmd/plugincheck -- and deliberately ignores the built-ins
// and the user's settings: the question is whether this folder is sound, not
// what a particular machine would make of it.
func CheckFolder(appVersion, dir string) ([]Plugin, []Problem) {
	var loaded []Plugin
	var problems []Problem
	files := addons.FilesIn(dir)
	if len(files) == 0 {
		return nil, []Problem{{Path: dir, Message: "there is no plugin file here -- a plugin's repository has its plugin.json at its root"}}
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			problems = append(problems, Problem{Path: file, Message: err.Error()})
			continue
		}
		if info.Size() > addons.MaxFile {
			problems = append(problems, Problem{Path: file, Message: fmt.Sprintf("file is %d bytes, which is too large to be a plugin", info.Size())})
			continue
		}
		raw, err := os.ReadFile(file) // #nosec G304 -- the folder being checked was named by whoever runs the check
		if err != nil {
			problems = append(problems, Problem{Path: file, Message: err.Error()})
			continue
		}
		found, refused, err := parseFile(file, raw, appVersion)
		if err != nil {
			problems = append(problems, Problem{Path: file, Message: err.Error()})
			continue
		}
		for _, reason := range refused {
			problems = append(problems, Problem{Path: file, Message: reason})
		}
		loaded = append(loaded, found...)
	}
	return loaded, problems
}
