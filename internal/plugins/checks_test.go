package plugins

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The checks made on load, and how what they find is worded. A plugin author
// meets these messages in Settings and in plugincheck, usually with the file
// open beside them, so each is tested for pointing at the place to look.

// problemsFor loads one plugin file on its own and returns the problems
// reported against it.
func problemsFor(t *testing.T, appVersion, body string) []string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "plugin.json", body)
	var out []string
	for _, p := range LoadAt(appVersion, dir, nil, nil).Problems {
		out = append(out, strings.Split(p.Message, "\n")...)
	}
	return out
}

func mustMention(t *testing.T, problems []string, want ...string) {
	t.Helper()
	all := strings.Join(problems, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("problems do not mention %q:\n%s", w, all)
		}
	}
}

func TestAnUnknownFieldIsRefusedWithWhereAndWhatWasMeant(t *testing.T) {
	problems := problemsFor(t, "", `{
    "id": "acme",
    "reqiures": [{ "kind": "pods" }],
    "views": [{ "id": "pods", "kind": "pods" }]
}`)
	mustMention(t, problems, `unknown field "reqiures"`, "line 3, column 5", `did you mean "requires"?`)

	problems = problemsFor(t, "", `{
    "id": "acme",
    "views": [
        { "id": "pods", "kind": "pods", "lable": "Pods" }
    ]
}`)
	mustMention(t, problems, `unknown field "lable" in views[0]`, "line 4", `did you mean "label"?`)
}

func TestAFieldNothingResemblesSaysItIsUnknownHere(t *testing.T) {
	problems := problemsFor(t, "", `{"id": "acme", "hologram": true, "views": [{ "id": "pods", "kind": "pods" }]}`)
	mustMention(t, problems, `unknown field "hologram"`, "not a field this version of the app knows", "set minAppVersion")

	// Already asking for a version, so that advice would be beside the point.
	problems = problemsFor(t, "", `{"id": "acme", "minAppVersion": "0.0.1", "hologram": true, "views": [{ "id": "pods", "kind": "pods" }]}`)
	if strings.Contains(strings.Join(problems, "\n"), "set minAppVersion") {
		t.Errorf("told to set a minAppVersion it already has: %v", problems)
	}
}

func TestAWrongTypeSaysWhatWasExpected(t *testing.T) {
	problems := problemsFor(t, "", `{
    "id": "acme",
    "views": [{ "id": "pods", "kind": "pods", "label": 5 }]
}`)
	mustMention(t, problems, "views[0].label", "line 3", "should be a string, not a number")
}

func TestBrokenJSONGivesALine(t *testing.T) {
	problems := problemsFor(t, "", "{\n  \"id\": \"acme\",\n  \"views\": [],\n}\n")
	mustMention(t, problems, "not valid JSON", "line 3, column 14", "no comma after the last item")
}

