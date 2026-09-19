package plugins

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/kube"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestBuiltinPluginsLoad(t *testing.T) {
	builtin := Builtin()
	if len(builtin) == 0 {
		t.Fatal("no builtin plugins")
	}

	seen := map[string]bool{}
	for _, p := range builtin {
		if seen[p.ID] {
			t.Errorf("builtin id %q appears twice", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" || p.Tagline == "" {
			t.Errorf("builtin %q has no name or tagline; both are shown in the sidebar", p.ID)
		}
		if !p.Builtin() {
			t.Errorf("builtin %q has origin %q", p.ID, p.Origin)
		}
		if len(p.Views) == 0 {
			t.Errorf("builtin %q offers no views", p.ID)
		}
		if len(p.Requires) == 0 {
			t.Errorf("builtin %q declares no requirements, so its overview cannot say whether it is installed", p.ID)
		}
	}
	for _, id := range []string{"argocd", "flux", "prometheus"} {
		if !seen[id] {
			t.Errorf("the %s plugin is missing", id)
		}
	}
}

// Every kind a built-in names has to be one the app can actually open, or the
// view is a row that opens onto an error. A page of its own lists no kind.
func TestBuiltinPluginKindsAreOpenable(t *testing.T) {
	for _, p := range Builtin() {
		for _, v := range p.Views {
			if v.Type == ViewCustom {
				continue
			}
			if !kube.IsKnownKind(v.Kind) {
				t.Errorf("%s/%s lists %q, which is not an openable kind", p.ID, v.ID, v.Kind)
			}
		}
		for _, c := range p.Cards {
			if !kube.IsKnownKind(c.Kind) {
				t.Errorf("%s card %q counts %q, which is not an openable kind", p.ID, c.Label, c.Kind)
			}
			if c.GroupBy != "" && !c.GroupBy.Valid() {
				t.Errorf("%s card %q groups by %q, which is not a field path", p.ID, c.Label, c.GroupBy)
			}
		}
	}
}

func TestViewKindRoundTrips(t *testing.T) {
	kind := ViewKind("argocd", "applications")
	if kind != "plugin:argocd/applications" {
		t.Fatalf("ViewKind = %q", kind)
	}
	plugin, view, ok := ParseViewKind(kind)
	if !ok || plugin != "argocd" || view != "applications" {
		t.Fatalf("ParseViewKind(%q) = %q, %q, %v", kind, plugin, view, ok)
	}

	for _, bad := range []string{"pods", "crd:applications.argoproj.io", "plugin:", "plugin:argocd", "plugin:/applications", "plugin:argocd/"} {
		if _, _, ok := ParseViewKind(bad); ok {
			t.Errorf("ParseViewKind(%q) accepted it", bad)
		}
	}
}

func TestResolveKind(t *testing.T) {
	cat := Catalogue{Plugins: Builtin()}

	got, err := cat.ResolveKind("plugin:argocd/components")
	if err != nil {
		t.Fatalf("ResolveKind: %v", err)
	}
	if got.Kind != "deployments" {
		t.Errorf("kind = %q, want the real kind to subscribe to", got.Kind)
	}
	if got.Selector == "" {
		t.Error("the view's selector was dropped, so the tab would list every Deployment in the cluster")
	}
	if got.Overview {
		t.Error("a table view came back marked as the overview")
	}

	// Every plugin has an overview whether or not it declares one.
	overview, err := cat.ResolveKind("plugin:argocd/overview")
	if err != nil {
		t.Fatalf("resolving the overview: %v", err)
	}
	if !overview.Overview || overview.Kind != "" {
		t.Errorf("overview resolved to %+v, want no kind and Overview set", overview)
	}
}

// A tab restored after its plugin's folder was dropped has to say so. An empty
// table would read as "Argo CD has no applications".
func TestResolveKindExplainsAMissingPlugin(t *testing.T) {
	cat := Catalogue{Plugins: Builtin()}

	_, err := cat.ResolveKind("plugin:nothing-here/applications")
	if err == nil {
		t.Fatal("resolving a view of an uninstalled plugin succeeded")
	}
	if !strings.Contains(err.Error(), "nothing-here") {
		t.Errorf("error %q does not name the missing plugin", err)
	}

	if _, err := cat.ResolveKind("plugin:argocd/not-a-view"); err == nil {
		t.Error("resolving a view the plugin does not have succeeded")
	}
}

