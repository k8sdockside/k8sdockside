// What was typed into a tab's own fields, so that coming back to the tab finds
// it still there.
//
// The pane wraps the active view in {#key tab.id}, so bringing another tab
// forward destroys whatever was showing and coming back builds it again. A
// resource table keeps its sort and search in ./views.ts; this is the same idea
// for everything else with a field in it -- a log filter, a find in the
// describe tab, the dashboard's event filters, the plugin search in settings,
// the half-filled compare form. Each component reads its fields from here as it
// mounts and writes them back as they change.
//
// Kept for the session and dropped with the tab, for the reasons views.ts
// gives: a filter left in a tab closed this morning is not wanted in the one
// opened this afternoon. Plugin pages are kept the same way, by PluginFrame --
// a page in a sandboxed frame cannot be read from here, so its SDK reports its
// fields instead; see internal/plugins/sdk.

/** Splits a scope from the field name, and a tab id from a sub-scope under it. */
const SEP = '\u0000';

const kept = new Map<string, unknown>();

function key(scope: string, field: string): string {
    return scope + SEP + field;
}

/** Copies through JSON, so a caller holding the value cannot change what is kept. */
function copy<T>(value: T): T {
    return value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T);
}

export const fields = {
    /**
     * A scope under a tab, for a tab that shows more than one thing over its
     * life -- the describe tab, refilled for each object. Forgetting the tab
     * forgets every scope under it.
     */
    scope(tabId: string, sub: string): string {
        return tabId + SEP + sub;
    },

    /** What a field held when its tab was last on screen, or the fallback. */
    recall<T>(scope: string, field: string, fallback: T): T {
        const k = key(scope, field);
        return kept.has(k) ? copy(kept.get(k) as T) : fallback;
    },

    /** Remembers a field's value. */
    keep(scope: string, field: string, value: unknown): void {
        if (!scope) return;
        kept.set(key(scope, field), copy(value));
    },

    /** Drops everything kept for a tab, and for every scope under it. */
    forget(tabId: string): void {
        const prefix = tabId + SEP;
        for (const k of [...kept.keys()]) {
            if (k.startsWith(prefix)) kept.delete(k);
        }
    },

    /** Drops everything. For tests, which share this module across cases. */
    forgetAll(): void {
        kept.clear();
    },
};
