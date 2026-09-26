// Package plugins is the app's second extension point: a solution plugin
// teaches k8sdockside about something installed *in a cluster* -- Argo CD,
// Flux, Prometheus -- and gives it a place of its own in the sidebar rather
// than leaving its custom resources scattered through the definitions tree
// under group names.
//
// Most of a plugin is a JSON file and nothing else: it names resource kinds the
// app already knows how to list, and says how to arrange and summarise them.
// That part is as safe to install as a theme, and keeps working as the app
// grows.
//
// A plugin may also ship views of its own -- HTML and script in a folder beside
// its file, or for a built-in, embedded in the app -- drawn in a sandboxed
// frame. Those are code, and are treated as such wherever they came from: they
// reach the cluster only through a narrow bridge the app answers, only for the
// kinds the plugin declares, and never write without the user saying yes. See
// ui.go.
//
// A plugin is installed on *this machine*. Whether the thing it describes is
// installed in the cluster in front of you is a separate question, asked per
// context and answered plainly -- see Summary. A plugin whose CRDs are absent
// is an ordinary state, not a broken install.
package plugins

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/updates"
)

// Prefix marks a tab opened on one of a plugin's views:
// "plugin:<pluginID>/<viewID>", e.g. "plugin:argocd/applications".
//
// It is a prefixed string for exactly the reason "crd:" is one. A tab's kind is
// persisted, reordered, restored and titled by machinery that never looks
// inside it, so a plugin view becomes a tab without any of that learning a
// second shape. Both sides parse it in one place: here, and the frontend
// catalogue.
const Prefix = "plugin:"

// ViewKind builds the kind string a tab for one view is opened with.
func ViewKind(pluginID, viewID string) string {
	return Prefix + pluginID + "/" + viewID
}

// ParseViewKind splits a "plugin:" kind back into its plugin and view.
func ParseViewKind(kind string) (pluginID, viewID string, ok bool) {
	rest, found := strings.CutPrefix(kind, Prefix)
	if !found {
		return "", "", false
	}
	pluginID, viewID, found = strings.Cut(rest, "/")
	if !found || pluginID == "" || viewID == "" {
		return "", "", false
	}
	return pluginID, viewID, true
}

// The kinds of view a plugin may declare.
const (
	// ViewOverview is the plugin's landing page: what it is, whether this
	// cluster has it, and a live count of what it manages. Every plugin gets
	// one whether or not it asks, because "is this even installed here?" is the
	// first question and it needs somewhere to be answered.
	ViewOverview = "overview"
	// ViewTable is a resource listing, which is most other views.
	ViewTable = "table"
	// ViewCustom is the plugin's own page: a file from its UI folder, drawn in
	// a sandboxed frame. It is how a plugin shows its solution the way that
	// solution is best read -- an application's resource tree, a VM's state
	// -- rather than as rows.
	ViewCustom = "custom"
)

// DefaultEntry is the file a custom view opens when it does not name one.
const DefaultEntry = "index.html"

// DefaultUIDir is the folder, beside the plugin's file, its views are read
// from when it does not name one.
const DefaultUIDir = "ui"

// OverviewID is the view id the generated overview takes. It is reserved: a
// plugin declaring a view of its own by this name is refused rather than
// silently shadowed.
const OverviewID = "overview"

// View is one entry under a plugin in the sidebar, and one tab when opened.
type View struct {
	// ID is stable and appears in the tab's kind, so it is what a restored tab
	// is found by. Renaming one loses its place in a saved session, which is
	// why it is required rather than derived from the label.
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitzero"`
	// Type is ViewTable or ViewCustom; empty means table, which is what most
	// views are.
	Type string `json:"type,omitzero"`
	// Kind is the resource this view lists: a built-in kind name, or a
	// "crd:<plural>.<group>" custom resource. Required for a table view and
	// meaningless on the overview and a custom view.
	Kind string `json:"kind,omitzero"`
	// Entry is the file a custom view opens, relative to the plugin's UI
	// folder. Defaults to DefaultEntry; meaningless on a table view.
	Entry string `json:"entry,omitzero"`
	// Namespace pins the view to one namespace. Empty means every namespace and
	// leaves the tab's own namespace filter free; set, it is where the view
	// opens and the filter is fixed there, because a view that says
	// "Argo CD's controllers" is not answering a question about kube-system.
	Namespace string `json:"namespace,omitzero"`
	// Selector narrows the view to objects carrying certain labels, in the
	// usual `a=b,c in (d,e)` syntax. It is what lets a plugin offer a view of a
	// built-in kind -- the Deployments that are Argo CD's -- rather than only
	// of custom resources nothing else owns.
	Selector string `json:"selector,omitzero"`
	// Focus says a custom view can be opened on one object of a kind, and
	// how to tell the page which. Meaningless on a table view, which is
	// opened on an object by filtering it.
	Focus *Focus `json:"focus,omitzero"`
}

