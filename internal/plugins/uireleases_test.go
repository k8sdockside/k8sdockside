package plugins

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyDeclaredRepositoriesMayBeAskedForReleases(t *testing.T) {
	cat, _ := installCustom(t)
	if p, _ := cat.Find("acme"); p.CanAskReleases("siderolabs/talos") {
		t.Error("a plugin that declares no releases may ask for them")
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, "talos"), "plugin.json", `{
    "id": "talos",
    "name": "Talos",
    "requires": [{ "kind": "nodes" }],
    "ui": { "releases": [{ "github": " siderolabs/talos " }] },
    "views": [{ "id": "board", "label": "Board", "type": "custom" }]
}`)
	write(t, filepath.Join(dir, "talos", "ui"), "index.html", "<p>board</p>")
	cat = Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("problems loading: %v", cat.Problems)
	}
	p, _ := cat.Find("talos")
	if !p.CanAskReleases("siderolabs/talos") || !p.CanAskReleases("SideroLabs/Talos") {
		t.Error("the declared repository may not be asked")
	}
	if p.CanAskReleases("siderolabs/omni") || p.CanAskReleases("") {
		t.Error("an undeclared repository may be asked")
	}
	if p.UI.Releases[0].GitHub != "siderolabs/talos" {
		t.Errorf("not trimmed: %q", p.UI.Releases[0].GitHub)
	}
}

func TestReleasesDeclarationsAreCheckedStrictly(t *testing.T) {
	many := make([]UIRelease, 11)
	for i := range many {
		many[i] = UIRelease{GitHub: "owner/repo" + string(rune('a'+i))}
	}
	for name, tc := range map[string]struct {
		list []UIRelease
		want string
	}{
		"no slash":  {[]UIRelease{{GitHub: "talos"}}, "not a GitHub owner/repo"},
		"a url":     {[]UIRelease{{GitHub: "https://github.com/siderolabs/talos"}}, "not a GitHub owner/repo"},
		"a path":    {[]UIRelease{{GitHub: "siderolabs/talos/releases"}}, "not a GitHub owner/repo"},
		"traversal": {[]UIRelease{{GitHub: "siderolabs/.."}}, "not a GitHub owner/repo"},
		"bad owner": {[]UIRelease{{GitHub: "-x/talos"}}, "not a GitHub owner/repo"},
		"query":     {[]UIRelease{{GitHub: "siderolabs/talos?x=1"}}, "not a GitHub owner/repo"},
		"empty":     {[]UIRelease{{GitHub: ""}}, "not a GitHub owner/repo"},
		"twice":     {[]UIRelease{{GitHub: "siderolabs/talos"}, {GitHub: "SideroLabs/talos"}}, "twice"},
		"too many":  {many, "at most 10"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validate(Plugin{ID: "x", Views: []View{{ID: "v", Type: ViewCustom}}, UI: &UI{Releases: tc.list}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want it to say %q", err, tc.want)
			}
		})
	}
}
