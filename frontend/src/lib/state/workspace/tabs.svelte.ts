// Tabs and panes: opening, bringing forward, closing and moving views, and the bottom pane under its old name.
//
// One layer of the workspace -- see ../workspace.svelte.ts for how they fit.

import { TerminalService } from '../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { changes } from '../changes.svelte';
import { editors } from '../editor.svelte';
import { logs } from '../logs.svelte';
import { terminals } from '../terminals.svelte';
import { views } from '../views';
import {
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    PANE_IDS,
    defaultPaneFor,
    defaultPanes,
    isDocumentView,
    resourceTabId,
    tabIdFor,
    type PaneId,
    type Tab,
    type TabView,
} from '../panes';
import {
    DASHBOARD,
    PODS,
    SETTINGS,
    HELP,
    KUBERNETES,
    FLEET,
    COMPARE,
    PLUGINS_GROUP,
    DEFINITIONS_GROUP,
    groupForKind,
    labelFor,
    parseCustomKind,
    parsePluginKind,
} from '../../catalogue';
import { clusters } from '../health.svelte';
import { fleet } from '../fleet.svelte';
import { compare, type CompareTarget } from '../compare.svelte';
import { notices } from '../notices.svelte';
import { session } from '../session.svelte';
import { detail, type DetailTarget } from '../detail.svelte';
import {
    isAppTab,
    message,
    moved,
    pinnedFirst,
} from './helpers';
import { WorkspaceContexts } from './contexts.svelte';

export abstract class WorkspaceTabs extends WorkspaceContexts {
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
     * Opens the pod listing searched for some text across every namespace --
     * a status such as Evicted, from the dashboard's attention panel.
     */
    showPodsMatching(contextId: string, query: string): void {
        views.focusQuery(resourceTabId(contextId, PODS), query);
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

    /** Opens the fleet view, every cluster's health on one page. */
    openFleet(): void {
        this.openAppTab(FLEET);
    }

    /**
     * Opens the comparison view, set to compare one object with the same
     * object in another cluster when one is given.
     */
    openCompare(target?: CompareTarget): void {
        if (target) compare.against(target);
        this.openAppTab(COMPARE);
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
    protected insertTab(pane: PaneId, tab: Tab, { atEnd = false, index }: { atEnd?: boolean; index?: number } = {}): void {
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
            // A plugin this cluster does not have is folded one level deeper,
            // under "not in this cluster".
            const plugin = this.plugins.find((p) => p.id === view.pluginId);
            if (
                plugin &&
                this.pluginInstalledIn(tab.contextId, plugin) === false &&
                !this.isAbsentPluginsExpanded(tab.contextId)
            ) {
                this.toggleAbsentPlugins(tab.contextId);
            }
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
    protected retain(pane: PaneId, keep: (tab: Tab) => boolean): void {
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
     *
     * The web version always opens it in a pane, whatever the settings say:
     * "the user's own terminal" would be a window on the server, and the
     * preference may well have been set in the desktop app before.
     */
    openShell(target: DetailTarget): void {
        if (this.terminal.mode === 'external' && !session.server) {
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
}