// DefaultFocusHash is the address fragment a focused view is opened with when
// its focus does not say otherwise.
const DefaultFocusHash = "namespace={namespace}&name={name}"

// Focus lets a custom view be opened on one object -- an Application shown
// selected on a board of them -- rather than on the page as a whole. The app
// offers it wherever it has an object of the kind in hand and somewhere to
// send the reader: a search hit, today.
//
// The object reaches the page in its own address, after the #, which is the
// one thing a page already reads before the bridge is up and which it can
// keep for itself as the reader moves about.
type Focus struct {
	// Kind is what the view can be focused on: a built-in name or a
	// "crd:<plural>.<group>" custom resource.
	Kind string `json:"kind"`
	// Hash is the part of the address after the #, with {namespace} and
	// {name} standing for the object's. Both are URL-encoded as they are put
	// in. Defaults to DefaultFocusHash.
	Hash string `json:"hash,omitzero"`
}

// Requirement is a kind the plugin needs the cluster to serve. It is what the
// overview's readiness check is made of, and what decides whether the sidebar
// shows the plugin as present in a given cluster.
type Requirement struct {
	Kind  string `json:"kind"`
	Label string `json:"label,omitzero"`
	// Optional requirements are reported but do not decide whether the plugin
	// counts as installed. Argo CD without ApplicationSets is still Argo CD.
	Optional bool `json:"optional,omitzero"`
	// Namespace and Selector turn "this cluster serves the kind" into "this
	// cluster has these objects". Most plugins need neither: a custom resource
	// is served only where the product that defines it is installed, so the
	// kind alone gives it away. A product that defines none -- Flannel is a
	// DaemonSet, a ConfigMap and nothing else -- would otherwise require only
	// kinds every cluster serves, and read as installed everywhere. With a
	// selector the requirement is met only if something matching it is there.
	//
	// The cost is a list per requirement, so it is worth naming only what
	// actually identifies the product.
	Namespace string `json:"namespace,omitzero"`
	Selector  string `json:"selector,omitzero"`
}

// Card is one live tile on the plugin's overview: how many of a kind there are,
// divided up by one of their own fields.
type Card struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	// GroupBy addresses the field the count is divided by -- see kube.FieldPath
	// for the two shapes it takes. Empty counts without dividing, which is
	// still useful for a kind with no status worth reading.
	GroupBy kube.FieldPath `json:"groupBy,omitzero"`
	// Tones maps a field value to how it should read: "ok", "warn", "error" or
	// "info". A value with no entry is drawn plainly, so a plugin only has to
	// name the ones that mean something.
	Tones map[string]string `json:"tones,omitzero"`
	// Namespace and Selector narrow what is counted, exactly as on a View, so a
	// card can agree with the view it sits above.
	Namespace string `json:"namespace,omitzero"`
	Selector  string `json:"selector,omitzero"`
}

// The surfaces a chart can be attached to, beyond a resource kind.
const (
	// AttachDashboard puts the chart on the cluster's own dashboard, where it
	// is about the cluster rather than about any one object.
	AttachDashboard = "dashboard"
	// AttachOverview puts it on the plugin's own landing page.
	AttachOverview = "overview"
)

