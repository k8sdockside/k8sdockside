package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/addons"
	"github.com/k8sdockside/k8sdockside/internal/kube"
	"github.com/k8sdockside/k8sdockside/internal/metrics"
	"github.com/k8sdockside/k8sdockside/internal/updates"
	"k8s.io/apimachinery/pkg/labels"
)

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
	fail(checkCategory(p.ID, p.Category))
	p.Category = category(p.Category)
	p.Logo = strings.TrimSpace(p.Logo)
	fail(checkLogo(p.ID, p.Logo))
	// The logo is served from the ui folder by the same handler that serves the
	// views, so a plugin naming one without shipping that folder has named a
	// file nothing can reach.
	if p.Logo != "" && p.UI == nil {
		fail(fmt.Errorf("plugin %q names a logo but ships no ui folder for it to be served from", p.ID))
	}

	for i, req := range p.Requires {
		req.Kind = strings.TrimSpace(req.Kind)
		if !kube.IsKnownKind(req.Kind) {
			fail(fmt.Errorf("plugin %q requires %q, which is not a kind this app can open", p.ID, req.Kind))
			continue
		}
		req.Namespace = strings.TrimSpace(req.Namespace)
		req.Selector = strings.TrimSpace(req.Selector)
		if err := checkFilter(req.Namespace, req.Selector); err != nil {
			fail(fmt.Errorf("plugin %q requires %s: %w", p.ID, req.Kind, err))
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
	if v.Focus != nil {
		return v, fmt.Errorf("plugin %q has a table view %q with a focus; a table is opened on an object by filtering it, and only a %q view needs telling which", pluginID, v.ID, ViewCustom)
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
	if v.Focus != nil {
		focus, err := validateFocus(pluginID, v.ID, *v.Focus)
		if err != nil {
			return v, err
		}
		v.Focus = &focus
	}
	return v, nil
}

// focusPlaceholders are what a focus's hash may say about the object.
var focusPlaceholders = []string{"{namespace}", "{name}"}

// validateFocus checks what a custom view says it can be opened on.
//
// The hash is checked for what it may name rather than for what it looks like:
// a placeholder the app does not fill would reach the page as literal braces,
// and the page would go looking for an object called "{uid}".
func validateFocus(pluginID, viewID string, f Focus) (Focus, error) {
	f.Kind = strings.TrimSpace(f.Kind)
	f.Hash = strings.TrimPrefix(strings.TrimSpace(f.Hash), "#")
	if f.Hash == "" {
		f.Hash = DefaultFocusHash
	}
	if f.Kind == "" {
		return f, fmt.Errorf("plugin %q has a custom view %q with a focus but no kind to focus on", pluginID, viewID)
	}
	if strings.HasPrefix(f.Kind, Prefix) || !kube.IsKnownKind(f.Kind) {
		return f, fmt.Errorf("plugin %q has a custom view %q focusing on %q, which is not a kind this app can open", pluginID, viewID, f.Kind)
	}
	if f.Kind == unreadableKind {
		return f, fmt.Errorf("plugin %q has a custom view %q focusing on %s, which no plugin view may read", pluginID, viewID, unreadableKind)
	}
	rest := f.Hash
	for _, p := range focusPlaceholders {
		rest = strings.ReplaceAll(rest, p, "")
	}
	if strings.ContainsAny(rest, "{}#") {
		return f, fmt.Errorf("plugin %q has a custom view %q whose focus hash %q names something other than %s", pluginID, viewID, f.Hash, strings.Join(focusPlaceholders, " and "))
	}
	return f, nil
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

	services, err := validateServices(p.ID, slices.Clone(ui.Services))
	if err != nil {
		return err
	}
	ui.Services = services

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
		// A view that can be opened on an object has to be able to read it.
		if view.Focus != nil {
			add(view.Focus.Kind)
		}
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