func TestValidateRefusesBadPlugins(t *testing.T) {
	view := View{ID: "things", Label: "Things", Kind: "pods"}
	cases := map[string]Plugin{
		"no id":              {Name: "x", Views: []View{view}},
		"id in caps":         {ID: "MyPlugin", Views: []View{view}},
		"nothing to show":    {ID: "empty", Name: "Empty"},
		"unknown kind":       {ID: "x", Views: []View{{ID: "v", Kind: "widgets"}}},
		"view with no kind":  {ID: "x", Views: []View{{ID: "v", Label: "V"}}},
		"view id in caps":    {ID: "x", Views: []View{{ID: "V", Kind: "pods"}}},
		"duplicate view ids": {ID: "x", Views: []View{{ID: "v", Kind: "pods"}, {ID: "v", Kind: "nodes"}}},
		"reserved view id":   {ID: "x", Views: []View{{ID: OverviewID, Kind: "pods"}}},
		"view type":          {ID: "x", Views: []View{{ID: "v", Kind: "pods", Type: "chart"}}},
		"nested plugin view": {ID: "x", Views: []View{{ID: "v", Kind: "plugin:other/thing"}}},
		"bad selector":       {ID: "x", Views: []View{{ID: "v", Kind: "pods", Selector: "!!! nope"}}},
		"bad namespace":      {ID: "x", Views: []View{{ID: "v", Kind: "pods", Namespace: "Not A Namespace"}}},
		"bad requirement":    {ID: "x", Views: []View{view}, Requires: []Requirement{{Kind: "widgets"}}},
		"bad field path":     {ID: "x", Views: []View{view}, Cards: []Card{{Kind: "pods", GroupBy: "status.$$$"}}},
		"bad tone":           {ID: "x", Views: []View{view}, Cards: []Card{{Kind: "pods", Tones: map[string]string{"a": "purple"}}}},
		// A plugin file is not allowed to hand the platform an arbitrary scheme
		// to open.
		"docs link scheme": {ID: "x", Views: []View{view}, Docs: "file:///etc/passwd"},
	}
	for name, plugin := range cases {
		if _, err := validate(plugin); err == nil {
			t.Errorf("%s: validate accepted it", name)
		}
	}
}

func TestValidateFillsInWhatItCan(t *testing.T) {
	p, err := validate(Plugin{ID: "minimal", Views: []View{{ID: "pods", Kind: "pods"}}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if p.Name != "minimal" {
		t.Errorf("name = %q, want the id", p.Name)
	}
	if p.Icon == "" || p.Views[0].Icon == "" {
		t.Error("an icon was left blank, which renders as nothing")
	}
	if p.Views[0].Label != "pods" {
		t.Errorf("label = %q, want the view id", p.Views[0].Label)
	}
	if p.Views[0].Type != ViewTable {
		t.Errorf("type = %q, want %q", p.Views[0].Type, ViewTable)
	}
}

func TestLoadReadsPluginsAndPacks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "solo.json", `{"id": "solo", "name": "Solo", "views": [{"id": "pods", "kind": "pods"}]}`)
	write(t, dir, "pack.json", `{
		"name": "Ops Pack",
		"plugins": [
			{"id": "one", "name": "One", "views": [{"id": "pods", "kind": "pods"}]},
			{"id": "two", "name": "Two", "views": [{"id": "nodes", "kind": "nodes"}]}
		]
	}`)
	write(t, filepath.Join(dir, "cloned"), "plugin.json", `{"id": "three", "name": "Three", "views": [{"id": "pods", "kind": "pods"}]}`)

	cat := Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("problems: %v", cat.Problems)
	}
	for _, id := range []string{"solo", "one", "two", "three"} {
		if _, ok := cat.Find(id); !ok {
			t.Errorf("plugin %q did not load", id)
		}
	}
	if p, _ := cat.Find("one"); p.Pack != "Ops Pack" {
		t.Errorf("pack name is %q, want it carried onto the plugin", p.Pack)
	}
	if _, ok := cat.Find("argocd"); !ok {
		t.Error("loading a folder dropped the builtin plugins")
	}
}

func TestLoadReportsBadFilesWithoutLosingGoodOnes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "broken.json", `{ not json`)
	write(t, dir, "wrong.json", `{"id": "wrong", "views": [{"id": "v", "kind": "widgets"}]}`)
	write(t, dir, "fine.json", `{"id": "fine", "name": "Fine", "views": [{"id": "pods", "kind": "pods"}]}`)

	cat := Load(dir, nil, nil)
	if _, ok := cat.Find("fine"); !ok {
		t.Error("a broken file alongside a good one cost us the good one")
	}
	if len(cat.Problems) != 2 {
		t.Fatalf("got %d problems, want one per bad file: %v", len(cat.Problems), cat.Problems)
	}
}

