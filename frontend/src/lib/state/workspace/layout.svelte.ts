// How the window is laid out: restoring and saving the panes, where the describe tab goes, folding and zoom.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import { SettingsService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as appconfig from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/appconfig/models.js';
import { adoptSettings, type ContextPrefs, type Settings } from '../adopt';
import { changes } from '../changes.svelte';
import { views } from '../views';
import {
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    PANE_IDS,
    clustersTab,
    detailsTab,
    type PaneId,
    type PaneState,
} from '../panes';
import { NAV_GROUPS } from '../../catalogue';
import { clusters } from '../health.svelte';
import { notices } from '../notices.svelte';
import { session } from '../session.svelte';
import { detail, type DetailTarget } from '../detail.svelte';
import {
    MAX_ZOOM,
    MIN_ZOOM,
    ZOOM_STEP,
    canRestore,
    debounce,
    isAppTab,
    message,
    tabFromRef,
    toggled,
} from './helpers';
import { WorkspaceTabs } from './tabs.svelte';

export abstract class WorkspaceLayout extends WorkspaceTabs {
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

    protected persistPanes(): void {
        this.savePanes(
            $state.snapshot({
                left: this.paneRef('left'),
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
    protected restorePanes(): void {
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
    protected dropTabsForMissingContexts(): void {
        const known = new Set(this.contexts.map((c) => c.id));
        for (const pane of PANE_IDS) {
            // The settings tab survives every sync, as the cluster tree does:
            // neither depends on a kubeconfig, so losing every cluster must not
            // close them.
            this.retain(pane, (tab) => isAppTab(tab) || known.has(tab.contextId));
        }
    }

    /** Keeps a context selected if there is one, so the sidebar is never idle. */
    protected ensureSelection(): void {
        if (this.selectedContextId && this.contexts.some((c) => c.id === this.selectedContextId)) {
            return;
        }
        const current = this.contexts.find((c) => c.current) ?? this.contexts[0];
        this.selectedContextId = current?.id ?? null;
        if (current) {
            this.selectContext(current.id);
        }
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
    protected showDetailsTab(target: DetailTarget): void {
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
    protected hideDetailsTab(): void {
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
    protected rememberDetailPane(pane: PaneId): void {
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
}
