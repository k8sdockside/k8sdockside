package plugins

import (
	"fmt"
	"sort"

	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/metrics"
)

// What a plugin's overview shows for one cluster: whether the thing it
// describes is installed here at all, and a live count of what it manages.
//
// The distinction this file exists to make is the one the sidebar cannot: a
// plugin is installed on this machine, and the solution it describes is
// installed in a cluster. Those come apart constantly -- you keep the Argo CD
// plugin and open a cluster that has never heard of it -- and the answer has to
// be a sentence rather than four empty tables.

// Presence is one of a plugin's requirements, checked against a cluster.
type Presence struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Selector is the requirement's own, when it asks for objects rather than
	// for a kind. Reported so the overview can say what it looked for.
	Selector string `json:"selector,omitzero"`
	// Optional requirements are reported but do not decide Installed.
	Optional bool `json:"optional"`
	// Served is whether the cluster has what the requirement asks for: the
	// kind, or -- with a selector -- at least one object matching it.
	Served bool `json:"served"`
	// Error is set when we could not find out, as opposed to finding out that
	// the kind is absent. An unreachable cluster must not read as "Argo CD is
	// not installed here".
	Error string `json:"error"`
}

// Bucket is one slice of a card: a value the grouped field took, and how many
// objects had it.
type Bucket struct {
	// Value as found on the objects. Empty means the field was absent, which
	// the frontend words rather than printing nothing.
	Value string `json:"value"`
	Count int    `json:"count"`
	Tone  string `json:"tone"`
}

// CardResult is one tile: a count, and how it divides up.
type CardResult struct {
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Total   int      `json:"total"`
	Buckets []Bucket `json:"buckets"`
	// Grouped says whether this card divides its count at all, so the frontend
	// can tell "no breakdown was asked for" from "everything landed in one
	// bucket".
	Grouped bool `json:"grouped"`
	// Error is why this tile has no number. A card whose kind the cluster does
	// not serve is the ordinary case and is reported here rather than failing
	// the whole overview.
	Error string `json:"error"`
}

// Summary is a plugin's overview for one context.
type Summary struct {
	PluginID string `json:"pluginId"`
	// Installed is whether every required (non-optional) kind is served. It is
	// the headline answer, and it is deliberately not "any of them": a cluster
	// serving one Argo CD CRD out of three is a broken install, not a working
	// one, and saying "installed" would send the reader looking in the wrong
	// place.
	Installed bool `json:"installed"`
	// Checked is whether we managed to ask at all. False leaves Installed
	// meaningless -- see Error.
	Checked      bool         `json:"checked"`
	Requirements []Presence   `json:"requirements"`
	Cards        []CardResult `json:"cards"`
	// Error is a failure that stopped the whole overview, as opposed to one
	// card's worth of trouble.
	Error string `json:"error"`
}

// Cluster is what Summarise needs from a cluster. It is an interface rather
// than the concrete watcher so that this file can be tested without one, and so
// that the summary logic -- which is all judgement about wording and ordering --
// is not tangled up with client-go.
type Cluster interface {
	// KindsServed answers for every kind in one go. It is plural because the
	// question is answered by a sweep of the whole discovery surface -- the
	// same one `kubectl api-resources` does -- and asking kind by kind meant a
	// sweep apiece.
	KindsServed(kinds []string) (map[string]bool, error)
	CountBy(kind, namespace, selector string, path kube.FieldPath) (kube.Tally, error)
}

