// Package appconfig persists the user's own choices -- which kubeconfig files
// they added, what they renamed each context to, the colour they gave it, and
// where they docked the detail panel. None of this can be derived from the
// kubeconfig files themselves, so it lives in its own file under the user's
// config directory -- ~/.config/k8sdockside, alongside the other Kubernetes
// tooling, rather than wherever the platform would hide application state.
package appconfig

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rogerwesterbo/k8sdockside/internal/themes"
)

// ContextPrefs is what the user decided about one kubeconfig context. Alias
// overrides the context's name in the UI; Color tints its sidebar entry and
// every tab opened against it. An empty field means "use the default".
type ContextPrefs struct {
	Alias string `json:"alias"`
	Color string `json:"color"`
	// Metrics overrides where this cluster's Prometheus is found, for the charts
	// a solution plugin draws. Empty means "look for it", which is what almost
	// every cluster wants: a Prometheus installed by the Operator or the
	// community chart is discoverable, and reaching it through the API server
	// needs no address at all.
	//
	// Two forms are accepted -- `namespace/service:port` and an http(s) URL --
	// because there are two genuinely different situations behind them; see
	// metrics.ParseEndpoint.
	Metrics string `json:"metrics,omitzero"`
	// CollapsedGroups overrides Layout.CollapsedGroups for this context alone.
	// Nil means "follow the global setting", which is the usual case -- a
	// cluster only carries its own list once the user folds a group for it
	// specifically, e.g. because this is the one cluster with the Gateway API
	// installed.
	CollapsedGroups []string `json:"collapsedGroups"`
	// Columns is what the user changed about each kind's table here, keyed by
	// the kind the tab lists. Per context because the same kind is not the same
	// table in two clusters: the pods of a dev cluster have short names and the
	// pods of a production one have long ones, and a width dragged for the
	// second is the wrong width for the first.
	Columns map[string]ColumnPrefs `json:"columns,omitempty"`
}

// ColumnPrefs is what the user changed about one kind's table: the widths they
// dragged columns to, and the columns they turned off. Absent fields mean "as
// the backend sends it", which is the default every kind starts at.
//
// Keyed by column name rather than by position. A kind's columns are not fixed
// -- a CRD declares its own printer columns, and the app's own lists gain and
// lose them between releases -- so an index written today can name a different
// column tomorrow, silently moving one column's width onto another and hiding
// something the user never hid. A name that has gone is simply not found.
//
// Where a kind declares the same name twice -- a CRD printer column called
// "Name" beside the Name the app puts first -- the repeats are distinguished by
// an occurrence suffix, "Name#2". The frontend builds these keys; see
// frontend/src/lib/columns.ts, which is the one place that rule lives.
type ColumnPrefs struct {
	// Widths is the width in px the user dragged a column to, by column key.
	// A column not listed here sizes itself to its contents, as it always did.
	Widths map[string]int `json:"widths,omitempty"`
	// Hidden are the columns the user turned off, by column key. Sorted, so a
	// settings file does not churn on a re-tick that changes nothing.
	Hidden []string `json:"hidden,omitempty"`
}

// isEmpty reports whether the user has said nothing about one kind's table, in
// which case the entry is dropped rather than kept as a blank one.
func (c ColumnPrefs) isEmpty() bool {
	return len(c.Widths) == 0 && len(c.Hidden) == 0
}

// isEmpty reports whether the user has said nothing about a context at all. An
// empty override is not nothing: CollapsedGroups of length zero means "show
// every group here", which is a choice and not the absence of one.
func (p ContextPrefs) isEmpty() bool {
	return p.Alias == "" && p.Color == "" && p.Metrics == "" && p.CollapsedGroups == nil && len(p.Columns) == 0
}

// TabRef identifies one open tab: a kubeconfig context and the resource kind
// shown in it. It is stored as a pair rather than a joined string because
// context IDs contain a file path, so a composite key could not be split back
// apart reliably.
type TabRef struct {
	ContextID string `json:"contextId"`
	Kind      string `json:"kind"`
}

// DockTabRef identifies one tab in the bottom dock. Where a TabRef names a
// collection -- the pods of a cluster -- a dock tab is a view onto one object,
// so it carries that object's namespace and name as well.
//
// Type names which view it is. There is only one today, "edit", and it is
// stored rather than assumed so that a dock which grows a second view can still
// read a file written by this one.
type DockTabRef struct {
	Type      string `json:"type"`
	ContextID string `json:"contextId"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Dock is the state of the strip at the foot of the window: what it has open,
// whether it is showing it, and how tall it stands when it is.
//
// It is one record with one writer rather than tabs here and a size in Layout,
// and that is load bearing. Everything in it changes together -- opening an
// editor adds a tab and unfolds the dock in the same gesture -- and every
// mutator on this store answers with the whole settings for the frontend to
// adopt. Two writers over one gesture would each answer with the whole of it,
// and the slower would carry the other's change back to what it was before it
// was made.
type Dock struct {
	Open bool `json:"open"`
	// Size is the dock's height in px when it is open. Zero from a file written
	// before the dock existed, which normalise repairs.
	Size int          `json:"size"`
	Tabs []DockTabRef `json:"tabs"`
}

// PaneTabRef identifies one tab, wherever it is. It is the union of what TabRef
// and DockTabRef each said: Type names the view, and the object fields are
// empty for a "resource" tab, which names a collection rather than an object.
//
// One shape for both because a tab is no longer tied to a place. The user
// decides which pane a view lives in, so a file that recorded "these are the
// top tabs and these are the dock's" could not say what they can now say.
type PaneTabRef struct {
	// Type is the view: resource, edit, helmvalues, logs or shell. Stored
	// rather than assumed, as DockTabRef.Type was, so a pane holding a view
	// this build has never heard of can be read and skipped rather than
	// misread as something else.
	Type      string `json:"type"`
	ContextID string `json:"contextId"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitzero"`
	Name      string `json:"name,omitzero"`
	// Namespaces is the filter a resource tab was narrowed to, empty for the
	// whole cluster. It is the one thing about how a table was *left* that is
	// worth carrying across a restart: which slice of the cluster you work in
	// is a standing fact about your job, and picking it again in every tab
	// after every launch is the cost of not writing it down.
	//
	// A list rather than a name because the picker takes several, and separate
	// from Namespace above, which names the object a document tab is open on
	// and means nothing for a collection.
	Namespaces []string `json:"namespaces,omitempty"`
}

