package plugins

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// docs/plugin.schema.json is what an editor checks a manifest against while
// it is being written. It is written by hand, so these tests hold it to the
// structs the loader reads: a field added to one and not the other would have
// the editor refuse what the app accepts, or wave through what it refuses.

type schemaDoc struct {
	Definitions map[string]struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	} `json:"definitions"`
}

func readSchema(t *testing.T) schemaDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "plugin.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc schemaDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("docs/plugin.schema.json: %v", err)
	}
	return doc
}

func TestTheSchemaDescribesEveryManifestField(t *testing.T) {
	doc := readSchema(t)

	// Filled in by the loader and never written by hand, so the schema leaves
	// them out even though the loader tolerates them.
	loaderOnly := map[string][]string{
		"plugin": {"origin", "pack", "repo", "official", "disabled"},
		"ui":     {"readable"},
	}
	types := map[string]reflect.Type{
		"plugin":      reflect.TypeFor[Plugin](),
		"link":        reflect.TypeFor[Link](),
		"requirement": reflect.TypeFor[Requirement](),
		"view":        reflect.TypeFor[View](),
		"card":        reflect.TypeFor[Card](),
		"chart":       reflect.TypeFor[Chart](),
		"usage":       reflect.TypeFor[UsageQueries](),
		"usagePair":   reflect.TypeFor[UsagePair](),
		"ui":          reflect.TypeFor[UI](),
		"uiService":   reflect.TypeFor[UIService](),
		"action":      reflect.TypeFor[Action](),
		"condition":   reflect.TypeFor[Condition](),
		"request":     reflect.TypeFor[Request](),
		"section":     reflect.TypeFor[Section](),
		"overview":    reflect.TypeFor[Overview](),
		"pack":        reflect.TypeFor[packFile](),
	}
	for name, typ := range types {
		def, ok := doc.Definitions[name]
		if !ok {
			t.Errorf("the schema has no definition %q for %s", name, typ.Name())
			continue
		}
		var want []string
		for _, member := range memberNames(typ) {
			if !slices.Contains(loaderOnly[name], member) {
				want = append(want, member)
			}
		}
		var got []string
		for member := range def.Properties {
			got = append(got, member)
		}
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(want, got) {
			t.Errorf("schema %q has %v, but %s reads %v", name, got, typ.Name(), want)
		}
	}
}

func TestTheSchemaKnowsTheIconsAndUnits(t *testing.T) {
	doc := readSchema(t)

	icons := slices.Clone(doc.Definitions["plugin"].Properties["icon"].Enum)
	ours := slices.Clone(Icons)
	slices.Sort(icons)
	slices.Sort(ours)
	if !slices.Equal(icons, ours) {
		t.Errorf("the schema's icons differ from the loader's:\n  schema %v\n  loader %v", icons, ours)
	}

	units := doc.Definitions["chart"].Properties["unit"].Enum
	if !slices.Equal(units, ChartUnits[1:]) {
		t.Errorf("the schema's units %v differ from the loader's %v", units, ChartUnits[1:])
	}
}
