// The boundary between the generated bindings and the rest of the app.
//
// The binding generator types every Go slice and map as nullable, because a nil
// one serialises to JSON null. Left alone that spreads `?? []` through every
// component that touches a service result. These adapters resolve it once, on
// the way in, and hand back the plain non-null shapes the app works with.
//
// Copying here also insulates the app from how the bindings were generated:
// with some flags the models arrive as class instances, which Svelte's $state
// does not deep-proxy, so nested writes to them would never reach the UI.

import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
import type * as appconfig from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/appconfig/models.js';
import { DEFAULT_THEME_ID } from '../theme/apply';
import type { ColumnPrefs } from '../columns';
import type { PaneId } from './panes';
import { DEFAULT_DATETIME, type DateTimeSettings } from '../datetime.svelte';

/** One pane as the settings file holds it. See ./panes.ts for what a pane is. */
export interface SavedPane {
    tabs: {
        type: string;
        contextId: string;
        kind: string;
        namespace: string;
        name: string;
        /** A resource tab's namespace filter; empty for the whole cluster. */
        namespaces: string[];
    }[];
    open: boolean;
    size: number;
}

/**
 * One pane out of the bindings, with the nulls resolved.
 *
 * The store normalises all of this, so the fallbacks here only cover a service
 * call that failed before it reached the store -- and the `open` default
 * differs per pane, which is why it is passed in rather than assumed.
 */
function adoptPane(pane: appconfig.PaneState | undefined | null, open: boolean, size: number): SavedPane {
    return {
        tabs: (pane?.tabs ?? []).map((tab) => ({
            type: tab.type,
            contextId: tab.contextId,
            kind: tab.kind,
            namespace: tab.namespace ?? '',
            name: tab.name ?? '',
            namespaces: [...(tab.namespaces ?? [])],
        })),
        open: pane?.open ?? open,
        size: pane?.size || size,
    };
}

/**
 * One context's table settings out of the bindings, by kind.
 *
 * Every level of this arrives nullable -- the map, each kind's record, and both
 * maps inside it -- because a context that has never had a column touched
 * carries none of them. Resolving it here is what lets the store read a width
 * without three fallbacks at every call site.
 */
function adoptColumns(
    columns: { [kind: string]: appconfig.ColumnPrefs | undefined } | undefined | null,
): Record<string, ColumnPrefs> {
    return Object.fromEntries(
        Object.entries(columns ?? {}).map(([kind, prefs]) => [
            kind,
            {
                widths: Object.fromEntries(
                    Object.entries(prefs?.widths ?? {}).filter(
                        (entry): entry is [string, number] => typeof entry[1] === 'number',
                    ),
                ),
                hidden: [...(prefs?.hidden ?? [])],
            },
        ]),
    );
}

/** A kubeconfig file and the contexts parsed out of it. */
export interface ConfigFile {
    path: string;
    source: string;
    contexts: kube.Context[];
    error: string;
}

/**
 * How a shell opens, resolved. Every field here has a value: the store fills
 * in what a settings file does not say, and the fallbacks below only cover a
 * service call that failed before it reached the store.
 */
export interface TerminalSettings {
    /** 'app' opens the terminal in the dock, 'external' the user's own. */
    mode: 'app' | 'external';
    /** The id of the external terminal to use; empty means this machine's default. */
    external: string;
    /** The shells tried in a container, in order, until one of them runs. */
    shells: string[];
    /** The image a node shell's debug pod runs, and where it is created. */
    nodeImage: string;
    nodeNamespace: string;
    fontSize: number;
    scrollback: number;
}

/**
 * How helm is run, resolved. Every field has a value here for the same reason
 * TerminalSettings does: the store fills in what the file does not say, and the
 * fallbacks below only cover a service call that failed before it got there.
 */
export interface HelmSettings {
    /** Where helm is, when the user has said. Empty means "find it". */
    path: string;
    /** Hold a change open until what it wrote reports ready. */
    wait: boolean;
    /** Roll a failed upgrade back. Implies wait, which the store enforces. */
    atomic: boolean;
    /** How long a command is given, in seconds. */
    timeoutSeconds: number;
}

/** Where the start page's picture comes from. */
export type BackgroundSource = 'builtin' | 'folder' | 'none';

/**
 * The start page's picture, resolved: which pictures, the one kept if any,
 * and how long each stays.
 */