// PaneState is one pane: what it holds, whether it is showing it, and how much
// room it takes.
//
// Open and Size are per pane rather than in Layout for the reason Dock kept its
// own: everything about a pane changes in one gesture -- dropping a tab into the
// right panel fills it, opens it and gives it a width at once -- and every
// mutator here answers with the whole settings for the frontend to adopt, so a
// second writer over the same gesture would carry the first's change back.
type PaneState struct {
	Tabs []PaneTabRef `json:"tabs"`
	Open bool         `json:"open"`
	// Size is the pane's extent along its own axis in px: a width for the
	// right panel, a height for the bottom one. Meaningless for main, which
	// takes whatever the other two leave.
	Size int `json:"size"`
}

// Panes is where every open view sits.
//
// A fixed record rather than a tree of splits, and that is the whole design: it
// covers a list here, its logs under it and an editor beside it, while staying a
// shape that can be written and read back without the file learning recursion.
type Panes struct {
	// Left holds the kubeconfig tree by default. It is a pane like the others,
	// so the tree can be moved out of it -- what cannot happen is the tree being
	// closed, which the frontend enforces by pinning its tab.
	Left   PaneState `json:"left"`
	Main   PaneState `json:"main"`
	Right  PaneState `json:"right"`
	Bottom PaneState `json:"bottom"`
}

// Layout is the arrangement the user last left the window in.
type Layout struct {
	// DetailPane is which pane the describe tab opens in: left, main, right or
	// bottom. The tab itself is never in Panes -- it comes and goes with the
	// row that is selected, and a restored window has no selection -- so this
	// is the whole of what survives about where the user put it.
	DetailPane string `json:"detailPane"`
	// DetailDock and DetailSize are superseded by DetailPane and by the size of
	// the pane the tab lands in. They are read only to bring a file written
	// before the describe panel became a tab across. See migrateDetailPane.
	DetailDock string `json:"detailDock,omitempty"`
	DetailSize int    `json:"detailSize,omitempty"`
	// SidebarWidth is superseded by Panes.Left.Size and is read only to bring
	// a file written before the sidebar became a pane across. See migratePanes.
	SidebarWidth int `json:"sidebarWidth"`
	// Zoom is the webview scale, 1 being normal size. Persisted so the window
	// comes back the size the user left it readable at.
	Zoom float64 `json:"zoom"`
	// CollapsedGroups are the sidebar's resource-tree headings the user has
	// folded away. It is deliberately nullable: nil means they have never
	// chosen, which is what lets the frontend fold the specialist groups once
	// on a fresh install, while an empty list is the real choice "show me
	// everything" and must not be defaulted over. The names are the frontend
	// catalogue's group labels; the store only remembers what it is handed.
	//
	// No omitempty: it would drop an empty list on write, turning "I expanded
	// everything" back into "never chosen" on the next read.
	CollapsedGroups []string `json:"collapsedGroups"`
}

// Window is where the main window was last left and how big, in the screen
// coordinates the window itself reports: from the top-left of the primary
// screen, Y downwards, so a screen to the left of it or above it gives
// negative values. The zero value is "never recorded", and the window opens
// centred at its default size.
type Window struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	// Maximised is kept apart from the size, which stays the one the window
	// had before it was maximised: that is where un-maximising returns to.
	Maximised bool `json:"maximised,omitzero"`
}

// Terminal is how the app opens a shell, and what it opens it with.
//
// It is one record rather than five loose preferences because the fields only
// make sense together: an external terminal ignores the font size, and the node
// image is only ever read on the way to creating a debug pod. The frontend
// reads it whole and the backend reads it whole.
type Terminal struct {
	// Mode is TerminalInApp or TerminalExternal. Empty means the built-in one,
	// which is the answer that always works: it needs nothing installed.
	Mode string `json:"mode"`
	// External is the id of the terminal emulator to launch when Mode is
	// external -- one of the ids internal/termapp knows. Empty means "whatever
	// this machine uses by default", which is deliberately not resolved to a
	// concrete id on save: a settings file synced between two machines should
	// go on meaning "the default here" on both of them.
	External string `json:"external"`
	// Shells are the commands tried in a container, in order, until one of them
	// runs. A container image is free to have bash, only sh, or neither, and
	// there is no way to know which without asking it.
	Shells []string `json:"shells"`
	// NodeImage is the image the node shell's debug pod runs, and NodeNamespace
	// is where it is created. Both are settings because a cluster can force
	// them: an air-gapped one mirrors its own images, and a namespace enforcing
	// the restricted pod security standard will not take a privileged pod.
	NodeImage     string `json:"nodeImage"`
	NodeNamespace string `json:"nodeNamespace"`
	// FontSize is the built-in terminal's type size in px, and Scrollback how
	// many lines it keeps.
	FontSize   int `json:"fontSize"`
	Scrollback int `json:"scrollback"`
}

// The values Terminal.Mode may take. Strings rather than an enum, for the
// reason Density is: the settings file stays readable, and a value from a
// hand-edited file normalises back to the default rather than failing to parse.
const (
	TerminalInApp    = "app"
	TerminalExternal = "external"
)

