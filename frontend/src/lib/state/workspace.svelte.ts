// The whole application state lives here: which kubeconfig files were found,
// what the user renamed and coloured each context, which views are open and in
// which pane, and what the slide-in detail panel is showing.
//
// It is one object rather than several stores because nearly every action
// touches more than one of those things -- opening a tab selects a context,
// closing one may change the active tab, dragging tabs persists settings -- and
// splitting them would only move the coordination somewhere less obvious.
//
// Tabs live in panes. There used to be two separate models here -- a strip of
// resource tabs along the top and a dock of documents at the foot, each with its
// own open/close/reorder/restore -- and which of the two a view was decided
// where on screen it could ever appear. They are one model now, and where a view
// sits is the user's choice: see ./panes.ts for the vocabulary.

import {
    KubeconfigService,
    ResourceService,
    MetricsService,
    PluginService,
    SettingsService,
    TerminalService,
    ThemeService,
} from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/kube/models.js';
import type * as appconfig from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/appconfig/models.js';
import {
    adoptFiles,
    adoptSettings,
    isEmptyContextPrefs,
    type ConfigFile,
    type ContextPrefs,
    type ContextSort,
    type Density,
    type Settings,
} from './adopt';
import { clampColumnWidth, columnKeys, noColumnPrefs, type ColumnPrefs } from '../columns';
import { changes } from './changes.svelte';
import { editors } from './editor.svelte';
import { forwards } from './forwards.svelte';
import { logs } from './logs.svelte';
import { terminals } from './terminals.svelte';
import { views } from './views';
import {
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    PANE_IDS,
    clustersTab,
    defaultPaneFor,
    defaultPanes,
    detailsTab,
    isDocumentView,
    isPaneId,
    isTabView,
    resourceTabId,
    tabIdFor,
    type PaneId,
    type PaneState,
    type Tab,
    type TabTarget,
    type TabView,
} from './panes';
import {
    DASHBOARD,
    DEFAULT_COLLAPSED_GROUPS,
    HELM_RELEASES,
    NAV_GROUPS,
    PLUGIN_OVERVIEW,
    PODS,
    SETTINGS,
    HELP,
    KUBERNETES,
    APP_KINDS,
    PLUGINS_GROUP,
    DEFINITIONS_GROUP,
    groupForKind,
    labelFor,
    parseCustomKind,
    parsePluginKind,
    pluginKindFor,
    registerPluginViews,
} from '../catalogue';
import { clusters } from './health.svelte';
import { notices } from './notices.svelte';
import { detail, type DetailTarget } from './detail.svelte';
import { rememberSection } from '../components/settings/section.svelte';
import { defaultColorFor } from '../colors';
import { adoptKnownPlugin, adoptPluginCatalogue } from '../plugins/adopt';
import {
    emptyPluginCatalogue,
    type KnownPlugin,
    type Plugin,
    type PluginCatalogue,
    type PluginSectionSpec,
    type PluginViewSpec,
} from '../plugins/types';
import { adoptSource, type MetricsSource } from '../charts/adopt';
import { adoptCatalogue, adoptTokens, emptyCatalogue } from '../theme/adopt';
import { DEFAULT_THEME_ID, pickTheme, type Theme, type ThemeCatalogue, type ThemeToken } from '../theme/apply';


// The pane model is the app's vocabulary for "what is open and where", and it
// is re-exported here so that a component reaching for the store gets the types
// that go with it from the same place.
export {
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    PANE_IDS,
    PANE_LABELS,
    clustersTab,
    defaultPaneFor,
    detailsTab,
    isHorizontal,
    isDocumentView,
    isPaneId,
    isTabView,
    iconForView,
    resourceTabId,
    tabIdFor,
    MIN_PANE_SIZE,
    PANE_HEADROOM,
} from './panes';
export type { PaneId, PaneState, Tab, TabTarget, TabView } from './panes';
export { beginTabDrag, currentTabDrag, endTabDrag } from './tabdrag.svelte';
export type { TabDrag } from './tabdrag.svelte';
// Reachability moved to its own store; re-exported so the components drawing an
// indicator import the type from beside the thing they are drawing.
export type { Health, HealthStatus } from './health.svelte';
export type { Notice } from './notices.svelte';
export type { DetailTarget } from './detail.svelte';

/**
 * One tab in a pane, under the name the document views knew it by.
 *
 * Kept as an alias rather than renamed at every call site: LogView, TerminalView
 * and YamlEditor each take one as a prop, and what they do with it -- read its
 * object, key their own state by its id -- is unchanged by the tab having become
 * something that can sit anywhere.
 */
export type DockTab = Tab;

/**
 * A request to bring one context into view in the sidebar.
 *
 * The nonce is the point: asking for the same context twice has to register as
 * two separate requests, because clicking the tab you are already on is how you
 * ask "where is this cluster?" and it must answer every time.
 */
export interface Reveal {
    contextId: string;
    /** The resource kind to show, so the sidebar can scroll to its own row. */
    kind: string;
    nonce: number;
}

/**
 * The custom resources a cluster serves, as the sidebar's definitions section
 * needs them: loaded when that section is first opened for a context, and kept
 * for as long as the context is around.
 */
export interface CustomApiGroup {
    group: string;
    /** Non-null here, unlike the binding: normalised on the way in. */
    kinds: kube.CustomResourceKind[];
}

export interface CustomKinds {
    status: 'idle' | 'loading' | 'ready' | 'error';
    groups: CustomApiGroup[];
    message: string;
}

const NOT_LOADED: CustomKinds = { status: 'idle', groups: [], message: '' };

/**
 * Whether one saved tab can come back.
 *
 * A view this build has never heard of is skipped rather than guessed at, and
 * so are logs and shells: they are connections rather than state, and dialling
 * every cluster in the window before it is up is not a session being restored.
 */
function canRestore(known: Set<string>) {
    return (ref: { type: string; contextId: string; kind: string }): boolean => {
        if (!isTabView(ref.type)) return false;
        // The tree belongs to the window, not to a cluster, so there is no
        // context for it to be known by.
        if (ref.type === 'clusters') return true;
        if (ref.type === 'resource') return isAppTab(ref) || known.has(ref.contextId);
        return isDocumentView(ref.type) && known.has(ref.contextId);
    };
}

/** A predicate that spares the pinned tabs whatever else it says. */
function pinnedFirst(keep: (tab: Tab) => boolean): (tab: Tab) => boolean {
    return (tab) => tab.pinned === true || keep(tab);
}

/** One saved tab, as the store holds it. */
function tabFromRef(ref: {
    type: string;
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
    namespaces?: string[];
}): Tab {
    const view = ref.type as TabView;
    if (view === 'clusters') return clustersTab();
    const title =
        view === 'resource'
            ? ref.kind === DASHBOARD
                ? 'Dashboard'
                : labelFor(ref.kind)
            : ref.name;
    const id = tabIdFor(view, ref);
    // Seeded into views rather than carried on the tab, because views is where
    // the table looks as it mounts -- and a restored tab has to find its filter
    // there just as a tab brought forward again does. Only the filter is
    // restored; the sort and the search are not written down at all, for the
    // reasons in views.ts.
    if (view === 'resource' && ref.namespaces?.length) {
        views.remember(id, {
            sortColumn: null,
            sortDescending: false,
            namespaces: [...ref.namespaces],
            query: '',
            node: '',
        });
    }
    return {
        id,
        view,
        contextId: ref.contextId,
        kind: ref.kind,
        namespace: ref.namespace,
        name: ref.name,
        title,
    };
}

/** The panes of a settings file nothing has been opened in yet. */
function defaultPaneSettings(): Settings['panes'] {
    return {
        left: {
            tabs: [
                // The cluster tree, which is not a collection and so has no
                // namespace filter to carry.
                { type: 'clusters', contextId: '', kind: 'clusters', namespace: '', name: '', namespaces: [] },
            ],
            open: true,
            size: 260,
        },
        main: { tabs: [], open: true, size: 0 },
        right: { tabs: [], open: true, size: 420 },
        bottom: { tabs: [], open: false, size: 320 },
    };
}

function defaultSettings(): Settings {
    return {
        manualFiles: [],
        manualFolders: [],
        excludedFiles: [],
        excludedContexts: [],
        themeFolders: [],
        pluginFolders: [],
        hiddenPluginSuggestions: [],
        contexts: {},
        panes: defaultPaneSettings(),
        preferences: {
            theme: DEFAULT_THEME_ID,
            density: 'comfortable',
            restoreTabs: true,
            confirmSourceRemoval: false,
            showKubeconfigNames: false,
            contextSort: 'name',
            showLineNumbers: true,
            checkForUpdates: true,
            metricsRange: 60,
            terminal: {
                mode: 'app',
                external: '',
                shells: ['bash', 'sh'],
                nodeImage: 'busybox',
                nodeNamespace: 'default',
                fontSize: 12,
                scrollback: 5000,
            },
            helm: { path: '', wait: false, atomic: false, timeoutSeconds: 300 },
        },
        portForwards: [],
        layout: { detailPane: 'right', sidebarWidth: 320, collapsedGroups: null, zoom: 1 },
    };
}

/**
 * The settings tab's colour. Every other tab is painted with its cluster's, and
 * the whole point of that is to tell clusters apart -- so the one tab that is
 * not about a cluster takes the app's own accent instead of borrowing a
 * cluster's identity.
 */
const NEUTRAL_TAB_COLOR = '#7b8794';

/**
 * How two context names are compared when the sidebar is sorted.
 *
 * Numeric so `node-2` comes before `node-10`, and case-insensitive so a
 * capitalised alias is not filed away in a block of its own above the rest --
 * cluster names are read as words, not as byte strings.
 */
const byName = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** Zoom bounds, matching appconfig.MinZoom/MaxZoom on the Go side. */
const MIN_ZOOM = 0.5;
const MAX_ZOOM = 2;
const ZOOM_STEP = 0.1;


/** Adds or removes one label, whichever way the toggle should go. */
function toggled(groups: string[], label: string): string[] {
    return groups.includes(label) ? groups.filter((g) => g !== label) : [...groups, label];
}

/**
 * The list with one item moved, or null when the move would change nothing --
 * which is every out-of-range index a drag can produce at the ends of a strip.
 */
function moved<T>(list: T[], from: number, to: number): T[] | null {
    if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) {
        return null;
    }
    const next = [...list];
    const [item] = next.splice(from, 1);
    next.splice(to, 0, item);
    return next;
}

/** Whether two folding lists say the same thing, regardless of order. */
function sameGroups(a: string[], b: string[]): boolean {
    return a.length === b.length && [...a].sort().join('\u0000') === [...b].sort().join('\u0000');
}

/** The key an API group's open state is remembered under. */
function apiGroupKey(contextId: string, group: string): string {
    return `${contextId}\u0000${group}`;
}


/**
 * Whether a tab is the app-wide settings view rather than a look at a cluster.
 *
 * It belongs to no context, so every place that resolves a tab's contextId --
 * to colour it, to name it, to decide whether its cluster still exists -- has
 * to ask this first. Keyed off the kind rather than the empty contextId
 * because the kind is the thing that is actually meaningful.
 */
export function isSettingsTab(tab: { kind: string }): boolean {
    return tab.kind === SETTINGS;
}

/**
 * Whether a tab belongs to the window rather than to a cluster: settings, or
 * one of the documentation pages. These carry no context, so everything that
 * keys off a tab's cluster -- restoring, colouring, counting how many clusters
 * a pane shows -- has to ask this first.
 */
export function isAppTab(tab: { kind: string }): boolean {
    return APP_KINDS.includes(tab.kind);
}

/** The id the settings tab always has: one per window, belonging to nothing. */
const SETTINGS_TAB_ID = resourceTabId('', SETTINGS);

function message(err: unknown): string {
    if (err instanceof Error) return err.message;
    return String(err);
}

/**
 * The first line of a message, for the status bar, which has room for one.
 * The whole of a plugin's refusal is in Settings, where there is room.
 */
function firstLine(text: string): string {
    const at = text.indexOf('\n');
    return at < 0 ? text : `${text.slice(0, at)} (Settings → Plugins has the rest)`;
}

/**
 * debounce delays a call until the caller stops firing. Used so that dragging a
 * splitter or typing in the rename field does not write the settings file on
 * every frame or keystroke.
 */
function debounce<A extends unknown[]>(fn: (...args: A) => void, ms: number): (...args: A) => void {
    let timer: ReturnType<typeof setTimeout> | undefined;
    return (...args: A) => {
        clearTimeout(timer);
        timer = setTimeout(() => fn(...args), ms);
    };
}

/**
 * The parts of the settings the app writes independently. Each has one
 * debounced writer, and each is what a stale answer can roll back -- see
 * Workspace.writer.
 */
type Section = 'contexts' | 'panes' | 'layout' | 'preferences';

class Workspace {
    /**
     * Tells the detail store how to put its report on screen.
     *
     * Done here rather than in the store because these two operations are the
     * only thing it needs from the tab machinery, and handing them over is what
     * lets the two modules stay pointed one way: this one knows about the
     * report, the report knows nothing about panes.
     */
    constructor() {
        detail.connect({
            show: (target) => this.showDetailsTab(target),
            hide: () => this.hideDetailsTab(),
        });
    }

