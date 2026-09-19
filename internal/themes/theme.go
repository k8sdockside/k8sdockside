// Package themes is the app's palette engine: the built-in themes, the format a
// third party writes one in, and the loader that reads them off disk.
//
// A theme is a set of design tokens and nothing else. It cannot ship CSS, run
// code, or reach any part of the app but its colours -- which is what makes
// dropping a stranger's theme file into a folder a reasonable thing to do. The
// cost of that is that a theme can only recolour what the app already draws;
// the benefit is that a bad theme is ugly rather than broken.
//
// The built-in themes are embedded JSON in exactly the format a user's own
// theme takes, rather than Go structs. That is deliberate: there is one parser,
// one validator and one shape to document, and every built-in doubles as a
// worked example someone can copy as a starting point.
package themes

import (
	"fmt"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/addons"
)

// Base says which of the two default palettes fills in the tokens a theme
// leaves out, and what `color-scheme` the webview is told to use -- which is
// what decides the colour of native scrollbars, form controls and the caret.
const (
	BaseDark  = "dark"
	BaseLight = "light"
)

// Theme is one palette: an identity, a base to inherit from, and the tokens it
// overrides. Tokens are keyed without the leading `--`, so "bg-sidebar" here is
// `--bg-sidebar` in the stylesheet.
//
// A theme need not set every token. Whatever it omits is inherited from the
// built-in theme matching its Base, so a three-token theme is a legitimate one
// and stays legible as the app grows tokens the author never saw.
type Theme struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tagline string `json:"tagline,omitzero"`
	Base    string `json:"base"`
	Author  string `json:"author,omitzero"`
	// Tokens as written in the file, before inheritance. Resolve fills the
	// gaps; this is kept unresolved so the loader can report what a theme
	// actually said rather than what it ended up with.
	Tokens map[string]string `json:"tokens"`

	// Origin is filled in by the loader and ignored on the way in. It is where
	// the theme came from -- BuiltinOrigin, or the path of the file it was read
	// from -- and is shown in the settings view so a user can tell their own
	// themes from ours and find the file again.
	Origin string `json:"origin"`
	// Pack is the name of the collection this theme arrived in, empty for a
	// theme that came on its own. Used to group the gallery.
	Pack string `json:"pack"`
	// Resolved is Tokens with everything the theme left out filled in from its
	// base, which is what actually gets written to the document. It is carried
	// on the theme rather than fetched per theme so the settings view can draw
	// a true preview of the whole gallery from one call, and so inheritance has
	// exactly one implementation -- see Resolve.
	Resolved map[string]string `json:"resolved"`
	// Warnings are the readability problems Audit found once the theme's gaps
	// were filled in -- see AA. They are shown against the theme in the
	// settings view rather than stopping it from loading, and are always empty
	// for a built-in, which a test holds to the same bar.
	Warnings []string `json:"warnings"`
}

// BuiltinOrigin marks the themes that ship with the app.
const BuiltinOrigin = "builtin"

// Builtin reports whether the theme came with the app rather than off disk.
func (t Theme) Builtin() bool { return t.Origin == BuiltinOrigin }

// Pack is a file carrying several themes at once, which is how a theme
// collection is distributed. The alternative -- one file per theme and a naming
// convention to tie them together -- would make a collection something the user
// has to keep together by hand.
//
// A file is a pack if it has a `themes` array and a plain theme otherwise, so
// the simplest possible contribution is still a single object.
type Pack struct {
	Name    string  `json:"name,omitzero"`
	Author  string  `json:"author,omitzero"`
	Version string  `json:"version,omitzero"`
	Themes  []Theme `json:"themes"`
}

// Problem is a theme file that could not be used, and why. It is addons.Problem
// under a local name, so the settings view has one shape to render whether the
// file that failed was a theme or a plugin.
type Problem = addons.Problem

// Catalogue is everything the settings view needs to draw the theme gallery:
// what is available, where user themes are read from, and what went wrong.
type Catalogue struct {
	Themes []Theme `json:"themes"`
	// Dir is the folder themes are read from by default, offered in the UI so
	// the user can be told where to put a file they have downloaded.
	Dir string `json:"dir"`
	// Folders are the extra directories the user has added.
	Folders  []string  `json:"folders"`
	Problems []Problem `json:"problems"`
}

// Find returns the theme with the given id.
func (c Catalogue) Find(id string) (Theme, bool) {
	for _, theme := range c.Themes {
		if theme.ID == id {
			return theme, true
		}
	}
	return Theme{}, false
}

// validate checks a theme is usable and returns it with its id and base
// normalised. It is deliberately forgiving about what a theme leaves out and
// strict about what it puts in: missing tokens have a defined meaning, while a
// misspelt one is a mistake the author wants to hear about.
func validate(t Theme) (Theme, error) {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	t.Base = strings.ToLower(strings.TrimSpace(t.Base))

	if t.ID == "" {
		return t, fmt.Errorf("theme has no id")
	}
	if !addons.ValidID(t.ID) {
		return t, fmt.Errorf("theme id %q must be lowercase letters, digits and dashes", t.ID)
	}
	if t.Name == "" {
		// Not fatal: an id is enough to render a row, and refusing a theme over
		// a missing label would be pedantry.
		t.Name = t.ID
	}
	switch t.Base {
	case BaseDark, BaseLight:
	case "":
		// The overwhelmingly common case for a hand-written theme, and dark is
		// what this app is by default.
		t.Base = BaseDark
	default:
		return t, fmt.Errorf("theme %q has base %q, which must be %q or %q", t.ID, t.Base, BaseDark, BaseLight)
	}

	cleaned := make(map[string]string, len(t.Tokens))
	var unknown, bad []string
	for name, value := range t.Tokens {
		name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "--"))
		if !IsToken(name) {
			unknown = append(unknown, name)
			continue
		}
		value = strings.TrimSpace(value)
		if !IsColor(value) {
			bad = append(bad, name)
			continue
		}
		cleaned[name] = value
	}
	if len(unknown) > 0 {
		return t, fmt.Errorf("theme %q sets tokens this app has no use for: %s", t.ID, list(unknown))
	}
	if len(bad) > 0 {
		return t, fmt.Errorf("theme %q has values that are not colours: %s", t.ID, list(bad))
	}
	t.Tokens = cleaned
	return t, nil
}

// list renders names for an error message, sorted so the same mistake reads the
// same way twice.
func list(names []string) string {
	addons.Sort(names)
	return strings.Join(names, ", ")
}