// Terminal defaults. The shells are bash for the comfort of it and sh because
// a container that has anything has sh; the image is busybox because it is tiny
// and mirrored everywhere, and the namespace follows kubectl debug.
//
// They are stated here rather than taken from internal/kube so that this
// package goes on knowing nothing about Kubernetes; kube has the same fallback
// for a caller that hands it nothing.
var defaultShells = []string{"bash", "sh"}

const (
	DefaultNodeImage     = "busybox"
	DefaultNodeNamespace = "default"
	DefaultTermFontSize  = 12
	DefaultScrollback    = 5000
)

// The bounds a hand-edited file is held to. A one-pixel terminal and a million
// lines of scrollback are both ways of making the app unusable from a text
// editor.
const (
	MinTermFontSize = 8
	MaxTermFontSize = 32
	MinScrollback   = 200
	MaxScrollback   = 200000
)

// Helm is how the app runs helm, and what it passes when it does.
//
// One record rather than four loose preferences, for the reason Terminal is
// one: the fields only make sense together, and both sides read it whole.
//
// It exists at all because reading a release needs helm and changing one does.
// A release is a Secret the app decodes itself, so the drawer works on a
// machine with no helm at all; upgrading, rolling back and uninstalling are
// Helm's own operations and are run by asking helm to do them. See
// internal/helmcli.
type Helm struct {
	// Path is where helm is, when the user has said. Empty means "find it",
	// which is what almost every Linux and Windows machine wants.
	//
	// It is a setting because of macOS specifically: an app launched from
	// Finder inherits a PATH of four system directories, so a helm installed by
	// Homebrew is invisible to it however well it works in a terminal. That is
	// not a fault anything can repair on the user's behalf -- there is no
	// reliable way to reconstruct a login shell's PATH -- so it is asked.
	Path string `json:"path,omitzero"`
	// Wait holds an upgrade, rollback or uninstall open until what it wrote
	// reports ready, rather than returning once the API server has taken it.
	//
	// Off by default, matching helm's own default. On, a slow rollout means a
	// button that stays busy for minutes, which is a thing to opt into rather
	// than to discover.
	Wait bool `json:"wait,omitzero"`
	// Atomic rolls a failed upgrade back to where it was. It implies Wait --
	// there is no knowing an upgrade failed without waiting for it -- and helm
	// enforces that itself.
	Atomic bool `json:"atomic,omitzero"`
	// TimeoutSeconds bounds a command. Zero means never chosen and resolves to
	// DefaultHelmTimeout.
	TimeoutSeconds int `json:"timeoutSeconds,omitzero"`
}

// The bounds a hand-edited timeout is held to, and the default between them.
// Five minutes is helm's own default for --wait; below the minimum an upgrade
// that waits could not finish, and above the maximum a stuck command would hold
// a button busy for most of an hour before saying so.
const (
	DefaultHelmTimeout = 300
	MinHelmTimeout     = 30
	MaxHelmTimeout     = 3600
)

// PortForward is one tunnel the user set up, remembered so the list survives a
// restart.
//
// What is remembered is the request, not the connection: nothing here can be
// live across a restart, and the app deliberately does not reconnect on its own
// at launch -- that would mean dialling every cluster in this list on startup,
// including the ones behind a VPN nobody is on yet. They come back listed and
// disconnected, with a button.
type PortForward struct {
	// ID is the app's own handle for the forward, kept so that a reconnect is
	// the same row rather than a new one.
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// RemotePort is what the user chose: a container port on a pod, a service
	// port on a Service.
	RemotePort int `json:"remotePort"`
	// LocalPort is the port it last came up on, so a forward that was given a
	// port at random still comes back on the one you bookmarked.
	LocalPort int `json:"localPort"`
	// Random records that the user asked for any free port rather than that
	// one. It is what makes falling back to another port acceptable when the
	// remembered one is taken.
	Random bool `json:"random"`
	// Browser records that this forward opens a browser when it comes up.
	Browser bool `json:"browser"`
}

