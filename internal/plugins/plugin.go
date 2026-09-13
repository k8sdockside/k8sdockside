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
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rogerwesterbo/k8sdockside/internal/addons"
	"github.com/rogerwesterbo/k8sdockside/internal/kube"
	"github.com/rogerwesterbo/k8sdockside/internal/metrics"
	"github.com/rogerwesterbo/k8sdockside/internal/updates"
	"k8s.io/apimachinery/pkg/labels"
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
	Icon    string `json:"icon,omitzero"`
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

// Pack is a file carrying several plugins, which is how a collection is
// distributed as one file. Mirrors themes.Pack; the two extension points are
// installed the same way on purpose.
type Pack struct {
	Name    string   `json:"name,omitzero"`
	Author  string   `json:"author,omitzero"`
	Version string   `json:"version,omitzero"`
	Plugins []Plugin `json:"plugins"`
}

// Problem is a plugin file that could not be used, and why -- addons.Problem
// under a local name, so the settings view renders one shape whatever failed.
type Problem = addons.Problem

// Catalogue is everything the sidebar and the settings view need: what is
// installed on this machine, where it was read from, and what would not load.
type Catalogue struct {
	Plugins []Plugin `json:"plugins"`
	// Dir is the folder plugins are read from by default.
	Dir      string    `json:"dir"`
	Folders  []string  `json:"folders"`
	Problems []Problem `json:"problems"`
}

// Enabled returns the plugins that are switched on, in catalogue order.
//
// This is what the sidebar, the charts and the overview go through. Find and
// Plugins deliberately still see everything: turning a plugin off hides what it
// offers, it does not uninstall it.
func (c Catalogue) Enabled() []Plugin {
	out := make([]Plugin, 0, len(c.Plugins))
	for _, p := range c.Plugins {
		if !p.Disabled {
			out = append(out, p)
		}
	}
	return out
}

// Attachments names every surface some enabled plugin draws a chart on: a
// resource kind, AttachDashboard, or one plugin's overview as OverviewSurface
// names it.
//
// It hangs off the catalogue rather than taking a list because it is what
// decides whether a chart panel is drawn at all, and a caller that passed the
// unfiltered plugins would leave a Metrics heading on the dashboard of a user
// who has just switched the only charting plugin off.
func (c Catalogue) Attachments() []string {
	seen := map[string]bool{}
	var out []string
	for _, plugin := range c.Enabled() {
		for _, chart := range plugin.Charts {
			surface := chart.Attach
			if surface == AttachOverview {
				surface = OverviewSurface(plugin.ID)
			}
			if seen[surface] {
				continue
			}
			seen[surface] = true
			out = append(out, surface)
		}
	}
	return out
}

// OverviewSurface is what one plugin's overview is called as a chart surface:
// its overview tab's own kind, "plugin:<id>/overview".
//
// The dashboard and a pod's detail panel are the same surface whichever plugin
// draws on them, so every plugin's charts for them belong together. An
// overview is not: it is one plugin's page, and bare "overview" as a surface
// put every plugin's overview charts on every plugin's overview.
func OverviewSurface(pluginID string) string {
	return ViewKind(pluginID, OverviewID)
}

// Surface turns a surface a chart panel asks for into the attachment its
// charts name and the plugins that may draw there. A plugin's overview is
// asked for as OverviewSurface and narrows the list to that one plugin; bare
// AttachOverview belongs to no plugin and draws nothing. Every other surface
// is shared, and keeps the list as it is.
func Surface(surface string, list []Plugin) (string, []Plugin) {
	if pluginID, viewID, ok := ParseViewKind(surface); ok && viewID == OverviewID {
		for _, p := range list {
			if p.ID == pluginID {
				return AttachOverview, []Plugin{p}
			}
		}
		return AttachOverview, nil
	}
	if surface == AttachOverview {
		return AttachOverview, nil
	}
	return surface, list
}

// Find returns the plugin with the given id.
func (c Catalogue) Find(id string) (Plugin, bool) {
	for _, p := range c.Plugins {
		if p.ID == id {
			return p, true
		}
	}
	return Plugin{}, false
}

// Resolve turns a "plugin:" tab kind into the plugin and view it names.
func (c Catalogue) Resolve(kind string) (Plugin, View, bool) {
	pluginID, viewID, ok := ParseViewKind(kind)
	if !ok {
		return Plugin{}, View{}, false
	}
	plugin, ok := c.Find(pluginID)
	if !ok {
		return Plugin{}, View{}, false
	}
	view, ok := plugin.View(viewID)
	if !ok {
		return Plugin{}, View{}, false
	}
	return plugin, view, true
}

