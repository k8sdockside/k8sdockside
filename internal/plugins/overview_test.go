package plugins

import (
	"slices"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/metrics"
)

// quietPrometheus answers every query with no series.
type quietPrometheus struct{}

func (quietPrometheus) QueryRange(string, string, metrics.Range) (metrics.Result, error) {
	return metrics.Result{}, nil
}

// Two plugins with charts on their overviews: the bug this guards against put
// both plugins' charts on both overviews.
func TestEachOverviewDrawsOnlyItsOwnCharts(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"one", "two"} {
		write(t, dir, id+".json", `{
			"id": "`+id+`",
			"name": "`+id+`",
			"views": [{"id": "pods", "kind": "pods"}],
			"charts": [{"id": "up", "label": "Up", "attach": "overview", "query": "up"}]
		}`)
	}
	cat := Load(dir, nil, nil)

	got := cat.Attachments()
	for _, want := range []string{"plugin:one/overview", "plugin:two/overview"} {
		if !slices.Contains(got, want) {
			t.Errorf("attachments = %v, want %s", got, want)
		}
	}
	if slices.Contains(got, AttachOverview) {
		t.Errorf("attachments = %v, want no bare %q: it belongs to no plugin", got, AttachOverview)
	}

	attach, list := Surface(OverviewSurface("one"), cat.Enabled())
	if attach != AttachOverview || len(list) != 1 || list[0].ID != "one" {
		t.Errorf("Surface(plugin:one/overview) = %q, %d plugins, want %q and only plugin one", attach, len(list), AttachOverview)
	}
	if charts := ChartsFor(list, attach, metrics.Variables{}, metrics.Range{Minutes: 60}, quietPrometheus{}); len(charts) != 1 || charts[0].PluginID != "one" {
		t.Errorf("one's overview draws %+v, want only its own chart", charts)
	}

	if _, list := Surface(AttachOverview, cat.Enabled()); len(list) != 0 {
		t.Errorf("bare overview draws for %d plugins, want none", len(list))
	}
	if _, list := Surface(OverviewSurface("gone"), cat.Enabled()); len(list) != 0 {
		t.Errorf("the overview of a plugin that is not installed draws for %d plugins, want none", len(list))
	}
	// Shared surfaces are untouched.
	if attach, list := Surface(AttachDashboard, cat.Enabled()); attach != AttachDashboard || len(list) != len(cat.Enabled()) {
		t.Errorf("Surface(dashboard) = %q, %d plugins, want all %d", attach, len(list), len(cat.Enabled()))
	}
}

func TestAPluginMayDrawItsOwnOverview(t *testing.T) {
	p, err := validate(Plugin{
		ID:       "x",
		Views:    []View{{ID: "pods", Kind: "pods"}},
		Overview: &Overview{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Overview.Entry != DefaultEntry {
		t.Errorf("entry = %q, want the default %q", p.Overview.Entry, DefaultEntry)
	}
	// A page of its own is code, so it gets the fenced UI like a custom view.
	if p.UI == nil || p.UI.Dir != DefaultUIDir {
		t.Errorf("ui = %+v, want the defaults filled in", p.UI)
	}

	cat := Catalogue{Plugins: []Plugin{p}}
	got, err := cat.ResolveKind("plugin:x/overview")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Overview || !got.Custom || got.Entry != DefaultEntry {
		t.Errorf("resolved = %+v, want an overview that is custom and opens %s", got, DefaultEntry)
	}
}

// Its overview charts are still its own: the page asks for them through the
// bridge and draws them itself.
func TestAnOwnOverviewKeepsItsOverviewCharts(t *testing.T) {
	p, err := validate(Plugin{
		ID:       "x",
		Views:    []View{{ID: "pods", Kind: "pods"}},
		Overview: &Overview{Entry: "home.html"},
		Charts:   []Chart{{ID: "up", Attach: AttachOverview, Query: "up"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cat := Catalogue{Plugins: []Plugin{p}}
	if !slices.Contains(cat.Attachments(), OverviewSurface("x")) {
		t.Errorf("attachments = %v, want the overview's charts still attached", cat.Attachments())
	}
}

// An overview page is all a plugin needs to be worth showing.
func TestAnOverviewAloneIsEnough(t *testing.T) {
	if _, err := validate(Plugin{ID: "x", Overview: &Overview{Entry: "home.html"}}); err != nil {
		t.Errorf("a plugin with only an overview page was refused: %v", err)
	}
}

func TestAnOwnOverviewIsCheckedWhenRead(t *testing.T) {
	cases := map[string]struct {
		plugin Plugin
		want   string
	}{
		"an entry outside the ui folder": {
			Plugin{ID: "x", Views: []View{{ID: "pods", Kind: "pods"}}, Overview: &Overview{Entry: "../escape.html"}},
			"not a file inside its UI folder",
		},
		"an absolute entry": {
			Plugin{ID: "x", Views: []View{{ID: "pods", Kind: "pods"}}, Overview: &Overview{Entry: "/etc/passwd"}},
			"not a file inside its UI folder",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := validate(tc.plugin)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one saying %q", err, tc.want)
			}
		})
	}
}

// A plugin without one keeps the generated page.
func TestTheGeneratedOverviewIsNotCustom(t *testing.T) {
	cat := Load(t.TempDir(), nil, nil)
	got, err := cat.ResolveKind("plugin:flux/overview")
	if err != nil {
		t.Fatal(err)
	}
	if got.Custom || got.Entry != "" {
		t.Errorf("resolved = %+v, want the generated overview", got)
	}
}