// Preferences are the app-wide choices that belong to neither one context nor
// the window's arrangement: how the app looks, and what it does on its own
// without being asked. They are kept apart from Layout deliberately -- Layout
// is "where the user left the furniture", which is written on every splitter
// drag, while these are settings someone sat down and chose.
type Preferences struct {
	// Theme is the id of the theme the user chose -- a built-in one, or one
	// they installed. Empty means the default.
	//
	// It is stored as a free string and deliberately not validated against a
	// list, because the store has no way to know what themes exist: a theme
	// may live in a folder that has not been read yet, or on a machine this
	// settings file has not reached. An id nothing answers to is kept as
	// written and falls back to the default at the point it is applied, so
	// that a theme temporarily missing does not silently unset the choice.
	//
	// Nothing here knows what a colour is; see internal/themes.
	Theme string `json:"theme"`
	// Density is comfortable, compact or spacious, and drives the table row
	// height. Empty means comfortable.
	Density string `json:"density"`
	// RestoreTabs reopens last session's tabs at launch.
	//
	// Nullable for the same reason Layout.CollapsedGroups is, and it matters
	// more here: the default is *true*, so a plain bool read from a settings
	// file written before this field existed would unmarshal to false and
	// silently stop restoring tabs for every existing user. Nil means "never
	// chosen" and resolves to true; only an explicit false turns it off.
	RestoreTabs *bool `json:"restoreTabs"`
	// ConfirmSourceRemoval asks before hiding a kubeconfig or dropping a
	// watched folder. No pointer needed: false is what the app does today, so
	// an older file reading as false is already correct.
	ConfirmSourceRemoval bool `json:"confirmSourceRemoval"`
	// ShowKubeconfigNames groups the sidebar's contexts under the file they
	// came from, headed by its name. Off by default -- most people keep every
	// context in one ~/.kube/config, where the heading only repeats itself --
	// leaving the contexts as one flat list. A file that could not be parsed is
	// named either way, because it has no contexts to show in its place and
	// would otherwise vanish from the sidebar silently.
	//
	// A plain bool, unlike RestoreTabs: here the default and Go's zero value
	// are the same, so a settings file written before this field existed reads
	// as "hidden", which is exactly right.
	ShowKubeconfigNames bool `json:"showKubeconfigNames"`
	// ContextSort is the order the sidebar lists contexts in: by the name shown,
	// by that name reversed, or in the order the kubeconfigs themselves give.
	// The name sorted on is the one on screen -- an alias when the user has set
	// one -- because sorting by a name nobody can see is sorting at random.
	//
	// A string rather than an enum for the same reason Density is: the settings
	// file stays readable, and an unknown value from a hand-edited file
	// normalises back to the default rather than failing to parse. Empty is that
	// default, which is why a file written before this field existed reads as
	// sorted by name: an unordered list of twenty clusters is a list nobody can
	// find anything in, and the kubeconfig's own order is the special case.
	ContextSort string `json:"contextSort"`
	// MetricsRange is how far back a chart looks, in minutes. Persisted because
	// it is a way of working rather than a moment: somebody watching a rollout
	// wants fifteen minutes every time they open a pod, and somebody reviewing
	// capacity wants a day.
	//
	// Zero means never chosen and resolves to an hour.
	MetricsRange int `json:"metricsRange,omitzero"`
	// ShowLineNumbers draws a line-number gutter down the side of the YAML
	// editor in the bottom dock.
	//
	// Nullable for the same reason RestoreTabs is, and for the same reason it
	// matters: the default is *on*, so a plain bool read from a file written
	// before this field existed would unmarshal to false and quietly take the
	// gutter away from everyone who already had it.
	ShowLineNumbers *bool `json:"showLineNumbers"`
	// CheckForUpdates lets the app ask GitHub, shortly after launch and every
	// few hours after, whether a newer release has been published, and say so
	// on the bell in the title bar. It is the one thing the app does that
	// reaches beyond this machine and its clusters, which is why it is a
	// choice; see UpdateService for exactly what is sent.
	//
	// Nullable for the same reason RestoreTabs is: the default is *on*, and
	// nil is how a file that has never said either way is told apart from an
	// explicit no.
	CheckForUpdates *bool `json:"checkForUpdates"`
	// Terminal is how a shell opens: in the dock or in the terminal emulator
	// the user already has, which shell to try, and what a node shell is made
	// of. See Terminal.
	Terminal Terminal `json:"terminal"`
	// Helm is where the helm binary is and how it is run, for the operations
	// that change a release. See Helm.
	Helm Helm `json:"helm"`
}

// Updates is what the app remembers about release checks, which is only what
// the user has already been told.
type Updates struct {
	// ReadVersion is the release the user marked as read from the bell in the
	// title bar. The bell stays quiet about that version, across restarts, and
	// speaks up again for the next one. Empty until a notice has been read.
	ReadVersion string `json:"readVersion,omitzero"`
}

// Settings is the whole persisted file.
type Settings struct {
	ManualFiles []string `json:"manualFiles"`
	// ManualFolders are directories the user asked us to watch. They are kept
	// as folders rather than expanded into their files at the time they were
	// added, so that a config dropped into one later is picked up by the next
	// sync instead of needing to be added by hand.
	ManualFolders []string `json:"manualFolders"`
	// ExcludedFiles are kubeconfigs found by discovery that the user has said
	// they do not want. A discovered file cannot simply be forgotten -- the
	// next scan would find it again -- so refusing it has to be recorded.
	ExcludedFiles []string `json:"excludedFiles"`
	// ExcludedContexts are single contexts the user has removed from the app,
	// by the id kube.ContextID gives them, without touching the file they
	// live in. The app never writes a kubeconfig, so this is the only way to
	// take one cluster out of the sidebar and leave the rest of its file
	// there. A removal is not offered back to the user: it lasts exactly as
	// long as the context is still in its file, and is forgotten the moment a
	// sync no longer finds it, so adding the context again -- to the
	// kubeconfig, or by re-adding the file -- makes it appear again.
	ExcludedContexts []string `json:"excludedContexts"`
	// ThemeFolders are extra directories to read themes from, on top of the
	// themes folder beside this file. They sit here rather than in Preferences
	// for the same reason ManualFolders does: this is where something comes
	// from, not a choice about how the app behaves.
	ThemeFolders []string `json:"themeFolders"`
	// PluginFolders is the same for solution plugins. Kept separate from
	// ThemeFolders rather than merged into one list of "add-on folders":
	// somebody who syncs a folder of themes has not asked for plugins out of
	// it, and one list would mean every folder was scanned for both.
	PluginFolders []string `json:"pluginFolders"`
	// DisabledPlugins are the plugins the user has switched off, built-in and
	// installed alike. It records the disabled ones rather than the enabled
	// ones on purpose: a plugin shipped in a later release, or dropped into a
	// watched folder, is on the moment it appears, and a settings file written
	// before this field existed reads as "nothing disabled".
	DisabledPlugins []string `json:"disabledPlugins"`
	// HiddenPluginSuggestions are known plugins the user has told the sidebar
	// to stop suggesting. One answer for every cluster: "not for me" is about
	// the plugin, not about where it was offered.
	HiddenPluginSuggestions []string `json:"hiddenPluginSuggestions"`
	// PluginState is what plugins' own pages keep between sessions through
	// the bridge's storage: plugin id -> context id -> key -> value. Per
	// context, because a page always looks at one cluster, and what it
	// remembers -- a folded section, a filter -- belongs to that cluster.
	PluginState map[string]map[string]map[string]string `json:"pluginState"`
	Contexts    map[string]ContextPrefs                 `json:"contexts"`
	TabOrder    []TabRef                                `json:"tabOrder"`
	// Dock is the bottom strip: its tabs in the order the user left them, and
	// whether it is showing them. Kept apart from TabOrder rather than merged
	// into it: the two strips are reordered independently and hold different
	// things, and one list would have to carry a discriminator to be split
	// back apart on read.
	Dock Dock `json:"dock"`
	// Panes is where every open view sits, and it supersedes TabOrder and Dock
	// above. It is a pointer so that "this file predates panes" is a state the
	// store can see: nil means migrate from the two old fields, which normalise
	// does once, after which it is never nil again.
	Panes *Panes `json:"panes"`
	// Updates is what the app remembers about release checks. It is not a
	// preference -- nobody chooses it in the settings view -- but it has to
	// outlive the process, and this file is where everything that does lives.
	Updates     Updates     `json:"updates,omitzero"`
	Layout      Layout      `json:"layout"`
	Preferences Preferences `json:"preferences"`
	// Window is where the main window was left. The app keeps it itself as the
	// window moves; nothing in the frontend reads or sets it.
	Window Window `json:"window,omitzero"`
	// PortForwards are the tunnels the user set up, remembered as requests
	// rather than as connections. They sit here rather than in Preferences for
	// the reason ManualFolders does: this is a list of things, not a choice
	// about how the app behaves.
	PortForwards []PortForward `json:"portForwards"`
}

