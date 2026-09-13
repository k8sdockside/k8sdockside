<!--
  One pane: a strip of tabs, and under it whatever the selected one is showing.

  There used to be two of these, written separately -- a strip along the top
  that could only hold resource kinds, and a dock at the foot that could only
  hold documents. This is both, and which pane a view lives in is now the
  user's answer rather than the view type's: a pod list can sit at the foot and
  an editor can fill the middle, because a pane does not know or care what its
  tabs are.

  A pane belongs to the window rather than to the view beside it. Switching
  clusters, opening another tab or closing every tab there is leaves the others
  exactly as they were, because what is in one may be a document you are part
  way through, and the one thing that must not lose an edit is looking at
  something else for a moment.
-->
<script lang="ts">
    import type { Snippet } from 'svelte';
    import {
        ACCESS_OVERVIEW,
        DASHBOARD,
        PORT_FORWARDS,
        SETTINGS,
        HELP,
        KUBERNETES,
        iconFor,
        isPluginOverview,
        singularFor,
    } from '../catalogue';
    import { editors } from '../state/editor.svelte';
    import {
        MIN_PANE_SIZE,
        PANE_HEADROOM,
        iconForView,
        isDocumentView,
        isHorizontal,
        type PaneId,
    } from '../state/panes';
    import { currentTabDrag, endTabDrag } from '../state/tabdrag.svelte';
    import { isAppTab, isSettingsTab, workspace } from '../state/workspace.svelte';
    import AccessOverview from '../access/AccessOverview.svelte';
    import Dashboard from './Dashboard.svelte';
    import DetailPanel from './DetailPanel.svelte';
    import Icon from './Icon.svelte';
    import LogView from './LogView.svelte';
    import { authorOf, standingOf } from '../plugins/credit';
    import PluginCredit from './PluginCredit.svelte';
    import PluginFrame from './PluginFrame.svelte';
    import PluginOverview from './PluginOverview.svelte';
    import PortForwards from './PortForwards.svelte';
    import ResourceTable from './ResourceTable.svelte';
    import Sidebar from './Sidebar.svelte';
    import TabStrip, { type StripTab } from './TabStrip.svelte';
    import TerminalView from './TerminalView.svelte';
    import YamlEditor from './YamlEditor.svelte';
    import SettingsView from './settings/SettingsView.svelte';
    import HelpPage from './HelpPage.svelte';
    import KubernetesPage from './KubernetesPage.svelte';

    interface Props {
        pane: PaneId;
        /** Shown in the body when the pane has nothing open. The main pane's welcome. */
        empty?: Snippet;
    }

    let { pane, empty }: Props = $props();

    /**
     * How each pane names itself, and what it says when it is empty.
     *
     * The bottom pane keeps the wording the dock had, because it is still the
     * place the details panel's buttons send things and that sentence is how
     * anybody finds that out.
     */
    const STRIPS: Record<PaneId, { label: string; hint?: string; foldable: boolean }> = {
        left: {
            label: 'Left panel',
            hint: 'Drag a tab here to keep it beside the main view.',
            foldable: false,
        },
        main: { label: 'Open views', foldable: false },
        right: {
            label: 'Right panel',
            hint: 'Drag a tab here to keep it beside the main view.',
            foldable: false,
        },
        bottom: {
            label: 'Dock',
            hint: 'Select a resource, then Edit, Logs or Shell in the details panel to open it here.',
            foldable: true,
        },
    };

    let strip = $derived(STRIPS[pane]);
    // Named for what it holds rather than `state`, which collides with the rune.
    let contents = $derived(workspace.panes[pane]);
    let active = $derived(workspace.activeTabIn(pane));
    let open = $derived(workspace.isPaneOpen(pane) && active !== null);

    function contextName(contextId: string): string {
        const context = workspace.contexts.find((c) => c.id === contextId);
        return context ? workspace.displayName(context) : contextId;
    }

    /**
     * Whether this pane holds tabs from more than one cluster, which is what
     * decides if a collection tab spends space naming its own and whether the
     * menu offers its cluster-scoped items.
     *
     * The settings tab is left out: it has no context, so counting its empty id
     * would make a pane showing one cluster plus settings look like two and
     * start labelling everything.
     */
    let severalClusters = $derived(
        new Set(contents.tabs.filter((t) => !isAppTab(t)).map((t) => t.contextId)).size > 1,
    );

    /** The tooltip for a tab that belongs to the window rather than a cluster. */
    function appHint(tab: { kind: string }): string | null {
        if (tab.kind === SETTINGS) return 'Application settings';
        if (tab.kind === HELP) return 'How to use K8s Dockside';
        if (tab.kind === KUBERNETES) return 'A primer on Kubernetes and its terms';
        return null;
    }

    let tabs = $derived(
        contents.tabs.map((tab): StripTab => {
            const cluster = contextName(tab.contextId);
            if (tab.view === 'clusters') {
                return {
                    id: tab.id,
                    title: tab.title,
                    icon: iconForView(tab.view),
                    // The app's own accent rather than a cluster's: the tree is
                    // about all of them, so borrowing one's colour would say
                    // something untrue about what it is showing.
                    color: 'var(--accent)',
                    hint: 'Every kubeconfig context found on this machine',
                    closable: false,
                };
            }
            if (tab.view === 'resource') {
                return {
                    id: tab.id,
                    title: tab.title,
                    subtitle: severalClusters && !isAppTab(tab) ? cluster : undefined,
                    icon: iconFor(tab.kind),
                    color: workspace.colorOf(tab.contextId),
                    hint: appHint(tab) ?? `${tab.title} — ${cluster}`,
                };
            }
            // A view onto one object always names its cluster, unlike a
            // collection: an object's name says nothing about which cluster it
            // is in, and "edit the wrong cluster's ingress" is the mistake this
            // whole feature could cause.
            return {
                id: tab.id,
                title: tab.title,
                subtitle: cluster,
                icon: iconForView(tab.view),
                color: workspace.colorOf(tab.contextId),
                hint: `${singularFor(tab.kind)} ${tab.name}${tab.namespace ? ` in ${tab.namespace}` : ''} — ${cluster}`,
                // Only a document has unsaved work; closing a log view loses
                // nothing that was not already in the cluster.
                modified: isDocumentView(tab.view) && editors.isDirty(tab.id),
            };
        }),
    );

    function run(action: () => void, dismiss: () => void): void {
        action();
        dismiss();
    }

    // ----- taking tabs in ------------------------------------------------
    //
    // A pane with nothing in it still has to be somewhere a tab can be dropped,
    // which means being visible while a drag is going on and not before. The
    // drag is a piece of shared state every pane reads, so all of them light
    // up as it begins and all of them go dark as it ends -- see tabdrag.

    /** Whether a tab from another pane is in the air and this pane could take it. */
    let receiving = $derived.by(() => {
        const drag = currentTabDrag();
        return drag !== null && drag.from !== pane;
    });

    /** What the drop target says. Names the place, since that is what is being chosen. */
    const DROP_HINTS: Record<PaneId, string> = {
        left: 'Drop to move to the left panel',
        main: 'Drop to move to the main view',
        right: 'Drop to move to the right panel',
        bottom: 'Drop to move to the dock',
    };

    function adopt(id: string, _from: PaneId, index: number): void {
        workspace.moveTabToPane(id, pane, index);
    }

    /** The body takes a dropped tab at the end, where the strip takes it in place. */
    function dropOnBody(event: DragEvent): void {
        const drag = currentTabDrag();
        if (!drag || drag.from === pane) return;
        event.preventDefault();
        workspace.moveTabToPane(drag.id, pane);
        endTabDrag();
    }

    function dragOverBody(event: DragEvent): void {
        const drag = currentTabDrag();
        if (!drag || drag.from === pane) return;
        event.preventDefault();
        if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    }

    // ----- resizing --------------------------------------------------------

    let paneEl = $state<HTMLElement | null>(null);
    let resizing = $state(false);

    /** Which way the pane is measured: the side panels by width, the bottom by height. */
    let horizontal = $derived(isHorizontal(pane));

    function startResize(event: PointerEvent): void {
        event.preventDefault();
        resizing = true;
        (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    }

    function onResize(event: PointerEvent): void {
        if (!resizing || !paneEl) return;
        // Measured from the pane's own far edge, so the arithmetic does not
        // depend on where the sidebar or the status bar happens to be.
        const box = paneEl.getBoundingClientRect();
        // The left panel grows rightwards from its own left edge; everything
        // else is measured back from its far edge.
        const size =
            pane === 'left'
                ? event.clientX - box.left
                : horizontal
                  ? box.right - event.clientX
                  : box.bottom - event.clientY;
        const limit = horizontal
            ? window.innerWidth - PANE_HEADROOM[pane]
            : window.innerHeight - PANE_HEADROOM.bottom;
        workspace.setPaneSize(pane, Math.max(MIN_PANE_SIZE[pane], Math.min(size, limit)));
    }

    function endResize(event: PointerEvent): void {
        resizing = false;
        (event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId);
    }

    /** Arrow keys resize the pane for anyone not using a pointer. */
    function onHandleKey(event: KeyboardEvent): void {
        const step = event.shiftKey ? 48 : 16;
        // Growing is always "away from the pane's own outer edge", which is the
        // opposite direction on the left from everywhere else.
        const grow = pane === 'left' ? 'ArrowRight' : horizontal ? 'ArrowLeft' : 'ArrowUp';
        const shrink = pane === 'left' ? 'ArrowLeft' : horizontal ? 'ArrowRight' : 'ArrowDown';
        const limit = horizontal
            ? window.innerWidth - PANE_HEADROOM[pane]
            : window.innerHeight - PANE_HEADROOM.bottom;

        if (event.key === grow) {
            event.preventDefault();
            workspace.setPaneSize(pane, Math.min(contents.size + step, limit));
        } else if (event.key === shrink) {
            event.preventDefault();
            workspace.setPaneSize(pane, Math.max(MIN_PANE_SIZE[pane], contents.size - step));
        }
    }

    /**
     * Whether the pane is on screen at all.
     *
     * The bottom one always is: its strip is what makes it a place things can
     * be put, and a place has to be visible before anything can be put there.
     * The side panels earn their room by holding something -- or by having a
     * tab hovering over them, which is how an empty one is aimed at.
     */
    let present = $derived(
        pane === 'main' ||
            strip.foldable ||
            (contents.tabs.length > 0 && contents.open) ||
            receiving,
    );
    /** On screen only as somewhere to drop the thing being dragged. */
    let bare = $derived(contents.tabs.length === 0);
</script>

<!-- A drag that ends anywhere but on a target -- let go over the table, or
     outside the window -- still has to end. The source's own dragend reaches
     here for those; a drop on a target ends the drag itself. -->
<svelte:window ondragend={endTabDrag} ondrop={endTabDrag} />

{#if present}
    <section
        class="pane {pane}"
        class:open
        class:receiving
        class:bare
        class:sized={pane !== 'main'}
        bind:this={paneEl}
        style:--size="{contents.size}px"
    >
        {#if pane !== 'main' && !bare}
            <!-- A focusable separator is the ARIA "window splitter" pattern; the
                 a11y rules below only key off the role, which they treat as static. -->
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
            <div
                class="handle"
                class:active={resizing}
                role="separator"
                aria-label="Resize the {STRIPS[pane].label.toLowerCase()}"
                aria-orientation={horizontal ? 'vertical' : 'horizontal'}
                aria-valuenow={contents.size}
                aria-valuemin={MIN_PANE_SIZE[pane]}
                tabindex="0"
                onpointerdown={startResize}
                onpointermove={onResize}
                onpointerup={endResize}
                onpointercancel={endResize}
                onkeydown={onHandleKey}
            ></div>
        {/if}

        <TabStrip
            {tabs}
            {pane}
            activeId={contents.activeId}
            label={strip.label}
            rule={pane === 'bottom' ? 'above' : 'below'}
            empty={strip.hint}
            onactivate={(id) => workspace.activateTab(id)}
            onclose={(id) => workspace.closeTab(id)}
            onmove={(from, to) => workspace.reorderTab(pane, from, to)}
            onadopt={adopt}
            menu={tabMenu}
            trailing={strip.foldable ? controls : undefined}
        />

        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="body" ondragover={dragOverBody} ondrop={dropOnBody}>
            <!-- Where the tab in the air can land: drawn over whatever the
                 pane is showing, the moment a drag begins, so the places a
                 tab can go are on screen before anybody has to hunt for
                 them. Decoration for the pointer only -- the menu's "Move
                 to" items are the route that needs no aim. -->
            {#if receiving}
                <div class="dropzone" aria-hidden="true">
                    <Icon name={pane === 'main' ? 'dashboard' : `dock-${pane}`} size={18} />
                    <span>{DROP_HINTS[pane]}</span>
                </div>
            {/if}
            {#if open && active}
                {#key active.id}
                    {#if active.view === 'clusters'}
                        <Sidebar />
                    {:else if active.view === 'details'}
                        <!-- Its id is the same whatever it is describing, so the
                             {#key} above does not remount it as the selection
                             moves down a list -- which is the difference between
                             refilling a panel and flickering a new one in. -->
                        <DetailPanel />
                    {:else if active.view === 'resource'}
                        {#if active.kind === SETTINGS}
                            <SettingsView />
                        {:else if active.kind === HELP}
                            <HelpPage />
                        {:else if active.kind === KUBERNETES}
                            <KubernetesPage />
                        {:else if active.kind === DASHBOARD}
                            <Dashboard contextId={active.contextId} />
                        {:else if active.kind === PORT_FORWARDS}
                            <PortForwards contextId={active.contextId} />
                        {:else if active.kind === ACCESS_OVERVIEW}
                            <AccessOverview contextId={active.contextId} />
                        {:else if isPluginOverview(active.kind) && workspace.pluginFor(active.kind)?.overview}
                            {@const owner = workspace.pluginFor(active.kind)!}
                            <!-- A plugin that draws its own landing page, with the
                                 app's own line under it saying who it is from --
                                 outside the frame, so the page cannot draw over it. -->
                            <div class="own-overview">
                                <PluginFrame contextId={active.contextId} kind={active.kind} />
                                <footer class="plugin-credit">
                                    <span class="plugin-name">{owner.name}</span>
                                    {#if owner.version}<span class="plugin-version">v{owner.version.replace(/^v/, '')}</span>{/if}
                                    <PluginCredit author={authorOf(owner)} authorUrl={owner.authorUrl} standing={standingOf(owner)} />
                                </footer>
                            </div>
                        {:else if isPluginOverview(active.kind)}
                            <PluginOverview contextId={active.contextId} kind={active.kind} />
                        {:else if workspace.pluginViewFor(active.kind)?.type === 'custom'}
                            <PluginFrame contextId={active.contextId} kind={active.kind} />
                        {:else}
                            <ResourceTable contextId={active.contextId} kind={active.kind} />
                        {/if}
                    {:else if active.view === 'logs'}
                        <LogView tab={active} />
                    {:else if active.view === 'shell'}
                        <TerminalView tab={active} />
                    {:else}
                        <YamlEditor tab={active} />
                    {/if}
                {/key}
            {:else if empty}
                {@render empty()}
            {/if}
        </div>
    </section>
{/if}

{#snippet controls()}
    <button
        class="fold"
        onclick={() => workspace.togglePane(pane)}
        disabled={contents.tabs.length === 0}
        aria-expanded={open}
        title={open ? 'Hide the dock' : 'Show the dock'}
        aria-label={open ? 'Hide the dock' : 'Show the dock'}
    >
        <Icon name={open ? 'chevron-down' : 'chevron-up'} size={15} />
    </button>
{/snippet}

{#snippet tabMenu(tab: StripTab, dismiss: () => void)}
    {@const contextId = contents.tabs.find((t) => t.id === tab.id)?.contextId ?? ''}
    <!-- A pinned tab has no close button, so it gets no Close item either.
         "Close Others" and "Close All" stay: they are about the rest, and the
         store spares the pinned one whatever the predicate says. -->
    {#if tab.closable !== false}
        <button role="menuitem" onclick={() => run(() => workspace.closeTab(tab.id), dismiss)}>
            Close
        </button>
    {/if}
    <button role="menuitem" onclick={() => run(() => workspace.closeOtherTabsIn(pane, tab.id), dismiss)}>
        Close Others
    </button>
    <button role="menuitem" onclick={() => run(() => workspace.closeAllTabsIn(pane), dismiss)}>
        Close All
    </button>

    <!-- Where else this view could go. The drag is the quick way and this is
         the discoverable one: a menu item says the panes exist, which a drop
         target nobody has dragged anything over does not. -->
    <hr />
    {#each ['left', 'main', 'right', 'bottom'] as const as target (target)}
        {#if target !== pane}
            <button
                role="menuitem"
                onclick={() => run(() => workspace.moveTabToPane(tab.id, target), dismiss)}
            >
                Move to {STRIPS[target].label}
            </button>
        {/if}
    {/each}

    {#if severalClusters}
        {@const cluster = contextName(contextId)}
        <hr />
        <button
            role="menuitem"
            onclick={() => run(() => workspace.closeOtherTabsIn(pane, tab.id, contextId), dismiss)}
        >
            Close Others in {cluster}
        </button>
        <button
            role="menuitem"
            onclick={() => run(() => workspace.closeAllTabsIn(pane, contextId), dismiss)}
        >
            Close All in {cluster}
        </button>
    {/if}
{/snippet}

<style>
    .pane {
        position: relative;
        display: flex;
        flex-direction: column;
        min-width: 0;
        min-height: 0;
    }

    /* Main takes what the others leave; they take what they were dragged to. */
    .pane.main {
        flex: 1 1 0;
    }

    .pane.sized {
        flex: 0 0 auto;
        background: var(--bg-panel);
    }

    /* The size the user dragged to, but never more than leaves the rest of the
       window something to be. A pane restored from a session on a wider screen
       would otherwise take almost all of it, and the view beside it would be a
       column of ellipses. Expressed against the row the pane sits in rather
       than the window, so it holds as the window is resized and needs nothing
       in script to keep it true.

       Never below the pane's own minimum, though: in a window too narrow to
       give both of them room, a pane capped to nothing would disappear rather
       than be small, and whatever you had open in it would simply not be
       there. */
    .pane.left,
    .pane.right {
        width: var(--size);
    }

    .pane.left {
        max-width: max(200px, calc(100% - 320px));
    }

    .pane.right {
        max-width: max(260px, calc(100% - 320px));
    }

    .pane.left {
        border-right: 1px solid var(--border);
        background: var(--bg-sidebar);
    }

    .pane.right {
        border-left: 1px solid var(--border);
    }

    /* Only an open bottom pane has a height of its own; folded, it is its strip. */
    .pane.bottom.open {
        height: var(--size);
        max-height: max(160px, calc(100% - 160px));
    }

    /* An empty side panel exists only while something is being dragged at it,
       and then only as a target wide enough to aim for. */
    .pane.left.bare,
    .pane.right.bare {
        width: 220px;
    }

    /* The grab strip sits over the seam between the pane and what is next to
       it, so the whole edge is grabbable rather than the one pixel of the rule. */
    .handle {
        position: absolute;
        z-index: 4;
        background: transparent;
        transition: background 120ms ease;
    }

    .pane.right .handle {
        top: 0;
        bottom: 0;
        left: -3px;
        width: 7px;
        cursor: col-resize;
    }

    /* The left panel's seam is on its right, where it meets the view. */
    .pane.left .handle {
        top: 0;
        bottom: 0;
        right: -3px;
        width: 7px;
        cursor: col-resize;
    }

    .pane.bottom .handle {
        left: 0;
        right: 0;
        top: -3px;
        height: 7px;
        cursor: row-resize;
    }

    .handle:hover,
    .handle.active,
    .handle:focus-visible {
        background: var(--accent);
    }

    .body {
        position: relative;
        display: flex;
        flex-direction: column;
        flex: 1 1 auto;
        min-height: 0;
        min-width: 0;
        overflow: hidden;
    }

    /* A plugin's own overview, and the app's line under it. */
    .own-overview {
        display: flex;
        flex-direction: column;
        flex: 1 1 auto;
        min-height: 0;
    }

    .plugin-credit {
        flex: none;
        display: flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
        padding: 3px 12px;
        font-size: 11px;
        color: var(--text-faint);
        background: var(--bg-panel);
        border-top: 1px solid var(--border-soft);
    }

    .plugin-name {
        color: var(--text-dim);
        white-space: nowrap;
    }

    .plugin-version {
        font-family: var(--mono);
        font-size: 10.5px;
    }

    /* A folded bottom pane is its strip and nothing else... */
    .pane.bottom:not(.open) .body {
        flex: 0 0 auto;
    }

    /* ...until a tab is in the air, when it opens a body's worth of room to
       aim at. A strip is a target a few pixels tall at the foot of the
       window, and most of that is the hint text -- which is the "where is
       the bottom area" that anybody dragging a tab there for the first time
       asks. */
    .pane.bottom.receiving:not(.open) .body {
        flex: 0 0 96px;
    }

    .dropzone {
        position: absolute;
        inset: 6px;
        z-index: 6;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 8px;
        border: 2px dashed var(--accent);
        border-radius: var(--radius);
        background: color-mix(in srgb, var(--accent) 12%, var(--bg-panel));
        font-size: 12.5px;
        color: var(--text);
        /* The drop lands on the body underneath, which is what handles it;
           the zone only says where. */
        pointer-events: none;
    }

    .fold {
        display: grid;
        place-items: center;
        width: 26px;
        height: 26px;
        border-radius: var(--radius-sm);
        color: var(--text-dim);
    }

    .fold:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .fold:disabled {
        opacity: 0.35;
        cursor: default;
    }
</style>