func TestEveryMistakeIsReportedAtOnce(t *testing.T) {
	problems := problemsFor(t, "", `{
    "id": "acme",
    "icon": "rocketship",
    "version": "one",
    "links": [{ "url": "ftp://acme.io" }],
    "requires": [{ "kind": "widgets" }],
    "views": [
        { "id": "a", "kind": "gadgets" },
        { "id": "b", "kind": "pods", "icon": "dashbord" }
    ],
    "charts": [{ "id": "c", "attach": "nowhere", "query": "up" }]
}`)
	mustMention(t, problems,
		`icon "rocketship"`,
		`version "one"`,
		`"ftp://acme.io"`,
		`requires "widgets"`,
		`"gadgets"`,
		`did you mean "dashboard"?`,
		`"nowhere"`,
	)
	if len(problems) != 7 {
		t.Errorf("got %d lines, want one per mistake:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

func TestAPluginNeedingANewerAppIsRefusedWithThatSaid(t *testing.T) {
	// Fields this app does not know are what a newer plugin looks like, and
	// the version is what should be reported, not the fields.
	body := `{"id": "acme", "minAppVersion": "0.0.15", "hologram": true, "views": [{ "id": "pods", "kind": "pods" }]}`
	problems := problemsFor(t, "v0.0.14", body)
	mustMention(t, problems, `plugin "acme" needs K8s Dockside 0.0.15 or newer, and this is 0.0.14`)
	if strings.Contains(strings.Join(problems, "\n"), "hologram") {
		t.Errorf("reported the fields of a plugin it is too old to read: %v", problems)
	}

	fine := `{"id": "acme", "minAppVersion": "0.0.15", "views": [{ "id": "pods", "kind": "pods" }]}`
	for _, version := range []string{"v0.0.15", "0.0.16", "v1.0.0", "development build", ""} {
		if problems := problemsFor(t, version, fine); len(problems) > 0 {
			t.Errorf("app %q refused a plugin asking for 0.0.15: %v", version, problems)
		}
	}
	if problems := problemsFor(t, "v0.0.15-rc.1", fine); len(problems) == 0 {
		t.Error("a release candidate of 0.0.15 is older than 0.0.15, and should be refused")
	}
}

func TestVersionsMustBeVersions(t *testing.T) {
	problems := problemsFor(t, "", `{"id": "acme", "version": "latest", "minAppVersion": "soon", "views": [{ "id": "pods", "kind": "pods" }]}`)
	mustMention(t, problems, `version "latest"`, `app version "soon"`)

	p, err := parsePlugin(t, `{"id": "acme", "version": " v1.2.0 ", "views": [{ "id": "pods", "kind": "pods" }]}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v1.2.0" {
		t.Errorf("version = %q, want it trimmed", p.Version)
	}
}

func TestLinksAreWebAddressesWithALabel(t *testing.T) {
	p, err := parsePlugin(t, `{
		"id": "acme",
		"links": [{ "url": "https://acme.io/docs" }, { "label": "Source", "url": "https://github.com/acme/acme" }],
		"views": [{ "id": "pods", "kind": "pods" }]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Links[0].Label != "acme.io" {
		t.Errorf("a link with no label reads %q, want its host", p.Links[0].Label)
	}
	if p.Links[1].Label != "Source" {
		t.Errorf("label = %q, want the one given", p.Links[1].Label)
	}

	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://", "acme.io"} {
		_, err := parsePlugin(t, `{"id": "acme", "links": [{ "url": "`+bad+`" }], "views": [{ "id": "pods", "kind": "pods" }]}`)
		if err == nil {
			t.Errorf("link %q was accepted", bad)
		}
	}

	many := strings.Repeat(`{ "url": "https://acme.io" },`, maxLinks+1)
	if _, err := parsePlugin(t, `{"id": "acme", "links": [`+strings.TrimSuffix(many, ",")+`], "views": [{ "id": "pods", "kind": "pods" }]}`); err == nil {
		t.Error("more links than are shown were accepted")
	}
}

func TestASchemaReferenceIsAccepted(t *testing.T) {
	problems := problemsFor(t, "", `{
    "$schema": "https://raw.githubusercontent.com/rogerwesterbo/k8sdockside/main/docs/plugin.schema.json",
    "id": "acme",
    "views": [{ "id": "pods", "kind": "pods" }]
}`)
	if len(problems) > 0 {
		t.Errorf("a manifest naming its schema was refused: %v", problems)
	}
}

func TestEveryPageAPluginOpensMustBeThere(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme", "ui"), "index.html", "<p>hi</p>")
	if err := os.MkdirAll(filepath.Join(dir, "acme", "ui", "folder.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "acme"), "plugin.json", `{
    "id": "acme",
    "views": [
        { "id": "home", "type": "custom" },
        { "id": "gone", "type": "custom", "entry": "gone.html" },
        { "id": "dir", "type": "custom", "entry": "folder.html" }
    ],
    "sections": [{ "id": "panel", "kind": "pods", "entry": "panel.js" }],
    "overview": { "entry": "overview.html" }
}`)

	cat := Load(dir, nil, nil)
	if _, ok := cat.Find("acme"); ok {
		t.Fatal("a plugin opening pages it does not have loaded")
	}
	var lines []string
	for _, p := range cat.Problems {
		lines = append(lines, strings.Split(p.Message, "\n")...)
	}
	mustMention(t, lines,
		`view "gone" opens gone.html, which is not in`,
		`view "dir" opens folder.html, which is a folder`,
		`section "panel" opens panel.js, which is not an HTML page`,
		`its overview opens overview.html, which is not in`,
	)
	if strings.Contains(strings.Join(lines, "\n"), `"home"`) {
		t.Errorf("the page that is there was reported: %v", lines)
	}
}

func TestAPackReportsEachPluginByItsPlace(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pack.json", `{
		"name": "Ops",
		"plugins": [
			{"id": "one", "views": [{"id": "pods", "kind": "pods"}]},
			{"id": "two", "views": [{"id": "pods", "kind": "pods", "lable": "x"}]}
		]
	}`)
	cat := Load(dir, nil, nil)
	if _, ok := cat.Find("one"); !ok {
		t.Error("the sound plugin in the pack was lost with the broken one")
	}
	if len(cat.Problems) != 1 || !strings.Contains(cat.Problems[0].Message, `unknown field "lable" in plugins[1].views[0]`) {
		t.Errorf("problems = %v, want the broken plugin named by its place in the pack", cat.Problems)
	}
}

func TestCheckFolderReadsARepositoryAsTheAppWould(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", `{"id": "acme", "views": [{"id": "pods", "kind": "pods"}]}`)
	write(t, dir, "package.json", `{"name": "not a plugin"}`)
	write(t, filepath.Join(dir, "ui"), "data.json", `{"not": "read"}`)

	loaded, problems := CheckFolder("", dir)
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	if len(loaded) != 1 || loaded[0].ID != "acme" {
		t.Errorf("loaded = %v, want just acme", loaded)
	}

	if _, problems := CheckFolder("", t.TempDir()); len(problems) != 1 || !strings.Contains(problems[0].Message, "no plugin file") {
		t.Errorf("an empty folder reported %v", problems)
	}
}

func TestTheKnownPluginsAreSound(t *testing.T) {
	list := KnownPlugins()
	if len(list) == 0 {
		t.Fatal("no known plugins")
	}
	for _, k := range list {
		for _, b := range Builtin() {
			if b.ID == k.ID {
				t.Errorf("%s is both built in and offered for installing", k.ID)
			}
		}
		if len(k.Links) == 0 {
			t.Errorf("%s has no links to what it is about", k.ID)
		}
		if k.Tagline == "" || k.Description == "" {
			t.Errorf("%s needs a tagline and a description to be offered", k.ID)
		}
		if k.Author == "" {
			t.Errorf("%s names no author to credit", k.ID)
		}
	}
}

// The author is shown as written, beside the plugin's name, so it is held to
// what fits there, and its address to the rule every link a plugin gives is.
func TestAuthorIsCheckedAndCredited(t *testing.T) {
	const views = `"views": [{ "id": "pods", "label": "Pods", "kind": "pods" }]`
	for name, tc := range map[string]struct {
		manifest string
		refused  string
	}{
		"a name and an address": {`{ "id": "acme", "author": " Acme Inc ", "authorUrl": "https://acme.example", ` + views + ` }`, ""},
		"a name alone":          {`{ "id": "acme", "author": "Acme Inc", ` + views + ` }`, ""},
		"not a web address":     {`{ "id": "acme", "author": "Acme", "authorUrl": "javascript:alert(1)", ` + views + ` }`, "not an http(s) address"},
		"an address alone":      {`{ "id": "acme", "authorUrl": "https://acme.example", ` + views + ` }`, "no author to link it from"},
		"a name too long":       {`{ "id": "acme", "author": "` + strings.Repeat("a", 81) + `", ` + views + ` }`, "keep it to 80"},
	} {
		t.Run(name, func(t *testing.T) {
			loaded, _, err := parseFile("/plugins/acme.json", []byte(tc.manifest), "")
			if tc.refused == "" {
				if err != nil || len(loaded) != 1 {
					t.Fatalf("refused: %v", err)
				}
				if loaded[0].Author != "Acme Inc" {
					t.Errorf("author = %q, want it trimmed", loaded[0].Author)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refused) {
				t.Errorf("err = %v, want it to say %q", err, tc.refused)
			}
		})
	}
}

// A pack credits its author on every plugin in it that does not name its own.
func TestAPacksAuthorIsGivenToItsPlugins(t *testing.T) {
	pack := `{ "name": "Acme Pack", "author": "Acme Inc", "authorUrl": "https://acme.example", "plugins": [
		{ "id": "one", "views": [{ "id": "pods", "label": "Pods", "kind": "pods" }] },
		{ "id": "two", "author": "Someone Else", "views": [{ "id": "pods", "label": "Pods", "kind": "pods" }] }
	] }`
	loaded, _, err := parseFile("/plugins/pack.json", []byte(pack), "")
	if err != nil || len(loaded) != 2 {
		t.Fatalf("loaded %d, err %v", len(loaded), err)
	}
	if loaded[0].Author != "Acme Inc" || loaded[0].AuthorURL != "https://acme.example" {
		t.Errorf("one: %q %q, want the pack's author", loaded[0].Author, loaded[0].AuthorURL)
	}
	if loaded[1].Author != "Someone Else" || loaded[1].AuthorURL != "" {
		t.Errorf("two: %q %q, want its own author and nothing of the pack's", loaded[1].Author, loaded[1].AuthorURL)
	}
}

func TestOfferMarksWhatIsAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "metallb"), "plugin.json", `{"id": "metallb", "views": [{"id": "pods", "kind": "pods"}]}`)
	for _, offer := range Load(dir, nil, nil).Offer() {
		if offer.Installed != (offer.ID == "metallb") {
			t.Errorf("%s: installed = %v", offer.ID, offer.Installed)
		}
	}
}

// TestIconsMatchTheOnesTheAppDraws keeps the loader's list and the frontend's
// glyphs the same: an icon the app draws but the loader refuses is as wrong as
// the other way round.
func TestIconsMatchTheOnesTheAppDraws(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "components", "Icon.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "export const PATHS")
	end := strings.Index(body[start:], "\n    };")
	if start < 0 || end < 0 {
		t.Fatal("cannot find PATHS in Icon.svelte")
	}
	key := regexp.MustCompile(`(?m)^ {8}'?([a-z0-9-]+)'?\s*:`)
	var drawn []string
	for _, m := range key.FindAllStringSubmatch(body[start:start+end], -1) {
		drawn = append(drawn, m[1])
	}
	slices.Sort(drawn)
	ours := slices.Clone(Icons)
	slices.Sort(ours)
	if !slices.Equal(drawn, ours) {
		t.Errorf("Icons and Icon.svelte's PATHS differ:\n  app draws %v\n  loader has %v", drawn, ours)
	}
}