    /** Kubeconfig files from the last sync, each with its parsed contexts. */
    files = $state<ConfigFile[]>([]);
    /** Persisted user preferences, replaced wholesale by every backend write. */
    settings = $state<Settings>(defaultSettings());
    /**
     * Every open view, by the pane it is in.
     *
     * Tabs belong to the window rather than to a cluster: selecting another
     * context, or closing every other tab there is, leaves a pane exactly as it
     * was, because what is open in it may be a document you are part way
     * through editing.
     */
    panes = $state<Record<PaneId, PaneState>>(defaultPanes());
    /** The context whose resource tree is showing, and whose settings the bottom panel edits. */
    selectedContextId = $state<string | null>(null);
    /** Contexts whose resource tree is expanded in the sidebar. */
    expanded = $state<string[]>([]);

    /**
     * The object's revision the report on screen was read at. The panel
     * compares it against the object's revision now: when they differ, the
     * object has been written since and the report is out of date.
     */
    /**
     * Which describe the panel is waiting for. Two can be in flight at once --
     * a save starts one while an open one is still out -- and closing the panel
     * must leave neither able to land.
     */

    /** Reachability per context, written by both probes and tab outcomes. */
    /** Custom resource definitions per context, loaded on demand. */
    customKinds = $state<Record<string, CustomKinds>>({});
    /**
     * Which API groups are open, as `contextId\0group`. Not persisted: it is
     * where you are looking right now rather than how you like the sidebar, and
     * it would otherwise grow the settings file a line per group per cluster.
     */
    expandedApiGroups = $state<string[]>([]);

    /** The sidebar's standing request to scroll a context into view. */
    reveal = $state<Reveal | null>(null);
    private revealCount = 0;

    /**
     * Every theme available: the ones that ship with the app, and the ones read
     * out of the user's theme folders. It arrives from the Go side already
     * resolved -- each theme carrying its complete token set -- so the settings
     * gallery can draw a true preview of all of them without a call per swatch.
     */
    themeCatalogue = $state<ThemeCatalogue>(emptyCatalogue());
    /**
     * The tokens a theme may set, with what each is for. Fetched once and shown
     * in the settings view, so writing a theme does not mean reading the source.
     */
    themeTokens = $state<ThemeToken[]>([]);

    /**
     * The solution plugins installed on this machine: the ones that ship with
     * the app, and whatever is in the user's plugin folders.
     *
     * Nothing here is per-cluster. Whether the solution a plugin describes is
     * actually in the cluster in front of you is a different question, answered
     * by pluginInstalledIn below and, authoritatively, by the plugin's overview.
     */
    pluginCatalogue = $state<PluginCatalogue>(emptyPluginCatalogue());
    /**
     * The plugins kept in repositories of their own that the app knows of --
     * see PluginService.Known. Read once: the list is compiled into the app.
     */
    knownPlugins = $state<KnownPlugin[]>([]);
    /** Whether the plugins that would not load have been mentioned yet this session. */
    private toldPluginProblems = false;
    /**
     * Which plugins are unfolded in the sidebar, as `contextId\0pluginId`.
     * Not persisted, for the same reason expandedApiGroups is not: it is where
     * you are looking right now rather than how you like the sidebar.
     */
    expandedPlugins = $state<string[]>([]);

    syncing = $state(false);
    loaded = $state(false);
    configPath = $state('');

    contexts = $derived(this.files.flatMap((f) => f.contexts));
    /** The folders being watched, listed in the sidebar so one can be dropped. */
    folders = $derived(this.settings.manualFolders);
    /** Files the user has hidden, so the sidebar can offer to show them again. */
    excluded = $derived(this.settings.excludedFiles);
    /**
     * Contexts the user has removed one by one, listed for the same reason the
     * hidden files are: a rescan finds them and hides them again, so this is
     * the only place the removal can be seen or undone.
     */
    removedContexts = $derived(this.settings.excludedContexts);
    /** Every open tab, in every pane. */
    allTabs = $derived(PANE_IDS.flatMap((pane) => this.panes[pane].tabs));
    /**
     * The main pane, under the names the app used before there were panes.
     *
     * They are kept because "the tabs" and "the dock" are still what most of
     * the app means: a resource view opens in the middle and a document at the
     * foot unless the user has moved it. Anything that has to be right about
     * *every* pane uses `allTabs` or asks by pane id.
     */
    tabs = $derived(this.panes.main.tabs);
    activeTabId = $derived(this.panes.main.activeId);
    activeTab = $derived(this.panes.main.tabs.find((t) => t.id === this.panes.main.activeId) ?? null);
    dockTabs = $derived(this.panes.bottom.tabs);
    activeDockTabId = $derived(this.panes.bottom.activeId);
    activeDockTab = $derived(
        this.panes.bottom.tabs.find((t) => t.id === this.panes.bottom.activeId) ?? null,
    );
    /**
     * The tab last brought forward, in whichever pane it lives.
     *
     * Distinct from `activeTab`, which is the main pane's: a pod list dragged
     * into the right panel is still the thing you are looking at, and the
     * sidebar has to be able to say so. What is *showing* is per pane; what has
     * the user's attention is one answer for the window.
     */
    focusedTabId = $state<string | null>(null);
    focusedTab = $derived(
        (this.focusedTabId === null ? null : this.tabFor(this.focusedTabId)) ?? this.activeTab,
    );
    selectedContext = $derived(this.contexts.find((c) => c.id === this.selectedContextId) ?? null);
    /** Files that could not be parsed, surfaced in the sidebar rather than dropped. */
    brokenFiles = $derived(this.files.filter((f) => f.error !== ''));
    /**
     * Whether any context on screen is open, which is what decides the
     * direction of the sidebar's expand/collapse toggle. It is checked against
     * the live contexts rather than the raw list, so ids left over from a
     * kubeconfig that has gone do not make the button offer to collapse
     * something nobody can see.
     */
    anyExpanded = $derived(this.contexts.some((c) => this.expanded.includes(c.id)));

    /**
     * The pane the describe tab opens in.
     *
     * Remembered rather than fixed, because the tab comes and goes with the
     * selection and so is never written into `panes` -- there would be nothing
     * to restore it against. Drag it into the bottom panel and this is what
     * remembers, so the next row you click describes itself down there.
     */
    detailPane = $derived(
        isPaneId(this.settings.layout.detailPane)
            ? this.settings.layout.detailPane
            : defaultPaneFor('details'),
    );
    /** The cluster tree's pane width. Kept under its old name for the settings view. */
    sidebarWidth = $derived(this.panes.left.size);
    /** How tall the bottom pane stands when it is open, in px. */
    dockSize = $derived(this.panes.bottom.size);
    /**
     * Whether the bottom pane is showing its contents rather than just its
     * tabs. Its strip is always on screen -- that is what makes it a place
     * things can be put -- so this only decides whether the view is drawn.
     */
    dockOpen = $derived(this.panes.bottom.open && this.panes.bottom.tabs.length > 0);
    /**
     * The folded groups, falling back to the catalogue's defaults only while
     * the user has never chosen. `?? ` rather than `||` is load-bearing: an
     * empty list is a real choice and must not be defaulted over.
     */
    collapsedGroups = $derived(this.settings.layout.collapsedGroups ?? DEFAULT_COLLAPSED_GROUPS);
    /** The webview scale, 1 being normal size. */
    zoom = $derived(this.settings.layout.zoom || 1);
    /** The range the scale may be set to, for the settings view's slider. */
    readonly minZoom = MIN_ZOOM;
    readonly maxZoom = MAX_ZOOM;
    readonly zoomStep = ZOOM_STEP;

    /** The id of the theme the user chose. May name one that is not installed. */
    theme = $derived(this.settings.preferences.theme);
    /** The themes on offer, in the order the gallery shows them. */
    themes = $derived(this.themeCatalogue.themes);
    /** The folder user themes are read from by default. */
    themeDir = $derived(this.themeCatalogue.dir);
    /** The extra folders the user has added themes from. */
    themeFolders = $derived(this.themeCatalogue.folders);
    /** Theme files that could not be read, surfaced rather than dropped. */
    themeProblems = $derived(this.themeCatalogue.problems);

    /**
     * Where each context's metrics come from, once anything has asked. Keyed by
     * context id and filled in lazily, because working it out costs a list of
     * every Service in the cluster and most contexts are never looked at.
     */
    metricsSources = $state<Record<string, MetricsSource>>({});
    /**
     * The surfaces some installed plugin draws charts on: resource kinds,
     * `dashboard`, `overview`.
     *
     * Known before any cluster is asked, because it depends only on which
     * plugins are installed. That is what lets a chart panel decide whether to
     * exist at all without first flashing a heading and then taking it away --
     * and it means a pod in a cluster with no charting plugin costs nothing.
     */
    metricsAttachments = $state<string[]>([]);

    /** Whether any installed plugin charts this surface. */
    chartsAttachTo(surface: string): boolean {
        return this.metricsAttachments.includes(surface);
    }
    /**
     * The theme actually in force. Null only before the catalogue has loaded,
     * when the app is wearing whatever style.css and the cache last left it in.
     *
     * It falls back rather than failing when the chosen id names nothing,
     * because a theme can be removed from under a settings file that still
     * names it -- by deleting the file, dropping the folder it came from, or
     * opening the same settings on a machine without it installed.
     */
    activeTheme = $derived<Theme | null>(pickTheme(this.themes, this.theme));
    /**
     * Whether the chosen theme is one we could not find. Distinct from having
     * no theme at all: the app looks fine either way, so the only place this
     * shows is the settings view, which says which id is missing.
     */
    themeMissing = $derived(
        this.themes.length > 0 && !this.themes.some((t) => t.id === this.theme),
    );

    /**
     * Every plugin installed on this machine, switched on or off. This is the
     * settings view's list: a disabled plugin has to appear somewhere or there
     * would be no way back.
     */
    plugins = $derived(this.pluginCatalogue.plugins);
    /**
     * The plugins on offer, in the order the sidebar shows them. Everything
     * that *offers* a plugin -- the sidebar, the charts, the overview -- goes
     * through this rather than through `plugins`.
     */
    enabledPlugins = $derived(this.pluginCatalogue.plugins.filter((p) => !p.disabled));
    /** The folder user plugins are read from by default. */
    pluginDir = $derived(this.pluginCatalogue.dir);
    /** The extra folders the user has added plugins from. */
    pluginFolders = $derived(this.pluginCatalogue.folders);
    /** Plugin files that could not be read, surfaced rather than dropped. */
    pluginProblems = $derived(this.pluginCatalogue.problems);
    density = $derived(this.settings.preferences.density);
    restoreTabsOnLaunch = $derived(this.settings.preferences.restoreTabs);
    confirmSourceRemoval = $derived(this.settings.preferences.confirmSourceRemoval);
    checkForUpdates = $derived(this.settings.preferences.checkForUpdates);
    /** Whether the sidebar groups contexts under the kubeconfig they came from. */
    showKubeconfigNames = $derived(this.settings.preferences.showKubeconfigNames);
    /**
     * The order the sidebar lists contexts in. See `orderContexts`, which is
     * the only thing that should be reading it.
     */
    contextSort = $derived(this.settings.preferences.contextSort);
    /** Whether the YAML editor draws a line-number gutter. On by default. */
    showLineNumbers = $derived(this.settings.preferences.showLineNumbers);
    /**
     * How far back a metrics chart looks, in minutes. One setting for every
     * chart on screen: a page whose charts each carried their own window would
     * be a page whose numbers cannot be compared, and comparing them is the
     * reason they are next to each other.
     */
    metricsRange = $derived(this.settings.preferences.metricsRange || 60);

    // ----- loading -------------------------------------------------------

    /** Loads settings and kubeconfigs, then restores the tabs from last time. */
    async load(): Promise<void> {
        try {
            this.settings = adoptSettings(await SettingsService.Get());
            this.configPath = await SettingsService.ConfigPath();
        } catch (err) {
            notices.fail(`Could not read settings: ${message(err)}`);
        }
        // Before the kubeconfig sync, which talks to clusters and can take a
        // while: the theme is what the user sees first, and waiting on a
        // cluster to find out what colour the window is would be the wrong way
        // round. The plugins go with it because the sidebar draws their rows
        // and the tab bar titles their tabs, both before any cluster answers.
        await Promise.all([this.loadThemes(), this.loadPlugins()]);
        // The forwards from last session, as rows waiting to be reconnected.
        // Nothing is dialled here: see PortForwardService for why launching the
        // app must not open every tunnel in the list.
        //
        // Not awaited -- the window should not wait on the forward list to come
        // up -- but the failure is still caught: an unhandled rejection here is
        // reported as an error against whatever happens to be running, and the
        // user is told nothing about the one read that actually failed.
        void forwards.load().catch((err: unknown) => {
            notices.fail(`Could not read the port forwards: ${message(err)}`);
        });
        await this.sync({ restoreTabs: true });
        this.loaded = true;
    }

