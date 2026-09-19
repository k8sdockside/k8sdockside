package plugins

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestEveryKnownPluginHasARealCategory(t *testing.T) {
	// The settings view groups by category, so a plugin filed under "other"
	// is one nobody finds by looking for what it does. Our own list is held
	// to a higher standard than a stranger's manifest: every entry on it
	// names one.
	for _, known := range KnownPlugins() {
		if known.Category == CategoryOther {
			t.Errorf("%s is on the known list with no category of its own", known.ID)
		}
		if !slices.Contains(Categories, known.Category) {
			t.Errorf("%s is in category %q, which is not one the app has", known.ID, known.Category)
		}
	}
}

func TestABuiltInPluginNamesItsCategory(t *testing.T) {
	for _, p := range Builtin() {
		if p.Category == CategoryOther {
			t.Errorf("the built-in plugin %s names no category", p.ID)
		}
	}
}

func TestAPluginThatNamesNoCategoryIsFiledUnderOther(t *testing.T) {
	p, err := validate(Plugin{ID: "acme", Views: []View{{ID: "x", Kind: "pods"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Category != CategoryOther {
		t.Errorf("a plugin with no category came back as %q, want %q", p.Category, CategoryOther)
	}
}

func TestACategoryTheAppDoesNotHaveIsRefused(t *testing.T) {
	_, err := validate(Plugin{ID: "acme", Category: "netwroking", Views: []View{{ID: "x", Kind: "pods"}}})
	if err == nil {
		t.Fatal("a misspelled category was accepted")
	}
	// The message is the one a person acts on, so it suggests the word they
	// were reaching for.
	if !strings.Contains(err.Error(), `did you mean "networking"`) {
		t.Errorf("the error does not suggest the right category: %v", err)
	}
}

func TestTheSchemaKnowsTheCategories(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "plugin.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc schemaDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	got := slices.Clone(doc.Definitions["plugin"].Properties["category"].Enum)
	want := slices.Clone(Categories)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the schema's categories differ from the loader's:\n  schema %v\n  loader %v", got, want)
	}
}

// TestCategoriesMatchTheOnesTheAppOffers keeps this list and the frontend's
// the same: a category the settings view offers but the loader refuses would
// filter a plugin that can never be in it, and one the loader accepts but the
// view does not know would go missing from the filter altogether.
func TestCategoriesMatchTheOnesTheAppOffers(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "plugins", "categories.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "export const CATEGORIES")
	end := strings.Index(body[start:], "\n];")
	if start < 0 || end < 0 {
		t.Fatal("cannot find CATEGORIES in categories.ts")
	}
	id := regexp.MustCompile(`id: '([a-z-]+)'`)
	var offered []string
	for _, m := range id.FindAllStringSubmatch(body[start:start+end], -1) {
		offered = append(offered, m[1])
	}
	if !slices.Equal(offered, Categories) {
		t.Errorf("Categories and categories.ts differ:\n  app offers %v\n  loader has %v", offered, Categories)
	}
}

// A plugin written before categories existed names none. The known list knows
// what it is, and it is the same plugin whether it is being offered or has
// just been installed, so the card should say the same thing either way.
func TestAnInstalledPluginWithoutACategoryTakesTheKnownListsOne(t *testing.T) {
	cilium, ok := FindKnown("cilium")
	if !ok {
		t.Fatal("cilium is not on the known list")
	}
	if got := knownCategory(Plugin{ID: "cilium", Category: CategoryOther}); got != cilium.Category {
		t.Errorf("an installed cilium is in category %q, want the known list's %q", got, cilium.Category)
	}
	// A manifest that names one is believed; the list does not overrule it.
	if got := knownCategory(Plugin{ID: "cilium", Category: "security"}); got != "security" {
		t.Errorf("the manifest's own category was overruled: %q", got)
	}
	// Nobody knows this one, so it stays under Other rather than blank.
	if got := knownCategory(Plugin{ID: "nobody-knows-this"}); got != CategoryOther {
		t.Errorf("an unknown plugin with no category is %q, want %q", got, CategoryOther)
	}
}
