// Where a view sits, and what a view is.
//
// The app used to have two kinds of tab: a strip along the top holding resource
// kinds, and a dock at the foot holding documents. They did the same things --
// opened, closed, reordered, restored -- through two parallel sets of code, and
// each was nailed to its own end of the window. A YAML editor could only ever
// be at the bottom, a pod list could only ever be in the middle.
//
// This is the one model both became. A tab names what it shows; a pane is a
// place tabs go; and which pane a tab is in is the user's to decide, not the
// view type's. Everything that used to be "the dock" is now the bottom pane,
// which is a pane like the others that happens to start out holding documents.
//
// Nothing here touches Svelte state or the backend: it is the vocabulary the
// store is written in, so it can be reasoned about -- and tested -- on its own.

/**
 * The places a tab can be.
 *
 * Four fixed containers rather than a tree of splits. That is a deliberate
 * ceiling: it covers what people actually reach for -- the cluster tree down
 * one side, a list in the middle, its logs under it, an editor beside it --
 * while keeping the layout a flat record that can be written to the settings
 * file and read back without a migration every time the shape grows a case.
 *
 * Left and right are hidden outright when they hold nothing; the bottom one
 * keeps its strip, because a place has to be visible before anything can be
 * dragged into it.
 */
export type PaneId = 'left' | 'main' | 'right' | 'bottom';

/** Every pane, in the order the drop menu and the settings file list them. */
export const PANE_IDS: PaneId[] = ['left', 'main', 'right', 'bottom'];

/** Whether a string names a pane, for reading a settings file we did not write. */
export function isPaneId(value: string): value is PaneId {
    return (PANE_IDS as string[]).includes(value);
}

/**
 * What a tab shows.
 *
 * `resource` is a collection -- the pods of a cluster, a plugin's view, the
 * dashboard, the settings. `clusters` is the kubeconfig tree, which belongs to
 * the window rather than to any cluster. The other four are views onto one
 * object, and carry that object's namespace and name.
 *
 * `details` is the odd one: like `clusters` it is a singleton belonging to the
 * window, but what it shows is whatever row is selected rather than a fixed
 * thing. It is a tab so that the describe panel can be put where the user
 * wants it, which is the one thing it could never do as a docked panel.
 */
export type TabView =
    | 'clusters'
    | 'details'
    | 'resource'
    | 'edit'
    | 'helmvalues'
    | 'logs'
    | 'shell'
    | 'screen';

/** Every view, used to reject a type a hand-edited settings file made up. */
export const TAB_VIEWS: TabView[] = [
    'clusters',
    'details',
    'resource',
    'edit',
    'helmvalues',
    'logs',
    'shell',
    'screen',
];

export function isTabView(value: string): value is TabView {
    return (TAB_VIEWS as string[]).includes(value);
}

/**
 * The views that are an editable document. They share an editor, a store and a
 * dirty mark, and differ only in what is read and what a save means.
 * See ../state/editor.svelte.ts.
 */
const DOCUMENT_VIEWS: TabView[] = ['edit', 'helmvalues'];

/** Whether a view holds an editable document rather than a stream or a list. */
export function isDocumentView(view: TabView): boolean {
    return DOCUMENT_VIEWS.includes(view);
}

