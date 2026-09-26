// The pure parts of the workspace: defaults, the shape of a restored tab, and
// the small list and text helpers its layers share. Nothing here holds state.

import type * as kube from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
import type * as appconfig from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/appconfig/models.js';
import { type Settings } from '../adopt';
import { logs } from '../logs.svelte';
import { views } from '../views';
import {
    clustersTab,
    isDocumentView,
    isTabView,
    resourceTabId,
    tabIdFor,
    type Tab,
    type TabView,
} from '../panes';
import {
    DASHBOARD,
    SETTINGS,
    APP_KINDS,
    labelFor,
} from '../../catalogue';
import { clusters } from '../health.svelte';
import { DEFAULT_DATETIME } from '../../datetime.svelte';
import { session } from '../session.svelte';
import { DEFAULT_THEME_ID } from '../../theme/apply';

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

export const NOT_LOADED: CustomKinds = { status: 'idle', groups: [], message: '' };

/**
 * How often a connected cluster is asked again which plugins it has -- see
 * Workspace.recheckPlugins. A minute is soon enough that installing a product
 * reads as bringing its plugin to life, and slow enough that the handful of
 * requests it costs goes unnoticed.
 */
export const PLUGIN_RECHECK_MS = 60_000;

/**
 * Whether one saved tab can come back.
 *
 * A view this build has never heard of is skipped rather than guessed at, and
 * so are logs and shells: they are connections rather than state, and dialling
 * every cluster in the window before it is up is not a session being restored.
 */
export function canRestore(known: Set<string>) {
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
export function pinnedFirst(keep: (tab: Tab) => boolean): (tab: Tab) => boolean {
    return (tab) => tab.pinned === true || keep(tab);
}

/** One saved tab, as the store holds it. */
export function tabFromRef(ref: {
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
export function defaultPaneSettings(): Settings['panes'] {
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

export function defaultSettings(): Settings {
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
            desktopNotifications: true,
            alerts: 'system',
            alertsSnoozedUntil: '',
            dateTime: { ...DEFAULT_DATETIME },
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
            background: { source: 'builtin', pinned: '', minutes: 15, palette: 'varied' },
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
export const NEUTRAL_TAB_COLOR = '#7b8794';

/**
 * How two context names are compared when the sidebar is sorted.
 *
 * Numeric so `node-2` comes before `node-10`, and case-insensitive so a
 * capitalised alias is not filed away in a block of its own above the rest --
 * cluster names are read as words, not as byte strings.
 */
export const byName = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** Zoom bounds, matching appconfig.MinZoom/MaxZoom on the Go side. */
export const MIN_ZOOM = 0.5;
export const MAX_ZOOM = 2;
export const ZOOM_STEP = 0.1;


/** Adds or removes one label, whichever way the toggle should go. */
export function toggled(groups: string[], label: string): string[] {
    return groups.includes(label) ? groups.filter((g) => g !== label) : [...groups, label];
}

/**
 * The list with one item moved, or null when the move would change nothing --
 * which is every out-of-range index a drag can produce at the ends of a strip.
 */
export function moved<T>(list: T[], from: number, to: number): T[] | null {
    if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) {
        return null;
    }
    const next = [...list];
    const [item] = next.splice(from, 1);
    next.splice(to, 0, item);
    return next;
}

/** Whether two folding lists say the same thing, regardless of order. */
export function sameGroups(a: string[], b: string[]): boolean {
    return a.length === b.length && [...a].sort().join('\u0000') === [...b].sort().join('\u0000');
}

/** The key an API group's open state is remembered under. */
export function apiGroupKey(contextId: string, group: string): string {
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
export const SETTINGS_TAB_ID = resourceTabId('', SETTINGS);

export function message(err: unknown): string {
    if (err instanceof Error) return err.message;
    return String(err);
}

/**
 * The first line of a message, for the status bar, which has room for one.
 * The whole of a plugin's refusal is in Settings, where there is room.
 */
export function firstLine(text: string): string {
    const at = text.indexOf('\n');
    return at < 0 ? text : `${text.slice(0, at)} (Settings → Plugins has the rest)`;
}

/**
 * debounce delays a call until the caller stops firing. Used so that dragging a
 * splitter or typing in the rename field does not write the settings file on
 * every frame or keystroke.
 */
export function debounce<A extends unknown[]>(fn: (...args: A) => void, ms: number): (...args: A) => void {
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
export type Section = 'contexts' | 'panes' | 'layout' | 'preferences';