// Summarise builds the overview for one plugin against one cluster.
//
// Nothing here returns an error. Every way this can go wrong -- an unreachable
// cluster, a kind that is not served, one card of five failing -- is a thing
// the page has to say rather than a reason to have no page.
func Summarise(p Plugin, cl Cluster) Summary {
	out := Summary{PluginID: p.ID, Checked: true}

	// Every kind the page will need is asked for up front, in one sweep. The
	// requirements and the cards go in together: a card left out of the sweep
	// would go back to the cluster on its own, which is the cost this exists to
	// avoid.
	served, reason := kindsServed(p, cl)
	check := func(kind string) (bool, string) {
		if reason != "" {
			return false, reason
		}
		return served[kind], ""
	}

	required, met := 0, 0
	for _, req := range p.Requires {
		ok, reason := check(req.Kind)
		// A requirement that names objects is not answered by discovery: the
		// kind being served is necessary but says nothing, because the kinds
		// such a plugin needs are ones every cluster serves. Only a cluster
		// that has the objects has the product.
		if ok && req.Selector != "" {
			ok, reason = found(cl, req)
		}
		out.Requirements = append(out.Requirements, Presence{
			Kind:     req.Kind,
			Label:    req.Label,
			Selector: req.Selector,
			Optional: req.Optional,
			Served:   ok,
			Error:    reason,
		})
		if reason != "" {
			// We could not ask. Nothing after this can be trusted either, so
			// the page says so instead of drawing a confident "not installed".
			out.Checked = false
			out.Error = reason
		}
		if !req.Optional {
			required++
			if ok {
				met++
			}
		}
	}
	// A plugin that declares no requirements is taken at its word: it is for
	// something that has no CRDs to look for.
	out.Installed = out.Checked && met == required

	for _, card := range p.Cards {
		out.Cards = append(out.Cards, summariseCard(card, cl, check))
	}
	return out
}

// found answers a requirement that names objects: are any of them there?
//
// The error it returns is why we could not tell, which Summarise treats the
// same way as a discovery failure -- an unreachable cluster must never read as
// "Flannel is not installed here".
func found(cl Cluster, req Requirement) (bool, string) {
	tally, err := cl.CountBy(req.Kind, req.Namespace, req.Selector, "")
	if err != nil {
		return false, fmt.Sprintf("could not look for %s: %v", req.Kind, err)
	}
	return tally.Total > 0, ""
}

// NeedsObjects reports whether any of a plugin's requirements names objects
// rather than a kind -- whether, in other words, the definitions alone cannot
// say if it belongs to a cluster.
func NeedsObjects(p Plugin) bool {
	for _, req := range p.Requires {
		if !req.Optional && req.Selector != "" {
			return true
		}
	}
	return false
}

// HasItsObjects answers whether a cluster has the objects a plugin requires,
// for the requirements that name any. told is false when the cluster could not
// be asked, which is not the same as the objects being absent: a plugin whose
// cluster would not answer is left as it was rather than called missing.
//
// Only the object requirements are checked. The kinds are the sidebar's
// question, answered from the definitions it has already read.
func HasItsObjects(p Plugin, cl Cluster) (here, told bool) {
	for _, req := range p.Requires {
		if req.Optional || req.Selector == "" {
			continue
		}
		ok, reason := found(cl, req)
		if reason != "" {
			return false, false
		}
		if !ok {
			return false, true
		}
	}
	return true, true
}

