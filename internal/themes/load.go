package themes

import (
	"embed"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/k8sdockside/k8sdockside/internal/addons"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// builtinOrder is the order the built-in themes are offered in, which is a
// judgement rather than something the files can express: the two defaults
// first, then the rest roughly dark-to-light, with the community ports last
// because they are somebody else's palette rather than ours.
var builtinOrder = []string{
	"k8sdockside-dark",
	"k8sdockside-light",
	"deep-sea",
	"midnight-watch",
	"fjord",
	"harbour-crane",
	"rusty-hull",
	"bioluminescence",
	"lighthouse",
	"driftwood",
	"fog",
	"signal-flags",
	"nord",
	"catppuccin-mocha",
}

// DefaultID is the theme a fresh install wears, and what anything unresolvable
// falls back to. It is the palette the app had before it had themes at all, so
// upgrading changes nothing about how it looks.
const DefaultID = "k8sdockside-dark"

// LightID is the built-in light theme, and the other half of what a theme with
// `"base": "light"` inherits from.
const LightID = "k8sdockside-light"

// parseBuiltin reads one embedded theme file. A failure is a mistake in our own
// data rather than anything a user did, which a test catches, so it is fatal
// rather than something every caller has to handle.
func parseBuiltin(file string) Theme {
	raw, err := builtinFS.ReadFile("builtin/" + file)
	if err != nil {
		panic(fmt.Sprintf("themes: reading builtin/%s: %v", file, err))
	}
	var theme Theme
	if err := json.Unmarshal(raw, &theme); err != nil {
		panic(fmt.Sprintf("themes: parsing builtin/%s: %v", file, err))
	}
	theme, err = validate(theme)
	if err != nil {
		panic(fmt.Sprintf("themes: builtin/%s: %v", file, err))
	}
	theme.Origin = BuiltinOrigin
	return theme
}

// baseThemes are the two palettes every other theme inherits from, parsed on
// their own rather than looked up in Builtin().
//
// That separation is load bearing: Builtin resolves each theme it returns,
// resolving reads the base tokens, and a base looked up through Builtin would
// have it waiting on itself.
var baseThemes = sync.OnceValue(func() map[string]Theme {
	return map[string]Theme{
		DefaultID: parseBuiltin(DefaultID + ".json"),
		LightID:   parseBuiltin(LightID + ".json"),
	}
})

// Builtin returns the themes that ship with the app, in the order they are
// offered, each with its tokens already resolved.
var Builtin = sync.OnceValue(func() []Theme {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		panic(fmt.Sprintf("themes: reading embedded builtins: %v", err))
	}

	byID := make(map[string]Theme, len(entries))
	for _, entry := range entries {
		theme := parseBuiltin(entry.Name())
		theme.Resolved = Resolve(theme)
		theme.Warnings = Audit(theme.Resolved)
		byID[theme.ID] = theme
	}

	out := make([]Theme, 0, len(byID))
	for _, id := range builtinOrder {
		theme, ok := byID[id]
		if !ok {
			panic(fmt.Sprintf("themes: builtinOrder names %q, which has no file", id))
		}
		out = append(out, theme)
		delete(byID, id)
	}
	// A file added without being listed would otherwise vanish silently.
	if len(byID) > 0 {
		missing := make([]string, 0, len(byID))
		for id := range byID {
			missing = append(missing, id)
		}
		panic(fmt.Sprintf("themes: builtin themes missing from builtinOrder: %s", list(missing)))
	}
	return out
})

// defaults returns the token set a theme with the given base inherits, which is
// whichever of the two built-in defaults matches it.
func defaults(base string) map[string]string {
	id := DefaultID
	if base == BaseLight {
		id = LightID
	}
	return baseThemes()[id].Tokens
}

// Resolve is the theme's tokens with everything it left out filled in from its
// base. This is what actually gets written to the document, and doing it here
// rather than in the frontend means a theme that sets three tokens behaves the
// same however it arrived.
func Resolve(t Theme) map[string]string {
	base := defaults(t.Base)
	out := make(map[string]string, len(base))
	for name, value := range base {
		out[name] = value
	}
	for name, value := range t.Tokens {
		out[name] = value
	}
	return out
}

