// Every piece of state the workspace holds, and the machinery that saves it.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import { PluginService, SettingsService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as kube from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
import type * as appconfig from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/appconfig/models.js';
import { adoptSettings, type ConfigFile, type Settings } from '../adopt';
import {
    PANE_IDS,
    defaultPaneFor,
    defaultPanes,
    isPaneId,
    type PaneId,
    type PaneState,
    type Tab,
} from '../panes';
import { DEFAULT_COLLAPSED_GROUPS } from '../../catalogue';
import { notices } from '../notices.svelte';
import { session } from '../session.svelte';
import { detail, type DetailTarget } from '../detail.svelte';
import {
    emptyPluginCatalogue,
    type KnownPlugin,
    type Plugin,
    type PluginCatalogue,
} from '../../plugins/types';
import { type MetricsSource } from '../../charts/adopt';
import { emptyCatalogue } from '../../theme/adopt';
import {
    pickTheme,
    type Theme,
    type ThemeCatalogue,
    type ThemeToken,
} from '../../theme/apply';
import {
    type CustomKinds,
    MAX_ZOOM,
    MIN_ZOOM,
    type Reveal,
    type Section,
    ZOOM_STEP,
    debounce,
    defaultSettings,
    message,
    moved,
} from './helpers';

export abstract class WorkspaceBase {
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
     * What only a query can say about a cluster's plugins, keyed by context id
     * and filled alongside that context's definitions -- see probeCluster.
     * `known` are known plugins found running by their workload; `absent` are
     * installed plugins whose object requirements this cluster does not meet.
     */
    pluginProbes = $state<Record<string, { known: string[]; absent: string[] }>>({});
    /**
     * Contexts whose definitions are being read again over an answer already
     * on screen, for the refresh button's spinner. The answer itself stays put
     * until the new one lands -- see loadCustomKinds.
     */
    rereadingKinds = $state<string[]>([]);
    /** Contexts with a read of their definitions in flight, whichever kind. */
    protected readingKinds = new Set<string>();
    /**
     * Bumped per context when it is disconnected, so a read that set out
     * before does not land an answer for a cluster that has been let go of.
     */
    protected kindsGenerations = new Map<string, number>();
    /**
     * Which API groups are open, as `contextId\0group`. Not persisted: it is
     * where you are looking right now rather than how you like the sidebar, and
     * it would otherwise grow the settings file a line per group per cluster.
     */
    expandedApiGroups = $state<string[]>([]);

    /** The sidebar's standing request to scroll a context into view. */
    reveal = $state<Reveal | null>(null);
    protected revealCount = 0;

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
    protected toldPluginProblems = false;
    /**
     * Which plugins are unfolded in the sidebar, as `contextId\0pluginId`.
     * Not persisted, for the same reason expandedApiGroups is not: it is where
     * you are looking right now rather than how you like the sidebar.
     */
    expandedPlugins = $state<string[]>([]);
    /**
     * Contexts whose "not in this cluster" plugins are unfolded in the
     * sidebar. Not persisted, like expandedPlugins.
     */
    expandedAbsentPlugins = $state<string[]>([]);

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
    desktopNotifications = $derived(this.settings.preferences.desktopNotifications);
    /** Where cluster alerts go. */
    alertMode = $derived(this.settings.preferences.alerts);
    /** When the alerts' snooze ends, as a timestamp; 0 when they are not snoozed. */
    alertsSnoozedUntil = $derived(Date.parse(this.settings.preferences.alertsSnoozedUntil) || 0);
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
    /** How a shell opens, and what it opens with. */
    terminal = $derived(this.settings.preferences.terminal);

    /**
     * Applies a change to the preference block and saves it. The UI updates
     * first and the write is debounced, matching how the layout is persisted:
     * the font size comes from a slider, so this is called on every step of a
     * drag.
     */
    protected updatePreferences(patch: Partial<Settings['preferences']>): void {
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

    private saveLayout = this.writer(
        'layout',
        (layout: appconfig.Layout) => SettingsService.SetLayout(layout),
        'Could not save layout',
        400,
    );

    protected persistLayout(): void {
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
    protected writer<A extends unknown[]>(
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

    // ----- implemented by the later layers ---------------------------------
    //
    // Declared here so a layer can call what a later one defines: the parts
    // of the workspace call each other freely, as they did when it was one
    // class, and the order of the files is only an order of reading.

    // contexts.svelte.ts
    abstract displayName(context: kube.Context): string;

    // tabs.svelte.ts
    abstract tabFor(id: string): Tab | null;
    protected abstract retain(pane: PaneId, keep: (tab: Tab) => boolean): void;

    // layout.svelte.ts
    protected abstract persistPanes(): void;
    protected abstract restorePanes(): void;
    protected abstract dropTabsForMissingContexts(): void;
    protected abstract ensureSelection(): void;
    protected abstract showDetailsTab(target: DetailTarget): void;
    protected abstract hideDetailsTab(): void;
    protected abstract rememberDetailPane(pane: PaneId): void;
    abstract isGroupCollapsed(contextId: string, label: string): boolean;
    abstract toggleGroup(contextId: string, label: string, options?: { allContexts?: boolean }): void;

    // plugins.svelte.ts
    abstract loadCustomKinds(contextId: string, options?: { force?: boolean; quiet?: boolean }): Promise<void>;
    protected abstract forgetDefinitions(contextId: string): void;
    abstract isApiGroupExpanded(contextId: string, group: string): boolean;
    abstract toggleApiGroup(contextId: string, group: string): void;
    abstract isPluginExpanded(contextId: string, pluginId: string): boolean;
    abstract togglePlugin(contextId: string, pluginId: string): void;
    abstract isAbsentPluginsExpanded(contextId: string): boolean;
    abstract toggleAbsentPlugins(contextId: string): void;
    abstract pluginInstalledIn(contextId: string, plugin: Plugin): boolean | null;
    protected abstract recheckCustomKinds(): void;
    protected abstract pruneCustomKinds(): void;
    abstract loadPlugins(): Promise<void>;

    // preferences.svelte.ts
    abstract loadThemes(): Promise<void>;
}