// Defaults returns a settings value that is safe to use before anything has
// been saved, and that also fills in any field missing from an older file.
func Defaults() Settings {
	return Settings{
		ManualFiles:             []string{},
		ManualFolders:           []string{},
		ExcludedFiles:           []string{},
		ExcludedContexts:        []string{},
		ThemeFolders:            []string{},
		PluginFolders:           []string{},
		DisabledPlugins:         []string{},
		HiddenPluginSuggestions: []string{},
		Contexts:                map[string]ContextPrefs{},
		TabOrder:                []TabRef{},
		Dock:                    Dock{Tabs: []DockTabRef{}},
		Panes: &Panes{
			Left: PaneState{
				Tabs: []PaneTabRef{{Type: ViewClusters, Kind: KindClusters}},
				Open: true,
				Size: 320,
			},
			Main:  PaneState{Tabs: []PaneTabRef{}, Open: true},
			Right: PaneState{Tabs: []PaneTabRef{}, Size: 420},
			// The one pane that starts folded. It is on screen from launch --
			// that is what makes it a place things can be put -- and an open
			// one with nothing in it is a third of the window showing nothing.
			Bottom: PaneState{Tabs: []PaneTabRef{}, Size: 320},
		},
		Layout: Layout{DetailPane: PaneRight, SidebarWidth: 320, Zoom: 1},
		Preferences: Preferences{
			Theme:       themes.DefaultID,
			Density:     DensityComfortable,
			ContextSort: ContextSortName,
			Terminal:    DefaultTerminal(),
			Helm:        DefaultHelm(),
		},
		PortForwards: []PortForward{},
	}
}

// DefaultHelm is the helm settings a fresh install starts with: find helm
// wherever it is, and run it the way helm runs itself.
func DefaultHelm() Helm {
	return Helm{TimeoutSeconds: DefaultHelmTimeout}
}

// DefaultTerminal is the terminal settings a fresh install starts with: the
// built-in terminal, bash then sh, and busybox in `default` for a node shell.
func DefaultTerminal() Terminal {
	return Terminal{
		Mode:          TerminalInApp,
		Shells:        slices.Clone(defaultShells),
		NodeImage:     DefaultNodeImage,
		NodeNamespace: DefaultNodeNamespace,
		FontSize:      DefaultTermFontSize,
		Scrollback:    DefaultScrollback,
	}
}

// The range the webview may be scaled to. Below MinZoom the native macOS
// traffic lights no longer fit the window's own title bar, which is drawn in
// CSS pixels and so shrinks with the zoom; above MaxZoom the sidebar can no
// longer show a context name.
const (
	MinZoom = 0.5
	MaxZoom = 2.0
)

// The values Preferences.Density may take. They are strings rather than an enum
// so the settings file stays readable and so an unknown value from a
// hand-edited file normalises back to the default instead of failing to parse.
const (
	DensityComfortable = "comfortable"
	DensityCompact     = "compact"
	// Taller rows than comfortable. For a big screen, a room the app is read
	// across, or eyes that would rather not lean in -- zoom scales everything
	// together, which is the wrong tool when only the table is too tight.
	DensitySpacious = "spacious"
)

// The values Preferences.ContextSort may take, strings for the same reason the
// densities are.
const (
	// ContextSortName is A to Z by the name the sidebar shows.
	ContextSortName = "name"
	// ContextSortNameDesc is the same order reversed.
	ContextSortNameDesc = "name-desc"
	// ContextSortKubeconfig leaves the contexts in the order their kubeconfigs
	// list them, which is what the app did before there was a choice. Worth
	// keeping: a hand-written config is often ordered on purpose, and that
	// order carries meaning no sort can reproduce.
	ContextSortKubeconfig = "kubeconfig"
)

// MaxMetricsRange bounds how far back a chart may look, in minutes. A week of
// samples is already more than a line four hundred pixels wide can say.
const MaxMetricsRange = 7 * 24 * 60

// The range a table column may be dragged to, in px. The floor leaves room for
// a sort chevron and an ellipsis, below which a column shows nothing and cannot
// be found again to widen; the ceiling is wider than any window the app is
// usable in, and is there so a hand-edited file cannot push every other column
// off the screen.
const (
	MinColumnWidth = 48
	MaxColumnWidth = 1600
)

// legacyThemes maps what Preferences.Theme held before the app had themes onto
// the theme ids that replaced them.
//
// "system" is here because following the OS appearance is no longer a thing the
// app does: with a gallery of themes there is no pair for the OS to choose
// between, and the setting became "which theme", full stop. Anyone who was on
// it lands on the dark palette they were most likely already looking at, which
// is also what the app defaults to.
var legacyThemes = map[string]string{
	"system": themes.DefaultID,
	"dark":   themes.DefaultID,
	"light":  "k8sdockside-light",
}