// Identity is the theme's id, which is what a settings file stores and what
// lets a user's file deliberately replace one of ours. It is how addons.Load
// tells two themes apart.
func (t Theme) Identity() string { return t.ID }

// Source is the file the theme came from, used when reporting a clash.
func (t Theme) Source() string { return t.Origin }

// Load builds the catalogue: the built-in themes, plus everything readable in
// dir and in each of the extra folders the user has added.
//
// The finding, the size limits, the one-level-deep rule and what happens to a
// file that will not parse are all in internal/addons, shared with the plugin
// loader -- see there. What is left here is only what a *theme* file is.
func Load(dir string, extra []string) Catalogue {
	// The default folder first, so a theme in one the user added later cannot
	// quietly take an id from the folder we told them to use.
	folders := append([]string{dir}, extra...)
	themes, problems := addons.Load(Builtin(), folders, parseFile)
	return Catalogue{
		Themes:   themes,
		Dir:      dir,
		Folders:  append([]string{}, extra...),
		Problems: problems,
	}
}

// parseFile reads one theme file, which may hold a single theme or a pack of
// them. The two are told apart by whether `themes` is present, so the simplest
// contribution is still one object with an id and some colours.
func parseFile(path string, raw []byte) (loaded []Theme, refused []string, err error) {
	var pack Pack
	if err := json.Unmarshal(raw, &pack); err != nil {
		return nil, nil, fmt.Errorf("not valid JSON: %w", err)
	}

	// A single theme unmarshals into Pack with no Themes, so the same bytes are
	// read again into the shape they actually are rather than being sniffed.
	if len(pack.Themes) == 0 {
		var theme Theme
		if err := json.Unmarshal(raw, &theme); err != nil {
			return nil, nil, fmt.Errorf("not valid JSON: %w", err)
		}
		pack = Pack{Themes: []Theme{theme}}
	}

	for _, theme := range pack.Themes {
		theme, err := validate(theme)
		if err != nil {
			refused = append(refused, err.Error())
			continue
		}
		theme.Origin = path
		theme.Pack = pack.Name
		theme.Resolved = Resolve(theme)
		theme.Warnings = Audit(theme.Resolved)
		loaded = append(loaded, theme)
	}
	// Nothing usable at all reads better as one failure for the file than as a
	// file that loaded and happens to contain nothing.
	if len(loaded) == 0 && len(refused) > 0 {
		return nil, nil, errors.New(strings.Join(refused, "; "))
	}
	return loaded, refused, nil
}

// Example is a starter theme, written into the themes folder on request so that
// "write your own" begins with a file that already works rather than a blank
// one and a page of documentation.
//
// Every token is spelled out even though all of them are optional, because the
// point of the file is to be edited: a complete one can be changed a colour at
// a time and reloaded, while a minimal one would send the reader back to the
// documentation to find out what else there is.
func Example() []byte {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(`    "id": "my-theme",` + "\n")
	b.WriteString(`    "name": "My Theme",` + "\n")
	b.WriteString(`    "tagline": "a starting point",` + "\n")
	b.WriteString(`    "base": "dark",` + "\n")
	b.WriteString(`    "author": "",` + "\n")
	b.WriteString(`    "tokens": {` + "\n")

	resolved := Resolve(Theme{Base: BaseDark})
	for i, token := range Tokens {
		comma := ","
		if i == len(Tokens)-1 {
			comma = ""
		}
		fmt.Fprintf(&b, "        %q: %q%s\n", token.Name, resolved[token.Name], comma)
	}
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return []byte(b.String())
}

// WriteExample writes the starter theme into dir without overwriting one that
// is already there, and returns the path it chose.
func WriteExample(dir string) (string, error) {
	return addons.WriteExample(dir, "my-theme", Example())
}

// EnsureDir creates the themes folder, so that "open the folder" has something
// to open on a machine where no theme has ever been installed.
func EnsureDir(dir string) error { return addons.EnsureDir(dir) }