export interface BackgroundSettings {
    source: BackgroundSource;
    /** `scene:<id>` or `file:<name>`; empty rotates through them all. */
    pinned: string;
    /** Minutes one picture stays. Never zero here: the store's zero is resolved. */
    minutes: number;
    /**
     * 'varied' draws each built-in picture in a colour scheme of its own;
     * 'theme' draws them all in the theme's. The theme decides dark or light
     * either way.
     */
    palette: 'varied' | 'theme';
}

/** One forward the user set up, as it is remembered between sessions. */
export interface SavedForward {
    id: string;
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
    remotePort: number;
    localPort: number;
    random: boolean;
    browser: boolean;
}

/**
 * What the user decided about one kubeconfig context: what to call it, what
 * colour to tint it, where its Prometheus is, which nav groups are folded here,
 * and what each of its tables looks like.
 *
 * Every field is resolved, so nothing downstream carries a fallback -- except
 * collapsedGroups, where null is a value in its own right meaning "follow the
 * global folding" and an empty list means "fold nothing here".
 */
export interface ContextPrefs {
    alias: string;
    color: string;
    metrics: string;
    collapsedGroups: string[] | null;
    /** What the user changed about each kind's table here, keyed by kind. */
    columns: Record<string, ColumnPrefs>;
}

/**
 * Whether the user has said nothing about a context, in which case it is
 * dropped rather than kept as a blank record. Mirrors ContextPrefs.isEmpty on
 * the Go side, which applies the same rule to what is written.
 */
export function isEmptyContextPrefs(prefs: ContextPrefs): boolean {
    return (
        !prefs.alias &&
        !prefs.color &&
        !prefs.metrics &&
        prefs.collapsedGroups === null &&
        Object.keys(prefs.columns).length === 0
    );
}

/** The persisted user preferences. */
/**
 * How tall a row is in a resource listing.
 *
 * Named rather than written out at each of its three uses, so that adding a
 * fourth is one edit here and a compile error everywhere it has to be handled.
 * Mirrors appconfig's Density constants, which are what the settings file holds.
 */
export type Density = 'comfortable' | 'compact' | 'spacious';

/**
 * The order the sidebar lists contexts in.
 *
 * Mirrors appconfig's ContextSort constants. 'kubeconfig' is the order the
 * files themselves give, which is what the app did before there was a choice.
 */
export type ContextSort = 'name' | 'name-desc' | 'kubeconfig';

export interface Settings {
    manualFiles: string[];
    manualFolders: string[];
    excludedFiles: string[];
    /**
     * Single contexts removed from this app, by id; their files are still
     * read. Not shown anywhere: a removal lasts only while the context is in
     * its file, so there is nothing for the user to restore.
     */
    excludedContexts: string[];
    /** Extra folders themes are read from, on top of the default one. */
    themeFolders: string[];
    /** Extra folders solution plugins are read from, on top of the default one. */
    pluginFolders: string[];
    /**
     * Known plugins the sidebar has been told to stop suggesting. Optional so
     * fixtures need not spell it out.
     */
    hiddenPluginSuggestions?: string[];
    /**
     * What plugins' own pages keep through the bridge's storage: plugin id ->
     * context id -> key -> value. Optional so fixtures need not spell it out.
     */
    pluginState?: Record<string, Record<string, Record<string, string>>>;
    contexts: Record<string, ContextPrefs>;
    /**
     * Where every open view sits: which pane holds it, in what order, whether
     * each pane is showing its contents and how much room it takes. All three
     * panes are written by one writer -- see appconfig.Panes for why.
     */
    panes: Record<PaneId, SavedPane>;
    /**
     * The app-wide preferences the settings view edits. Unlike `layout`, every
     * field here is resolved: `restoreTabs` arrives nullable from Go and is
     * settled to a boolean on the way in, because "never chosen" is a storage
     * concern and nothing downstream should have to know about it.
     */
    preferences: {
        /**
         * The id of the chosen theme. A free string rather than a union: the
         * themes are data, not code, and the set of valid ids is whatever is
         * installed at the time -- see internal/themes.
         */
        theme: string;
        density: Density;
        restoreTabs: boolean;
        confirmSourceRemoval: boolean;
        showKubeconfigNames: boolean;
        /** How the sidebar orders contexts: by name, by name reversed, or as found. */
        contextSort: ContextSort;
        showLineNumbers: boolean;
        /** Whether the app asks GitHub, on its own, if a newer release is out. */
        checkForUpdates: boolean;
        /** Whether cluster alerts are posted as the system's notifications too. Superseded by alerts. */
        desktopNotifications: boolean;
        /** Where cluster alerts go: the bell and the system, the bell only, or nowhere. */
        alerts: AlertMode;
        /** When a snooze of the alerts ends, RFC3339; empty when not snoozed. */
        alertsSnoozedUntil: string;
        /** How dates and times are written, in the app and in plugins' pages. */
        dateTime: DateTimeSettings;
        /** How far back a metrics chart looks, in minutes. */
        metricsRange: number;
        /** How a shell opens, and what it opens with. */
        terminal: TerminalSettings;
        /** Where helm is, and how it is run. */
        helm: HelmSettings;
        /** The picture behind the start page. */
        background: BackgroundSettings;
    };
    /**
     * The forwards the user set up. The live state of each lives in the
     * forwards store, which the backend feeds; this is only what survives a
     * restart, and nothing in the app reads it directly.
     */
    portForwards: SavedForward[];
    layout: {
        /** Which pane the describe tab opens in: left | main | right | bottom. */
        detailPane: string;
        sidebarWidth: number;
        zoom: number;
        /** null means the user has never chosen; an empty list means "fold nothing". */
        collapsedGroups: string[] | null;
    };
}