func TestLoadLetsAUserPluginReplaceABuiltin(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mine.json", `{"id": "argocd", "name": "My Argo", "views": [{"id": "apps", "kind": "pods"}]}`)

	cat := Load(dir, nil, nil)
	p, ok := cat.Find("argocd")
	if !ok {
		t.Fatal("argocd is missing entirely")
	}
	if p.Name != "My Argo" {
		t.Errorf("name = %q, want the user's own -- overriding a builtin by id is how you retune it", p.Name)
	}
	count := 0
	for _, each := range cat.Plugins {
		if each.ID == "argocd" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("argocd appears %d times, want 1", count)
	}
}

func TestExampleIsAPluginThatLoads(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteExample(dir)
	if err != nil {
		t.Fatalf("WriteExample: %v", err)
	}

	cat := Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("the starter plugin does not load: %v", cat.Problems)
	}
	if _, ok := cat.Find("my-solution"); !ok {
		t.Fatalf("the starter plugin written to %s is not in the catalogue", path)
	}

	again, err := WriteExample(dir)
	if err != nil {
		t.Fatalf("second WriteExample: %v", err)
	}
	if again == path {
		t.Errorf("both starters went to %s", path)
	}
}

func TestLoadMarksADisabledPluginWithoutDroppingIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "solo.json", `{"id": "solo", "name": "Solo", "views": [{"id": "pods", "kind": "pods"}]}`)

	cat := Load(dir, nil, []string{"solo", "argocd"})

	// Still in the catalogue: the settings view has to list a switched-off
	// plugin to offer switching it back on.
	solo, ok := cat.Find("solo")
	if !ok {
		t.Fatal("a disabled plugin fell out of the catalogue, so nothing could re-enable it")
	}
	if !solo.Disabled {
		t.Error("solo is disabled in settings but did not come back marked")
	}
	if argocd, _ := cat.Find("argocd"); !argocd.Disabled {
		t.Error("a built-in must be switchable off like any other plugin")
	}

	enabled := cat.Enabled()
	for _, p := range enabled {
		if p.ID == "solo" || p.ID == "argocd" {
			t.Errorf("Enabled() offered %q, which is switched off", p.ID)
		}
	}
	if len(enabled) != len(cat.Plugins)-2 {
		t.Errorf("Enabled() returned %d of %d plugins, want two fewer", len(enabled), len(cat.Plugins))
	}
}

func TestLoadIgnoresADisabledIDThatNamesNothing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "solo.json", `{"id": "solo", "name": "Solo", "views": [{"id": "pods", "kind": "pods"}]}`)

	// A plugin switched off and then deleted leaves its id behind in settings.
	cat := Load(dir, nil, []string{"long-gone"})

	if len(cat.Problems) > 0 {
		t.Fatalf("problems: %v", cat.Problems)
	}
	if len(cat.Enabled()) != len(cat.Plugins) {
		t.Error("an id naming no installed plugin should switch nothing off")
	}
}

func TestResolveKindSaysWhenAPluginIsSwitchedOff(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "solo.json", `{"id": "solo", "name": "Solo", "views": [{"id": "pods", "kind": "pods"}]}`)

	cat := Load(dir, nil, []string{"solo"})

	_, err := cat.ResolveKind(ViewKind("solo", "pods"))
	if err == nil {
		t.Fatal("a tab on a switched-off plugin's view should not resolve")
	}
	// A restored tab on a plugin the user turned off needs telling apart from
	// one whose folder went missing: the fix is a switch, not a lost file.
	if !strings.Contains(err.Error(), "switched off") {
		t.Errorf("error is %q, want it to say the plugin is switched off", err)
	}
}

