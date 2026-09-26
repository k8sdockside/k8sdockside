// Reading a settings file back in: renamed and moved fields migrated, every
// value held to its limits, and the deep copy the store hands out.
package appconfig

import (
	"slices"
	"strings"
	"time"
)

// renamedGroups maps sidebar section labels that have changed name to what
// they are called now. The folded state is remembered under the label, so a
// file written before a rename would otherwise unfold that section for
// everyone who had it shut.
var renamedGroups = map[string]string{
	"Solutions": "Plugins",
}

// migrateGroups rewrites renamed labels in one folded list. Nil stays nil,
// since nil is "the user has never said" and must not become a choice.
func migrateGroups(groups []string) []string {
	if groups == nil {
		return nil
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		if now, ok := renamedGroups[g]; ok {
			g = now
		}
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

func normalise(s Settings) Settings {
	s.Layout.CollapsedGroups = migrateGroups(s.Layout.CollapsedGroups)
	for id, prefs := range s.Contexts {
		prefs.CollapsedGroups = migrateGroups(prefs.CollapsedGroups)
		s.Contexts[id] = prefs
	}
	if s.ManualFiles == nil {
		s.ManualFiles = []string{}
	}
	if s.ManualFolders == nil {
		s.ManualFolders = []string{}
	}
	if s.ExcludedFiles == nil {
		s.ExcludedFiles = []string{}
	}
	if s.ExcludedContexts == nil {
		s.ExcludedContexts = []string{}
	}
	if s.ThemeFolders == nil {
		s.ThemeFolders = []string{}
	}
	if s.PluginFolders == nil {
		s.PluginFolders = []string{}
	}
	if s.DisabledPlugins == nil {
		s.DisabledPlugins = []string{}
	}
	if s.HiddenPluginSuggestions == nil {
		s.HiddenPluginSuggestions = []string{}
	}
	if s.PluginState == nil {
		s.PluginState = map[string]map[string]map[string]string{}
	}
	// A context or plugin with nothing left in it is dropped, so forgetting
	// the last value leaves the file as if nothing had been kept.
	for plugin, contexts := range s.PluginState {
		for context, keys := range contexts {
			if len(keys) == 0 {
				delete(contexts, context)
			}
		}
		if len(contexts) == 0 {
			delete(s.PluginState, plugin)
		}
	}
	if s.Contexts == nil {
		s.Contexts = map[string]ContextPrefs{}
	}
	// After the columns are cleaned up, not before: a record whose only
	// preference was one kind's table is empty once that kind has been put back
	// to its defaults, and only normaliseColumns knows that it has been. Doing
	// it here rather than in each mutator is what makes the rule hold for a
	// hand-edited file too.
	for id, prefs := range s.Contexts {
		prefs.Columns = normaliseColumns(prefs.Columns)
		if prefs.isEmpty() {
			delete(s.Contexts, id)
			continue
		}
		s.Contexts[id] = prefs
	}
	if s.TabOrder == nil {
		s.TabOrder = []TabRef{}
	}
	if s.Dock.Tabs == nil {
		s.Dock.Tabs = []DockTabRef{}
	}
	s = migratePanes(s)
	s = migrateDetailPane(s)
	d := Defaults().Layout
	switch s.Layout.DetailPane {
	case PaneLeft, PaneMain, PaneRight, PaneBottom:
	default:
		s.Layout.DetailPane = d.DetailPane
	}
	if s.Layout.SidebarWidth < 180 {
		s.Layout.SidebarWidth = d.SidebarWidth
	}
	// A file written before zoom existed unmarshals to 0, which would render
	// the window at nothing.
	if s.Layout.Zoom < MinZoom || s.Layout.Zoom > MaxZoom {
		s.Layout.Zoom = d.Zoom
	}
	// Below this the dock would show its tab strip and a couple of lines of
	// YAML, which is not an editor. A file written before the dock existed
	// reads as 0 and lands here.
	if s.Dock.Size < 160 {
		s.Dock.Size = Defaults().Dock.Size
	}

	p := Defaults().Preferences
	// Only the three legacy values are rewritten. Anything else is a theme id
	// and is kept as written even if nothing currently answers to it -- see
	// Preferences.Theme for why.
	if replacement, legacy := legacyThemes[s.Preferences.Theme]; legacy {
		s.Preferences.Theme = replacement
	} else if s.Preferences.Theme == "" {
		s.Preferences.Theme = p.Theme
	}
	switch s.Preferences.Density {
	case DensityComfortable, DensityCompact, DensitySpacious:
	default:
		s.Preferences.Density = p.Density
	}
	switch s.Preferences.ContextSort {
	case ContextSortName, ContextSortNameDesc, ContextSortKubeconfig:
	default:
		s.Preferences.ContextSort = p.ContextSort
	}
	// A hand-edited file could ask for a year of samples at fifteen-second
	// resolution, which is a query no cluster should be asked to answer.
	if s.Preferences.MetricsRange < 0 || s.Preferences.MetricsRange > MaxMetricsRange {
		s.Preferences.MetricsRange = 0
	}
	s.Preferences.Terminal = normaliseTerminal(s.Preferences.Terminal)
	s.Preferences.Helm = normaliseHelm(s.Preferences.Helm)
	s.Preferences.Background = normaliseBackground(s.Preferences.Background)
	s.Preferences.DateTime = normaliseDateTime(s.Preferences.DateTime)
	s.Preferences = normaliseAlerts(s.Preferences)
	s.BackgroundFolder = strings.TrimSpace(s.BackgroundFolder)
	if s.PortForwards == nil {
		s.PortForwards = []PortForward{}
	}
	s.Updates.ReadVersion = strings.TrimSpace(s.Updates.ReadVersion)
	// RestoreTabs, ShowLineNumbers, CheckForUpdates and DesktopNotifications
	// are deliberately not defaulted: nil is a value in its own right for
	// each, meaning "never chosen", and it is resolved to true where it is
	// read.
	return s
}

// migratePanes brings a settings file up to the pane model, and repairs one
// that is already there.
//
// A file written before panes existed says where its views were in two separate
// fields: TabOrder held the strip along the top, Dock held the strip at the
// foot. Those map exactly onto the main and bottom panes, so an upgrade lands
// the user's session where they left it rather than in an empty window.
//
// The two old fields are cleared once they have been read. Leaving them would
// put the same session in the file twice with nothing keeping the copies
// honest, which is the sort of thing that is only ever noticed once one of them
// is wrong. The cost is that downgrading loses the arrangement, not the data.
func migratePanes(s Settings) Settings {
	if s.Panes == nil {
		main := make([]PaneTabRef, 0, len(s.TabOrder))
		for _, ref := range s.TabOrder {
			main = append(main, PaneTabRef{Type: ViewResource, ContextID: ref.ContextID, Kind: ref.Kind})
		}
		// This was a straight struct conversion while the two shapes agreed,
		// and it stopped compiling when PaneTabRef gained Namespaces -- which
		// is what that arrangement was for. Spelled out now: a dock tab is a
		// view onto one object and has no namespace filter to carry, so the new
		// field is simply absent rather than defaulted to something.
		bottom := make([]PaneTabRef, 0, len(s.Dock.Tabs))
		for _, ref := range s.Dock.Tabs {
			bottom = append(bottom, PaneTabRef{
				Type:      ref.Type,
				ContextID: ref.ContextID,
				Kind:      ref.Kind,
				Namespace: ref.Namespace,
				Name:      ref.Name,
			})
		}
		s.Panes = &Panes{
			// The sidebar was a fixed strip with its width in Layout; it is a
			// pane holding the cluster tree now, and that width is its size.
			Left: PaneState{
				Tabs: []PaneTabRef{{Type: ViewClusters, Kind: KindClusters}},
				Open: true,
				Size: s.Layout.SidebarWidth,
			},
			Main:   PaneState{Tabs: main, Open: true},
			Right:  PaneState{Tabs: []PaneTabRef{}},
			Bottom: PaneState{Tabs: bottom, Open: s.Dock.Open, Size: s.Dock.Size},
		}
	}
	// The tree cannot be closed, so a file that has lost it -- hand-edited, or
	// written by a build where it was not yet a tab -- gets it back rather than
	// opening a window with no way to navigate.
	s.Panes = withClustersTab(s.Panes)
	s.TabOrder = []TabRef{}
	s.Dock = Dock{Tabs: []DockTabRef{}}

	def := Defaults().Panes
	s.Panes.Left = normalisePane(s.Panes.Left, def.Left)
	s.Panes.Main = normalisePane(s.Panes.Main, def.Main)
	s.Panes.Right = normalisePane(s.Panes.Right, def.Right)
	s.Panes.Bottom = normalisePane(s.Panes.Bottom, def.Bottom)
	// Main is not a panel that can be folded away: it is what the window is,
	// and the other two are arranged around it.
	s.Panes.Main.Open = true
	return s
}

// migrateDetailPane carries a file written before the describe panel became a
// tab across to the field that replaces its two.
//
// The panel used to dock to an edge of the window with a size of its own; it is
// a tab in a pane now, so the edge is a pane id and the size is that pane's.
// The edge and the pane happen to be named the same, which makes the first half
// a lookup rather than a decision.
//
// The size is only carried into a pane holding nothing. A pane with tabs in it
// has a size the user arrived at while looking at those tabs, and a width
// chosen for a describe panel is not an improvement on it; an empty pane has
// never been sized by anyone, so the old panel's width is the better guess.
//
// Both old fields are cleared once read, for the reason migratePanes clears
// its two: the same setting in the file twice, with nothing keeping the copies
// honest, is only ever noticed once one of them is wrong.
func migrateDetailPane(s Settings) Settings {
	if s.Layout.DetailPane == "" {
		switch s.Layout.DetailDock {
		case PaneLeft, PaneRight, PaneBottom:
			s.Layout.DetailPane = s.Layout.DetailDock
		default:
			s.Layout.DetailPane = Defaults().Layout.DetailPane
		}

		if s.Panes != nil && s.Layout.DetailSize >= minPaneSize {
			switch s.Layout.DetailPane {
			case PaneLeft:
				s.Panes.Left = sizeIfEmpty(s.Panes.Left, s.Layout.DetailSize)
			case PaneRight:
				s.Panes.Right = sizeIfEmpty(s.Panes.Right, s.Layout.DetailSize)
			case PaneBottom:
				s.Panes.Bottom = sizeIfEmpty(s.Panes.Bottom, s.Layout.DetailSize)
			}
		}
	}
	s.Layout.DetailDock = ""
	s.Layout.DetailSize = 0
	return s
}

// sizeIfEmpty gives a pane a size only if it is holding nothing, so a size the
// user chose while looking at something is never overwritten.
func sizeIfEmpty(p PaneState, size int) PaneState {
	if len(p.Tabs) == 0 {
		p.Size = size
	}
	return p
}

// withClustersTab guarantees the kubeconfig tree is open somewhere.
//
// It is the one view that cannot be closed -- it is how everything else gets
// opened -- so its absence is a broken file rather than a choice, and the repair
// is to put it back where it started.
func withClustersTab(p *Panes) *Panes {
	for _, pane := range []PaneState{p.Left, p.Main, p.Right, p.Bottom} {
		for _, tab := range pane.Tabs {
			if tab.Type == ViewClusters {
				return p
			}
		}
	}
	p.Left.Tabs = append([]PaneTabRef{{Type: ViewClusters, Kind: KindClusters}}, p.Left.Tabs...)
	p.Left.Open = true
	return p
}

// normaliseColumns brings one context's table settings into a shape the rest of
// the app can trust: widths inside MinColumnWidth..MaxColumnWidth, hidden lists
// deduplicated and sorted, and nothing kept for a kind the user has since put
// back to its defaults.
//
// The sorting is not tidiness. The settings file is written whole on every
// change, so a hidden list in whatever order the clicks arrived would rewrite
// the file each time a column was unhidden and hidden again, producing a diff
// where nothing changed.
func normaliseColumns(in map[string]ColumnPrefs) map[string]ColumnPrefs {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ColumnPrefs, len(in))
	for kind, prefs := range in {
		clean := ColumnPrefs{}
		for column, px := range prefs.Widths {
			// A column with no name cannot be matched back to anything on
			// screen, so it could only ever sit in the file unreachable.
			if column == "" {
				continue
			}
			if clean.Widths == nil {
				clean.Widths = map[string]int{}
			}
			clean.Widths[column] = min(max(px, MinColumnWidth), MaxColumnWidth)
		}
		for _, column := range prefs.Hidden {
			if column == "" || slices.Contains(clean.Hidden, column) {
				continue
			}
			clean.Hidden = append(clean.Hidden, column)
		}
		slices.Sort(clean.Hidden)
		if clean.isEmpty() {
			continue
		}
		out[kind] = clean
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalisePane fills in what one pane's record does not say. The minimum size
// is the point: below it a pane shows its tab strip and three lines of whatever
// is in it, which is not a view of anything.
func normalisePane(p PaneState, def PaneState) PaneState {
	if p.Tabs == nil {
		p.Tabs = []PaneTabRef{}
	}
	if p.Size < minPaneSize {
		p.Size = def.Size
	}
	return p
}

// The views a tab may hold. They are the frontend's TabView, named here because
// the store writes one into every PaneTabRef and has to be able to say which
// ones it wrote.
const (
	// ViewClusters is the kubeconfig tree, which belongs to the window rather
	// than to any cluster and so carries no context.
	ViewClusters   = "clusters"
	ViewResource   = "resource"
	ViewEdit       = "edit"
	ViewHelmValues = "helmvalues"
	ViewLogs       = "logs"
	ViewShell      = "shell"
)

// KindClusters is the kind a clusters tab carries. A tab is identified by its
// view and its target, and this one has no target, so the kind stands in for it
// and keeps every tab the same shape.
const KindClusters = "clusters"

// The panes, named so that Layout.DetailPane can be checked against them. The
// panes themselves are fields on Panes rather than a map, so these exist only
// for the fields that hold a pane id as a value.
const (
	PaneLeft   = "left"
	PaneMain   = "main"
	PaneRight  = "right"
	PaneBottom = "bottom"
)

// minPaneSize is the smallest a pane may be recorded at, below which the size is
// taken to be missing rather than chosen. A file written before panes existed
// has 0 for the right panel and lands here.
const minPaneSize = 160

// normaliseTerminal fills in what a settings file does not say and repairs what
// it says wrongly.
//
// Everything here has a working default, so an older file -- which has none of
// these fields at all -- comes back with a terminal that opens in the dock and
// tries bash then sh, which is what somebody upgrading into this feature would
// expect it to do without being asked anything.
//
// The one field deliberately left alone is External: it names a terminal that
// may not be installed on *this* machine, and blanking it here would mean a
// settings file synced between a desktop with kitty and a laptop without it
// lost the choice on every sync. An id nothing answers to falls back at the
// point it is launched instead.
func normaliseTerminal(t Terminal) Terminal {
	d := DefaultTerminal()
	switch t.Mode {
	case TerminalInApp, TerminalExternal:
	default:
		t.Mode = d.Mode
	}

	shells := make([]string, 0, len(t.Shells))
	for _, shell := range t.Shells {
		if trimmed := strings.TrimSpace(shell); trimmed != "" && !slices.Contains(shells, trimmed) {
			shells = append(shells, trimmed)
		}
	}
	// A list with nothing in it is a shell that can never open, which is not a
	// choice anybody makes on purpose.
	if len(shells) == 0 {
		shells = slices.Clone(d.Shells)
	}
	t.Shells = shells

	if strings.TrimSpace(t.NodeImage) == "" {
		t.NodeImage = d.NodeImage
	}
	if strings.TrimSpace(t.NodeNamespace) == "" {
		t.NodeNamespace = d.NodeNamespace
	}
	if t.FontSize < MinTermFontSize || t.FontSize > MaxTermFontSize {
		t.FontSize = d.FontSize
	}
	if t.Scrollback < MinScrollback || t.Scrollback > MaxScrollback {
		t.Scrollback = d.Scrollback
	}
	return t
}

// normaliseHelm repairs a timeout a settings file states impossibly, and tidies
// the path.
//
// The path itself is deliberately not checked for existence here. This runs on
// every read of the settings file, including on a machine the file was synced
// to rather than written on, and a path that is not there today is still the
// user's answer -- it is reported as missing at the point helm is looked for,
// where there is somewhere to say so. See helmcli.Locate.
func normaliseHelm(h Helm) Helm {
	h.Path = strings.TrimSpace(h.Path)
	if h.TimeoutSeconds < MinHelmTimeout || h.TimeoutSeconds > MaxHelmTimeout {
		h.TimeoutSeconds = DefaultHelmTimeout
	}
	// Atomic without Wait is not a state helm has: --atomic waits, whatever
	// else was asked for. Recording it as it will behave means the settings
	// view shows the truth rather than a box that is off while the flag it
	// implies is on.
	if h.Atomic {
		h.Wait = true
	}
	return h
}

// normaliseBackground repairs a source nothing answers to and an interval
// outside what anyone would choose. The pinned picture is kept as written --
// see Background.Pinned.
// normaliseAlerts fills in where alerts go for a file older than the choice,
// from the switch that came before it, and drops a snooze that is not a time.
// A snooze that has run out is left for the reader to see as over: the file is
// not rewritten on a timer.
func normaliseAlerts(p Preferences) Preferences {
	switch p.Alerts {
	case AlertsSystem, AlertsBell, AlertsOff:
	default:
		if p.DesktopNotifications != nil && !*p.DesktopNotifications {
			p.Alerts = AlertsBell
		} else {
			p.Alerts = AlertsSystem
		}
	}
	p.AlertsSnoozedUntil = strings.TrimSpace(p.AlertsSnoozedUntil)
	if p.AlertsSnoozedUntil != "" {
		if _, err := time.Parse(time.RFC3339, p.AlertsSnoozedUntil); err != nil {
			p.AlertsSnoozedUntil = ""
		}
	}
	return p
}

// normaliseDateTime puts anything it does not know back to the default, so
// every reader can switch on the constants alone.
func normaliseDateTime(d DateTime) DateTime {
	switch d.Clock {
	case ClockSystem, Clock24, Clock12:
	default:
		d.Clock = ClockSystem
	}
	switch d.Dates {
	case DatesSystem, DatesISO, DatesDMY, DatesMDY, DatesLong:
	default:
		d.Dates = DatesSystem
	}
	switch d.Zone {
	case ZoneLocal, ZoneUTC:
	default:
		d.Zone = ZoneLocal
	}
	switch d.Ages {
	case AgesRelative, AgesAbsolute:
	default:
		d.Ages = AgesRelative
	}
	return d
}

func normaliseBackground(b Background) Background {
	switch b.Source {
	case BackgroundBuiltin, BackgroundFolder, BackgroundNone:
	default:
		b.Source = BackgroundBuiltin
	}
	switch b.Palette {
	case BackgroundPaletteVaried, BackgroundPaletteTheme:
	default:
		b.Palette = BackgroundPaletteVaried
	}
	if b.Minutes < 0 || b.Minutes > MaxBackgroundMinutes {
		b.Minutes = 0
	}
	b.Pinned = strings.TrimSpace(b.Pinned)
	return b
}

func clone(s Settings) Settings {
	out := s
	out.ManualFiles = slices.Clone(s.ManualFiles)
	out.ManualFolders = slices.Clone(s.ManualFolders)
	out.ExcludedFiles = slices.Clone(s.ExcludedFiles)
	out.ExcludedContexts = slices.Clone(s.ExcludedContexts)
	out.ThemeFolders = slices.Clone(s.ThemeFolders)
	out.PluginFolders = slices.Clone(s.PluginFolders)
	out.DisabledPlugins = slices.Clone(s.DisabledPlugins)
	out.HiddenPluginSuggestions = slices.Clone(s.HiddenPluginSuggestions)
	out.TabOrder = slices.Clone(s.TabOrder)
	out.Dock.Tabs = slices.Clone(s.Dock.Tabs)
	// A copy of the struct shares the pointer, and every pane behind it shares
	// its slice. Both have to be broken for the result to be the caller's.
	if s.Panes != nil {
		panes := *s.Panes
		panes.Main.Tabs = slices.Clone(s.Panes.Main.Tabs)
		panes.Right.Tabs = slices.Clone(s.Panes.Right.Tabs)
		panes.Bottom.Tabs = slices.Clone(s.Panes.Bottom.Tabs)
		out.Panes = &panes
	}
	// slices.Clone keeps nil as nil, which is what preserves "never chosen".
	out.Layout.CollapsedGroups = slices.Clone(s.Layout.CollapsedGroups)
	// A copy of the struct shares the pointer; the whole point of clone is that
	// a caller holding the result cannot reach back into the store.
	if s.Preferences.RestoreTabs != nil {
		restore := *s.Preferences.RestoreTabs
		out.Preferences.RestoreTabs = &restore
	}
	if s.Preferences.ShowLineNumbers != nil {
		numbers := *s.Preferences.ShowLineNumbers
		out.Preferences.ShowLineNumbers = &numbers
	}
	if s.Preferences.CheckForUpdates != nil {
		check := *s.Preferences.CheckForUpdates
		out.Preferences.CheckForUpdates = &check
	}
	if s.Preferences.DesktopNotifications != nil {
		notify := *s.Preferences.DesktopNotifications
		out.Preferences.DesktopNotifications = &notify
	}
	out.Preferences.Terminal.Shells = slices.Clone(s.Preferences.Terminal.Shells)
	out.PortForwards = slices.Clone(s.PortForwards)
	out.PluginState = make(map[string]map[string]map[string]string, len(s.PluginState))
	for plugin, contexts := range s.PluginState {
		copied := make(map[string]map[string]string, len(contexts))
		for context, keys := range contexts {
			values := make(map[string]string, len(keys))
			for k, v := range keys {
				values[k] = v
			}
			copied[context] = values
		}
		out.PluginState[plugin] = copied
	}
	out.Contexts = make(map[string]ContextPrefs, len(s.Contexts))
	for k, v := range s.Contexts {
		v.CollapsedGroups = slices.Clone(v.CollapsedGroups)
		v.Columns = cloneColumns(v.Columns)
		out.Contexts[k] = v
	}
	return out
}

// cloneColumns copies one context's table settings, maps and slices and all, so
// that a caller holding the result cannot reach back into the store. Nil stays
// nil: a context that has never had a column touched carries no map.
func cloneColumns(in map[string]ColumnPrefs) map[string]ColumnPrefs {
	if in == nil {
		return nil
	}
	out := make(map[string]ColumnPrefs, len(in))
	for kind, prefs := range in {
		copied := ColumnPrefs{Hidden: slices.Clone(prefs.Hidden)}
		if prefs.Widths != nil {
			copied.Widths = make(map[string]int, len(prefs.Widths))
			for column, px := range prefs.Widths {
				copied.Widths[column] = px
			}
		}
		out[kind] = copied
	}
	return out
}
