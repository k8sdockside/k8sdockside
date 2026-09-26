// The settings file's shape: every type written to it, the limits and
// defaults its fields are held to, and Defaults, what a fresh install starts from.
package appconfig

import (
	"slices"

	"github.com/k8sdockside/k8sdockside/internal/themes"
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
	// DesktopNotifications posts the cluster alerts -- a node gone NotReady,
	// pods starting to crash, a certificate about to expire -- as the
	// system's own notifications, as well as on the bell in the title bar.
	// Only the clusters connected in this window are watched, so nothing is
	// woken up for it.
	//
	// Nullable for the same reason CheckForUpdates is: the default is on.
	//
	// Superseded by Alerts, which can also say "not at all"; still read, from
	// a file older than it, to fill Alerts in.
	DesktopNotifications *bool `json:"desktopNotifications"`
	// Alerts is where the cluster alerts go: AlertsSystem (the bell and the
	// system's notifications), AlertsBell (the bell only) or AlertsOff (not
	// raised at all -- the sidebar's marks and the fleet view still show
	// what is wrong). Empty in a file older than the choice, and then read
	// from DesktopNotifications: on or unset is AlertsSystem, off AlertsBell.
	Alerts string `json:"alerts,omitzero"`
	// AlertsSnoozedUntil is when a snooze of the alerts ends, RFC3339; empty
	// when they are not snoozed. While snoozed, alerts are kept on the bell
	// but raise no system notification and no unread dot. Kept in the file so
	// a snooze outlasts a restart, which is when it is most often wanted.
	AlertsSnoozedUntil string `json:"alertsSnoozedUntil,omitzero"`
	// Terminal is how a shell opens: in the dock or in the terminal emulator
	// the user already has, which shell to try, and what a node shell is made
	// of. See Terminal.
	Terminal Terminal `json:"terminal"`
	// Helm is where the helm binary is and how it is run, for the operations
	// that change a release. See Helm.
	Helm Helm `json:"helm"`
	// Background is the picture behind the start page: which pictures, how
	// often it changes, and whether one of them is kept. See Background.
	Background Background `json:"background"`
	// DateTime is how dates and times are written, in the app and in every
	// plugin's pages, which are handed the same choice. See DateTime.
	DateTime DateTime `json:"dateTime"`
}

// Where cluster alerts go.
const (
	AlertsSystem = "system"
	AlertsBell   = "bell"
	AlertsOff    = "off"
)

// How a time of day is written.
const (
	// ClockSystem follows the operating system's locale. The default.
	ClockSystem = "system"
	Clock24     = "24h"
	Clock12     = "12h"
)

// How a date is written.
const (
	// DatesSystem follows the operating system's locale. The default.
	DatesSystem = "system"
	// DatesISO is 2026-09-24: unambiguous, and sorts as text.
	DatesISO = "iso"
	// DatesDMY is 24.09.2026.
	DatesDMY = "dmy"
	// DatesMDY is 09/24/2026.
	DatesMDY = "mdy"
	// DatesLong is 24 Sep 2026, with the month in words.
	DatesLong = "long"
)

// Which clock times are read on.
const (
	// ZoneLocal is this machine's time zone. The default.
	ZoneLocal = "local"
	// ZoneUTC is UTC, which is what the cluster's own logs and events say.
	ZoneUTC = "utc"
)

// How a moment in a table is shown.
const (
	// AgesRelative is how long ago: "5m", "3d". What kubectl shows, and the
	// default.
	AgesRelative = "relative"
	// AgesAbsolute is the moment itself, written as the rest of this says.
	AgesAbsolute = "absolute"
)

// DateTime is how dates and times are written.
//
// Strings rather than enums, for the reason Density is one: the file stays
// readable, and a value from a hand-edited file that nothing knows is put back
// to the default rather than failing the whole file.
type DateTime struct {
	Clock string `json:"clock"`
	Dates string `json:"dates"`
	Zone  string `json:"zone"`
	Ages  string `json:"ages"`
}

// The places the start page's picture can come from.
const (
	// BackgroundBuiltin is the pictures the app draws itself, in the colours
	// of the theme in use. The default.
	BackgroundBuiltin = "builtin"
	// BackgroundFolder is the images in Settings.BackgroundFolder.
	BackgroundFolder = "folder"
	// BackgroundNone is no picture at all: the theme's own ground.
	BackgroundNone = "none"
)

// The colours the built-in pictures are drawn in.
const (
	// BackgroundPaletteVaried draws each picture in a colour scheme of its
	// own, dark or light as the theme is. The default.
	BackgroundPaletteVaried = "varied"
	// BackgroundPaletteTheme draws every picture in the theme's own colours.
	BackgroundPaletteTheme = "theme"
)

// DefaultBackgroundMinutes is how long one picture stays when nothing says
// otherwise: long enough not to be a slideshow, short enough that the start
// page is not the same every time it is looked at.
const DefaultBackgroundMinutes = 15

// MaxBackgroundMinutes bounds a hand-edited interval at a day.
const MaxBackgroundMinutes = 24 * 60

// Background is the start page's picture.
type Background struct {
	// Source is builtin, folder or none. Empty or unknown reads as builtin.
	Source string `json:"source"`
	// Pinned is the one picture to keep showing, by the id the frontend gives
	// it -- a built-in scene or a file in the folder. Empty rotates through
	// them all. Kept as written when nothing answers to it, for the reason
	// Theme is: a file gone from the folder today may be back tomorrow.
	Pinned string `json:"pinned,omitzero"`
	// Minutes is how long one picture stays before the next. Zero means never
	// chosen and resolves to DefaultBackgroundMinutes.
	Minutes int `json:"minutes,omitzero"`
	// Palette is varied or theme: whether the built-in pictures take turns
	// through colour schemes of their own or all wear the theme's. Empty or
	// unknown reads as varied. Either way the theme decides dark or light.
	Palette string `json:"palette,omitzero"`
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
	// BackgroundFolder is a directory of the user's own images for the start
	// page to show, when Preferences.Background says to. Here rather than in
	// Preferences for the reason ThemeFolders is: it is where something comes
	// from, and it is set through the folder picker rather than typed. Empty
	// is none.
	BackgroundFolder string `json:"backgroundFolder,omitzero"`
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
			Background:  Background{Source: BackgroundBuiltin, Palette: BackgroundPaletteVaried},
			DateTime:    normaliseDateTime(DateTime{}),
			// Alerts is left empty: a settings file is read over these
			// defaults, and only an empty value lets normaliseAlerts tell a
			// file from before the choice -- whose switch decides -- from one
			// that made it. A fresh install is normalised the same way.
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