// Store is the settings file plus the lock guarding it. Wails calls service
// methods from multiple goroutines, so every read and write goes through the
// mutex and hands back a copy.
type Store struct {
	mu   sync.Mutex
	path string
	data Settings
}

// Open loads the settings file, creating nothing on disk. A missing file is not
// an error -- it just means the user has not customised anything yet. A file
// that exists but cannot be parsed *is* an error, because silently replacing it
// with defaults would throw away the user's colours and aliases.
func Open() (*Store, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	legacy, err := legacyPath()
	if err != nil {
		return nil, err
	}
	// A failed migration is fatal rather than ignored: carrying on would open
	// an empty file at the new path and present the user with a store that has
	// lost every alias, colour and tab they had.
	if err := migrate(legacy, path); err != nil {
		return nil, fmt.Errorf("migrating settings from %s: %w", legacy, err)
	}
	return openAt(path)
}

// openAt is Open against a named file. It exists so the tests can run against a
// temporary directory without going near the real settings file, and so that
// the loading rules can be exercised without also exercising the migration.
func openAt(path string) (*Store, error) {
	s := &Store{path: path, data: Defaults()}

	raw, err := os.ReadFile(path) // #nosec G304 -- the app's own settings file
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	loaded := Defaults()
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	// What this file predates, which is what decides which sessions have to be
	// brought across: panes from TabOrder and Dock, the describe tab's pane
	// from the edge the panel used to dock to. Unmarshalling over Defaults
	// cannot answer either question -- the defaults have already filled the
	// fields in, so an absent key and an empty one look identical afterwards --
	// so both are put to the file itself, and the answer is written back as the
	// zero value the migrations look for.
	var probe struct {
		Panes  *Panes `json:"panes"`
		Layout *struct {
			DetailPane *string `json:"detailPane"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		if probe.Panes == nil {
			loaded.Panes = nil
		}
		if probe.Layout == nil || probe.Layout.DetailPane == nil {
			loaded.Layout.DetailPane = ""
		}
	}
	s.data = normalise(loaded)
	return s, nil
}

// Path is where the settings are stored, surfaced in the UI so the user can
// find (or delete) the file.
func (s *Store) Path() string { return s.path }

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.data)
}

// ManualFiles returns the kubeconfig paths the user added by hand.
func (s *Store) ManualFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ManualFiles)
}

// ManualFolders returns the directories the user asked us to scan.
func (s *Store) ManualFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ManualFolders)
}

// update applies a change under the lock and flushes to disk. If the write
// fails the in-memory state is rolled back, so what the UI shows after an error
// still matches what is on disk.
func (s *Store) update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := clone(s.data)
	fn(&s.data)
	s.data = normalise(s.data)

	if err := s.flush(); err != nil {
		s.data = before
		return clone(s.data), err
	}
	return clone(s.data), nil
}

// SetContextPrefs records the alias and colour for one context.
func (s *Store) SetContextPrefs(id string, prefs ContextPrefs) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		// A folding override is a preference in its own right, so a context
		// carrying only that one is kept -- see ContextPrefs.isEmpty.
		if prefs.isEmpty() {
			delete(d.Contexts, id)
			return
		}
		d.Contexts[id] = prefs
	})
}

// MetricsEndpoint returns where a context's Prometheus was configured to be,
// empty when the user has not said and discovery should look.
func (s *Store) MetricsEndpoint(contextID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Contexts[contextID].Metrics
}

// SetMetricsEndpoint records where a context's Prometheus is. An empty value
// clears the override rather than recording an empty one, which is what puts
// discovery back in charge.
func (s *Store) SetMetricsEndpoint(contextID, value string) (Settings, error) {
	if contextID == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		prefs := d.Contexts[contextID]
		prefs.Metrics = value
		// The same emptiness rule SetContextPrefs applies: a context with
		// nothing left to say about it is forgotten rather than kept as a blank
		// entry cluttering the settings file.
		if prefs.isEmpty() {
			delete(d.Contexts, contextID)
			return
		}
		d.Contexts[contextID] = prefs
	})
}

// AddManualFile remembers a kubeconfig path the user chose. Adding a path that
// is already known is a no-op rather than an error.
func (s *Store) AddManualFile(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ManualFiles, path) {
			d.ManualFiles = append(d.ManualFiles, path)
		}
	})
}

// RemoveManualFile forgets a path the user added. Auto-discovered files cannot
// be removed this way -- they will simply reappear on the next sync.
func (s *Store) RemoveManualFile(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ManualFiles = slices.DeleteFunc(d.ManualFiles, func(p string) bool { return p == path })
	})
}

// ExcludedFiles returns the discovered kubeconfigs the user has hidden.
func (s *Store) ExcludedFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ExcludedFiles)
}

// ExcludeFile hides a discovered kubeconfig from the sidebar.
func (s *Store) ExcludeFile(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ExcludedFiles, path) {
			d.ExcludedFiles = append(d.ExcludedFiles, path)
		}
	})
}

// UnexcludeFile lets a hidden kubeconfig be discovered again.
func (s *Store) UnexcludeFile(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ExcludedFiles = slices.DeleteFunc(d.ExcludedFiles, func(p string) bool { return p == path })
	})
}

// ExcludedContexts returns the contexts the user has removed one by one.
func (s *Store) ExcludedContexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ExcludedContexts)
}

// ExcludeContext removes one context from the sidebar, leaving its file and
// the other contexts in it where they are.
func (s *Store) ExcludeContext(id string) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ExcludedContexts, id) {
			d.ExcludedContexts = append(d.ExcludedContexts, id)
		}
	})
}

// UnexcludeContext brings back a context the user removed, so the next scan
// lists it again.
//
// The counterpart to ExcludeContext, and there for the same reason
// UnexcludeFile is: a removal that nothing can undo is a removal that has to be
// got right first time, and this one is a click away from the row it hides.
func (s *Store) UnexcludeContext(id string) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("context id is required")
	}
	return s.update(func(d *Settings) {
		d.ExcludedContexts = slices.DeleteFunc(d.ExcludedContexts, func(c string) bool { return c == id })
	})
}

// ForgetContexts drops the removals of contexts that are no longer in any
// kubeconfig. A removal is only meant to outlast the context it names for as
// long as the file still holds it; once the context is gone, keeping the
// removal would silently swallow the same name the next time it is added.
func (s *Store) ForgetContexts(ids []string) (Settings, error) {
	if len(ids) == 0 {
		return s.Get(), nil
	}
	return s.update(func(d *Settings) {
		d.ExcludedContexts = slices.DeleteFunc(d.ExcludedContexts, func(c string) bool {
			return slices.Contains(ids, c)
		})
	})
}

// ClearExclusionsIn forgets what was hidden inside one directory, so that
// re-adding a folder starts from a clean slate rather than quietly continuing
// to hide files the user can no longer see listed anywhere.
func (s *Store) ClearExclusionsIn(dir string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ExcludedFiles = slices.DeleteFunc(d.ExcludedFiles, func(p string) bool {
			return filepath.Dir(p) == dir
		})
	})
}

// AddManualFolder remembers a directory to scan for kubeconfigs. Adding one
// that is already known is a no-op rather than an error.
func (s *Store) AddManualFolder(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ManualFolders, path) {
			d.ManualFolders = append(d.ManualFolders, path)
		}
	})
}

// RemoveManualFolder stops scanning a directory. The configs found through it
// disappear from the sidebar on the next sync.
func (s *Store) RemoveManualFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ManualFolders = slices.DeleteFunc(d.ManualFolders, func(p string) bool { return p == path })
	})
}

// ThemeFolders returns the extra directories themes are read from.
func (s *Store) ThemeFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.ThemeFolders)
}

// AddThemeFolder remembers a directory to read themes from. Adding one that is
// already known is a no-op rather than an error.
func (s *Store) AddThemeFolder(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.ThemeFolders, path) {
			d.ThemeFolders = append(d.ThemeFolders, path)
		}
	})
}

// RemoveThemeFolder stops reading themes from a directory. Nothing is deleted
// from disk; the themes simply stop being offered.
func (s *Store) RemoveThemeFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.ThemeFolders = slices.DeleteFunc(d.ThemeFolders, func(p string) bool { return p == path })
	})
}

// PluginFolders returns the extra directories plugins are read from.
func (s *Store) PluginFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.PluginFolders)
}

// AddPluginFolder remembers a directory to read plugins from. Adding one that
// is already known is a no-op rather than an error.
func (s *Store) AddPluginFolder(path string) (Settings, error) {
	if path == "" {
		return s.Get(), errors.New("path is required")
	}
	return s.update(func(d *Settings) {
		if !slices.Contains(d.PluginFolders, path) {
			d.PluginFolders = append(d.PluginFolders, path)
		}
	})
}

// RemovePluginFolder stops reading plugins from a directory. Nothing is deleted
// from disk; the plugins in it just stop being offered.
func (s *Store) RemovePluginFolder(path string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.PluginFolders = slices.DeleteFunc(d.PluginFolders, func(p string) bool { return p == path })
	})
}

// DisabledPlugins returns the ids of the plugins the user has switched off.
func (s *Store) DisabledPlugins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.DisabledPlugins)
}

// SetPluginEnabled switches one plugin on or off by id.
//
// It takes the wanted state rather than being a toggle so that the frontend
// cannot drift out of step with the file: a switch that sends "off" twice ends
// up off, where a toggle would end up back on.
func (s *Store) SetPluginEnabled(id string, enabled bool) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("plugin id is required")
	}
	return s.update(func(d *Settings) {
		if enabled {
			d.DisabledPlugins = slices.DeleteFunc(d.DisabledPlugins, func(p string) bool { return p == id })
			return
		}
		if !slices.Contains(d.DisabledPlugins, id) {
			d.DisabledPlugins = append(d.DisabledPlugins, id)
		}
	})
}

// HidePluginSuggestion stops the sidebar suggesting one known plugin, or
// lets it suggest it again. Like SetPluginEnabled it takes the wanted state.
func (s *Store) HidePluginSuggestion(id string, hidden bool) (Settings, error) {
	if id == "" {
		return s.Get(), errors.New("plugin id is required")
	}
	return s.update(func(d *Settings) {
		if !hidden {
			d.HiddenPluginSuggestions = slices.DeleteFunc(d.HiddenPluginSuggestions, func(p string) bool { return p == id })
			return
		}
		if !slices.Contains(d.HiddenPluginSuggestions, id) {
			d.HiddenPluginSuggestions = append(d.HiddenPluginSuggestions, id)
		}
	})
}

// Limits on what one plugin's pages may keep on one context: enough for
// folded sections, filters and a few choices, not a database in the
// settings file.
const (
	maxPluginStateKeys  = 64
	maxPluginStateKey   = 128
	maxPluginStateValue = 16 << 10
)

// SetPluginState keeps one value for a plugin's pages on one context, or
// forgets it when value is empty.
func (s *Store) SetPluginState(pluginID, contextID, key, value string) (Settings, error) {
	switch {
	case pluginID == "" || contextID == "":
		return s.Get(), errors.New("plugin id and context id are required")
	case key == "" || len(key) > maxPluginStateKey:
		return s.Get(), fmt.Errorf("a stored key is 1 to %d characters", maxPluginStateKey)
	case len(value) > maxPluginStateValue:
		return s.Get(), fmt.Errorf("a stored value is at most %d KiB, and this one is %d bytes", maxPluginStateValue>>10, len(value))
	}
	if value != "" {
		s.mu.Lock()
		keys := s.data.PluginState[pluginID][contextID]
		_, have := keys[key]
		full := !have && len(keys) >= maxPluginStateKeys
		s.mu.Unlock()
		if full {
			return s.Get(), fmt.Errorf("a plugin may keep at most %d values per cluster; remove one first", maxPluginStateKeys)
		}
	}
	return s.update(func(d *Settings) {
		if value == "" {
			// Indexing nil maps reads as empty, and deleting from one is a no-op.
			delete(d.PluginState[pluginID][contextID], key)
			return
		}
		if d.PluginState == nil {
			d.PluginState = map[string]map[string]map[string]string{}
		}
		if d.PluginState[pluginID] == nil {
			d.PluginState[pluginID] = map[string]map[string]string{}
		}
		if d.PluginState[pluginID][contextID] == nil {
			d.PluginState[pluginID][contextID] = map[string]string{}
		}
		d.PluginState[pluginID][contextID][key] = value
	})
}

// SetLayout records the sidebar width and detail-panel dock and size.
func (s *Store) SetLayout(l Layout) (Settings, error) {
	return s.update(func(d *Settings) { d.Layout = l })
}

// SetWindow records where the main window is and how big.
func (s *Store) SetWindow(w Window) (Settings, error) {
	return s.update(func(d *Settings) { d.Window = w })
}

// SetPreferences records the app-wide preferences. It replaces the block
// wholesale rather than patching one field, matching SetLayout: the settings
// view edits them together and hands back the whole thing, so a partial
// mutator would only add a way for the two to disagree.
func (s *Store) SetPreferences(p Preferences) (Settings, error) {
	return s.update(func(d *Settings) {
		// Copied on the way in for the same reason clone copies it on the way
		// out: the caller's pointer must not become the store's.
		if p.RestoreTabs != nil {
			restore := *p.RestoreTabs
			p.RestoreTabs = &restore
		}
		if p.ShowLineNumbers != nil {
			numbers := *p.ShowLineNumbers
			p.ShowLineNumbers = &numbers
		}
		if p.CheckForUpdates != nil {
			check := *p.CheckForUpdates
			p.CheckForUpdates = &check
		}
		d.Preferences = p
	})
}

// CheckForUpdates is whether the app should look for new releases on its own,
// with "never chosen" resolved to yes. It is read here, on the service's side,
// rather than passed from the window, so that the loop that asks and the
// toggle that governs it can never disagree about what was chosen.
func (s *Store) CheckForUpdates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Preferences.CheckForUpdates == nil || *s.data.Preferences.CheckForUpdates
}

// MarkUpdateRead records that the user has seen the notice about one release.
func (s *Store) MarkUpdateRead(version string) (Settings, error) {
	return s.update(func(d *Settings) {
		d.Updates.ReadVersion = version
	})
}

// SetPanes records where every open view sits: which pane holds it, in what
// order, and how much room each pane takes.
//
// All three panes are written together rather than one at a time, for the
// reason the dock it replaces was written whole. One gesture moves a tab out of
// a pane and into another, filling and opening the second; two writers over
// that would each answer with the whole settings, and the slower would carry
// the other's half of the move back.
func (s *Store) SetPanes(panes Panes) (Settings, error) {
	return s.update(func(d *Settings) {
		// Cloned on the way in for the same reason clone copies it on the way
		// out: the caller's slices must not become the store's.
		panes.Main.Tabs = slices.Clone(panes.Main.Tabs)
		panes.Right.Tabs = slices.Clone(panes.Right.Tabs)
		panes.Bottom.Tabs = slices.Clone(panes.Bottom.Tabs)
		d.Panes = &panes
	})
}

// settingsFormat pins the file format to what encoding/json v1 wrote, so moving
// to v2 does not rewrite every existing settings file. Two of these are load
// bearing rather than cosmetic:
//
//   - Deterministic keeps the contexts map in a stable key order. v2 otherwise
//     follows Go's randomised map iteration, which would reshuffle the file on
//     every save and make it useless to diff or eyeball.
//   - FormatNilSliceAsNull keeps nil distinct from empty, which Layout and
//     ContextPrefs both depend on -- see CollapsedGroups. v2 would write [] for
//     a nil slice, turning "never chosen" into the explicit choice "show me
//     everything" the first time a fresh install saved anything.
//
// EscapeForHTML and FormatNilMapAsNull only keep the bytes identical to what
// earlier releases produced; nothing reads the file that would care either way.
var settingsFormat = json.JoinOptions(
	jsontext.WithIndent("  "),
	jsontext.EscapeForHTML(true),
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)

// flush writes the settings via a temp file and a rename, so an interrupted
// write cannot leave a half-written config behind. The caller holds the lock.
func (s *Store) flush() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	raw, err := json.Marshal(s.data, settingsFormat)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// no-op once the rename below has succeeded
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// normalise fills in anything a hand-edited or older settings file left out, so
// the rest of the app never has to check for zero values.
// PortForwards returns the forwards the user has set up.
func (s *Store) PortForwards() []PortForward {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.PortForwards)
}

// SetPortForwards replaces the remembered list.
//
// The whole list rather than one entry: the service that owns them holds the
// live state and writes what it has, and an add-one/remove-one pair would give
// two writers to a list with one owner.
func (s *Store) SetPortForwards(forwards []PortForward) (Settings, error) {
	return s.update(func(d *Settings) {
		d.PortForwards = slices.Clone(forwards)
	})
}

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
	if s.PortForwards == nil {
		s.PortForwards = []PortForward{}
	}
	s.Updates.ReadVersion = strings.TrimSpace(s.Updates.ReadVersion)
	// RestoreTabs, ShowLineNumbers and CheckForUpdates are deliberately not
	// defaulted: nil is a value in its own right for each, meaning "never
	// chosen", and it is resolved to true where it is read.
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