/** Where cluster alerts go. */
export type AlertMode = 'system' | 'bell' | 'off';

/** One resource in a listing. */
export interface Row {
    id: string;
    name: string;
    namespace: string;
    cells: kube.Cell[];
}

/** A resource listing. */
export interface Table {
    kind: string;
    columns: string[];
    rows: Row[];
    namespaced: boolean;
    error: string;
}

/** The dashboard payload for one context. */
export interface Overview {
    context: string;
    cluster: string;
    server: string;
    version: string;
    distribution: string;
    namespaces: string[];
    stats: kube.Stat[];
    /** What is wrong with the cluster's pods, beyond the running count. */
    pods: PodTrouble;
    /** The same shape a resource tab renders, so both sort through one path. */
    events: Table;
}

export function adoptSettings(settings: appconfig.Settings): Settings {
    return {
        manualFiles: [...(settings.manualFiles ?? [])],
        manualFolders: [...(settings.manualFolders ?? [])],
        excludedFiles: [...(settings.excludedFiles ?? [])],
        excludedContexts: [...(settings.excludedContexts ?? [])],
        themeFolders: [...(settings.themeFolders ?? [])],
        pluginFolders: [...(settings.pluginFolders ?? [])],
        hiddenPluginSuggestions: [...(settings.hiddenPluginSuggestions ?? [])],
        pluginState: Object.fromEntries(
            Object.entries(settings.pluginState ?? {}).map(([plugin, contexts]) => [
                plugin,
                Object.fromEntries(
                    Object.entries(contexts ?? {}).map(([context, keys]) => [context, { ...(keys ?? {}) } as Record<string, string>]),
                ),
            ]),
        ),
        contexts: Object.fromEntries(
            Object.entries(settings.contexts ?? {}).map(([id, prefs]) => [
                id,
                {
                    alias: prefs?.alias ?? '',
                    color: prefs?.color ?? '',
                    metrics: prefs?.metrics ?? '',
                    // null is "follows the global folding" and must survive.
                    collapsedGroups: prefs?.collapsedGroups ?? null,
                    columns: adoptColumns(prefs?.columns),
                },
            ]),
        ),
        panes: {
            left: adoptPane(settings.panes?.left, true, 320),
            main: adoptPane(settings.panes?.main, true, 0),
            right: adoptPane(settings.panes?.right, true, 420),
            // Folded until something is opened in it, which is what an older
            // file's dock reads as too.
            bottom: adoptPane(settings.panes?.bottom, false, 320),
        },
        preferences: {
            // The store normalises these, so the fallbacks only cover a
            // service call that failed before it reached the store. An id that
            // names no installed theme is kept as it is and resolved where the
            // theme is applied, not here.
            theme: settings.preferences?.theme || DEFAULT_THEME_ID,
            density: (settings.preferences?.density || 'comfortable') as Density,
            // null is "never chosen", and the default is on. `??` rather than
            // `||`: an explicit false is a choice and must survive.
            restoreTabs: settings.preferences?.restoreTabs ?? true,
            confirmSourceRemoval: settings.preferences?.confirmSourceRemoval ?? false,
            // Off by default: most people keep every context in one
            // ~/.kube/config, where a heading per file only repeats itself.
            showKubeconfigNames: settings.preferences?.showKubeconfigNames ?? false,
            // Sorted by name unless the file says otherwise, including when it
            // says nothing: see appconfig.Preferences.ContextSort for why the
            // kubeconfig's own order is the case that has to be asked for.
            contextSort: (settings.preferences?.contextSort || 'name') as ContextSort,
            // On by default, and nullable on the Go side for exactly that
            // reason -- see RestoreTabs above.
            showLineNumbers: settings.preferences?.showLineNumbers ?? true,
            // On by default, and nullable on the Go side for the same reason
            // again: an older file must not read as having switched it off.
            checkForUpdates: settings.preferences?.checkForUpdates ?? true,
            // On by default, nullable on the Go side for the same reason.
            desktopNotifications: settings.preferences?.desktopNotifications ?? true,
            // Filled in by the store from the switch before it; empty only
            // from an older backend, where the switch still says.
            alerts: ((settings.preferences?.alerts as AlertMode) ||
                (settings.preferences?.desktopNotifications === false ? 'bell' : 'system')) as AlertMode,
            alertsSnoozedUntil: settings.preferences?.alertsSnoozedUntil ?? '',
            // Normalised by the store, so an unknown value never arrives; an
            // empty one is a file older than the setting.
            dateTime: {
                clock: (settings.preferences?.dateTime?.clock || DEFAULT_DATETIME.clock) as DateTimeSettings['clock'],
                dates: (settings.preferences?.dateTime?.dates || DEFAULT_DATETIME.dates) as DateTimeSettings['dates'],
                zone: (settings.preferences?.dateTime?.zone || DEFAULT_DATETIME.zone) as DateTimeSettings['zone'],
                ages: (settings.preferences?.dateTime?.ages || DEFAULT_DATETIME.ages) as DateTimeSettings['ages'],
            },
            // Zero from the store means never chosen. An hour is long enough to
            // show a rollout and short enough to still show a spike.
            metricsRange: settings.preferences?.metricsRange || 60,
            terminal: {
                // 'app' is the answer that always works: the terminal in the
                // dock needs nothing installed on this machine.
                mode: (settings.preferences?.terminal?.mode || 'app') as 'app' | 'external',
                // Deliberately kept as written even when nothing on this
                // machine answers to it -- see appconfig.Terminal.External.
                external: settings.preferences?.terminal?.external ?? '',
                shells: [...(settings.preferences?.terminal?.shells ?? ['bash', 'sh'])],
                nodeImage: settings.preferences?.terminal?.nodeImage || 'busybox',
                nodeNamespace: settings.preferences?.terminal?.nodeNamespace || 'default',
                fontSize: settings.preferences?.terminal?.fontSize || 12,
                scrollback: settings.preferences?.terminal?.scrollback || 5000,
            },
            helm: {
                // Empty is the right default rather than an omission: helm is
                // normally on PATH, and a path guessed here would be wrong more
                // often than the search is.
                path: settings.preferences?.helm?.path ?? '',
                // helm's own defaults, so a release changed from this app
                // behaves the way the same command would from a shell.
                wait: settings.preferences?.helm?.wait ?? false,
                atomic: settings.preferences?.helm?.atomic ?? false,
                // Zero from the store means never chosen. Five minutes is
                // helm's own default for --wait.
                timeoutSeconds: settings.preferences?.helm?.timeoutSeconds || 300,
            },
            background: {
                // The store normalises the source; this only covers a call
                // that failed before it got there.
                source: (['builtin', 'folder', 'none'].includes(settings.preferences?.background?.source ?? '')
                    ? settings.preferences?.background?.source
                    : 'builtin') as BackgroundSource,
                pinned: settings.preferences?.background?.pinned ?? '',
                // Zero from the store means never chosen: a quarter of an hour.
                minutes: settings.preferences?.background?.minutes || 15,
                palette: settings.preferences?.background?.palette === 'theme' ? 'theme' : 'varied',
            },
        },
        portForwards: (settings.portForwards ?? []).map((forward) => ({
            id: forward.id,
            contextId: forward.contextId,
            kind: forward.kind,
            namespace: forward.namespace,
            name: forward.name,
            remotePort: forward.remotePort,
            localPort: forward.localPort,
            random: forward.random,
            browser: forward.browser,
        })),
        layout: {
            ...settings.layout,
            // Preserved as null rather than defaulted to []: the two mean
            // different things to the sidebar.
            collapsedGroups: settings.layout?.collapsedGroups ?? null,
            zoom: settings.layout?.zoom || 1,
        },
    };
}