// validate checks a plugin is usable and returns it normalised. As with a
// theme, it is forgiving about what is left out and strict about what is put
// in: a missing label has a defensible answer, a kind that does not exist does
// not.
//
// Past the id, every mistake is gathered rather than returned on the first,
// one per line of the error: someone writing a plugin should see all of what
// is wrong with the file at once, not one thing per reload.
func validate(p Plugin) (Plugin, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)

	// Every other message names the plugin by its id, so without a usable one
	// there is nothing further worth saying.
	if p.ID == "" {
		return p, fmt.Errorf("plugin has no id")
	}
	if !addons.ValidID(p.ID) {
		return p, fmt.Errorf("plugin id %q must be lowercase letters, digits and dashes", p.ID)
	}

	var errs []error
	fail := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	if p.Name == "" {
		p.Name = p.ID
	}
	if p.Docs != "" && !webLink(p.Docs) {
		fail(fmt.Errorf("plugin %q has a docs link that is not http(s): %q", p.ID, p.Docs))
	}
	fail(validateLinks(&p))
	fail(validateAuthor(&p))
	fail(validateVersions(&p))
	fail(checkIcon(p.ID, "itself", p.Icon))
	if p.Icon == "" {
		p.Icon = "puzzle"
	}

	for i, req := range p.Requires {
		req.Kind = strings.TrimSpace(req.Kind)
		if !kube.IsKnownKind(req.Kind) {
			fail(fmt.Errorf("plugin %q requires %q, which is not a kind this app can open", p.ID, req.Kind))
			continue
		}
		if req.Label == "" {
			req.Label = req.Kind
		}
		p.Requires[i] = req
	}

	seen := map[string]bool{}
	views := make([]View, 0, len(p.Views))
	for _, view := range p.Views {
		view, err := validateView(p.ID, view)
		if err != nil {
			fail(err)
			continue
		}
		if seen[view.ID] {
			fail(fmt.Errorf("plugin %q has two views with id %q", p.ID, view.ID))
			continue
		}
		seen[view.ID] = true
		views = append(views, view)
	}
	p.Views = views

	for i, card := range p.Cards {
		card, err := validateCard(p.ID, card)
		if err != nil {
			fail(err)
			continue
		}
		p.Cards[i] = card
	}

	seenCharts := map[string]bool{}
	for i, chart := range p.Charts {
		chart, err := validateChart(p.ID, chart)
		if err != nil {
			fail(err)
			continue
		}
		if seenCharts[chart.ID] {
			fail(fmt.Errorf("plugin %q has two charts with id %q", p.ID, chart.ID))
			continue
		}
		seenCharts[chart.ID] = true
		p.Charts[i] = chart
	}

	if p.Usage != nil {
		usage, err := validateUsage(p.ID, *p.Usage)
		fail(err)
		p.Usage = &usage
	}

	fail(validateActions(&p))
	fail(validateSections(&p))
	fail(validateOverview(&p))
	fail(validateUI(&p))

	if len(errs) > 0 {
		return p, errors.Join(errs...)
	}
	// Only asked of a plugin that is otherwise sound: one whose every view was
	// just refused would read as empty, and saying so would be beside the point.
	if len(p.Views) == 0 && len(p.Cards) == 0 && len(p.Charts) == 0 && len(p.Actions) == 0 && len(p.Sections) == 0 && p.Overview == nil {
		return p, fmt.Errorf("plugin %q has no views, actions or sections, nothing to summarise and nothing to chart, so there would be nothing to show", p.ID)
	}
	return p, nil
}

// validateLinks checks a plugin's links: http(s) only, like Docs, and a label
// for each, taken from the address where the file gives none.
func validateLinks(p *Plugin) error {
	if len(p.Links) > maxLinks {
		return fmt.Errorf("plugin %q has %d links; at most %d are shown, so trim the list", p.ID, len(p.Links), maxLinks)
	}
	var errs []error
	for i, link := range p.Links {
		link.Label = strings.TrimSpace(link.Label)
		link.URL = strings.TrimSpace(link.URL)
		parsed, err := url.Parse(link.URL)
		if err != nil || !webLink(link.URL) || parsed.Host == "" {
			errs = append(errs, fmt.Errorf("plugin %q has a link %q that is not an http(s) address", p.ID, link.URL))
			continue
		}
		if link.Label == "" {
			link.Label = parsed.Host
		}
		p.Links[i] = link
	}
	return errors.Join(errs...)
}