/** What a tab is opened against: a kind, and for the object views an object. */
export interface TabTarget {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

/**
 * One tab, wherever it happens to live.
 *
 * A resource tab leaves `namespace` and `name` empty, which is what makes one
 * id scheme serve both: `resource:prod#pods##` and `edit:prod#pods#default#web`
 * are the same shape and neither is ever parsed back.
 */
export interface Tab {
    /** `${view}:${contextId}#${kind}#${namespace}#${name}` -- a key, never parsed back. */
    id: string;
    view: TabView;
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
    /** What the strip shows: the kind's label, or the object's own name. */
    title: string;
    /**
     * A tab that cannot be closed, only moved or hidden with its pane.
     *
     * One view is: the kubeconfig tree. It is how anything else gets opened, so
     * a close button on it is a button that strands you in an empty window with
     * no way out. Hiding the pane does the job people actually want, and gives
     * it back again.
     */
    pinned?: boolean;
}

/** One pane's contents. Which pane it is lives in the record's key. */
export interface PaneState {
    tabs: Tab[];
    activeId: string | null;
    /**
     * Whether the pane is showing at all.
     *
     * The bottom one is the exception: folded, it keeps its strip and gives
     * back only the room under it, which is what makes it a dock rather than a
     * panel. The side panels fold away entirely -- that is what hiding the
     * cluster tree means, and it is the gesture Cmd/Ctrl+B does.
     */
    open: boolean;
    /** How much room the pane takes along its own axis, in px. */
    size: number;
}

/**
 * The tab id for one view of one thing.
 *
 * Content-addressed on purpose: a tab *is* what it shows, so opening the same
 * thing twice focuses the one that is open rather than making a second. That
 * also means dragging a tab from one pane to another keeps its id, and so keeps
 * its editor buffer, its scrollback and its shell -- moving a view does not
 * restart it.
 */
export function tabIdFor(view: TabView, target: TabTarget): string {
    return `${view}:${target.contextId}#${target.kind}#${target.namespace}#${target.name}`;
}

/** The id of the tab listing one kind in one context. */
export function resourceTabId(contextId: string, kind: string): string {
    return tabIdFor('resource', { contextId, kind, namespace: '', name: '' });
}

/**
 * Where a view opens when nothing says otherwise: a collection in the middle,
 * a document or a stream at the foot.
 *
 * Only ever consulted for a tab that is not already open somewhere. Once the
 * user has moved one, that is where its kind of thing goes as far as they are
 * concerned, and reopening it must not drag it back.
 */
export function defaultPaneFor(view: TabView): PaneId {
    if (view === 'clusters') return 'left';
    // Beside the list rather than under it: a describe report is read against
    // the row it belongs to, and the user moves it from here if they disagree.
    if (view === 'details') return 'right';
    return view === 'resource' ? 'main' : 'bottom';
}

/** The icon a view's tab wears, before the resource catalogue has its say. */
export function iconForView(view: TabView): string {
    switch (view) {
        case 'clusters':
            return 'server';
        case 'details':
            return 'info';
        case 'logs':
            return 'rows';
        case 'shell':
            return 'terminal';
        case 'screen':
            return 'display';
        case 'helmvalues':
            return 'helm';
        default:
            return 'edit';
    }
}

/** How a pane names itself to a screen reader, and in its own drop hint. */
export const PANE_LABELS: Record<PaneId, string> = {
    left: 'Left panel',
    main: 'Main',
    right: 'Right panel',
    bottom: 'Bottom panel',
};

/** An empty pane, at the size it takes the first time something lands in it. */
export function emptyPane(size: number): PaneState {
    return { tabs: [], activeId: null, open: true, size };
}

/** The default sizes, matching appconfig.Defaults on the Go side. */
export const DEFAULT_PANE_SIZE: Record<PaneId, number> = {
    left: 320,
    main: 0, // fills what is left; never read
    right: 420,
    bottom: 320,
};

/** The panes of a window nothing has been opened in yet. */
export function defaultPanes(): Record<PaneId, PaneState> {
    return {
        left: {
            ...emptyPane(DEFAULT_PANE_SIZE.left),
            tabs: [clustersTab()],
            activeId: CLUSTERS_TAB_ID,
        },
        main: emptyPane(DEFAULT_PANE_SIZE.main),
        right: emptyPane(DEFAULT_PANE_SIZE.right),
        // The one pane that starts folded: it is on screen from launch, and an
        // open one with nothing in it is a third of the window showing nothing.
        bottom: { ...emptyPane(DEFAULT_PANE_SIZE.bottom), open: false },
    };
}

/**
 * The smallest each pane may be dragged to, and the room it must leave the rest
 * of the window. Below these a pane shows its strip and three lines of content,
 * which is not a view of anything.
 */
export const MIN_PANE_SIZE: Record<PaneId, number> = { left: 200, main: 0, right: 260, bottom: 160 };
export const PANE_HEADROOM: Record<PaneId, number> = { left: 420, main: 0, right: 420, bottom: 220 };

/** Whether a pane is measured across rather than down. */
export function isHorizontal(pane: PaneId): boolean {
    return pane === 'left' || pane === 'right';
}

/**
 * The kubeconfig tree, as a tab.
 *
 * There is exactly one, it belongs to no cluster, and it cannot be closed --
 * see Tab.pinned. It is a tab at all so that it can be put where the user wants
 * it: on the right, at the foot, or beside a table in the middle.
 */
export const CLUSTERS_TAB_ID = tabIdFor('clusters', {
    contextId: '',
    kind: 'clusters',
    namespace: '',
    name: '',
});

/**
 * The describe panel, as a tab.
 *
 * One id for the window rather than one per object, because this tab follows
 * the selection: clicking row after row refills it rather than opening a
 * strip of them. That is the behaviour it had as a docked panel, and the
 * reason it is a singleton and not content-addressed like the editor.
 *
 * It is never written to the settings file. There is no selection to restore
 * it against, so what persists is only which pane it opens in -- see
 * Layout.detailPane on the Go side.
 */
export const DETAILS_TAB_ID = tabIdFor('details', {
    contextId: '',
    kind: 'details',
    namespace: '',
    name: '',
});

/** The describe tab for one object, titled with the object's own name. */
export function detailsTab(target: TabTarget): Tab {
    return {
        id: DETAILS_TAB_ID,
        view: 'details',
        contextId: target.contextId,
        kind: target.kind,
        namespace: target.namespace,
        name: target.name,
        title: target.name || 'Details',
    };
}

export function clustersTab(): Tab {
    return {
        id: CLUSTERS_TAB_ID,
        view: 'clusters',
        contextId: '',
        kind: 'clusters',
        namespace: '',
        name: '',
        title: 'Clusters',
        pinned: true,
    };
}