    /**
     * Reads the theme catalogue. Also the "reload" the settings view offers,
     * which is how a theme edited in an editor gets picked up without
     * restarting the app.
     */
    async loadThemes(): Promise<void> {
        try {
            this.themeCatalogue = adoptCatalogue(await ThemeService.List());
            if (this.themeTokens.length === 0) {
                this.themeTokens = adoptTokens(await ThemeService.Tokens());
            }
        } catch (err) {
            notices.fail(`Could not read themes: ${message(err)}`);
        }
    }

    /**
     * Rescans every kubeconfig source. `restoreTabs` is for the initial load,
     * which reopens last session's tabs; a later sync keeps the tabs that are
     * open and only drops those whose context has gone.
     */
    async sync({ restoreTabs = false } = {}): Promise<void> {
        this.syncing = true;
        try {
            this.files = adoptFiles(await KubeconfigService.Sync());
            clusters.prune(this.contexts.map((c) => c.id));
            this.pruneCustomKinds();
            if (restoreTabs) {
                this.restorePanes();
            } else {
                this.dropTabsForMissingContexts();
                clusters.recheck();
                this.recheckCustomKinds();
                // Only for a sync the user asked for: a rescan that finds
                // nothing new looks identical to one that did not run.
                notices.inform(
                    `${this.contexts.length} context${this.contexts.length === 1 ? '' : 's'} ` +
                        `in ${this.files.length} file${this.files.length === 1 ? '' : 's'}`,
                );
            }
            this.ensureSelection();
        } catch (err) {
            notices.fail(`Sync failed: ${message(err)}`);
        } finally {
            this.syncing = false;
        }
    }