/**
 * Splits a context id into the two halves it is made of.
 *
 * The id is `<kubeconfig path>::<context name>`, built by kube.ContextID. It is
 * normally never taken apart on this side -- a context arrives with both fields
 * already on it -- but a *removed* context is only ever an id in the settings,
 * and it still has to be shown to somebody as a name and a file.
 */
export function splitContextId(id: string): { file: string; name: string } {
    const at = id.lastIndexOf('::');
    if (at === -1) return { file: '', name: id };
    return { file: id.slice(0, at), name: id.slice(at + 2) };
}

export function adoptFiles(files: kube.File[] | null): ConfigFile[] {
    return (files ?? []).map((file) => ({
        path: file.path,
        source: file.source,
        contexts: [...(file.contexts ?? [])],
        error: file.error,
    }));
}

export function adoptTable(table: kube.Table): Table {
    return {
        kind: table.kind,
        columns: [...(table.columns ?? [])],
        rows: (table.rows ?? []).map((row) => ({
            id: row.id,
            name: row.name,
            namespace: row.namespace,
            cells: [...(row.cells ?? [])],
        })),
        namespaced: table.namespaced,
        error: table.error,
    };
}

export function adoptOverview(overview: kube.Overview): Overview {
    return {
        context: overview.context,
        cluster: overview.cluster,
        server: overview.server,
        version: overview.version,
        distribution: overview.distribution,
        namespaces: [...(overview.namespaces ?? [])],
        stats: [...(overview.stats ?? [])],
        pods: adoptPodTrouble(overview.pods),
        events: adoptTable(overview.events),
    };
}