// The Metrics panel is drawn, or left out entirely, on the strength of this
// list. A switched-off plugin's surfaces have to leave it: otherwise switching
// Prometheus off leaves a Metrics heading behind on the dashboard saying no
// Prometheus was found -- talk of a thing the user has just switched off.
func TestAttachmentsLoseTheSurfacesOfASwitchedOffPlugin(t *testing.T) {
	// Prometheus is the built-in that charts the cluster dashboard. If that
	// ever stops being true this test should be the thing that says so.
	if !slices.Contains(Load(t.TempDir(), nil, nil).Attachments(), AttachDashboard) {
		t.Fatal("no built-in charts the dashboard, so this test is checking nothing")
	}

	got := Load(t.TempDir(), nil, []string{"prometheus"}).Attachments()

	if slices.Contains(got, AttachDashboard) {
		t.Errorf("attachments = %v, want the dashboard gone with the plugin that charts it", got)
	}
	// Its per-object charts go too, or every Pod keeps a Metrics section.
	if slices.Contains(got, "pods") || slices.Contains(got, "nodes") {
		t.Errorf("attachments = %v, want the detail-panel surfaces gone as well", got)
	}
}

// With everything switched off nothing anywhere should draw a chart panel.
func TestAttachmentsAreEmptyWhenEveryPluginIsOff(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "watcher.json", `{
		"id": "watcher",
		"name": "Watcher",
		"views": [{"id": "pods", "kind": "pods"}],
		"charts": [{"id": "cpu", "label": "CPU", "attach": "dashboard", "query": "up"}]
	}`)

	all := Load(dir, nil, nil)
	ids := make([]string, 0, len(all.Plugins))
	for _, p := range all.Plugins {
		ids = append(ids, p.ID)
	}

	if got := Load(dir, nil, ids).Attachments(); len(got) != 0 {
		t.Errorf("attachments = %v, want none", got)
	}
}

// Two plugins charting the same surface must not draw two panels.
func TestAttachmentsNamesEachSurfaceOnce(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"one", "two"} {
		write(t, dir, id+".json", `{
			"id": "`+id+`",
			"name": "`+id+`",
			"views": [{"id": "pods", "kind": "pods"}],
			"charts": [{"id": "cpu", "label": "CPU", "attach": "dashboard", "query": "up"}]
		}`)
	}

	got := Load(dir, nil, nil).Attachments()

	seen := 0
	for _, s := range got {
		if s == "dashboard" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("attachments = %v, want dashboard named once", got)
	}
}

func TestValidateChecksUsageQueries(t *testing.T) {
	// Nothing here checks PromQL syntax: the app has no PromQL parser, for
	// charts either, and a malformed query comes back as Prometheus's own
	// error when it is run. What is checked is everything this app is the only
	// one able to check.
	view := View{ID: "things", Label: "Things", Kind: "pods"}
	cases := map[string]Plugin{
		"empty query": {ID: "x", Views: []View{view}, Usage: &UsageQueries{
			Node: UsagePair{CPU: "   ", Memory: "sum by (node) (x)"},
		}},
		// A usage query is asked for the whole cluster at once, so there is no
		// object to substitute -- a $variable here would reach Prometheus
		// unexpanded and be read as an operator.
		"query with a variable": {ID: "x", Views: []View{view}, Usage: &UsageQueries{
			Node: UsagePair{
				CPU:    `sum by (node) (rate(x{pod="$name"}[5m]))`,
				Memory: "sum by (node) (x)",
			},
		}},
		// Half a pair is worse than none: the used column would show CPU and
		// silently report zero memory.
		"only one of the pair": {ID: "x", Views: []View{view}, Usage: &UsageQueries{
			Node: UsagePair{CPU: "sum by (node) (x)"},
		}},
	}
	for name, plugin := range cases {
		if _, err := validate(plugin); err == nil {
			t.Errorf("%s: validate accepted it", name)
		}
	}
}