// maxAuthor is how long an author's name may be: it is written on one line
// beside the plugin's name.
const maxAuthor = 80

// validateAuthor checks who the plugin credits. The name is shown as it is
// written, so it is kept to a line; the address, like every other link a
// plugin gives, is http(s) only, and needs a name to hang on.
func validateAuthor(p *Plugin) error {
	p.Author = strings.TrimSpace(p.Author)
	p.AuthorURL = strings.TrimSpace(p.AuthorURL)

	var errs []error
	if n := len([]rune(p.Author)); n > maxAuthor {
		errs = append(errs, fmt.Errorf("plugin %q names an author %d characters long; keep it to %d", p.ID, n, maxAuthor))
	}
	if p.AuthorURL != "" {
		parsed, err := url.Parse(p.AuthorURL)
		switch {
		case err != nil || !webLink(p.AuthorURL) || parsed.Host == "":
			errs = append(errs, fmt.Errorf("plugin %q has an authorUrl %q that is not an http(s) address", p.ID, p.AuthorURL))
		case p.Author == "":
			errs = append(errs, fmt.Errorf("plugin %q has an authorUrl but no author to link it from", p.ID))
		}
	}
	return errors.Join(errs...)
}

// validateVersions checks the plugin's own version and the app version it
// asks for are versions at all. Whether this app is new enough is a separate
// question, asked by the loader, which knows what this app is.
func validateVersions(p *Plugin) error {
	p.Version = strings.TrimSpace(p.Version)
	p.MinAppVersion = strings.TrimSpace(p.MinAppVersion)

	var errs []error
	if p.Version != "" && !updates.IsVersion(p.Version) {
		errs = append(errs, fmt.Errorf("plugin %q has version %q; it must be a semantic version such as \"1.2.0\"", p.ID, p.Version))
	}
	if p.MinAppVersion != "" && !updates.IsVersion(p.MinAppVersion) {
		errs = append(errs, fmt.Errorf("plugin %q asks for app version %q; minAppVersion must be a release such as \"0.0.15\"", p.ID, p.MinAppVersion))
	}
	return errors.Join(errs...)
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

// validateUsage checks a plugin's usage queries, which are held to a stricter
// rule than a chart's: they are asked for the whole cluster at once, so they
// are given no object and may not refer to one.
func validateUsage(pluginID string, u UsageQueries) (UsageQueries, error) {
	pairs := []struct {
		name string
		pair *UsagePair
	}{
		{"node", &u.Node},
		{"pod", &u.Pod},
	}

	declared := false
	for _, p := range pairs {
		p.pair.CPU = strings.TrimSpace(p.pair.CPU)
		p.pair.Memory = strings.TrimSpace(p.pair.Memory)

		if p.pair.CPU == "" && p.pair.Memory == "" {
			continue
		}
		declared = true
		if !p.pair.Filled() {
			return u, fmt.Errorf("plugin %q gives only one of the %s usage queries; both cpu and memory are needed", pluginID, p.name)
		}
		for what, query := range map[string]string{"cpu": p.pair.CPU, "memory": p.pair.Memory} {
			if err := metrics.CheckQuery(query); err != nil {
				return u, fmt.Errorf("plugin %q has a bad %s %s usage query: %w", pluginID, p.name, what, err)
			}
			// PromQL has no $ of its own, so anything holding one is a chart
			// variable that nothing here will expand.
			if strings.ContainsRune(query, '$') {
				return u, fmt.Errorf("plugin %q has a %s %s usage query using a variable; usage queries cover the whole cluster and are given no object to substitute", pluginID, p.name, what)
			}
		}
	}

	if !declared {
		return u, fmt.Errorf("plugin %q has a usage block with no queries in it", pluginID)
	}
	return u, nil
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

func validateChart(pluginID string, c Chart) (Chart, error) {
	c.ID = strings.TrimSpace(c.ID)
	c.Label = strings.TrimSpace(c.Label)
	c.Attach = strings.TrimSpace(c.Attach)
	c.Query = strings.TrimSpace(c.Query)

	if !addons.ValidID(c.ID) {
		return c, fmt.Errorf("plugin %q has a chart with id %q, which must be lowercase letters, digits and dashes", pluginID, c.ID)
	}
	if c.Label == "" {
		c.Label = c.ID
	}
	switch c.Attach {
	case AttachDashboard, AttachOverview:
	case "":
		return c, fmt.Errorf("plugin %q has a chart %q that does not say where it is drawn", pluginID, c.ID)
	default:
		if !kube.IsKnownKind(c.Attach) {
			return c, fmt.Errorf("plugin %q attaches chart %q to %q, which is neither %q, %q, nor a kind this app can open",
				pluginID, c.ID, c.Attach, AttachDashboard, AttachOverview)
		}
	}
	// Checked when the file is read rather than when the chart is drawn, so a
	// typo is reported against the plugin instead of appearing as an empty box.
	if err := metrics.CheckQuery(c.Query); err != nil {
		return c, fmt.Errorf("plugin %q, chart %q: %w", pluginID, c.ID, err)
	}
	// A chart drawn for the cluster has no object to name, so a query wanting
	// one would always come out empty.
	if c.Attach == AttachDashboard || c.Attach == AttachOverview {
		if strings.Contains(c.Query, "$name") || strings.Contains(c.Query, "$namespace") || strings.Contains(c.Query, "$node") {
			return c, fmt.Errorf("plugin %q, chart %q is drawn for the whole cluster but its query asks about one object", pluginID, c.ID)
		}
	}
	if !slices.Contains(ChartUnits, c.Unit) {
		return c, fmt.Errorf("plugin %q, chart %q has unit %q; it must be one of %s",
			pluginID, c.ID, c.Unit, strings.Join(quoted(ChartUnits[1:]), ", "))
	}
	return c, nil
}

// quoted renders a list for an error message.
func quoted(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strconv.Quote(v))
	}
	return out
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

func validateView(pluginID string, v View) (View, error) {
	v.ID = strings.TrimSpace(v.ID)
	v.Label = strings.TrimSpace(v.Label)
	v.Kind = strings.TrimSpace(v.Kind)

	if !addons.ValidID(v.ID) {
		return v, fmt.Errorf("plugin %q has a view with id %q, which must be lowercase letters, digits and dashes", pluginID, v.ID)
	}
	if v.ID == OverviewID {
		return v, fmt.Errorf("plugin %q declares a view called %q, which is the name of the overview every plugin already has", pluginID, OverviewID)
	}
	if v.Label == "" {
		v.Label = v.ID
	}
	if err := checkIcon(pluginID, fmt.Sprintf("view %q", v.ID), v.Icon); err != nil {
		return v, err
	}
	if v.Icon == "" {
		v.Icon = "puzzle"
	}
	if v.Type == "" {
		v.Type = ViewTable
	}
	if v.Type == ViewCustom {
		return validateCustomView(pluginID, v)
	}
	if v.Type != ViewTable {
		return v, fmt.Errorf("plugin %q has a view of type %q; only %q or %q may be declared", pluginID, v.Type, ViewTable, ViewCustom)
	}
	if v.Entry != "" {
		return v, fmt.Errorf("plugin %q has a table view %q with an entry file; only a %q view opens one", pluginID, v.ID, ViewCustom)
	}
	if v.Kind == "" {
		return v, fmt.Errorf("plugin %q has a view %q with no kind to list", pluginID, v.ID)
	}
	if strings.HasPrefix(v.Kind, Prefix) {
		return v, fmt.Errorf("plugin %q has a view %q pointing at another plugin's view", pluginID, v.ID)
	}
	if !kube.IsKnownKind(v.Kind) {
		return v, fmt.Errorf("plugin %q has a view %q on %q, which is not a kind this app can open", pluginID, v.ID, v.Kind)
	}
	if err := checkFilter(v.Namespace, v.Selector); err != nil {
		return v, fmt.Errorf("plugin %q, view %q: %w", pluginID, v.ID, err)
	}
	return v, nil
}

// validateCustomView checks a view that opens one of the plugin's own files.
// It lists nothing itself, so the fields that narrow a listing are refused
// rather than silently ignored.
func validateCustomView(pluginID string, v View) (View, error) {
	if v.Kind != "" || v.Namespace != "" || v.Selector != "" {
		return v, fmt.Errorf("plugin %q has a custom view %q with a kind, namespace or selector; a custom view reads what it needs through the bridge instead", pluginID, v.ID)
	}
	v.Entry = strings.TrimSpace(v.Entry)
	if v.Entry == "" {
		v.Entry = DefaultEntry
	}
	if !fs.ValidPath(v.Entry) || v.Entry == "." {
		return v, fmt.Errorf("plugin %q has a custom view %q opening %q, which is not a file inside its UI folder", pluginID, v.ID, v.Entry)
	}
	return v, nil
}

// validateOverview checks a plugin's own landing page.
//
// Charts attached to "overview" stay allowed: the page asks for them through
// the bridge's charts call and draws them its own way, since the generated
// panel they would otherwise sit in is not on screen.
func validateOverview(p *Plugin) error {
	if p.Overview == nil {
		return nil
	}
	entry := strings.TrimSpace(p.Overview.Entry)
	if entry == "" {
		entry = DefaultEntry
	}
	if !fs.ValidPath(entry) || entry == "." {
		return fmt.Errorf("plugin %q has an overview opening %q, which is not a file inside its UI folder", p.ID, entry)
	}
	p.Overview = &Overview{Entry: entry}
	return nil
}

// validateUI checks what a plugin's own views may do, and works out the one
// list of kinds they may read.
//
// A plugin with a custom view and no ui block gets one with the defaults, so
// the simplest plugin with a view of its own is still just "type": "custom".
func validateUI(p *Plugin) error {
	hasCustom := slices.ContainsFunc(p.Views, func(v View) bool { return v.Type == ViewCustom }) ||
		len(p.Sections) > 0 || p.Overview != nil
	if p.UI == nil {
		if !hasCustom {
			return nil
		}
		p.UI = &UI{}
	}

	ui := *p.UI
	ui.Dir = strings.TrimSpace(ui.Dir)
	if ui.Dir == "" {
		ui.Dir = DefaultUIDir
	}
	if !fs.ValidPath(ui.Dir) || ui.Dir == "." {
		return fmt.Errorf("plugin %q has a ui folder %q, which is not a folder beside its file", p.ID, ui.Dir)
	}

	for i, kind := range ui.Kinds {
		kind = strings.TrimSpace(kind)
		if !kube.IsKnownKind(kind) {
			return fmt.Errorf("plugin %q lets its views read %q, which is not a kind this app can open", p.ID, kind)
		}
		if kind == unreadableKind {
			return fmt.Errorf("plugin %q lets its views read %s, which no plugin view may read", p.ID, unreadableKind)
		}
		ui.Kinds[i] = kind
	}

	ui.Readable = readableKinds(*p, ui.Kinds)
	p.UI = &ui
	return nil
}

// unreadableKind is kept away from plugin views whatever they declare. Secrets
// are the one kind whose contents are the credential, and a view reading them
// could hand them anywhere.
const unreadableKind = "secrets"

// readableKinds is every kind a plugin names anywhere, plus the extras its UI
// asks for, once each and in the order first named.
func readableKinds(p Plugin, extra []string) []string {
	var out []string
	add := func(kind string) {
		if kind == "" || kind == unreadableKind || strings.HasPrefix(kind, Prefix) || slices.Contains(out, kind) {
			return
		}
		out = append(out, kind)
	}
	for _, req := range p.Requires {
		add(req.Kind)
	}
	for _, view := range p.Views {
		add(view.Kind)
	}
	for _, card := range p.Cards {
		add(card.Kind)
	}
	for _, section := range p.Sections {
		add(section.Kind)
	}
	for _, action := range p.Actions {
		add(action.Kind)
		add(action.Request.Kind)
	}
	for _, kind := range extra {
		add(kind)
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

func validateCard(pluginID string, c Card) (Card, error) {
	c.Kind = strings.TrimSpace(c.Kind)
	c.Label = strings.TrimSpace(c.Label)

	if !kube.IsKnownKind(c.Kind) {
		return c, fmt.Errorf("plugin %q has a card on %q, which is not a kind this app can open", pluginID, c.Kind)
	}
	if c.Label == "" {
		c.Label = c.Kind
	}
	if c.GroupBy != "" && !c.GroupBy.Valid() {
		return c, fmt.Errorf("plugin %q has a card grouped by %q, which is not a field path", pluginID, c.GroupBy)
	}
	for value, tone := range c.Tones {
		switch tone {
		case "ok", "warn", "error", "info":
		default:
			return c, fmt.Errorf("plugin %q gives %q the tone %q; it must be ok, warn, error or info", pluginID, value, tone)
		}
	}
	if err := checkFilter(c.Namespace, c.Selector); err != nil {
		return c, fmt.Errorf("plugin %q, card %q: %w", pluginID, c.Label, err)
	}
	return c, nil
}

// checkFilter validates the namespace and selector a view or card narrows with.
// The selector is parsed here, when the file is read, so a malformed one is
// reported against the plugin rather than surfacing later as a tab that will
// not open.
func checkFilter(namespace, selector string) error {
	if namespace != "" && !dnsLabel(namespace) {
		return fmt.Errorf("%q is not a namespace name", namespace)
	}
	if selector != "" {
		if _, err := labels.Parse(selector); err != nil {
			return fmt.Errorf("label selector %q: %w", selector, err)
		}
	}
	return nil
}

// webLink reports whether a URL is one the app is willing to open. A plugin
// file comes from outside the app, and handing the platform an arbitrary scheme
// to open is not something a list of colours and kind names has any business
// doing.
func webLink(address string) bool {
	return strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
}

// dnsLabel is the Kubernetes name format a namespace has to be in.
func dnsLabel(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(s)-1:
		default:
			return false
		}
	}
	return true
}

// Resolved is what a "plugin:" tab kind actually means: the kind to list, and
// the filters the view pins around it.
//
// It is one value rather than three returns because the frontend needs it too:
// a tab whose view fixes a namespace must not draw a namespace picker that
// looks like it would work.
type Resolved struct {
	// Kind is the real kind to subscribe to -- a built-in name or a "crd:" one.
	Kind string `json:"kind"`
	// Namespace is fixed for this view, or empty to leave the tab's own filter
	// free.
	Namespace string `json:"namespace"`
	Selector  string `json:"selector"`
	// PluginID and ViewID are carried back so the frontend can title the tab
	// and find its way to the plugin without parsing the kind a second time.
	PluginID   string `json:"pluginId"`
	PluginName string `json:"pluginName"`
	ViewID     string `json:"viewId"`
	Label      string `json:"label"`
	Icon       string `json:"icon"`
	// Overview is true for the plugin's landing page, which is not a listing at
	// all and has no Kind.
	Overview bool `json:"overview"`
	// Custom is true for one of the plugin's own views, which has no Kind
	// either: it is a file from the plugin's UI folder, named by Entry.
	Custom bool   `json:"custom"`
	Entry  string `json:"entry"`
}

// ResolveKind turns a "plugin:" tab kind into what it names.
//
// A kind naming a plugin that is not installed is an error with a sentence in
// it rather than a silent empty tab: it is what a restored session looks like
// after the plugin's folder was dropped, and the reader needs telling that the
// tab is fine and the plugin is missing.
func (c Catalogue) ResolveKind(kind string) (Resolved, error) {
	pluginID, viewID, ok := ParseViewKind(kind)
	if !ok {
		return Resolved{}, fmt.Errorf("%q does not name a plugin view", kind)
	}

	plugin, ok := c.Find(pluginID)
	if !ok {
		return Resolved{}, fmt.Errorf("no plugin called %q is installed -- the folder it came from may have been removed", pluginID)
	}
	// Told apart from the missing case on purpose: a restored tab on a plugin
	// the user switched off is one switch away from working again, and saying
	// "not installed" would send them looking for a file that is right there.
	if plugin.Disabled {
		return Resolved{}, fmt.Errorf("the %s plugin is switched off in Settings", plugin.Name)
	}

	// Every plugin has an overview whether or not it declares one, so it is
	// answered here rather than looked up. One the plugin draws itself is
	// also custom, and opens its own page.
	if viewID == OverviewID {
		resolved := Resolved{
			PluginID:   plugin.ID,
			PluginName: plugin.Name,
			ViewID:     OverviewID,
			Label:      plugin.Name,
			Icon:       plugin.Icon,
			Overview:   true,
		}
		if plugin.Overview != nil {
			resolved.Custom = true
			resolved.Entry = plugin.Overview.Entry
		}
		return resolved, nil
	}

	view, ok := plugin.View(viewID)
	if !ok {
		return Resolved{}, fmt.Errorf("the %s plugin has no view called %q", plugin.Name, viewID)
	}
	return Resolved{
		Kind:       view.Kind,
		Namespace:  view.Namespace,
		Selector:   view.Selector,
		PluginID:   plugin.ID,
		PluginName: plugin.Name,
		ViewID:     view.ID,
		Label:      view.Label,
		Icon:       view.Icon,
		Custom:     view.Type == ViewCustom,
		Entry:      view.Entry,
	}, nil
}