// Chart is one time series drawn from the cluster's Prometheus.
//
// A chart is where a plugin stops describing Kubernetes objects and starts
// describing what is happening to them, which is the one thing the API server
// cannot answer. It is still declarative: a query, a label and a unit. The query
// is passed through to Prometheus untouched -- see internal/metrics for why that
// is a different kind of thing from the field paths in Card.
type Chart struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Attach names where the chart is drawn: AttachDashboard, AttachOverview,
	// or a kind, in which case it appears in the detail panel of any object of
	// that kind.
	Attach string `json:"attach"`
	// Query is PromQL. It may refer to the object being drawn for through the
	// variables in metrics.VariableNames -- $namespace, $name, $node -- which
	// are the only things interpolable into it.
	Query string `json:"query"`
	// Legend names the Prometheus label each series is titled by. Empty falls
	// back to the whole label set, which is the honest answer when a query
	// returns several series and the plugin has not said how to tell them
	// apart.
	Legend string `json:"legend,omitzero"`
	// Unit decides how values are written: see ChartUnits. Empty prints the
	// number as it comes.
	Unit string `json:"unit,omitzero"`
	// Description is a sentence under the chart's title, for saying what the
	// query actually measures -- which is rarely obvious from a label like
	// "CPU".
	Description string `json:"description,omitzero"`
}

// ChartUnits are the units a chart may declare. They decide only how a number
// is written -- 1.5 cores, 512 MiB, 87% -- so an unknown one would silently
// print raw numbers, which is why the set is closed and checked on load.
var ChartUnits = []string{"", "cores", "bytes", "bytes/s", "percent", "ops/s", "seconds", "count"}

// UsagePair is the two queries that make up a usage reading. Both or neither:
// half a pair would draw a CPU figure next to a memory figure that is silently
// zero, which is worse than drawing nothing.
type UsagePair struct {
	// CPU must return cores, and Memory bytes -- the units the readings are
	// converted from. A query returning anything else is not wrong in a way
	// this app can detect, only in a way the numbers look wrong.
	CPU    string `json:"cpu,omitzero"`
	Memory string `json:"memory,omitzero"`
}

// Filled reports whether both halves are present.
func (p UsagePair) Filled() bool { return p.CPU != "" && p.Memory != "" }

// UsageQueries let a plugin stand in for metrics-server on a cluster that has
// none, by saying how to ask its Prometheus the same two questions.
//
// These are instant queries over the whole cluster rather than charts, so
// unlike a chart's query they are given no object and may not refer to one:
// there is nothing to substitute, and a `$name` left in would reach Prometheus
// unexpanded. Node is keyed by the `node` label, Pod by `namespace` and `pod`.
type UsageQueries struct {
	Node UsagePair `json:"node,omitzero"`
	Pod  UsagePair `json:"pod,omitzero"`
}

// UI is what a plugin's own views are allowed: where their files are, what
// they may read, and whether they may ask to change anything.
//
// It is declared rather than inferred so the settings view can say, before a
// custom view is ever opened, what that code can reach.
type UI struct {
	// Dir is the folder the views are read from, relative to the plugin's own
	// file. It may not leave that file's folder.
	Dir string `json:"dir,omitzero"`
	// Kinds are what the views may read beyond the kinds the plugin already
	// names in its requirements, views and cards.
	Kinds []string `json:"kinds,omitzero"`
	// Write lets the views ask to merge-patch objects of those kinds. Every
	// patch is shown to the user and applied only when they say yes.
	Write bool `json:"write,omitzero"`
	// Registries lets the views ask the registries of images the cluster runs
	// which tags they have -- anonymously, through the app, since the views
	// themselves cannot reach the network. See registry.Client.
	Registries bool `json:"registries,omitzero"`
	// Services are the in-cluster Services the views may make GET requests
	// to, through the API server. See UIService.
	Services []UIService `json:"services,omitzero"`
	// Readable is every kind the views may read, worked out by the loader --
	// Kinds plus everything else the plugin names -- and ignored on the way
	// in. Both sides check against this one list.
	Readable []string `json:"readable"`
}