func TestValidateAcceptsACompleteUsageBlock(t *testing.T) {
	p, err := validate(Plugin{
		ID:    "prom",
		Views: []View{{ID: "things", Kind: "pods"}},
		Usage: &UsageQueries{
			Node: UsagePair{CPU: " sum by (node) (x) ", Memory: "sum by (node) (y)"},
			Pod:  UsagePair{CPU: "sum by (namespace, pod) (x)", Memory: "sum by (namespace, pod) (y)"},
		},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if p.Usage.Node.CPU != "sum by (node) (x)" {
		t.Errorf("node CPU query = %q, want it trimmed", p.Usage.Node.CPU)
	}
}

func TestUsageQueriesComeFromTheFirstEnabledPluginThatHasThem(t *testing.T) {
	// A plugin the user has switched off must not go on answering, and two
	// plugins declaring usage must not be silently mixed.
	list := []Plugin{
		{ID: "off", Disabled: true, Usage: &UsageQueries{
			Node: UsagePair{CPU: "disabled", Memory: "disabled"},
		}},
		{ID: "none"},
		{ID: "prom", Usage: &UsageQueries{
			Node: UsagePair{CPU: "wanted", Memory: "wanted-mem"},
		}},
	}

	got, ok := UsageQueriesFor(list)

	if !ok {
		t.Fatal("no usage queries found though an enabled plugin declares them")
	}
	if got.Node.CPU != "wanted" {
		t.Errorf("node CPU query = %q, want the enabled plugin's", got.Node.CPU)
	}
}

func TestNoUsageQueriesWhenNoEnabledPluginDeclaresThem(t *testing.T) {
	if _, ok := UsageQueriesFor([]Plugin{{ID: "none"}}); ok {
		t.Error("usage queries were reported where none exist")
	}
}

func TestTheBuiltinPrometheusPluginCarriesUsageQueriesAndCommitmentCharts(t *testing.T) {
	// The snapshot views fall back to these on a cluster with no
	// metrics-server, so a typo that made them vanish would show up only as a
	// permanently empty "used" column.
	var prom Plugin
	for _, p := range Builtin() {
		if p.ID == "prometheus" {
			prom = p
		}
	}
	if prom.ID == "" {
		t.Fatal("no builtin prometheus plugin")
	}

	if prom.Usage == nil {
		t.Fatal("the prometheus plugin declares no usage queries")
	}
	if !prom.Usage.Node.Filled() {
		t.Errorf("node usage queries = %+v, want both filled", prom.Usage.Node)
	}
	if !prom.Usage.Pod.Filled() {
		t.Errorf("pod usage queries = %+v, want both filled", prom.Usage.Pod)
	}

	// One requested/limits/used chart per resource on each of the three
	// surfaces.
	//
	// Checked by id rather than by counting the charts whose legend is
	// "series". That legend only says a query labels its own lines, which the
	// storage and disk-throughput charts do too -- counting it made adding any
	// multi-series chart fail a test about commitment.
	ids := map[string]bool{}
	for _, c := range prom.Charts {
		ids[c.ID] = true
	}
	for _, id := range []string{
		"cluster-cpu-commitment", "cluster-memory-commitment",
		"node-cpu-commitment", "node-memory-commitment",
		"pod-cpu-commitment", "pod-memory-commitment",
	} {
		if !ids[id] {
			t.Errorf("the prometheus plugin has no %q chart", id)
		}
	}
}

// The Prometheus overview is where somebody goes to ask "is monitoring itself
// healthy", and Prometheus can always answer that about itself -- these need
// no exporter beyond the server. So the plugin has to chart there, or the
// page is a list of CRD counts.
func TestTheBuiltinPrometheusPluginChartsItsOwnHealthOnItsOverview(t *testing.T) {
	var prom Plugin
	for _, p := range Builtin() {
		if p.ID == "prometheus" {
			prom = p
		}
	}
	if prom.ID == "" {
		t.Fatal("no builtin prometheus plugin")
	}

	overview := prom.ChartsFor(AttachOverview)
	if len(overview) < 4 {
		t.Fatalf("the prometheus overview has %d charts, want at least targets, ingestion, series and alerts", len(overview))
	}
	for _, want := range []string{"up", "prometheus_tsdb_head_samples_appended_total", "prometheus_tsdb_head_series", "ALERTS"} {
		found := false
		for _, c := range overview {
			if strings.Contains(c.Query, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no overview chart asks about %s", want)
		}
	}
}

// A chart of CPU usage cannot answer the question people actually arrive with
// mid-incident: a container pinned against its limit and one running
// comfortably under it draw the same line. Throttling and pressure are where
// the difference shows, so the built-in plugin carries them on every surface it
// charts CPU on -- including namespaces, which has no other reason to have a
// Metrics panel at all.
func TestTheBuiltinPrometheusPluginChartsCPUDelayEverywhereItChartsCPU(t *testing.T) {
	var prom Plugin
	for _, p := range Builtin() {
		if p.ID == "prometheus" {
			prom = p
		}
	}
	if prom.ID == "" {
		t.Fatal("no builtin prometheus plugin")
	}

	for _, attach := range []string{AttachDashboard, "nodes", "pods", "namespaces"} {
		var throttling, pressure bool
		for _, chart := range prom.ChartsFor(attach) {
			if strings.Contains(chart.Query, "container_cpu_cfs_throttled") {
				throttling = true
			}
			if strings.Contains(chart.Query, "_pressure_") {
				pressure = true
				// PSI and throttle ratios are fractions of wall time, and the
				// frontend writes a percent by multiplying by a hundred. A
				// chart that returned 0-100 here would read as 8,700%.
				if chart.Unit != "percent" {
					t.Errorf("%s chart %q has unit %q, want percent", attach, chart.ID, chart.Unit)
				}
			}
		}
		if !throttling {
			t.Errorf("%s has no CFS throttling chart", attach)
		}
		if !pressure {
			t.Errorf("%s has no pressure (PSI) chart", attach)
		}
	}
}

// Storage was the dimension the app could not draw at all: CPU and memory had
// charts on every surface and disk had none, on a cluster whose nodes always
// have one under them whether or not it has a StorageClass.
func TestTheBuiltinPrometheusPluginChartsStorageEverywhereItChartsCPU(t *testing.T) {
	prom := builtinPrometheus(t)

	surfaces := map[string][]string{
		AttachDashboard: {"cluster-storage", "cluster-disk-io"},
		"nodes":         {"node-storage", "node-disk-io"},
		"pods":          {"pod-storage", "pod-disk-io"},
		"namespaces":    {"namespace-storage", "namespace-disk-io"},
	}
	ids := chartIDs(prom)
	for surface, want := range surfaces {
		for _, id := range want {
			if !ids[id] {
				t.Errorf("%s has no %q chart", surface, id)
			}
		}
	}
}

// The workload kinds had no charts of any sort. A Deployment is the object
// people actually look at -- "is my rollout healthy" is asked of it far more
// often than of the pods underneath.
func TestTheBuiltinPrometheusPluginChartsTheWorkloadKinds(t *testing.T) {
	prom := builtinPrometheus(t)

	for _, kind := range []string{"deployments", "statefulsets", "daemonsets", "replicasets"} {
		charts := prom.ChartsFor(kind)
		if len(charts) == 0 {
			t.Errorf("%s has no charts at all", kind)
			continue
		}
		// The replica chart is the exact one: kube-state-metrics publishes the
		// counts under the workload's own name, with nothing to infer.
		var hasReplicas bool
		for _, c := range charts {
			if strings.HasSuffix(c.ID, "-replicas") {
				hasReplicas = true
			}
		}
		if !hasReplicas {
			t.Errorf("%s has no replica chart, which is the one reading that needs no guessing", kind)
		}
	}
}

// A PVC is where "how full is it" is actually asked, and the answer is in the
// kubelet's volume stats rather than in the claim.
func TestTheBuiltinPrometheusPluginChartsPersistentVolumeClaims(t *testing.T) {
	prom := builtinPrometheus(t)

	if charts := prom.ChartsFor("persistentvolumeclaims"); len(charts) == 0 {
		t.Error("persistent volume claims have no charts")
	}
	if ids := chartIDs(prom); !ids["pvc-full"] || !ids["pvc-inodes"] {
		// Inodes get their own line because a volume can run out of them while
		// it still has bytes free, and it fails writes exactly as if it were full.
		t.Error("a PVC should chart both how full it is and its inodes")
	}
}

// A chart drawn for one object may only ask about that object. The loader
// enforces it, but the workload charts interpolate $name into a regex rather
// than into an equality matcher, which is the case worth pinning.
func TestEveryWorkloadChartAsksOnlyAboutItsOwnObject(t *testing.T) {
	prom := builtinPrometheus(t)

	for _, kind := range []string{"deployments", "statefulsets", "daemonsets", "replicasets"} {
		for _, c := range prom.ChartsFor(kind) {
			if !strings.Contains(c.Query, "$name") {
				t.Errorf("chart %q is drawn for one %s but its query never names it", c.ID, kind)
			}
			if !strings.Contains(c.Query, "$namespace") {
				t.Errorf("chart %q does not scope to the object's namespace, so it would answer for every namespace at once", c.ID)
			}
		}
	}
}

func builtinPrometheus(t *testing.T) Plugin {
	t.Helper()
	for _, p := range Builtin() {
		if p.ID == "prometheus" {
			return p
		}
	}
	t.Fatal("no builtin prometheus plugin")
	return Plugin{}
}

func chartIDs(p Plugin) map[string]bool {
	out := map[string]bool{}
	for _, c := range p.Charts {
		out[c.ID] = true
	}
	return out
}
