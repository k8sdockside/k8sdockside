// One context at a time: its name and colour, its tables' columns, and whether it is selected and unfolded.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import { SettingsService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as kube from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
import type * as appconfig from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/appconfig/models.js';
import { isEmptyContextPrefs, type ContextPrefs } from '../adopt';
import {
    clampColumnWidth,
    columnKeys,
    noColumnPrefs,
    type ColumnPrefs,
} from '../../columns';
import { changes } from '../changes.svelte';
import { clusters } from '../health.svelte';
import { compare } from '../compare.svelte';
import { defaultColorFor } from '../../colors';
import {
    NEUTRAL_TAB_COLOR,
    byName,
} from './helpers';
import { WorkspaceSources } from './sources.svelte';

export abstract class WorkspaceContexts extends WorkspaceSources {
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
    protected writeContextPrefs(contextId: string, patch: Partial<ContextPrefs>): void {
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
}