// Plugin is one solution the app knows how to show.
type Plugin struct {
	// Schema is where an editor finds the JSON schema for the file. Accepted so
	// a manifest can name it, and otherwise ignored. First, so a file written
	// from this struct names it first.
	Schema  string `json:"$schema,omitzero"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tagline string `json:"tagline,omitzero"`
	// Category is what the plugin is about, in one word from Categories:
	// storage, networking, security and the rest. It is what the settings
	// view groups and filters by, so a manifest that names none is filed
	// under "other" rather than left blank. See categories.go.
	Category string `json:"category,omitzero"`
	Icon     string `json:"icon,omitzero"`
	// Logo is the plugin's own mark -- a file in its ui folder -- shown
	// wherever the app names the plugin, in place of Icon. A plugin for a
	// product is recognised by that product's mark long before its name is
	// read, which a shared icon set cannot do. Checked on load like the pages
	// are, so a manifest naming a file that is not there is refused rather
	// than leaving a broken image in the sidebar.
	Logo string `json:"logo,omitzero"`
	// Author is who wrote the plugin -- a person or a company -- and AuthorURL
	// where to find them. The app credits them wherever it shows the plugin:
	// its card in settings, its overview, and a strip under an overview page of
	// its own, which the plugin's own code cannot draw over.
	Author    string `json:"author,omitzero"`
	AuthorURL string `json:"authorUrl,omitzero"`
	// Docs is a link shown on the overview. Only http(s) is accepted; a plugin
	// file is not allowed to hand the app an arbitrary URL scheme to open.
	Docs string `json:"docs,omitzero"`
	// Links point at what the plugin is about -- the product's own site, its
	// source, its documentation -- and are shown on the plugin's card in
	// settings and on its overview. Held to the same rule as Docs.
	Links []Link `json:"links,omitzero"`
	// Version is the plugin's own version, shown on its card. Optional; when
	// given it is a semantic version, "1.2.0" or "v1.2.0".
	Version string `json:"version,omitzero"`
	// MinAppVersion is the oldest release of this app the plugin works with.
	// A plugin wanting a newer app than the one reading it is refused on load
	// with that said, rather than half-working because a field it relies on
	// means nothing here yet. See LoadAt.
	MinAppVersion string        `json:"minAppVersion,omitzero"`
	Description   string        `json:"description,omitzero"`
	Requires      []Requirement `json:"requires,omitzero"`
	Views         []View        `json:"views"`
	Cards         []Card        `json:"cards,omitzero"`
	Charts        []Chart       `json:"charts,omitzero"`
	// Usage is optional: a plugin that knows a Prometheus can offer it as a
	// stand-in for metrics-server. Absent for almost every plugin.
	Usage *UsageQueries `json:"usage,omitzero"`
	// UI is present when the plugin ships views of its own. See UI.
	UI *UI `json:"ui,omitzero"`
	// Actions are buttons on the action bar of objects of a kind. See actions.go.
	Actions []Action `json:"actions,omitzero"`
	// Sections are the plugin's own panels in the detail view of objects of a
	// kind. Like custom views, their pages come from the plugin's UI files.
	Sections []Section `json:"sections,omitzero"`
	// Overview replaces the generated landing page with one of the plugin's
	// own, from its UI files like a custom view.
	Overview *Overview `json:"overview,omitzero"`

	// Origin is filled in by the loader and ignored on the way in: BuiltinOrigin
	// or the path of the file it was read from.
	Origin string `json:"origin"`
	// Pack is the collection it arrived in, empty for one that came on its own.
	Pack string `json:"pack"`
	// Repo is the git checkout the file is in, filled in by the loader, so
	// the settings view can offer to update it. Empty for everything else.
	Repo string `json:"repo"`
	// Official is set by the loader, and ignored on the way in, for a plugin
	// cloned from the repository of an official entry on the known list. The
	// check is the repository, not the id: a plugin cannot call itself
	// official by taking an official one's name.
	Official bool `json:"official,omitzero"`
	// Disabled is set by the loader for a plugin the user has switched off in
	// settings. A disabled plugin stays in the catalogue rather than being
	// dropped from it, because the settings view has to list it to offer
	// switching it back on; everything that *offers* a plugin reads Enabled
	// instead. Ignored on the way in, like Origin.
	Disabled bool `json:"disabled"`
}

// Overview is a plugin's own landing page, standing in for the one the app
// generates from its requirements, cards and charts.
//
// The generated page is the right answer for a plugin that is only data --
// it is all such a plugin can have, so every one of them gets the same page.
// A plugin that already ships pages of its own can say more about its solution
// than a list of counts, and this is where it does. The page is told whether
// the solution is installed through the bridge's summary call, so "is this
// even in this cluster?" can still be answered first.
type Overview struct {
	// Entry is the file it opens, relative to the plugin's UI folder. Defaults
	// to DefaultEntry.
	Entry string `json:"entry,omitzero"`
}

// Link is one place a plugin points its reader at.
type Link struct {
	// Label is what the link reads as. Defaults to the address's host.
	Label string `json:"label,omitzero"`
	URL   string `json:"url"`
}

// maxLinks bounds a plugin's links. They are a row on a card, not a page of
// bookmarks.
const maxLinks = 8

// BuiltinOrigin marks the plugins that ship with the app.
const BuiltinOrigin = "builtin"

// Builtin reports whether the plugin came with the app rather than off disk.
func (p Plugin) Builtin() bool { return p.Origin == BuiltinOrigin }

// Identity is the plugin's id -- see addons.Identified.
func (p Plugin) Identity() string { return p.ID }

// Source is the file it came from -- see addons.Sourced.
func (p Plugin) Source() string { return p.Origin }

// View returns the view with the given id.
func (p Plugin) View(id string) (View, bool) {
	for _, v := range p.Views {
		if v.ID == id {
			return v, true
		}
	}
	return View{}, false
}

// NeedsNewerApp reports whether the plugin asks for a newer release of this
// app than appVersion, and says so in words when it does.
//
// A version that is not a release -- a development build -- is taken as new
// enough for anything: it is built from a tree at least as new as the last
// release, and refusing plugins there would make them impossible to work on.
func (p Plugin) NeedsNewerApp(appVersion string) (string, bool) {
	if p.MinAppVersion == "" || !updates.IsVersion(appVersion) || !updates.IsVersion(p.MinAppVersion) {
		return "", false
	}
	if updates.Compare(appVersion, p.MinAppVersion) >= 0 {
		return "", false
	}
	who := "this plugin"
	if p.ID != "" {
		who = fmt.Sprintf("plugin %q", p.ID)
	}
	return fmt.Sprintf("%s needs K8s Dockside %s or newer, and this is %s -- update the app to use it",
		who, strings.TrimPrefix(p.MinAppVersion, "v"), strings.TrimPrefix(appVersion, "v")), true
}

// UsageQueriesFor returns the usage queries of the first enabled plugin that
// declares any.
//
// First rather than merged: two plugins pointing at differently configured
// Prometheuses would otherwise have their answers silently mixed into one set
// of numbers.
func UsageQueriesFor(list []Plugin) (UsageQueries, bool) {
	for _, p := range list {
		if p.Disabled || p.Usage == nil {
			continue
		}
		return *p.Usage, true
	}
	return UsageQueries{}, false
}

// ChartsFor returns the plugin's charts drawn on one surface.
func (p Plugin) ChartsFor(attach string) []Chart {
	var out []Chart
	for _, chart := range p.Charts {
		if chart.Attach == attach {
			out = append(out, chart)
		}
	}
	return out
}

// CanRead reports whether the plugin's own views may read a kind.
func (p Plugin) CanRead(kind string) bool {
	return p.UI != nil && slices.Contains(p.UI.Readable, kind)
}

// CanWrite reports whether the plugin's own views may ask to patch a kind.
func (p Plugin) CanWrite(kind string) bool {
	return p.CanRead(kind) && p.UI.Write
}

// CanAskRegistries reports whether the plugin's own views may ask image
// registries about the images a cluster runs.
func (p Plugin) CanAskRegistries() bool {
	return p.UI != nil && p.UI.Registries
}

// UIRoot is the folder on disk the plugin's own views are served from. Only a
// plugin read from a file has one; a built-in's pages are embedded instead --
// see UIFiles, which serves both.
func (p Plugin) UIRoot() (string, bool) {
	if p.UI == nil || p.Builtin() || p.Origin == "" {
		return "", false
	}
	return filepath.Join(filepath.Dir(p.Origin), filepath.FromSlash(p.UI.Dir)), true
}

// UIFiles opens the files the plugin's own views are served from: its ui
// folder on disk, or for a built-in the folder embedded in the app. Call done
// when finished with it.
//
// A folder on disk is opened through os.Root, which refuses a path that
// escapes it through a symlink; an embedded one has nothing to escape to.
func (p Plugin) UIFiles() (files fs.FS, done func(), ok bool) {
	if p.UI == nil {
		return nil, nil, false
	}
	if p.Builtin() {
		sub, err := fs.Sub(builtinFS, builtinUIDir+"/"+p.ID)
		if err != nil {
			return nil, nil, false
		}
		return sub, func() {}, true
	}
	root, ok := p.UIRoot()
	if !ok {
		return nil, nil, false
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, false
	}
	return dir.FS(), func() { _ = dir.Close() }, true
}