    /** Opens the native picker and adds whichever kubeconfig the user chose. */
    async addFile(): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.BrowseForFile());
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            // Picking several files at once can partly succeed. The message
            // names what failed, but the ones that worked are already stored,
            // so the sidebar has to be brought up to date regardless.
            notices.fail(message(err));
            await this.reload();
        }
    }

    /** Watches a folder, adding every kubeconfig in it. */
    async addFolder(): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.BrowseForFolder());
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /** Shows a hidden kubeconfig again. */
    async restoreFile(path: string): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.RestoreFile(path));
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /** Shows a removed context again. */
    async restoreContext(id: string): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.RestoreContext(id));
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /**
     * Asks before dropping a kubeconfig source, when the user has turned that
     * on. Every one of them is undoable -- a hidden file and a removed context
     * are both listed under Hidden, and a folder can be re-added -- so this is
     * off by default and exists for people who would rather not have to undo.
     */
    private confirmRemoval(question: string): boolean {
        if (!this.confirmSourceRemoval) return true;
        if (typeof window === 'undefined' || !window.confirm) return true;
        return window.confirm(question);
    }

    /** Stops watching a folder; its configs leave the sidebar with it. */
    async removeFolder(path: string): Promise<void> {
        if (!this.confirmRemoval(`Stop watching ${path}?\n\nIts kubeconfigs leave the sidebar with it.`)) {
            return;
        }
        try {
            this.files = adoptFiles(await KubeconfigService.RemoveFolder(path));
            this.settings = adoptSettings(await SettingsService.Get());
            this.dropTabsForMissingContexts();
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /** Re-reads the current file list without rescanning the disk. */
    private async reload(): Promise<void> {
        try {
            this.files = adoptFiles(await KubeconfigService.Files());
            this.settings = adoptSettings(await SettingsService.Get());
            this.ensureSelection();
        } catch {
            // Already reporting the failure that got us here.
        }
    }

    /** Forgets a kubeconfig the user added, closing any tabs that depended on it. */
    async removeFile(path: string): Promise<void> {
        if (!this.confirmRemoval(`Remove ${path}?\n\nAny tabs open against its contexts will close.`)) {
            return;
        }
        try {
            this.files = adoptFiles(await KubeconfigService.RemoveFile(path));
            this.settings = adoptSettings(await SettingsService.Get());
            this.dropTabsForMissingContexts();
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /**
     * Removes one context from this app, leaving its file and the rest of the
     * contexts in it alone. Nothing is written to the kubeconfig: the removal
     * is the app's own, and it is listed under Hidden in the sidebar until it
     * is undone -- see restoreContext.
     */
    async removeContext(contextId: string): Promise<void> {
        const name = this.contexts.find((c) => c.id === contextId)?.name ?? contextId;
        if (!this.confirmRemoval(`Remove ${name}?\n\nAny tabs open on it will close. The kubeconfig is not changed, and a rescan will not bring it back — it is listed under Hidden in the sidebar until you show it again.`)) {
            return;
        }
        try {
            this.files = adoptFiles(await KubeconfigService.RemoveContext(contextId));
            this.settings = adoptSettings(await SettingsService.Get());
            this.dropTabsForMissingContexts();
            this.ensureSelection();
        } catch (err) {
            notices.fail(message(err));
        }
    }

    // ----- contexts ------------------------------------------------------

    /** The name to show for a context: the user's alias, or the kubeconfig name. */
    displayName(context: kube.Context): string {
        return this.settings.contexts[context.id]?.alias?.trim() || context.name;
    }

    /**
     * Contexts in the order the sidebar shows them.
     *
     * Sorted on the *displayed* name rather than the kubeconfig's, so a context
     * the user renamed lands where its new name says it should; sorting on a
     * name nobody can see would look like no sorting at all. The list is copied
     * before it is sorted -- the array belongs to the file it came from, and
     * reordering that in place would leave 'kubeconfig' with nothing to go back
     * to after a sync.
     */
    orderContexts(contexts: kube.Context[]): kube.Context[] {
        if (this.contextSort === 'kubeconfig') return contexts;
        const direction = this.contextSort === 'name-desc' ? -1 : 1;
        return [...contexts].sort(
            (a, b) => direction * byName.compare(this.displayName(a), this.displayName(b)),
        );
    }

    /**
     * Kubeconfig files in the order the sidebar heads them, by file name rather
     * than by the whole path: the path is what the heading's tooltip says, and
     * the name is what it shows.
     */
    orderFiles<T extends { path: string }>(files: T[]): T[] {
        if (this.contextSort === 'kubeconfig') return files;
        const direction = this.contextSort === 'name-desc' ? -1 : 1;
        const name = (path: string) => path.split('/').pop() || path;
        return [...files].sort((a, b) => direction * byName.compare(name(a.path), name(b.path)));
    }

    /**
     * The colour for a context: the user's choice, or one derived from its id.
     *
     * An empty id is the settings tab, which belongs to no cluster. Deriving a
     * colour for it would paint it as if it did, and always the same one, so
     * it is given the neutral accent instead.
     */
    colorOf(contextId: string): string {
        if (!contextId) return NEUTRAL_TAB_COLOR;
        return this.settings.contexts[contextId]?.color || defaultColorFor(contextId);
    }

    /** True if the user has set an alias or colour for this context. */
    isCustomised(contextId: string): boolean {
        const prefs = this.settings.contexts[contextId];
        return Boolean(prefs?.alias || prefs?.color);
    }

    /**
     * Records an alias and colour. The UI updates immediately and the write is
     * debounced, because this is called from a text field on every keystroke.
     */
    setContextPrefs(contextId: string, alias: string, color: string): void {
        this.writeContextPrefs(contextId, { alias, color });
    }

    /**
     * Changes part of what is remembered about one context and saves the whole
     * record.
     *
     * Every caller here changes one thing -- a colour, a folding override, a
     * column width -- but SetContextPrefs takes the whole record and replaces
     * it, so anything not carried through is deleted. Rebuilding it by hand at
     * each call site is how the metrics endpoint used to be lost by the folding
     * writer: a field added later is a field some earlier writer forgets. One
     * merge means a new preference is carried by all of them at once.
     *
     * A context left with nothing said about it is forgotten rather than kept as
     * an empty record, which is the same rule the store applies on the far side.
     */
    private writeContextPrefs(contextId: string, patch: Partial<ContextPrefs>): void {
        const current = this.settings.contexts[contextId];
        const merged: ContextPrefs = {
            alias: current?.alias ?? '',
            color: current?.color ?? '',
            metrics: current?.metrics ?? '',
            collapsedGroups: current?.collapsedGroups ?? null,
            columns: current?.columns ?? {},
            ...patch,
        };

        if (isEmptyContextPrefs(merged)) {
            delete this.settings.contexts[contextId];
        } else {
            this.settings.contexts[contextId] = merged;
        }
        this.persistContextPrefs(contextId, $state.snapshot(merged) as appconfig.ContextPrefs);
    }

    private persistContextPrefs = this.writer(
        'contexts',
        (contextId: string, prefs: appconfig.ContextPrefs) =>
            SettingsService.SetContextPrefs(contextId, prefs),
        'Could not save context settings',
        300,
    );

    /** Clears the alias and colour, returning the context to its defaults. */
    resetContextPrefs(contextId: string): void {
        this.setContextPrefs(contextId, '', '');
    }

    // ----- table columns -------------------------------------------------
    //
    // Per kind per context, because the same kind is not the same table in two
    // clusters: the pods of a dev cluster have short names and the pods of a
    // production one have long ones, and a width dragged for the second is the
    // wrong width for the first.
    //
    // Stored on the context record with the alias and the colour, so it goes
    // out through the writer those already use. A second writer over the same
    // record would race them -- every settings call answers with the whole
    // file -- and dragging a column while a rename was in flight would carry
    // the old name back.

    /** What the user changed about one kind's table here. Never null. */
    columnPrefs(contextId: string, kind: string): ColumnPrefs {
        return this.settings.contexts[contextId]?.columns[kind] ?? noColumnPrefs();
    }

    /** Records the width a column was dragged to, clamped to a usable range. */
    setColumnWidth(contextId: string, kind: string, column: string, px: number): void {
        const prefs = this.columnPrefs(contextId, kind);
        this.writeColumns(contextId, kind, {
            ...prefs,
            widths: { ...prefs.widths, [column]: clampColumnWidth(px) },
        });
    }

    /**
     * Puts one column back to sizing itself to its contents. What a double
     * click on its edge does, and the only way back to the default width --
     * dragging can reach any width but the one the browser would have chosen.
     */
    clearColumnWidth(contextId: string, kind: string, column: string): void {
        const prefs = this.columnPrefs(contextId, kind);
        if (!(column in prefs.widths)) return;
        const widths = { ...prefs.widths };
        delete widths[column];
        this.writeColumns(contextId, kind, { ...prefs, widths });
    }

    /** Whether a column is turned off in one kind's table here. */
    isColumnHidden(contextId: string, kind: string, column: string): boolean {
        return this.columnPrefs(contextId, kind).hidden.includes(column);
    }

    /**
     * Shows or hides one column.
     *
     * Sorted on the way in for the reason the store sorts it: the settings file
     * is written whole, so a list in click order would produce a diff every
     * time a column was hidden and shown again.
     */
    setColumnHidden(contextId: string, kind: string, column: string, hidden: boolean): void {
        const prefs = this.columnPrefs(contextId, kind);
        if (prefs.hidden.includes(column) === hidden) return;
        const next = hidden
            ? [...prefs.hidden, column].sort()
            : prefs.hidden.filter((name) => name !== column);
        this.writeColumns(contextId, kind, { ...prefs, hidden: next });
    }

    /** Shows every column again, leaving the widths as they are. */
    showAllColumns(contextId: string, kind: string): void {
        const prefs = this.columnPrefs(contextId, kind);
        if (prefs.hidden.length === 0) return;
        this.writeColumns(contextId, kind, { ...prefs, hidden: [] });
    }

    /**
     * Puts one kind's table back to how it arrives: every column shown, each at
     * the width its contents want.
     */
    resetColumns(contextId: string, kind: string): void {
        if (!this.settings.contexts[contextId]?.columns[kind]) return;
        this.writeColumns(contextId, kind, noColumnPrefs());
    }

    /** Whether the user has changed anything about one kind's table here. */
    hasColumnPrefs(contextId: string, kind: string): boolean {
        const prefs = this.settings.contexts[contextId]?.columns[kind];
        return !!prefs && (Object.keys(prefs.widths).length > 0 || prefs.hidden.length > 0);
    }

    /**
     * Writes one kind's table settings, dropping the entry when the user has
     * put everything back -- which is what lets a context with nothing else set
     * be forgotten rather than kept for an empty record.
     */
    private writeColumns(contextId: string, kind: string, prefs: ColumnPrefs): void {
        const columns = { ...(this.settings.contexts[contextId]?.columns ?? {}) };
        if (Object.keys(prefs.widths).length === 0 && prefs.hidden.length === 0) {
            delete columns[kind];
        } else {
            columns[kind] = prefs;
        }
        this.writeContextPrefs(contextId, { columns });
    }

    /**
     * Forgets the settings for columns a kind no longer has.
     *
     * A CRD's printer columns are the definition's to change, and a kind the
     * app itself lists can gain or lose one between releases. Neither is worth
     * a prompt, but a width kept for a column nobody can see is a width that
     * comes back if the column ever does -- at whatever the user dragged it to
     * a year ago -- so a table reports its real columns as it loads and
     * anything else is dropped.
     *
     * Only ever narrows, and only for a kind the user has actually touched, so
     * a table that fails to load and reports nothing cannot clear the record.
     */
    pruneColumns(contextId: string, kind: string, columns: string[]): void {
        const prefs = this.settings.contexts[contextId]?.columns[kind];
        if (!prefs || columns.length === 0) return;

        const known = new Set(columnKeys(columns));
        const widths = Object.fromEntries(
            Object.entries(prefs.widths).filter(([column]) => known.has(column)),
        );
        const hidden = prefs.hidden.filter((column) => known.has(column));
        if (
            Object.keys(widths).length === Object.keys(prefs.widths).length &&
            hidden.length === prefs.hidden.length
        ) {
            return;
        }
        this.writeColumns(contextId, kind, { widths, hidden });
    }

    /** Selects a context in the sidebar and expands its resource tree. */
    selectContext(contextId: string): void {
        this.selectedContextId = contextId;
        if (!this.expanded.includes(contextId)) {
            this.expanded = [...this.expanded, contextId];
        }
        void clusters.probe(contextId);
    }

    /**
     * What clicking a context's name does: show it, and close it again if it is
     * the one already showing.
     *
     * The second click is the point -- opening a context and then having the
     * same click do nothing is a dead end. But it only closes the context you
     * are already on: clicking a *different* one means "show me this instead",
     * and folding away what you just reached for would be the opposite of that.
     */
    activateContext(contextId: string): void {
        if (this.selectedContextId === contextId && this.isExpanded(contextId)) {
            // Left selected: its settings panel is about the context, not about
            // whether its resource tree happens to be open.
            this.expanded = this.expanded.filter((id) => id !== contextId);
            return;
        }
        this.selectContext(contextId);
    }

    toggleExpanded(contextId: string): void {
        const opening = !this.expanded.includes(contextId);
        this.expanded = opening
            ? [...this.expanded, contextId]
            : this.expanded.filter((id) => id !== contextId);
        // Opening a context is a reason to know whether it answers; collapsing
        // one is not.
        if (opening) void clusters.probe(contextId);
    }

    isExpanded(contextId: string): boolean {
        return this.expanded.includes(contextId);
    }

    /**
     * Opens every context's resource tree.
     *
     * Note what this deliberately does not do: probe. `toggleExpanded` checks a
     * cluster when you open it, which is affordable one at a time, but building
     * a client can run an exec credential plugin -- so routing "expand all"
     * through it would launch a subprocess per context from a single click.
     * Unfolding the tree is a view operation; it asks no cluster anything.
     *
     * Assigning the whole list rather than merging also drops any stale ids
     * left behind by contexts that have since left the kubeconfig.
     */
    expandAll(): void {
        this.expanded = this.contexts.map((c) => c.id);
    }

    /** Closes every context's resource tree. */
    collapseAll(): void {
        this.expanded = [];
    }

    // ----- tabs and panes ------------------------------------------------
    //
    // One set of operations for every pane. Which pane a tab is in is a
    // property of the tab, not of the code that opens or closes it, so the only
    // thing that differs between "close this editor" and "close this pod list"
    // is which pane the predicate runs over.

    /** The cluster tree's tab id, so a component can ask where the tree is. */
    readonly CLUSTERS_TAB_ID = CLUSTERS_TAB_ID;

    /** The pane a tab is in, or null if no pane holds it. */
    paneOf(id: string): PaneId | null {
        return PANE_IDS.find((pane) => this.panes[pane].tabs.some((t) => t.id === id)) ?? null;
    }

    /** One tab, wherever it is. */
    tabFor(id: string): Tab | null {
        for (const pane of PANE_IDS) {
            const tab = this.panes[pane].tabs.find((t) => t.id === id);
            if (tab) return tab;
        }
        return null;
    }

    /** The tab a pane is showing. */
    activeTabIn(pane: PaneId): Tab | null {
        const state = this.panes[pane];
        return state.tabs.find((t) => t.id === state.activeId) ?? null;
    }

    /**
     * Opens a tab, or focuses it if this context/kind pair is already open --
     * in whichever pane the user has put it.
     *
     * A new tab lands immediately right of the pane's active one rather than at
     * the far end of the strip: it was almost always opened from the view you
     * were looking at, so that is where you will look for it, and a long strip
     * means the end of it may not even be on screen.
     */
    openTab(contextId: string, kind: string): void {
        const id = resourceTabId(contextId, kind);
        if (this.paneOf(id) === null) {
            this.insertTab('main', {
                id,
                view: 'resource',
                contextId,
                kind,
                namespace: '',
                name: '',
                title: kind === DASHBOARD ? 'Dashboard' : labelFor(kind),
            });
        }
        this.activateTab(id);
    }

    /**
     * Opens the pod listing narrowed to one node.
     *
     * The question a node raises is "what is running on it" -- before a drain,
     * after an eviction, or when one node is the busy one -- and the answer was
     * only reachable by opening every pod in the cluster and reading the Node
     * column. It is the same drill-through a CustomResourceDefinition's name
     * offers, applied to the other place in the app where one object names a
     * list of others.
     *
     * One pods tab per cluster, as always: this narrows the tab rather than
     * opening a second one, the way the namespace picker does. The filter is
     * written before the tab is opened so a tab being built for the first time
     * reads it as it mounts.
     */
    showPodsOnNode(contextId: string, node: string): void {
        views.focusNode(resourceTabId(contextId, PODS), node);
        this.openTab(contextId, PODS);
    }

    /**
     * Opens the app-wide settings, or focuses it if already open.
     *
     * It goes to the far end of the strip rather than beside the current tab,
     * unlike openTab: it was not opened *from* the view you were looking at,
     * and pushing it into the middle of a row of clusters would break up the
     * grouping the user built by hand.
     */
    openSettings(): void {
        this.openAppTab(SETTINGS);
    }

    /** Opens the help page for this app, or focuses it if already open. */
    openHelp(): void {
        this.openAppTab(HELP);
    }

    /** Opens the Kubernetes primer, or focuses it if already open. */
    openKubernetesPrimer(): void {
        this.openAppTab(KUBERNETES);
    }

    /**
     * Opens one of the window's own tabs -- settings, help, the primer -- at
     * the far end of the strip, once. See openSettings for why the end.
     */
    private openAppTab(kind: string): void {
        const id = resourceTabId('', kind);
        if (this.paneOf(id) === null) {
            this.insertTab(
                'main',
                {
                    id,
                    view: 'resource',
                    contextId: '',
                    kind,
                    namespace: '',
                    name: '',
                    title: labelFor(kind),
                },
                { atEnd: true },
            );
        }
        this.activateTab(id);
    }

    /**
     * Puts a tab into a pane, beside whatever that pane is showing.
     *
     * Nothing here activates it or opens the pane: the callers differ on both --
     * restoring a session opens nothing, and moving a tab between panes must not
     * re-run the "opened from here" placement.
     */
    private insertTab(pane: PaneId, tab: Tab, { atEnd = false, index }: { atEnd?: boolean; index?: number } = {}): void {
        const state = this.panes[pane];
        const at =
            index !== undefined
                ? Math.max(0, Math.min(index, state.tabs.length))
                : atEnd
                  ? state.tabs.length
                  : state.tabs.findIndex((t) => t.id === state.activeId) + 1 || state.tabs.length;

        state.tabs = [...state.tabs.slice(0, at), tab, ...state.tabs.slice(at)];
        this.persistPanes();
    }

    /**
     * Focuses a tab, and gives the room back when it is the one already showing
     * in a pane that can fold.
     *
     * The second click is the point, as it is for a context in the sidebar:
     * clicking the tab you are on has to do something, and in the bottom panel
     * what it should do is hand the space back to the view above.
     */
    activateTab(id: string): void {
        const pane = this.paneOf(id);
        if (pane === null) return;

        const state = this.panes[pane];
        const tab = state.tabs.find((t) => t.id === id);
        if (!tab) return;

        if (pane === 'bottom' && state.activeId === id && state.open) {
            this.setPaneOpen('bottom', false);
            return;
        }

        const changed = state.activeId !== id;
        state.activeId = id;
        this.focusedTabId = id;
        if (!state.open) this.setPaneOpen(pane, true);

        // A collection tab says which cluster it is looking at, and the sidebar
        // follows it there. A document or a stream does not: it is one object,
        // and you opened it from wherever you already were.
        if (tab.view === 'resource' && !isAppTab(tab)) {
            this.selectContext(tab.contextId);
            this.showSectionFor(tab);
            // Asked for here rather than in selectContext, which the sidebar
            // also calls: a context clicked in the sidebar is already under the
            // pointer, and scrolling it would move it out from under them.
            this.reveal = { contextId: tab.contextId, kind: tab.kind, nonce: ++this.revealCount };
        }
        // Only when the view actually changed, only for a collection, and only
        // for a different collection: the report describes an object in a list,
        // so keeping it over an unrelated one would be misleading -- but going
        // back to the list the object came from is not leaving it, which is a
        // round trip you can make now that the report is a tab beside it.
        // Bringing an editor forward is not leaving the list either.
        if (changed && tab.view === 'resource' && !detail.describesTheListIn(tab)) {
            detail.close();
        }
    }

    /**
     * Unfolds everything an activated tab's row is folded inside, so that the
     * tree shows where the tab you are looking at actually lives.
     *
     * For most kinds that is one section. Two of them nest a second level, and
     * a row two levels down is just as hidden as one: a custom resource sits
     * under its API group under Custom Resource Definitions, and a plugin view
     * under its plugin under Plugins. Opening only the outer section left the
     * row unrendered, so the sidebar had nothing to scroll to and fell back to
     * the cluster's name -- which is what "switching to my CRD tab does not
     * find it in the tree" looked like.
     *
     * Each step does nothing when it is already open, which matters for the
     * sections: writing unconditionally would give the context its own folding
     * on every tab click, pinning it against later changes applied to every
     * cluster. The API group and plugin folds are per context already, so they
     * carry no such cost.
     */
    private showSectionFor(tab: Tab): void {
        const custom = parseCustomKind(tab.kind);
        if (custom) {
            this.openGroup(tab.contextId, DEFINITIONS_GROUP);
            if (!this.isApiGroupExpanded(tab.contextId, custom.group)) {
                this.toggleApiGroup(tab.contextId, custom.group);
            }
            // The rows come from the cluster, and the sidebar only asks for
            // them once its section is showing. Reaching a definition through
            // a restored tab rather than by opening that section means nothing
            // has asked yet, and the reveal would arrive before the row exists.
            void this.loadCustomKinds(tab.contextId);
            return;
        }

        const view = parsePluginKind(tab.kind);
        if (view) {
            this.openGroup(tab.contextId, PLUGINS_GROUP);
            if (!this.isPluginExpanded(tab.contextId, view.pluginId)) {
                this.togglePlugin(tab.contextId, view.pluginId);
            }
            return;
        }

        const group = groupForKind(tab.kind);
        if (group === null) return;
        this.openGroup(tab.contextId, group);
    }

    /** Unfolds one section for a context, leaving an already open one alone. */
    private openGroup(contextId: string, label: string): void {
        if (!this.isGroupCollapsed(contextId, label)) return;
        this.toggleGroup(contextId, label);
    }

    /** Closes a tab, moving focus to its neighbour so the pane is never blank. */
    closeTab(id: string): void {
        const pane = this.paneOf(id);
        if (pane === null) return;
        this.retain(pane, (tab) => tab.id !== id);
    }

    /**
     * Hides or shows the cluster tree, wherever it happens to live.
     *
     * The pane rather than the tab, because hiding is what people mean: the
     * tree cannot be closed, and folding the panel away is the reversible
     * version of the thing a close button looks like it would do.
     */
    toggleClusters(): void {
        const pane = this.paneOf(CLUSTERS_TAB_ID);
        if (pane === null) return;
        if (this.isPaneOpen(pane) && this.panes[pane].activeId === CLUSTERS_TAB_ID) {
            this.setPaneOpen(pane, false);
            return;
        }
        this.setPaneOpen(pane, true);
        this.panes[pane].activeId = CLUSTERS_TAB_ID;
    }

    /**
     * Closes every tab in a pane but one. Pass a context to spare the tabs
     * belonging to other clusters -- "clear out staging, leave prod alone".
     */
    closeOtherTabsIn(pane: PaneId, id: string, withinContextId?: string): void {
        this.retain(
            pane,
            (tab) => tab.id === id || (withinContextId !== undefined && tab.contextId !== withinContextId),
        );
    }

    /** Closes every tab in a pane, or every one belonging to one context. */
    closeAllTabsIn(pane: PaneId, withinContextId?: string): void {
        this.retain(pane, (tab) => withinContextId !== undefined && tab.contextId !== withinContextId);
    }

    /** Closes every tab in the main pane but one. */
    closeOtherTabs(id: string, withinContextId?: string): void {
        this.closeOtherTabsIn(this.paneOf(id) ?? 'main', id, withinContextId);
    }

    /** Closes every tab in the main pane, or every one belonging to a context. */
    closeAllTabs(withinContextId?: string): void {
        this.closeAllTabsIn('main', withinContextId);
    }

    /**
     * Keeps the tabs in one pane matching `keep` and drops the rest, which is
     * every closing operation there is. Closing one tab and closing nine differ
     * only in the predicate; what they share -- where focus lands, that the
     * state behind a closed view goes with it, and that the panes are written
     * once -- is the part worth having in one place.
     */
    private retain(pane: PaneId, keep: (tab: Tab) => boolean): void {
        const state = this.panes[pane];
        // A pinned tab survives every predicate. "Close all" is a request about
        // the things you opened, and the cluster tree is not one of them: it is
        // how you opened them.
        keep = pinnedFirst(keep);
        const survivors = state.tabs.filter(keep);
        if (survivors.length === state.tabs.length) return;

        const closing = state.tabs.filter((tab) => !keep(tab));
        for (const tab of closing) this.forget(tab);

        const active = state.tabs.find((t) => t.id === state.activeId) ?? null;
        const stillActive = survivors.some((t) => t.id === state.activeId);
        const successor = stillActive ? null : this.successorFor(state.tabs, state.activeId, keep);

        state.tabs = survivors;
        if (this.focusedTabId !== null && closing.some((t) => t.id === this.focusedTabId)) {
            this.focusedTabId = null;
        }
        if (!stillActive) {
            state.activeId = successor?.id ?? null;
            if (successor && successor.view === 'resource') this.selectContext(successor.contextId);
            // Only when the list the report was read from has gone. Closing an
            // editor leaves the object it was editing on screen above it, and
            // taking the description away with it would be gratuitous -- and so
            // would taking it away because some other list was closed.
            if (active && active.view === 'resource' && detail.describesTheListIn(active)) {
                detail.close();
            }
        }
        // A pane showing nothing is a blank panel taking up a third of the
        // window. The bottom one keeps its strip and hands the room back; the
        // others simply stop being drawn.
        if (state.tabs.length === 0 && pane === 'bottom') state.open = false;
        this.persistPanes();
    }

    /**
     * Whether the report on screen was read from the list this tab shows.
     *
     * A report belongs to a collection -- you got to it by clicking a row --
     * and both of the rules that close it turn on that belonging: leaving the
     * list, or closing it. Neither should fire for some other list that
     * happens to be in the way.
     */
    /**
     * Drops whatever a closed tab was holding: an editor's buffer, a log
     * stream's scrollback, a shell, a list's sort and filter.
     *
     * A reopened tab must not come back holding an edit made against a version
     * of the object the cluster has moved past, or scrollback from a stream that
     * closed hours ago. Moving a tab between panes deliberately does not go
     * through here -- see moveTabToPane.
     */
    private forget(tab: Tab): void {
        if (tab.view === 'logs') logs.forget(tab.id);
        else if (tab.view === 'shell') terminals.forget(tab.id);
        else if (tab.view === 'details') detail.clear();
        else if (isDocumentView(tab.view)) editors.forget(tab.id);
        else if (tab.view === 'resource') views.forget(tab.id);
    }

    /**
     * The tab that takes over when the active one closes: the first survivor to
     * its right, else the nearest to its left, so focus moves the short way and
     * lands where the eye already is.
     */
    private successorFor<T extends { id: string }>(
        tabs: T[],
        activeId: string | null,
        keep: (tab: T) => boolean,
    ): T | null {
        const index = tabs.findIndex((t) => t.id === activeId);
        if (index === -1) return null;

        for (let i = index + 1; i < tabs.length; i++) {
            if (keep(tabs[i])) return tabs[i];
        }
        for (let i = index - 1; i >= 0; i--) {
            if (keep(tabs[i])) return tabs[i];
        }
        return null;
    }

    /** Reorders tabs within one pane. Both indices are positions in that pane. */
    reorderTab(pane: PaneId, from: number, to: number): void {
        const next = moved(this.panes[pane].tabs, from, to);
        if (!next) return;
        this.panes[pane].tabs = next;
        this.persistPanes();
    }

    /** Reorders the main pane's tabs after a drag. */
    moveTab(from: number, to: number): void {
        this.reorderTab('main', from, to);
    }

    /**
     * Moves a tab into another pane, at a position in it.
     *
     * The tab keeps its id, and that is the whole point: an id says what a tab
     * shows, so a half-written manifest, a shell's session and a log stream's
     * scrollback all survive the move. Dragging a view somewhere else rearranges
     * the window; it does not restart what is in it.
     */
    moveTabToPane(id: string, to: PaneId, index?: number): void {
        const from = this.paneOf(id);
        if (from === null) return;

        const tab = this.panes[from].tabs.find((t) => t.id === id);
        if (!tab) return;

        if (from === to) {
            const at = this.panes[to].tabs.findIndex((t) => t.id === id);
            if (index !== undefined && index !== at) this.reorderTab(to, at, index);
            return;
        }

        const source = this.panes[from];
        const wasActive = source.activeId === id;
        const survivors = source.tabs.filter((t) => t.id !== id);
        const successor = wasActive
            ? this.successorFor(source.tabs, id, (t) => t.id !== id)
            : null;

        source.tabs = survivors;
        if (wasActive) source.activeId = successor?.id ?? null;
        if (survivors.length === 0 && from === 'bottom') source.open = false;

        this.insertTab(to, tab, { index, atEnd: index === undefined });
        this.panes[to].activeId = id;
        this.panes[to].open = true;
        if (id === DETAILS_TAB_ID) this.rememberDetailPane(to);
        this.persistPanes();
    }

    /** Whether a pane is showing its contents rather than just its tabs. */
    isPaneOpen(pane: PaneId): boolean {
        return this.panes[pane].open && this.panes[pane].tabs.length > 0;
    }

    /** Shows or folds away a pane's contents. Its strip, where it has one, stays. */
    setPaneOpen(pane: PaneId, open: boolean): void {
        if (this.panes[pane].open === open) return;
        this.panes[pane].open = open;
        this.persistPanes();
    }

    togglePane(pane: PaneId): void {
        this.setPaneOpen(pane, !this.isPaneOpen(pane));
    }

    setPaneSize(pane: PaneId, px: number): void {
        this.panes[pane].size = Math.round(px);
        this.persistPanes();
    }

    /**
     * Puts every view back where it would have opened, at the sizes a fresh
     * install has.
     *
     * The way out of an arrangement that has gone wrong -- a panel dragged to
     * one pixel, everything piled into one pane, a layout restored from a much
     * larger screen. Nothing is closed: the tabs are the user's work and only
     * their arrangement is being given up, so each goes back to the pane its
     * kind of view opens in.
     */
    resetLayout(): void {
        const open = PANE_IDS.flatMap((pane) => this.panes[pane].tabs);
        const fresh = defaultPanes();

        for (const tab of open) {
            // The tree is already in the fresh panes; adding it again would
            // give the left panel two of it.
            if (tab.view === 'clusters') continue;
            fresh[defaultPaneFor(tab.view)].tabs.push(tab);
        }
        for (const pane of PANE_IDS) {
            fresh[pane].activeId = fresh[pane].tabs[0]?.id ?? null;
        }
        // The bottom pane unfolds only if the reset put something in it, which
        // is the rule it follows everywhere else.
        fresh.bottom.open = fresh.bottom.tabs.length > 0;

        this.panes = fresh;
        this.focusedTabId = null;
        this.rememberDetailPane(defaultPaneFor('details'));
        this.persistPanes();
        notices.inform('Layout reset');
    }

    // ----- the bottom pane, under the names the dock had ------------------

    /** Opens the YAML editor for one object, or focuses it if it is open. */
    openEditor(target: DetailTarget): void {
        this.openObjectTab('edit', target);
    }

    /**
     * Opens a Helm release's values, or focuses them if they are open.
     *
     * The same editor an object's YAML gets, deliberately: it is the same
     * gesture on the same document, and everything that editor already does --
     * folding, search, the dirty mark, surviving a switch to another tab --
     * is worth as much to a values file as to a manifest. What differs is what
     * a save means, and that lives in the editor store rather than here.
     */
    openHelmValues(target: DetailTarget): void {
        this.openObjectTab('helmvalues', target);
    }

    /** Opens the log view for one object, or focuses it if it is open. */
    openLogs(target: DetailTarget): void {
        this.openObjectTab('logs', target);
    }

    /**
     * Opens a shell on one object -- in a pane, or in the user's own terminal
     * if that is what they have chosen.
     *
     * The choice is read here rather than at the button, so that every way of
     * asking for a shell honours it: the action bar, the terminal view's own
     * "External", and whatever asks next.
     */
    openShell(target: DetailTarget): void {
        if (this.terminal.mode === 'external') {
            void this.openExternalShell(target);
            return;
        }
        this.openObjectTab('shell', target);
    }

    /**
     * Opens a shell in the terminal emulator installed on this machine.
     *
     * What runs over there is kubectl: this app's connection to a cluster lives
     * in its own process and cannot be handed to another one. A machine without
     * kubectl is told so plainly, because the fix is a thing the user can do.
     */
    async openExternalShell(target: DetailTarget): Promise<void> {
        try {
            if (target.kind === 'nodes') {
                await TerminalService.LaunchNode(target.contextId, target.name);
            } else {
                await TerminalService.Launch(
                    target.contextId,
                    target.kind,
                    target.namespace,
                    target.name,
                    '',
                    '',
                );
            }
            notices.inform(`Opened a shell on ${target.name} in your terminal`);
        } catch (err) {
            notices.fail(message(err));
        }
    }

    /**
     * Opens one view onto an object, or focuses it where it already is.
     *
     * The view is part of a tab's id, so an object's YAML and its logs are two
     * tabs rather than one that changes what it shows. A view that has never
     * been opened lands in the bottom pane; one the user has since dragged
     * elsewhere is focused there, because that is where they put it.
     */
    private openObjectTab(view: TabView, target: DetailTarget): void {
        const id = tabIdFor(view, target);

        if (this.paneOf(id) === null) {
            this.insertTab('bottom', {
                id,
                view,
                contextId: target.contextId,
                kind: target.kind,
                namespace: target.namespace,
                name: target.name,
                title: target.name,
            });
            // Not through activateTab: opening something is never a request to
            // fold the pane away, which is what that does on a second click.
            this.panes.bottom.activeId = id;
            this.focusedTabId = id;
            this.setPaneOpen('bottom', true);
            return;
        }
        const pane = this.paneOf(id) as PaneId;
        this.panes[pane].activeId = id;
        this.focusedTabId = id;
        this.setPaneOpen(pane, true);
    }

    /** Focuses a tab in the bottom pane, or folds it away if it is showing. */
    activateDockTab(id: string): void {
        this.activateTab(id);
    }

    /** Closes one tab in the bottom pane, discarding whatever was unsaved in it. */
    closeDockTab(id: string): void {
        this.closeTab(id);
    }

    /** Closes every tab in the bottom pane but one, optionally sparing other clusters'. */
    closeOtherDockTabs(id: string, withinContextId?: string): void {
        this.closeOtherTabsIn('bottom', id, withinContextId);
    }

    /** Closes every tab in the bottom pane, or every one belonging to a context. */
    closeAllDockTabs(withinContextId?: string): void {
        this.closeAllTabsIn('bottom', withinContextId);
    }

    /** Reorders the bottom pane's tabs after a drag. */
    moveDockTab(from: number, to: number): void {
        this.reorderTab('bottom', from, to);
    }

    /** Whether one object already has an editor open on it, in any pane. */
    isEditing(target: DetailTarget): boolean {
        return this.paneOf(tabIdFor('edit', target)) !== null;
    }

    /** Shows or folds away the bottom pane's contents. Its strip always stays. */
    setDockOpen(open: boolean): void {
        this.setPaneOpen('bottom', open);
    }

    toggleDock(): void {
        this.togglePane('bottom');
    }

    setDockSize(px: number): void {
        this.setPaneSize('bottom', px);
    }

    // ----- restoring and persisting --------------------------------------

    /**
     * Writes every pane: what each holds, in what order, whether it is showing
     * it and how big it is.
     *
     * One writer for all of it, which is what stops the parts undoing each
     * other. Dragging a tab from the bottom panel into the right one empties one
     * pane and fills, opens and sizes another in a single gesture, and every
     * settings call answers with the whole file for the store to adopt -- so two
     * debounced writers over that gesture would race, and whichever answered
     * second would carry the other's half back.
     *
     * Debounced because dragging reorders on every pointer move, and so does
     * dragging a pane's edge; without it one drag would write the file dozens
     * of times.
     */
    private savePanes = this.writer(
        'panes',
        (panes: appconfig.Panes) => SettingsService.SetPanes(panes),
        'Could not save the layout',
        250,
    );

    /**
     * Writes a resource tab's namespace filter to the settings file.
     *
     * Called by the table when the picker changes. The filter itself is already
     * in views by then -- this only asks for the disk write, which the pane
     * writer debounces, so the redundant call every table makes as it mounts
     * costs nothing.
     *
     * It is the one part of how a table was left that survives a restart. Which
     * namespaces you work in is a standing fact about your job rather than
     * something about this session, and choosing them again in every tab after
     * every launch is the cost of not writing them down. The sort and the
     * search stay in memory -- see views.ts for why.
     */
    rememberNamespaces(): void {
        this.persistPanes();
    }

    private persistPanes(): void {
        this.savePanes(
            $state.snapshot({
                main: this.paneRef('main'),
                right: this.paneRef('right'),
                bottom: this.paneRef('bottom'),
            }) as appconfig.Panes,
        );
    }

    /**
     * One pane in the shape the settings file holds it.
     *
     * The describe tab is left out. It shows whatever row is selected, and a
     * restored window has no selection, so writing it down would put a tab in
     * the file that can only come back describing nothing. What does persist
     * about it is which pane it was in -- see rememberDetailPane.
     */
    private paneRef(pane: PaneId): appconfig.PaneState {
        const state = this.panes[pane];
        return {
            open: state.open,
            size: state.size,
            tabs: state.tabs
                .filter((t) => t.view !== 'details')
                .map((t) => ({
                    type: t.view,
                    contextId: t.contextId,
                    kind: t.kind,
                    namespace: t.namespace,
                    name: t.name,
                    // Read out of views rather than held here as well: the
                    // table is what knows its filter, and a second copy on the
                    // tab would be the one that went stale.
                    namespaces: views.recall(t.id)?.namespaces ?? [],
                })),
        } as appconfig.PaneState;
    }

    /**
     * Reopens the views from the previous session, skipping any whose context
     * is no longer in a kubeconfig and any view this build does not know about
     * -- a hand-edited file, or one written by a later version.
     *
     * The documents themselves are not restored, only the tabs: an editor
     * reopens on what the cluster says now, because a draft written against a
     * fortnight-old resourceVersion could not be saved anyway. Logs and shells
     * do not come back at all -- they are connections rather than state, and
     * reopening one at launch would mean dialling every cluster in the window
     * before it is up.
     */
    private restorePanes(): void {
        // Turned off, every pane starts empty -- but what was in them is left
        // on disk untouched, so switching restore back on brings back the
        // session it was switched off during rather than nothing.
        const restoring = this.settings.preferences.restoreTabs;
        const known = new Set(this.contexts.map((c) => c.id));

        for (const pane of PANE_IDS) {
            const saved = this.settings.panes[pane];
            const tabs = restoring ? saved.tabs.filter(canRestore(known)).map(tabFromRef) : [];
            this.panes[pane] = {
                tabs,
                activeId: tabs[0]?.id ?? null,
                open: saved.open,
                size: saved.size,
            };
        }

        this.ensureClustersTab();

        const first = this.panes.main.tabs[0];
        if (first && first.view === 'resource' && !isAppTab(first)) {
            this.selectContext(first.contextId);
        }
    }

    /**
     * Puts the cluster tree back if nothing has it.
     *
     * The store repairs this too, but a session restored with tabs turned off
     * never reaches the store's copy: it starts from empty panes, and empty
     * would mean a window with no way to open anything.
     */
    private ensureClustersTab(): void {
        if (this.paneOf(CLUSTERS_TAB_ID) !== null) return;
        this.panes.left.tabs = [clustersTab(), ...this.panes.left.tabs];
        this.panes.left.activeId = CLUSTERS_TAB_ID;
        this.panes.left.open = true;
    }

    /** Drops tabs whose context disappeared from disk between syncs. */
    private dropTabsForMissingContexts(): void {
        const known = new Set(this.contexts.map((c) => c.id));
        for (const pane of PANE_IDS) {
            // The settings tab survives every sync, as the cluster tree does:
            // neither depends on a kubeconfig, so losing every cluster must not
            // close them.
            this.retain(pane, (tab) => isAppTab(tab) || known.has(tab.contextId));
        }
    }

    /** Keeps a context selected if there is one, so the sidebar is never idle. */
    private ensureSelection(): void {
        if (this.selectedContextId && this.contexts.some((c) => c.id === this.selectedContextId)) {
            return;
        }
        const current = this.contexts.find((c) => c.current) ?? this.contexts[0];
        this.selectedContextId = current?.id ?? null;
        if (current) {
            this.selectContext(current.id);
        }
    }

    // ----- custom resources ----------------------------------------------

    /** What a context's definitions section should show. */
    customKindsFor(contextId: string): CustomKinds {
        return this.customKinds[contextId] ?? NOT_LOADED;
    }

    /**
     * Fetches the definitions a cluster serves.
     *
     * Called when the definitions section is opened rather than when a context
     * is, for the same reason probing is lazy: this is a request to a cluster,
     * and a kubeconfig with twenty contexts should not make twenty of them
     * because the sidebar was expanded. The answer is kept, so opening and
     * closing the section costs nothing after the first time.
     */
    async loadCustomKinds(contextId: string, { force = false } = {}): Promise<void> {
        const status = this.customKindsFor(contextId).status;
        if (status === 'loading') return;
        if (!force && status !== 'idle') return;

        this.customKinds[contextId] = { status: 'loading', groups: [], message: '' };
        try {
            const groups = await ResourceService.CustomResourceKinds(contextId);
            // Normalised here rather than guarded at every use: the generated
            // bindings type both the list and each group's kinds as nullable.
            this.customKinds[contextId] = {
                status: 'ready',
                groups: (groups ?? []).map((g) => ({ group: g.group, kinds: g.kinds ?? [] })),
                message: '',
            };
        } catch (err) {
            this.customKinds[contextId] = { status: 'error', groups: [], message: message(err) };
        }
    }

    /** Whether one API group's definitions are showing, for one context. */
    isApiGroupExpanded(contextId: string, group: string): boolean {
        return this.expandedApiGroups.includes(apiGroupKey(contextId, group));
    }

    toggleApiGroup(contextId: string, group: string): void {
        this.expandedApiGroups = toggled(this.expandedApiGroups, apiGroupKey(contextId, group));
    }

    // ----- solution plugins ----------------------------------------------

    /** Whether a plugin's views are unfolded under a context in the sidebar. */
    isPluginExpanded(contextId: string, pluginId: string): boolean {
        return this.expandedPlugins.includes(apiGroupKey(contextId, pluginId));
    }

    togglePlugin(contextId: string, pluginId: string): void {
        this.expandedPlugins = toggled(this.expandedPlugins, apiGroupKey(contextId, pluginId));
    }

    /** Opens a plugin's landing page for a context. */
    openPluginOverview(contextId: string, pluginId: string): void {
        this.openTab(contextId, pluginKindFor(pluginId, PLUGIN_OVERVIEW));
    }

    /**
     * Whether a cluster serves what a plugin needs, answered from the custom
     * resource definitions the sidebar has already read.
     *
     * Reusing that list is the point: the definitions are loaded lazily and
     * cached per context, so asking "does this cluster have Argo CD" costs
     * nothing beyond what the definitions section already fetched, and one
     * refresh button updates both. The plugin's own overview asks the cluster
     * directly and is the authority; this is only what decides how the sidebar
     * row reads.
     *
     * `null` means we do not know yet -- the definitions have not been read for
     * this context -- which is deliberately distinct from "no". A row that said
     * "not installed" before it had looked would be wrong more often than
     * right.
     */
    pluginInstalledIn(contextId: string, plugin: Plugin): boolean | null {
        const loaded = this.customKinds[contextId];
        if (!loaded || loaded.status !== 'ready') return null;

        const served = new Set(loaded.groups.flatMap((group) => group.kinds.map((kind) => kind.kind)));
        const required = plugin.requires.filter((req) => !req.optional);
        if (required.length === 0) return true;

        return required.every((req) => {
            // Only custom resources can be looked up this way. A requirement on
            // a built-in kind -- Deployments, say -- is served by every cluster
            // worth connecting to, so it is taken as met rather than reported
            // as unknown.
            if (!req.kind.startsWith('crd:')) return true;
            return served.has(req.kind);
        });
    }

    /** The plugin a `plugin:` tab kind belongs to, if it is installed. */
    pluginFor(kind: string): Plugin | null {
        const view = parsePluginKind(kind);
        if (!view) return null;
        return this.plugins.find((p) => p.id === view.pluginId) ?? null;
    }

    /**
     * The view a `plugin:` tab kind opens, if its plugin is installed and still
     * declares it. Null for the overview, which no plugin declares.
     */
    pluginViewFor(kind: string): PluginViewSpec | null {
        const view = parsePluginKind(kind);
        if (!view) return null;
        return this.pluginFor(kind)?.views.find((v) => v.id === view.viewId) ?? null;
    }

    /**
     * The panels enabled plugins draw in the detail view of an object of this
     * kind, each with the plugin it belongs to.
     */
    pluginSectionsFor(kind: string): { plugin: Plugin; section: PluginSectionSpec }[] {
        return this.enabledPlugins.flatMap((plugin) =>
            (plugin.sections ?? []).filter((s) => s.kind === kind).map((section) => ({ plugin, section })),
        );
    }

    /**
     * Whether an enabled plugin from outside the app puts buttons on this kind.
     * One that does takes over from the app's own product-specific buttons for
     * it -- a VM's lifecycle bar -- rather than drawing a second set beside them.
     */
    pluginActsOn(kind: string, opts: { external?: boolean } = {}): boolean {
        return this.enabledPlugins.some(
            (p) => (!opts.external || p.origin !== 'builtin') && (p.actions ?? []).some((a) => a.kind === kind),
        );
    }

    /**
     * The kind a tab's rows actually are: for a plugin view, the kind the
     * view lists; for anything else, the kind itself.
     *
     * A row opened from a plugin view has to be described, edited and acted
     * on as what it is -- an Application, not "plugin:argocd/applications",
     * which the backend rightly refuses as a kind. The backend resolves the
     * view for the listing itself, so this is the one place the window has to
     * do the same.
     */
    listedKind(kind: string): string {
        const view = parsePluginKind(kind);
        if (!view) return kind;
        return this.pluginFor(kind)?.views.find((v) => v.id === view.viewId)?.kind ?? kind;
    }

    /**
     * The namespace a plugin view pins itself to, or the empty string when it
     * leaves the tab's own filter free.
     *
     * The table asks before drawing its namespace picker: a view that says
     * "Argo CD's own workloads" is not answering a question about kube-system,
     * and a filter that silently did nothing would be worse than one that is
     * not offered.
     */
    pinnedNamespace(kind: string): string {
        const view = parsePluginKind(kind);
        if (!view) return '';
        const plugin = this.pluginFor(kind);
        return plugin?.views.find((v) => v.id === view.viewId)?.namespace ?? '';
    }

    /**
     * Re-reads the definitions of contexts that already had them, so the sync
     * button is the way back from a cluster that was unreachable when its
     * section was first opened.
     *
     * Without it a failure is permanent: the sidebar asks only while a context
     * has no answer, and an error counts as one. Contexts nobody has opened the
     * section for are left alone, which is what keeps this lazy.
     */
    private recheckCustomKinds(): void {
        for (const id of Object.keys(this.customKinds)) {
            void this.loadCustomKinds(id, { force: true });
        }
    }

    /** Forgets the definitions of contexts that are no longer in a kubeconfig. */
    private pruneCustomKinds(): void {
        const known = new Set(this.contexts.map((c) => c.id));
        const kept: Record<string, CustomKinds> = {};
        for (const [id, loaded] of Object.entries(this.customKinds)) {
            if (known.has(id)) kept[id] = loaded;
        }
        this.customKinds = kept;
    }

    // ----- the describe tab's place among the panes -----------------------
    //
    // The report itself lives in ./detail.svelte.ts. What stays here is the
    // half that is about tabs: where the report opens, which pane it was
    // dragged into, and closing it. The store is handed these two operations
    // in the constructor.

    /**
     * Puts the describe tab on screen, titled with what it is describing.
     *
     * Retitling in place rather than closing and reopening: the tab is the
     * same tab, and a strip that flickered a tab out and back on every click
     * down a list would be the wrong answer to "this now describes something
     * else".
     */
    private showDetailsTab(target: DetailTarget): void {
        const pane = this.paneOf(DETAILS_TAB_ID) ?? this.detailPane;
        const tab = detailsTab(target);

        const at = this.panes[pane].tabs.findIndex((t) => t.id === DETAILS_TAB_ID);
        if (at === -1) {
            this.insertTab(pane, tab, { atEnd: true });
        } else {
            this.panes[pane].tabs[at] = tab;
        }
        // Not through activateTab: it folds the bottom pane away on a second
        // click, and selecting a second row is not a request to put the report
        // you just asked for out of sight.
        this.panes[pane].activeId = DETAILS_TAB_ID;
        this.setPaneOpen(pane, true);
        this.persistPanes();
    }

    /**
     * Takes the describe tab away. The pane it was in goes with it if it held
     * nothing else, the way any pane does.
     */
    private hideDetailsTab(): void {
        if (this.paneOf(DETAILS_TAB_ID) !== null) this.closeTab(DETAILS_TAB_ID);
    }

    /**
     * Remembers the pane the describe tab was dragged into, so the next
     * selection opens it there.
     *
     * The tab itself is never persisted -- there is no selection to restore it
     * against -- so without this a move would last only as long as the report
     * that happened to be open when it was made.
     */
    private rememberDetailPane(pane: PaneId): void {
        if (this.settings.layout.detailPane === pane) return;
        this.settings.layout.detailPane = pane;
        this.persistLayout();
    }

    // ----- folding the sidebar's resource groups --------------------------
    //
    // Folding is per context with a shared default behind it: a cluster either
    // has its own list of shut sections or follows the global one, and
    // collapsedGroupsFor is where that fallback happens. Everything below reads
    // through it rather than at settings.contexts directly, so the two cannot
    // disagree about what an absent list means.

    /** The groups folded for one context: its own list, or the shared default. */
    collapsedGroupsFor(contextId: string): string[] {
        return this.settings.contexts[contextId]?.collapsedGroups ?? this.collapsedGroups;
    }

    /** Whether this context has diverged from the global folding. */
    hasFoldingOverride(contextId: string): boolean {
        return (this.settings.contexts[contextId]?.collapsedGroups ?? null) !== null;
    }

    /** Whether one group is folded differently here than everywhere else. */
    groupDiffersFromGlobal(contextId: string, label: string): boolean {
        if (!this.hasFoldingOverride(contextId)) return false;
        return this.isGroupCollapsed(contextId, label) !== this.collapsedGroups.includes(label);
    }

    /** Whether a sidebar resource group is folded for one context. */
    isGroupCollapsed(contextId: string, label: string): boolean {
        return this.collapsedGroupsFor(contextId).includes(label);
    }

    /**
     * Folds or unfolds one resource group for one context.
     *
     * Scoped to the context because that is what a section being folded means
     * to the person looking at it -- this cluster serves no Gateway API, so put
     * those rows away here. `allContexts` is the deliberate bulk version, for
     * when the answer really is the same everywhere.
     */
    toggleGroup(contextId: string, label: string, { allContexts = false } = {}): void {
        const next = toggled(this.collapsedGroupsFor(contextId), label);

        if (allContexts) {
            // "Make every cluster look like this": the shared default moves,
            // and the per-context answers are cleared so none is left quietly
            // disagreeing with what was just asked for. It also reaches
            // clusters added later, which nothing per-context can.
            this.settings.layout.collapsedGroups = next;
            this.persistLayout();
            for (const contextId of Object.keys(this.settings.contexts)) {
                this.setFoldingOverride(contextId, null);
            }
            return;
        }

        // The ordinary click is about the cluster in front of you. It is kept
        // even when it agrees with the shared default, because this context now
        // has an answer of its own and a later "apply to every cluster" should
        // not silently move it back.
        this.setFoldingOverride(contextId, next);
    }

    /**
     * Records (or clears) one context's own folding. Clearing returns it to the
     * shared default.
     */
    private setFoldingOverride(contextId: string, groups: string[] | null): void {
        this.writeContextPrefs(contextId, { collapsedGroups: groups });
    }

    /** Whether any of a context's sections is open, and so worth collapsing. */
    anyGroupOpen(contextId: string): boolean {
        const folded = this.collapsedGroupsFor(contextId);
        return NAV_GROUPS.some((group) => !folded.includes(group.label));
    }

    /** Shuts every section for one context. */
    collapseAllGroups(contextId: string): void {
        this.setFoldingOverride(contextId, NAV_GROUPS.map((group) => group.label));
    }

    /** Opens every section for one context. */
    expandAllGroups(contextId: string): void {
        this.setFoldingOverride(contextId, []);
    }

    // ----- zoom and the sidebar's width ----------------------------------
    //
    // Both are window-wide rather than per context, and both are persisted, so
    // they sit together rather than beside the panes they happen to resize.

    /** Returns a context to following the shared default folding. */
    clearFoldingOverride(contextId: string): void {
        this.setFoldingOverride(contextId, null);
    }

    zoomIn(): void {
        this.setZoom(this.zoom + ZOOM_STEP);
    }

    zoomOut(): void {
        this.setZoom(this.zoom - ZOOM_STEP);
    }

    resetZoom(): void {
        this.setZoom(1);
    }

    /**
     * Sets the scale directly, for the settings view's slider. The steppers go
     * through zoomIn/zoomOut instead.
     *
     * Clamped at both ends. The floor is not arbitrary: the window's title bar
     * is drawn by the frontend and so shrinks with the zoom, while the macOS
     * traffic lights over it do not -- far enough down and they no longer fit.
     */
    setZoom(scale: number): void {
        const next = Math.round(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, scale)) * 100) / 100;
        if (next === this.zoom) return;
        this.settings.layout.zoom = next;
        this.persistLayout();
    }

    /** Sets the cluster tree's pane width. The settings view still calls it this. */
    setSidebarWidth(px: number): void {
        this.setPaneSize('left', px);
    }

    // ----- preferences ---------------------------------------------------

    /**
     * Reads the plugin catalogue, and tells the nav catalogue how the views it
     * found are titled and iconed.
     *
     * That registration is what lets labelFor() go on being a pure function of
     * a kind string everywhere else in the app: a `crd:` kind carries its own
     * name, but a plugin view's name lives in a file, so it has to be put
     * somewhere the tab bar can reach without knowing plugins exist.
     */
    async loadPlugins(): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.List());
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
        } catch (err) {
            notices.fail(`Could not read plugins: ${message(err)}`);
            return;
        }
        this.registerViews();
        this.tellPluginProblems();
        if (this.knownPlugins.length === 0) void this.loadKnownPlugins();
    }

    /**
     * Says once per session, in the status bar, that some plugin would not
     * load. Settings has the reasons, but nobody opens Settings to find out
     * why a plugin they installed is not in the sidebar unless something tells
     * them to look.
     */
    private tellPluginProblems(): void {
        const count = this.pluginProblems.length;
        if (count === 0 || this.toldPluginProblems) return;
        this.toldPluginProblems = true;
        notices.fail(
            count === 1
                ? 'A plugin would not load. Settings → Plugins says why.'
                : `${count} plugin problems. Settings → Plugins says what is wrong.`,
        );
    }

    /** Reads the list of plugins the app knows of. */
    async loadKnownPlugins(): Promise<void> {
        try {
            this.knownPlugins = ((await PluginService.Known()) ?? []).map(adoptKnownPlugin);
        } catch (err) {
            notices.fail(`Could not read the list of known plugins: ${message(err)}`);
        }
    }

    /** Whether a plugin with this id is installed here, switched on or off. */
    hasPlugin(id: string): boolean {
        return this.plugins.some((p) => p.id === id);
    }

    /**
     * Known plugins worth suggesting for one cluster: not installed, not
     * hidden, and with a kind that gives their product away served there.
     *
     * Answered from the definitions the sidebar has already read, as
     * pluginInstalledIn is, so it costs nothing and says nothing until they
     * have been read.
     */
    pluginSuggestionsFor(contextId: string): KnownPlugin[] {
        const loaded = this.customKinds[contextId];
        if (!loaded || loaded.status !== 'ready') return [];
        const served = new Set(loaded.groups.flatMap((group) => group.kinds.map((kind) => kind.kind)));
        const hidden = this.settings.hiddenPluginSuggestions ?? [];
        return this.knownPlugins.filter(
            (known) =>
                !this.hasPlugin(known.id) &&
                !hidden.includes(known.id) &&
                known.detect.some((kind) => served.has(kind)),
        );
    }

    /**
     * The clusters, by the name the sidebar shows, whose definitions -- where
     * they have been read -- show a known plugin's product running.
     */
    clustersRunning(known: KnownPlugin): string[] {
        if (known.detect.length === 0) return [];
        return this.orderContexts(this.contexts)
            .filter((context) => {
                const loaded = this.customKinds[context.id];
                if (!loaded || loaded.status !== 'ready') return false;
                return loaded.groups.some((group) => group.kinds.some((kind) => known.detect.includes(kind.kind)));
            })
            .map((context) => this.displayName(context));
    }

    /**
     * Installs one of the known plugins from the repository the app has for
     * it. Returns whether it worked.
     */
    async installKnownPlugin(id: string): Promise<boolean> {
        const name = this.knownPlugins.find((k) => k.id === id)?.name ?? id;
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.InstallKnown(id));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Installed ${name}`);
            return true;
        } catch (err) {
            // A clone that would not load still left its folder behind, and
            // Settings should list it with the reason.
            await this.loadPlugins();
            notices.fail(`Could not install ${name}: ${firstLine(message(err))}`);
            return false;
        }
    }

    /** Stops the sidebar suggesting a known plugin, or lets it again. */
    async hidePluginSuggestion(id: string, hidden = true): Promise<void> {
        try {
            this.settings = adoptSettings(await PluginService.HideSuggestion(id, hidden));
        } catch (err) {
            notices.fail(`Could not save that: ${message(err)}`);
        }
    }

    /** Opens Settings on its Plugins section. */
    openPluginSettings(): void {
        rememberSection('plugins');
        this.openSettings();
    }

    /** Publishes every installed view's label and icon to the nav catalogue. */
    private registerViews(): void {
        registerPluginViews(
            this.plugins.flatMap((plugin) => [
                // The overview is not one of the plugin's declared views -- every
                // plugin has one whether it asks or not -- so it is named here.
                {
                    kind: pluginKindFor(plugin.id, PLUGIN_OVERVIEW),
                    label: plugin.name,
                    icon: plugin.icon,
                },
                ...plugin.views.map((view) => ({
                    kind: pluginKindFor(plugin.id, view.id),
                    label: view.label,
                    icon: view.icon,
                })),
            ]),
        );
    }

    /** Rereads the plugin folders, picking up a file edited since launch. */
    async reloadPlugins(): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.Reload());
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            const count = this.plugins.length;
            notices.inform(`${count} plugin${count === 1 ? '' : 's'} available`);
        } catch (err) {
            notices.fail(`Could not read plugins: ${message(err)}`);
        }
    }

    /**
     * Switches a plugin on or off.
     *
     * The wanted state is sent rather than a toggle, so a switch that fires
     * twice cannot end up disagreeing with what is on disk.
     */
    async setPluginEnabled(id: string, enabled: boolean): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.SetEnabled(id, enabled));
            // Refreshed here as well as on load, because this is what decides
            // whether a chart panel is drawn at all: leaving it stale would
            // keep a Metrics heading on the dashboard talking about a
            // Prometheus the user has just switched off.
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
        } catch (err) {
            notices.fail(`Could not ${enabled ? 'enable' : 'disable'} the plugin: ${message(err)}`);
        }
    }

    /** Opens the plugins folder in the file manager, creating it if need be. */
    async revealPluginDir(): Promise<void> {
        try {
            await PluginService.RevealDir();
        } catch (err) {
            notices.fail(`Could not open the plugins folder: ${message(err)}`);
        }
    }

    /** Writes a starter plugin into the plugins folder and reloads. */
    async createExamplePlugin(): Promise<void> {
        try {
            const path = await PluginService.CreateExample();
            await this.loadPlugins();
            notices.inform(`Wrote ${path}`);
        } catch (err) {
            notices.fail(`Could not write a starter plugin: ${message(err)}`);
        }
    }

    /** Opens the native picker and reads plugins from the folder chosen. */
    async addPluginFolder(): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.BrowseForFolder());
            this.registerViews();
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not add the folder: ${message(err)}`);
        }
    }

    /**
     * Clones a plugin repository into the plugins folder and reads it. Returns
     * whether it worked, so the form can clear itself only then.
     */
    async installPluginFromGit(url: string): Promise<boolean> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.InstallFromGit(url));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Installed ${url}`);
            return true;
        } catch (err) {
            // A clone that would not load still left its folder behind, and
            // Settings should list it with the reason.
            await this.loadPlugins();
            notices.fail(`Could not install the plugin: ${firstLine(message(err))}`);
            return false;
        }
    }

    /** Pulls the repository a plugin was cloned from and reads it again. */
    async updatePluginFromGit(id: string): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.UpdateFromGit(id));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Updated ${id}`);
        } catch (err) {
            notices.fail(`Could not update the plugin: ${message(err)}`);
        }
    }

    /**
     * What uninstalling a plugin would delete -- its clone, folder or file, and
     * every plugin read from there -- for its card to ask about before
     * anything is deleted. `null` when the app refuses, with the reason shown.
     */
    async pluginRemoval(id: string): Promise<{ path: string; plugins: string[] } | null> {
        try {
            const removal = await PluginService.UninstallPreview(id);
            return { path: removal.path, plugins: removal.plugins ?? [] };
        } catch (err) {
            const name = this.plugins.find((p) => p.id === id)?.name || id;
            notices.fail(`Could not uninstall ${name}: ${message(err)}`);
            return null;
        }
    }

    /**
     * Deletes an installed plugin from the plugins folder and reads the
     * folders again. Returns whether it worked.
     *
     * The card asks first, in the page, with what pluginRemoval says goes: the
     * macOS webview answers window.confirm with a silent "no" and shows
     * nothing, so a native question would never be seen.
     */
    async uninstallPlugin(id: string): Promise<boolean> {
        const name = this.plugins.find((p) => p.id === id)?.name || id;
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.Uninstall(id));
            this.metricsAttachments = (await MetricsService.Attachments()) ?? [];
            this.registerViews();
            notices.inform(`Uninstalled ${name}`);
            return true;
        } catch (err) {
            notices.fail(`Could not uninstall ${name}: ${message(err)}`);
            return false;
        }
    }

    /** Stops reading plugins from a folder. Nothing on disk is touched. */
    async removePluginFolder(path: string): Promise<void> {
        try {
            this.pluginCatalogue = adoptPluginCatalogue(await PluginService.RemoveFolder(path));
            this.registerViews();
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not drop the folder: ${message(err)}`);
        }
    }

    /** Wears a theme. The id is stored as given, whether or not we have it. */
    setTheme(theme: string): void {
        this.updatePreferences({ theme });
    }

    // ----- themes ---------------------------------------------------------

    /** Rereads the theme folders, picking up files edited since launch. */
    async reloadThemes(): Promise<void> {
        await this.loadThemes();
        const count = this.themes.length;
        notices.inform(`${count} theme${count === 1 ? '' : 's'} available`);
    }

    /** Opens the themes folder in the file manager, creating it if need be. */
    async revealThemeDir(): Promise<void> {
        try {
            await ThemeService.RevealDir();
        } catch (err) {
            notices.fail(`Could not open the themes folder: ${message(err)}`);
        }
    }

    /**
     * Writes a starter theme into the themes folder and reloads, so "write your
     * own" begins with a file that already works rather than a blank one.
     */
    async createExampleTheme(): Promise<void> {
        try {
            const path = await ThemeService.CreateExample();
            await this.loadThemes();
            notices.inform(`Wrote ${path}`);
        } catch (err) {
            notices.fail(`Could not write a starter theme: ${message(err)}`);
        }
    }

    /** Opens the native picker and reads themes from the folder chosen. */
    async addThemeFolder(): Promise<void> {
        try {
            this.themeCatalogue = adoptCatalogue(await ThemeService.BrowseForFolder());
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not add the folder: ${message(err)}`);
        }
    }

    /** Stops reading themes from a folder. Nothing on disk is touched. */
    async removeThemeFolder(path: string): Promise<void> {
        try {
            this.themeCatalogue = adoptCatalogue(await ThemeService.RemoveFolder(path));
            this.settings = adoptSettings(await SettingsService.Get());
        } catch (err) {
            notices.fail(`Could not drop the folder: ${message(err)}`);
        }
    }

    setDensity(density: Density): void {
        this.updatePreferences({ density });
    }

    setRestoreTabs(restoreTabs: boolean): void {
        this.updatePreferences({ restoreTabs });
    }

    setConfirmSourceRemoval(confirmSourceRemoval: boolean): void {
        this.updatePreferences({ confirmSourceRemoval });
    }

    setCheckForUpdates(checkForUpdates: boolean): void {
        this.updatePreferences({ checkForUpdates });
    }

    setShowKubeconfigNames(showKubeconfigNames: boolean): void {
        this.updatePreferences({ showKubeconfigNames });
    }

    setContextSort(contextSort: ContextSort): void {
        this.updatePreferences({ contextSort });
    }

    /**
     * Steps the sidebar's sort on to the next order. One button rather than a
     * menu: there are three orders, the header is already crowded at narrow
     * widths, and the button draws which one is on.
     */
    cycleContextSort(): void {
        const orders: ContextSort[] = ['name', 'name-desc', 'kubeconfig'];
        const at = orders.indexOf(this.contextSort);
        this.setContextSort(orders[(at + 1) % orders.length]);
    }

    setShowLineNumbers(showLineNumbers: boolean): void {
        this.updatePreferences({ showLineNumbers });
    }

    // ----- terminals ------------------------------------------------------

    /** How a shell opens, and what it opens with. */
    terminal = $derived(this.settings.preferences.terminal);

    /** The built-in terminal's type size and how many lines it keeps. */
    terminalFontSize = $derived(this.settings.preferences.terminal.fontSize);
    terminalScrollback = $derived(this.settings.preferences.terminal.scrollback);

    /**
     * Changes part of the terminal settings.
     *
     * A patch rather than the whole record, because the settings view edits one
     * field at a time and every one of them is written through the preferences
     * writer the rest of this block uses -- one writer for the section, which is
     * what stops two of them undoing each other.
     */
    setTerminal(patch: Partial<Settings['preferences']['terminal']>): void {
        this.updatePreferences({ terminal: { ...this.settings.preferences.terminal, ...patch } });
    }

    // ----- helm -----------------------------------------------------------

    /** Where helm is, and how it is run when a release is changed. */
    helm = $derived(this.settings.preferences.helm);

    /**
     * Changes part of the helm settings. A patch, for the reason setTerminal
     * takes one: the settings view edits a field at a time through one writer.
     *
     * --atomic waits whether or not waiting was asked for, so turning it on
     * turns waiting on here too. The Go store enforces the same thing on read;
     * doing it here as well is what stops the checkbox sitting visibly off
     * beside the flag that implies it until the settings are next loaded.
     */
    setHelm(patch: Partial<Settings['preferences']['helm']>): void {
        const next = { ...this.settings.preferences.helm, ...patch };
        if (next.atomic) next.wait = true;
        this.updatePreferences({ helm: next });
    }

    /** Sets the window every chart on screen covers, in minutes. */
    setMetricsRange(metricsRange: number): void {
        this.updatePreferences({ metricsRange });
    }

    // ----- metrics --------------------------------------------------------

    /**
     * Where a context's metrics come from, asking the first time it is wanted.
     *
     * Returns null until the answer arrives rather than blocking: this is read
     * from a component's render, and the context settings panel showing "…" for
     * a moment is better than the sidebar waiting on a cluster.
     */
    metricsSourceFor(contextId: string): MetricsSource | null {
        const known = this.metricsSources[contextId];
        if (known) return known;
        void this.loadMetricsSource(contextId);
        return null;
    }

    private loading = new Set<string>();

    private async loadMetricsSource(contextId: string): Promise<void> {
        if (this.loading.has(contextId)) return;
        this.loading.add(contextId);
        try {
            const source = await MetricsService.Source(contextId);
            this.metricsSources = { ...this.metricsSources, [contextId]: adoptSource(source) };
        } catch {
            // Left unanswered rather than recorded as a failure: the panel that
            // actually draws charts reports its own errors, and this is only
            // what the settings row shows.
        } finally {
            this.loading.delete(contextId);
        }
    }

    /**
     * Points a context at a Prometheus, or clears the override with an empty
     * value. Returns the reason it was refused, or the empty string.
     */
    async setMetricsEndpoint(contextId: string, value: string): Promise<string> {
        try {
            const source = await MetricsService.SetEndpoint(contextId, value);
            this.metricsSources = { ...this.metricsSources, [contextId]: adoptSource(source) };
            this.settings = adoptSettings(await SettingsService.Get());
            return '';
        } catch (err) {
            return message(err);
        }
    }

    /**
     * Applies a change to the preference block and saves it. The UI updates
     * first and the write is debounced, matching how the layout is persisted:
     * the font size comes from a slider, so this is called on every step of a
     * drag.
     */
    private updatePreferences(patch: Partial<Settings['preferences']>): void {
        this.settings.preferences = { ...this.settings.preferences, ...patch };
        this.persistPreferences();
    }

    private savePreferences = this.writer(
        'preferences',
        (prefs: appconfig.Preferences) => SettingsService.SetPreferences(prefs),
        'Could not save preferences',
        250,
    );

    private persistPreferences(): void {
        this.savePreferences($state.snapshot(this.settings.preferences));
    }

    // ----- layout defaults, as edited from the settings view --------------

    /**
     * Sets the folding every context follows unless it has an override of its
     * own. Distinct from toggleGroup, which records a choice against one
     * cluster -- this is the shared baseline behind all of them.
     */
    setDefaultCollapsedGroups(groups: string[]): void {
        this.settings.layout.collapsedGroups = [...groups];
        this.persistLayout();
    }

    /** Folds or unfolds one group in the shared default. */
    toggleDefaultGroup(label: string): void {
        this.setDefaultCollapsedGroups(toggled(this.collapsedGroups, label));
    }

    /** How many contexts have overridden the shared folding with their own. */
    foldingOverrideCount = $derived(
        Object.values(this.settings.contexts).filter((prefs) => prefs.collapsedGroups !== null).length,
    );

    /**
     * Drops every context's folding override, returning all of them to the
     * shared default.
     *
     * The writes are issued here rather than through setFoldingOverride, which
     * looks like the obvious way to do it but is wrong: persistContextPrefs is
     * one shared debounce, so a loop through it would send only the last
     * context and leave every other override on disk to reappear on the next
     * read. Each is awaited in turn instead, and only the last result is
     * adopted -- every call returns the whole settings, so the final one is
     * already the complete picture.
     */
    async clearAllFoldingOverrides(): Promise<void> {
        const entries = Object.entries(this.settings.contexts).filter(
            ([, prefs]) => prefs.collapsedGroups !== null,
        );
        if (entries.length === 0) return;

        try {
            let saved: appconfig.Settings | null = null;
            for (const [id, prefs] of entries) {
                // Only the folding is given up. Everything else on the record
                // -- the alias, the colour, the metrics endpoint, the table
                // columns -- is the user's and is carried through; the store
                // forgets a context left with nothing set.
                const cleared = $state.snapshot({
                    ...prefs,
                    collapsedGroups: null,
                }) as appconfig.ContextPrefs;
                saved = await SettingsService.SetContextPrefs(id, cleared);
            }
            if (saved) this.settings = adoptSettings(saved);
            notices.inform(
                `${entries.length} context${entries.length === 1 ? '' : 's'} now follow the default folding`,
            );
        } catch (err) {
            notices.fail(`Could not clear the folding overrides: ${message(err)}`);
            await this.reloadSettings();
        }
    }

    /** Re-reads the settings after a write that may have partly succeeded. */
    private async reloadSettings(): Promise<void> {
        try {
            this.settings = adoptSettings(await SettingsService.Get());
        } catch {
            // Already reporting the failure that got us here.
        }
    }

    private saveLayout = this.writer(
        'layout',
        (layout: appconfig.Layout) => SettingsService.SetLayout(layout),
        'Could not save layout',
        400,
    );

    private persistLayout(): void {
        this.saveLayout($state.snapshot(this.settings.layout));
    }

    // ----- saving --------------------------------------------------------
    //
    // Every mutator on the Go side answers with the whole settings, and the
    // store replaces its own with that -- so what is on screen is what actually
    // reached disk rather than an optimistic guess. Writes are debounced,
    // because they come from drags and keystrokes.
    //
    // Those two together are what the machinery below exists for. A write in
    // flight is carrying an answer from *before* whatever was changed after it
    // was sent, so adopting it whole would roll that change back a moment after
    // it was made -- and the writer that would have saved it reads the state it
    // was just rolled back to, so the change is lost from the file as well as
    // from the screen. Opening an editor did exactly that: it adds a dock tab
    // and unfolds the dock, and any other write in flight undid the second.

    /** Per section, the last write scheduled and the last one answered. */
    private writes = new Map<Section, { scheduled: number; answered: number }>();

    /** Whether a section has a write scheduled or in flight. */
    private isPending(section: Section): boolean {
        const write = this.writes.get(section);
        return write !== undefined && write.scheduled !== write.answered;
    }

    /**
     * Builds the debounced writer for one section: it records that the section
     * is on its way out, sends the last value it was given, and adopts the
     * answer.
     *
     * The value is passed in rather than read at send time, so that a rollback
     * arriving in between cannot change what gets written.
     */
    private writer<A extends unknown[]>(
        section: Section,
        send: (...args: A) => Promise<appconfig.Settings>,
        failure: string,
        ms: number,
    ): (...args: A) => void {
        const flush = debounce((...args: A) => {
            const id = this.writes.get(section)?.scheduled ?? 0;
            send(...args)
                .then((saved) => this.adopt(saved, section, id))
                .catch((err: unknown) => {
                    this.settle(section, id);
                    notices.fail(`${failure}: ${message(err)}`);
                });
        }, ms);

        return (...args: A) => {
            const write = this.writes.get(section) ?? { scheduled: 0, answered: 0 };
            this.writes.set(section, { ...write, scheduled: write.scheduled + 1 });
            flush(...args);
        };
    }

    /**
     * Takes what the backend saved, keeping the sections whose own write has
     * not landed yet. Their answer is on its way and is the one that settles
     * them; this one is by definition older than the change they are carrying.
     */
    private adopt(saved: appconfig.Settings, section: Section, id: number): void {
        this.settle(section, id);

        const next = adoptSettings(saved);
        if (this.isPending('contexts')) next.contexts = this.settings.contexts;
        if (this.isPending('panes')) next.panes = this.settings.panes;
        if (this.isPending('layout')) next.layout = this.settings.layout;
        if (this.isPending('preferences')) next.preferences = this.settings.preferences;
        this.settings = next;
    }

    /**
     * Marks one write finished. A later one scheduled while it was in flight
     * leaves the section pending, so its own answer is still awaited.
     */
    private settle(section: Section, id: number): void {
        const write = this.writes.get(section);
        if (write) this.writes.set(section, { ...write, answered: Math.max(write.answered, id) });
    }

}

export const workspace = new Workspace();