/** What is wrong with a cluster's pods, with the lists Go may send as null. */
export type PodTrouble = Omit<kube.PodTrouble, 'worst'> & { worst: kube.PodIssue[] };

export function adoptPodTrouble(pods: kube.PodTrouble | null | undefined): PodTrouble {
    return {
        evicted: pods?.evicted ?? 0,
        failed: pods?.failed ?? 0,
        crashLooping: pods?.crashLooping ?? 0,
        restarting: pods?.restarting ?? 0,
        restarts: pods?.restarts ?? 0,
        restartedRecently: pods?.restartedRecently ?? 0,
        worst: (pods?.worst ?? []).map((issue) => ({ ...issue })),
    };
}

/** One cluster's health, as the fleet view and the alerts read it. */
export type ClusterHealth = Omit<kube.ClusterHealth, 'notReadyNodes' | 'pods' | 'warningReasons'> & {
    notReadyNodes: string[];
    pods: PodTrouble;
    warningReasons: kube.ReasonCount[];
};

export function adoptHealth(health: kube.ClusterHealth): ClusterHealth {
    return {
        contextId: health.contextId,
        nodesReady: health.nodesReady,
        nodesTotal: health.nodesTotal,
        notReadyNodes: [...(health.notReadyNodes ?? [])],
        podsRunning: health.podsRunning,
        podsTotal: health.podsTotal,
        pods: adoptPodTrouble(health.pods),
        warnings: health.warnings,
        warningReasons: (health.warningReasons ?? []).map((r) => ({ ...r })),
        error: health.error,
    };
}

/** When a context's credentials expire, soonest first. */
export type Credentials = Omit<kube.Credentials, 'items'> & { items: kube.Credential[] };

export function adoptCredentials(credentials: kube.Credentials): Credentials {
    return {
        contextId: credentials.contextId,
        items: (credentials.items ?? []).map((item) => ({ ...item })),
        error: credentials.error,
    };
}

/** A cluster's events over a window, newest first. */
export type Timeline = Omit<kube.Timeline, 'events'> & { events: kube.TimelineEvent[] };

export function adoptTimeline(timeline: kube.Timeline): Timeline {
    return {
        from: timeline.from,
        to: timeline.to,
        events: (timeline.events ?? []).map((e) => ({ ...e })),
        truncated: timeline.truncated,
        error: timeline.error,
    };
}

/** Two copies of an object, and the difference between them. */
export type Comparison = Omit<kube.Comparison, 'lines'> & { lines: kube.DiffLine[] };

export function adoptComparison(comparison: kube.Comparison): Comparison {
    return {
        left: comparison.left,
        right: comparison.right,
        leftError: comparison.leftError,
        rightError: comparison.rightError,
        lines: (comparison.lines ?? []).map((l) => ({ ...l })),
        same: comparison.same,
        changes: comparison.changes,
    };
}
