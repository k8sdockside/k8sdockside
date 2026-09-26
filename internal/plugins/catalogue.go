package plugins

import (
	"fmt"

	"github.com/k8sdockside/k8sdockside/internal/addons"
)

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
