// What each resource tab was showing when it was last on screen: its sort, its
// namespace filter and its search.
//
// The pane wraps the active view in {#key tab.id}, so bringing another tab
// forward destroys the table and coming back builds a new one, which would
// otherwise land on the default order, every namespace and an empty search --
// and pods sorted by age is exactly the view somebody leaves for a moment and
// expects to find again. So a table writes what it is showing here as it
// changes, and reads it back as it mounts.
//
// Kept for the session and no longer. A closed tab takes its view with it, the
// way it takes an editor's buffer: a search left in a tab closed this morning
// is not the search wanted from the one opened this afternoon, and hiding rows
// behind a filter nobody remembers typing would be the worse surprise. Not
// written to disk either: a sort is a column index, and a settings file older
// than a change to a kind's columns would sort by the wrong one.

/** How one tab was left. */
export interface TabView {
    sortColumn: number | null;
    sortDescending: boolean;
    namespaces: string[];
    query: string;
    /**
     * The node a pod listing is narrowed to, empty for all of them.
     *
     * Kept beside the namespace filter because it is the same kind of thing --
     * which slice of the cluster this tab is showing -- and it is remembered
     * for the same reason: "the pods on the node I am draining" is exactly the
     * view somebody leaves for a moment and expects to find again.
     */
    node: string;
}

const remembered = new Map<string, TabView>();

/** How a tab that has never been on screen starts. */
function blank(): TabView {
    return { sortColumn: null, sortDescending: false, namespaces: [], query: '', node: '' };
}

/**
 * Bumped whenever something outside a table changes what that table should be
 * showing -- today, focusNode.
 *
 * A plain Map cannot be watched, and the tables that read it are Svelte
 * components. Rather than make this module depend on Svelte, it publishes a
 * counter and a subscribe: a mounted table reads the counter, and reads its own
 * view again when it moves. A table that is not mounted needs none of this --
 * it reads its view as it is built.
 */
let revision = 0;
const watchers = new Set<() => void>();

function bump(): void {
    revision++;
    for (const notify of watchers) notify();
}

export const views = {
    /** How a tab was left, or null for one that has not been seen. */
    recall(tabId: string): TabView | null {
        const view = remembered.get(tabId);
        return view ? { ...view, namespaces: [...view.namespaces] } : null;
    },

    /**
     * Narrows a pod listing to one node, whether or not its tab is on screen.
     *
     * Written here rather than passed to the table because the table may not
     * exist yet: clicking a node opens the pods tab, and the component that
     * will show it is built afterwards and reads its view as it mounts. A tab
     * that *is* already mounted watches this through views.revision.
     */
    focusNode(tabId: string, node: string): void {
        const view = remembered.get(tabId) ?? blank();
        remembered.set(tabId, { ...view, node });
        bump();
    },

    /**
     * Narrows a list to one object's name, whether or not its tab is on
     * screen, so that opening a search hit lands on its row.
     *
     * Whatever else would hide the row goes with it: a node filter, and a
     * namespace filter that leaves out the object's namespace. A namespace
     * filter that already includes it is kept -- it is what the reader chose,
     * and it is not in the way.
     */
    focusName(tabId: string, name: string, namespace: string): void {
        const view = remembered.get(tabId) ?? blank();
        const hidden = namespace !== '' && view.namespaces.length > 0 && !view.namespaces.includes(namespace);
        remembered.set(tabId, { ...view, query: name, node: '', namespaces: hidden ? [] : view.namespaces });
        bump();
    },

    remember(tabId: string, view: TabView): void {
        remembered.set(tabId, { ...view, namespaces: [...view.namespaces] });
    },

    /** Drops a tab's view, as closing it does. */
    forget(tabId: string): void {
        remembered.delete(tabId);
    },

    /** Drops every tab's view. For tests, which share this module across cases. */
    forgetAll(): void {
        remembered.clear();
    },

    /** How many times a view has been changed from outside its own table. */
    revision(): number {
        return revision;
    },

    /** Calls back on every such change. Returns the unsubscribe. */
    watch(notify: () => void): () => void {
        watchers.add(notify);
        return () => watchers.delete(notify);
    },
};