// kindsServed asks the cluster about every kind the plugin names, once.
//
// The reason it returns is why we could not ask at all, which is a different
// thing from a kind being absent: an unreachable cluster must never read as
// "Argo CD is not installed here".
func kindsServed(p Plugin, cl Cluster) (map[string]bool, string) {
	seen := map[string]bool{}
	kinds := make([]string, 0, len(p.Requires)+len(p.Cards))
	add := func(kind string) {
		if kind == "" || seen[kind] {
			return
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	for _, req := range p.Requires {
		add(req.Kind)
	}
	for _, card := range p.Cards {
		add(card.Kind)
	}

	if len(kinds) == 0 {
		return map[string]bool{}, ""
	}
	served, err := cl.KindsServed(kinds)
	if err != nil {
		return nil, err.Error()
	}
	return served, ""
}

// summariseCard counts one card, or explains why it has no number.
func summariseCard(card Card, cl Cluster, check func(string) (bool, string)) CardResult {
	result := CardResult{
		Label:   card.Label,
		Kind:    card.Kind,
		Grouped: card.GroupBy != "",
		Buckets: []Bucket{},
	}

	// Asked before counting, so that "this cluster does not have this" reads as
	// itself rather than as a listing error.
	ok, reason := check(card.Kind)
	if reason != "" {
		result.Error = reason
		return result
	}
	if !ok {
		result.Error = "this cluster does not serve " + card.Kind
		return result
	}

	tally, err := cl.CountBy(card.Kind, card.Namespace, card.Selector, card.GroupBy)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Total = tally.Total
	if !result.Grouped {
		return result
	}
	for value, count := range tally.Counts {
		result.Buckets = append(result.Buckets, Bucket{Value: value, Count: count, Tone: card.Tones[value]})
	}
	sortBuckets(result.Buckets)
	return result
}

// toneRank orders the buckets by how much they want looking at, not by count
// and not alphabetically. One Degraded application among forty Healthy ones is
// the whole reason the tile is on screen, and sorting by count would bury it.
var toneRank = map[string]int{"error": 0, "warn": 1, "ok": 2, "info": 3}

func sortBuckets(buckets []Bucket) {
	rank := func(b Bucket) int {
		if r, ok := toneRank[b.Tone]; ok {
			return r
		}
		return 4 // untoned values, then the absent-field bucket below
	}
	sort.SliceStable(buckets, func(i, j int) bool {
		a, b := buckets[i], buckets[j]
		// The absent-field bucket last whatever else is true: it is the least
		// informative thing on the tile.
		if (a.Value == "") != (b.Value == "") {
			return b.Value == ""
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Value < b.Value
	})
}

// ---- charts ----------------------------------------------------------------

// ChartResult is one drawn chart: what it is called, and the series behind it.
type ChartResult struct {
	PluginID    string           `json:"pluginId"`
	PluginName  string           `json:"pluginName"`
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Unit        string           `json:"unit"`
	Description string           `json:"description"`
	Series      []metrics.Series `json:"series"`
	// Error is why this chart is empty, if it is. Carried per chart rather than
	// returned, so one query that will not run does not take a page of charts
	// with it -- a cluster missing kube-state-metrics has some of these working
	// and some not, and that is worth seeing.
	Error string `json:"error"`
}

// Metrics is what ChartsFor needs from a Prometheus. An interface for the same
// reason Cluster is one: the deciding -- which charts, in what order, expanded
// how -- is testable without a server.
type Metrics interface {
	QueryRange(query, legend string, r metrics.Range) (metrics.Result, error)
}

// ChartsFor draws every chart the installed plugins attach to one surface.
//
// `attach` is a kind, AttachDashboard or AttachOverview; vars carry the object
// being drawn for, and are empty for the two cluster-wide surfaces.
func ChartsFor(list []Plugin, attach string, vars metrics.Variables, r metrics.Range, m Metrics) []ChartResult {
	var out []ChartResult
	for _, plugin := range list {
		for _, chart := range plugin.ChartsFor(attach) {
			out = append(out, drawChart(plugin, chart, vars, r, m))
		}
	}
	return out
}

func drawChart(plugin Plugin, chart Chart, vars metrics.Variables, r metrics.Range, m Metrics) ChartResult {
	result := ChartResult{
		PluginID:    plugin.ID,
		PluginName:  plugin.Name,
		ID:          chart.ID,
		Label:       chart.Label,
		Unit:        chart.Unit,
		Description: chart.Description,
		Series:      []metrics.Series{},
	}

	query, err := metrics.Expand(chart.Query, vars)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	answer, err := m.QueryRange(query, chart.Legend, r)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Series = answer.Series
	return result
}

// HasChartsFor reports whether any installed plugin draws on a surface, so a
// panel that would be empty is never drawn at all.
func HasChartsFor(list []Plugin, attach string) bool {
	for _, plugin := range list {
		if len(plugin.ChartsFor(attach)) > 0 {
			return true
		}
	}
	return false
}
