<!--
  One kubeconfig context in the sidebar, plus the resource tree it expands into.
  Clicking a resource opens (or focuses) a tab for it.
-->
<script lang="ts">
    import { tick } from 'svelte';
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import {
        DASHBOARD_ITEM,
        DEFINITIONS_GROUP,
        NAV_GROUPS,
        NETWORK_GROUP,
        PLUGIN_OVERVIEW,
        PLUGINS_GROUP,
        PORT_FORWARDS,
        pluginKindFor,
        type NavGroup,
        type NavItem,
    } from '../catalogue';
    import { classify } from '../errors';
    import { alpha } from '../colors';
    import { forwards, type Forward } from '../state/forwards.svelte';
    import { session } from '../state/session.svelte';
    import { workspace, type Health } from '../state/workspace.svelte';
    import { clusters } from '../state/health.svelte';
    import Icon from './Icon.svelte';
    import PluginMark from './PluginMark.svelte';
    import { notices } from '../state/notices.svelte';

    interface Props {
        context: kube.Context;
    }

    let { context }: Props = $props();

    /** The context's own row, and the whole block, for the reveal below. */
    let head = $state<HTMLElement>();
    let root = $state<HTMLElement>();

    let color = $derived(workspace.colorOf(context.id));
    let health = $derived(clusters.of(context.id));
    let expanded = $derived(workspace.isExpanded(context.id));
    let selected = $derived(workspace.selectedContextId === context.id);
    // The focused tab rather than the main pane's: a list dragged into another
    // panel is still what the user is looking at, and this row is how the tree
    // says where it is.
    let activeTab = $derived(workspace.focusedTab);

    function isOpen(kind: string): boolean {
        return activeTab?.contextId === context.id && activeTab.kind === kind;
    }

    /** How much room to leave above a revealed row, so it is not jammed to the edge. */
    const REVEAL_MARGIN = 8;

    /**
     * How long to keep waiting for a revealed row that is still being fetched,
     * and how often to look for it.
     *
     * Only a custom resource's row can be late: activating its tab unfolds the
     * definitions section, and the kinds under it are read from the cluster at
     * that point. The deadline is for the cluster that answers slowly or not at
     * all -- past it the reveal settles for the context's header, which is what
     * it would have done immediately before.
     */
    const REVEAL_DEADLINE = 4000;
    const REVEAL_POLL = 50;

    const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

    /** The nearest ancestor that is actually scrolling, if any. */
    function scrollParent(el: HTMLElement): HTMLElement | null {
        for (let node = el.parentElement; node; node = node.parentElement) {
            const overflow = getComputedStyle(node).overflowY;
            if ((overflow === 'auto' || overflow === 'scroll') && node.scrollHeight > node.clientHeight) {
                return node;
            }
        }
        return null;
    }

    // Bring this context into view when a tab for it is clicked.
    //
    // A context is a header with its resource tree hanging below it, and what
    // the user wants to see is both. `scrollIntoView({block: 'nearest'})` does
    // the minimum to reveal the *header*, which going downwards means parking
    // its 30px strip flush on the bottom edge with the whole tree still off
    // screen -- indistinguishable from not having scrolled at all. Going
    // upwards it lands at the top and looks fine, which is why this only shows
    // up switching to a context below the one you are on.
    //
    // So: leave it alone when the row is already fully visible, and otherwise
    // put the row near the top, where its tree has somewhere to be.
    $effect(() => {
        const request = workspace.reveal;
        if (!request || request.contextId !== context.id || !head) return;
        request.nonce;

        // Cancelled rather than awaited: a second reveal arriving while the
        // first is still waiting for its row must win, and a context scrolled
        // out of existence must not be scrolled to at all.
        let stale = false;
        void reveal(request.kind, () => stale);
        return () => {
            stale = true;
        };
    });

    /**
     * Brings one kind's row into view and flashes it, waiting for a row that is
     * still on its way.
     *
     * The tab's own row where it is showing, and the context's row only as a
     * fallback. With fifty rows under a context, bringing the cluster's name
     * into view says nothing about where in them the tab lives -- the sidebar
     * moves, lands on the wrong thing, and reads as a flicker.
     */
    async function reveal(kind: string, cancelled: () => boolean): Promise<void> {
        // Activating a tab unfolds whatever its row is folded inside, which is
        // state; the row cannot be looked for until that has rendered.
        await tick();
        if (cancelled()) return;

        let target = rowFor(kind);
        // A custom resource's row is the one that can be a fetch away: unfolding
        // the definitions section is what sends the app to ask the cluster which
        // kinds it serves. Waiting for that answer is the difference between the
        // reveal landing on the row and landing on the header a moment before
        // the row appears. Nothing else is waited for, so a kind that simply is
        // not in the tree still falls back at once.
        const until = Date.now() + REVEAL_DEADLINE;
        while (
            !target &&
            !cancelled() &&
            Date.now() < until &&
            workspace.customKindsFor(context.id).status === 'loading'
        ) {
            await pause(REVEAL_POLL);
            target = rowFor(kind);
        }
        if (cancelled() || !head) return;

        const found = target ?? head;
        const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        const behavior: ScrollBehavior = still ? 'auto' : 'smooth';
        const scroller = scrollParent(found);

        if (scroller) {
            const view = scroller.getBoundingClientRect();
            const box = found.getBoundingClientRect();
            if (box.top < view.top || box.bottom > view.bottom) {
                scroller.scrollTo({ top: scroller.scrollTop + (box.top - view.top) - REVEAL_MARGIN, behavior });
            }
        } else {
            // No scrolling ancestor: nothing to move, but still flash.
            found.scrollIntoView({ block: 'nearest', behavior });
        }

        // Removed and re-added around a forced reflow, the standard way to
        // restart an animation that may still be running from a prior reveal.
        found.classList.remove('flash');
        void found.offsetWidth;
        found.classList.add('flash');
    }

    /**
     * This context's row for one kind, when the tree is showing it. Absent for a
     * kind the tree is not drawing -- a section still shut, or definitions not
     * read yet -- in which case the reveal waits or falls back to the context
     * itself. See reveal().
     */
    function rowFor(kind: string): HTMLElement | null {
        if (!kind || !root) return null;
        return root.querySelector<HTMLElement>(`[data-kind="${CSS.escape(kind)}"]`);
    }

    // The modifier key is named for the platform, since the whole point of the
    // hint is that someone can act on it.
    const ALT = navigator.platform.startsWith('Mac') ? '\u2325' : 'Alt';

    function groupTitle(label: string, folded: boolean, local: boolean): string {
        const action = folded ? `Show ${label}` : `Hide ${label}`;
        const here = `${action} for this cluster (${ALT}-click for every cluster)`;
        return local ? `${here} \u2014 currently set differently here than elsewhere` : here;
    }

    // The definitions come from the cluster, so they are asked for whenever the
    // section is actually showing -- not when its heading is clicked.
    //
    // Clicking is only one of the ways it comes to be open: it may have been
    // left open in this context's folding from a previous session, or unfolded
    // on its own when a tab for one of its kinds was activated. Hanging the
    // fetch off the click missed all of those, and the section then sat empty
    // until it was collapsed and opened again. Keyed off the state, every route
    // in loads it.
    //
    // Still lazy: nothing is asked of a cluster whose definitions section is
    // shut, and loadCustomKinds does nothing once a context has an answer.
    $effect(() => {
        if (!expanded) return;
        const wanted =
            !workspace.isGroupCollapsed(context.id, DEFINITIONS_GROUP) ||
            !workspace.isGroupCollapsed(context.id, PLUGINS_GROUP);
        if (!wanted) return;
        void workspace.loadCustomKinds(context.id);
    });

    /**
     * How a plugin's row reads for this cluster.
     *
     * `unknown` is a real answer and not a placeholder: until the cluster's
     * definitions have been read we genuinely do not know, and a row that said
     * "not installed" before it had looked would be wrong more often than
     * right. The plugin is still openable in every state -- its overview is
     * where "this cluster does not have it" gets explained properly.
     */
    function presence(pluginId: string): 'installed' | 'absent' | 'unknown' {
        const plugin = workspace.plugins.find((p) => p.id === pluginId);
        if (!plugin) return 'unknown';
        const found = workspace.pluginInstalledIn(context.id, plugin);
        if (found === null) return 'unknown';
        return found ? 'installed' : 'absent';
    }

    function presenceTitle(name: string, state: string): string {
        switch (state) {
            case 'installed':
                return `${name} is installed in this cluster`;
            case 'absent':
                return `${name} does not appear to be installed in this cluster — open it to see what is missing`;
            default:
                return name;
        }
    }

    /**
     * This cluster's forwards, which hang under the Network heading. None in
     * the web version, where a forward would be a port on the server.
     */
    let tunnels = $derived(session.server ? [] : forwards.forContext(context.id));

    /**
     * The rows a section lists. All of them, except that the web version
     * leaves out the port forwards view with the forwards themselves.
     */
    function itemsOf(group: NavGroup): NavItem[] {
        return session.server ? group.items.filter((item) => item.kind !== PORT_FORWARDS) : group.items;
    }

    /** What a forward's row says it is doing, for the title attribute. */
    function forwardTitle(forward: Forward): string {
        const target = `${forward.name}:${forward.remotePort}`;
        switch (forward.state) {
            case 'active':
                return `localhost:${forward.localPort} → ${target} — click to open in your browser`;
            case 'connecting':
                return `Connecting to ${target}…`;
            case 'error':
                return `${target} — ${forward.error}`;
            default:
                return `${target} — disconnected`;
        }
    }

    /** Opens a live forward, and does nothing for one that is not up. */
    function visit(forward: Forward): void {
        if (forward.state !== 'active') return;
        void forwards.open(forward.id).catch((err: unknown) => {
            notices.fail(err instanceof Error ? err.message : String(err));
        });
    }

    /** The one button a forward's row carries: stop it, or start it again. */
    function toggleForward(forward: Forward): void {
        if (forward.state === 'active' || forward.state === 'connecting') {
            forwards.stop(forward.id);
            return;
        }
        void forwards.reconnect(forward.id).catch((err: unknown) => {
            notices.fail(err instanceof Error ? err.message : String(err));
        });
    }

    function statusTitle(state: Health): string {
        switch (state.status) {
            case 'connected':
                return 'Connected';
            case 'checking':
                return 'Checking the connection…';
            case 'error':
                return `Cannot connect — ${state.message}`;
            default:
                // Unchecked contexts show no indicator, so there is nothing to
                // describe.
                return '';
        }
    }
</script>

<div class="context" bind:this={root} class:selected style:--ctx-color={color} style:--ctx-tint={alpha(color, 0.16)}>
    <div class="head" bind:this={head}>
        <button
            class="twisty"
            onclick={() => workspace.toggleExpanded(context.id)}
            aria-label={expanded ? 'Collapse' : 'Expand'}
            aria-expanded={expanded}
        >
            <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={14} />
        </button>

        <button
            class="label"
            onclick={() => workspace.activateContext(context.id)}
            aria-expanded={expanded}
            title={context.server || context.name}
        >
            <span class="swatch" style:background={color}></span>
            <span class="name">{workspace.displayName(context)}</span>
            {#if context.current}
                <span class="badge" title="current-context in this kubeconfig">current</span>
            {/if}
        </button>

        <!-- Only while the context is open: collapsing the sections of a
             context you cannot see is a control with nothing to act on. -->
        {#if expanded}
            {@const shutting = workspace.anyGroupOpen(context.id)}
            <button
                class="sections"
                onclick={() => (shutting
                    ? workspace.collapseAllGroups(context.id)
                    : workspace.expandAllGroups(context.id))}
                title={shutting ? 'Collapse every section here' : 'Expand every section here'}
                aria-label={shutting ? 'Collapse every section here' : 'Expand every section here'}
            >
                <Icon name={shutting ? 'collapse-all' : 'expand-all'} size={13} />
            </button>
        {/if}

        <!-- Let go of the cluster without removing the context: its tabs close
             and nothing talks to it until it is opened again. Only for a
             context that is connected -- there is nothing to let go of
             otherwise, and opening one is what connects it. -->
        {#if workspace.isConnected(context.id)}
            <button
                class="remove disconnect"
                onclick={() => void workspace.disconnect(context.id)}
                title="Disconnect: close this context's tabs and stop talking to the cluster. Open it again to reconnect."
                aria-label="Disconnect {workspace.displayName(context)}"
            >
                <Icon name="power" size={12} />
            </button>
        {/if}

        <!-- Remove this one context. On the row itself rather than on the file
             heading, which is not shown by default and would remove the whole
             file anyway. Only the app's list changes; the kubeconfig does
             not, and the context appears again if it is added again. -->
        {#if session.admin}
            <button
                class="remove"
                onclick={() => workspace.removeContext(context.id)}
                title="Remove this context from k8sdockside. The kubeconfig is not changed; the context appears again if it is added again."
                aria-label="Remove {workspace.displayName(context)}"
            >
                <Icon name="close" size={12} />
            </button>
        {/if}

        <!-- Reachability sits at the far right, opposite the colour swatch, so
             "which cluster is this" and "can I reach it" never get confused for
             one another. -->
        {#if health.status === 'error'}
            <span class="status broken" title={statusTitle(health)} aria-label={statusTitle(health)}>
                <Icon name="alert" size={12} />
            </span>
        {:else if health.status === 'unknown'}
            <!-- Nothing drawn until a cluster has actually been checked: a mark
                 meaning "no news" is noise on a list this long. The slot is
                 still held open so names do not shift as results arrive. -->
            <span class="status" aria-hidden="true"></span>
        {:else}
            <span
                class="status dot {health.status}"
                title={statusTitle(health)}
                aria-label={statusTitle(health)}
            ></span>
        {/if}
    </div>

    {#if expanded}
        <div class="tree">
            <!-- Outside the sections: the overview is what is most often wanted
                 straight after opening a context, and a section holding one row
                 is a heading you have to open to reach a single thing. -->
            <button
                class="item"
                data-kind={DASHBOARD_ITEM.kind}
                class:open={isOpen(DASHBOARD_ITEM.kind)}
                onclick={() => workspace.openTab(context.id, DASHBOARD_ITEM.kind)}
            >
                <Icon name={DASHBOARD_ITEM.icon} size={15} />
                <span>{DASHBOARD_ITEM.label}</span>
            </button>

            {#each NAV_GROUPS as group (group.label)}
                {@const folded = workspace.isGroupCollapsed(context.id, group.label)}
                {@const local = workspace.groupDiffersFromGlobal(context.id, group.label)}
                <div class="group-row">
                <button
                    class="group"
                    class:folded
                    class:local
                    onclick={(event) => workspace.toggleGroup(context.id, group.label, {
                        allContexts: event.altKey,
                    })}
                    aria-expanded={!folded}
                    title={groupTitle(group.label, folded, local)}
                >
                    <Icon name={folded ? 'chevron-right' : 'chevron-down'} size={11} />
                    <span>{group.label}</span>
                    <!-- The count only earns its place when the group is shut:
                         open, the items themselves say how many there are. -->
                    {#if folded}
                        <span class="tally">
                            {group.label === PLUGINS_GROUP ? workspace.enabledPlugins.length : itemsOf(group).length}
                        </span>
                    {/if}
                </button>

                <!-- Only the definitions section can go stale: its contents are
                     the cluster's, not this list's, and they are read once. A
                     CRD installed since is a deliberate act, so asking again is
                     offered rather than done by holding a watch open.

                     Nested inside the heading it would fold the section on its
                     way through, so it sits beside it and stops the click. -->
                {#if !folded && (group.label === DEFINITIONS_GROUP || group.label === PLUGINS_GROUP)}
                    {@const reading = workspace.customKindsFor(context.id).status === 'loading'}
                    <button
                        class="reload"
                        class:spinning={reading}
                        disabled={reading}
                        onclick={(event) => {
                            event.stopPropagation();
                            void workspace.loadCustomKinds(context.id, { force: true });
                        }}
                        title="Read this cluster's definitions again"
                        aria-label="Read this cluster's definitions again"
                    >
                        <Icon name="refresh" size={12} />
                    </button>
                {/if}
                </div>

                {#if !folded}
                    {#each itemsOf(group) as item (item.kind)}
                        <button
                            class="item"
                            data-kind={item.kind}
                            class:open={isOpen(item.kind)}
                            onclick={() => workspace.openTab(context.id, item.kind)}
                        >
                            <Icon name={item.icon} size={15} />
                            <span>{item.label}</span>
                        </button>
                    {/each}

                    <!-- The network section carries the forwards this app is
                         holding open into this cluster. They are here rather
                         than only in their own tab because a tunnel is a thing
                         that is *running*: it should be visible without going
                         to look for it, and stoppable from where it is seen. -->
                    {#if group.label === NETWORK_GROUP && tunnels.length > 0}
                        {#each tunnels as forward (forward.id)}
                            <div class="tunnel">
                                <button
                                    class="tunnel-label {forward.state}"
                                    title={forwardTitle(forward)}
                                    onclick={() => visit(forward)}
                                >
                                    <span class="dot"></span>
                                    <span class="local">
                                        {forward.localPort ? `:${forward.localPort}` : '—'}
                                    </span>
                                    <span class="target">{forward.name}:{forward.remotePort}</span>
                                </button>
                                <button
                                    class="tunnel-act"
                                    title={forward.state === 'active' || forward.state === 'connecting'
                                        ? 'Disconnect this forward'
                                        : 'Connect this forward again'}
                                    aria-label={forward.state === 'active' || forward.state === 'connecting'
                                        ? `Disconnect ${forward.name}:${forward.remotePort}`
                                        : `Reconnect ${forward.name}:${forward.remotePort}`}
                                    onclick={() => toggleForward(forward)}
                                >
                                    <Icon
                                        name={forward.state === 'active' || forward.state === 'connecting'
                                            ? 'close'
                                            : 'refresh'}
                                        size={11}
                                    />
                                </button>
                            </div>
                        {/each}
                    {/if}

                    <!-- The plugins section: one row per plugin installed on
                         this machine, each unfolding into its own views. The
                         rows are the same whatever cluster this is -- a plugin
                         is installed here, not there -- and whether the cluster
                         actually has it is said in the margin. -->
                    {#if group.label === PLUGINS_GROUP}
                        {@const suggested = workspace.pluginSuggestionsFor(context.id)}
                        {#if workspace.enabledPlugins.length === 0 && suggested.length === 0}
                            <p class="note">No plugins installed</p>
                        {:else}
                            {#each workspace.enabledPlugins as plugin (plugin.id)}
                                {@const open = workspace.isPluginExpanded(context.id, plugin.id)}
                                {@const state = presence(plugin.id)}
                                <button
                                    class="plugin {state}"
                                    onclick={() => workspace.togglePlugin(context.id, plugin.id)}
                                    aria-expanded={open}
                                    title={presenceTitle(plugin.name, state)}
                                >
                                    <Icon name={open ? 'chevron-down' : 'chevron-right'} size={11} />
                                    <PluginMark id={plugin.id} icon={plugin.icon} logo={plugin.logo} size={14} />
                                    <span>{plugin.name}</span>
                                    {#if state === 'absent'}
                                        <span class="missing">not installed</span>
                                    {/if}
                                </button>

                                {#if open}
                                    <!-- Always first, and always there whether
                                         or not the plugin declared it: "is this
                                         even in this cluster?" is the question
                                         that has to have somewhere to be
                                         answered, especially when the CRDs are
                                         missing and every other row below would
                                         open onto an error. -->
                                    {@const overview = pluginKindFor(plugin.id, PLUGIN_OVERVIEW)}
                                    <button
                                        class="item nested"
                                        data-kind={overview}
                                        class:open={isOpen(overview)}
                                        onclick={() => workspace.openTab(context.id, overview)}
                                    >
                                        <Icon name="dashboard" size={14} />
                                        <span>Overview</span>
                                    </button>

                                    {#each plugin.views as view (view.id)}
                                        {@const kind = pluginKindFor(plugin.id, view.id)}
                                        <button
                                            class="item nested"
                                            data-kind={kind}
                                            class:open={isOpen(kind)}
                                            onclick={() => workspace.openTab(context.id, kind)}
                                            title={view.namespace ? `${view.label} in ${view.namespace}` : view.label}
                                        >
                                            <Icon name={view.icon} size={14} />
                                            <span>{view.label}</span>
                                        </button>
                                    {/each}
                                {/if}
                            {/each}

                            <!-- Known plugins for what this cluster runs and
                                 this machine has no plugin for: a quiet row
                                 each, opening Settings where the plugin is
                                 described and installed -- nothing is cloned
                                 from the sidebar. The cross hides the
                                 suggestion for good. -->
                            {#each suggested as offer (offer.id)}
                                <div class="suggestion">
                                    <button
                                        class="plugin suggested"
                                        onclick={() => workspace.openPluginSettings()}
                                        title="{offer.name} runs in this cluster, and there is a plugin for it — open Settings to install it"
                                    >
                                        <Icon name="plus" size={11} />
                                        <PluginMark id={offer.id} icon={offer.icon} size={14} />
                                        <span>{offer.name}</span>
                                        <span class="missing get">get plugin</span>
                                    </button>
                                    <button
                                        class="hide"
                                        onclick={() => void workspace.hidePluginSuggestion(offer.id)}
                                        title="Stop suggesting the {offer.name} plugin"
                                        aria-label="Stop suggesting the {offer.name} plugin"
                                    >
                                        <Icon name="close" size={10} />
                                    </button>
                                </div>
                            {/each}
                        {/if}
                    {/if}

                    <!-- The definitions section continues into the cluster: the
                         API groups it serves, each holding its own kinds. -->
                    {#if group.label === DEFINITIONS_GROUP}
                        {@const loaded = workspace.customKindsFor(context.id)}
                        {#if loaded.status === 'loading'}
                            <p class="note">Looking for definitions…</p>
                        {:else if loaded.status === 'error'}
                            <!-- Named rather than generic. "Could not read
                                 definitions" leaves no permission, no network
                                 and none installed looking identical, and they
                                 call for different reactions -- so this uses
                                 the same reading of the error the error pages
                                 do, with the wire text on hover. -->
                            <p class="note failed" title={loaded.message}>
                                {classify(loaded.message).headline}
                            </p>
                        {:else if loaded.status === 'ready' && loaded.groups.length === 0}
                            <p class="note">No custom resources installed</p>
                        {:else}
                            {#each loaded.groups as api (api.group)}
                                {@const open = workspace.isApiGroupExpanded(context.id, api.group)}
                                <button
                                    class="api"
                                    onclick={() => workspace.toggleApiGroup(context.id, api.group)}
                                    aria-expanded={open}
                                    title={api.group}
                                >
                                    <Icon name={open ? 'chevron-down' : 'chevron-right'} size={11} />
                                    <span>{api.group}</span>
                                    <span class="tally">{api.kinds.length}</span>
                                </button>

                                {#if open}
                                    {#each api.kinds as custom (custom.kind)}
                                        <button
                                            class="item nested"
                                            data-kind={custom.kind}
                                            class:open={isOpen(custom.kind)}
                                            onclick={() => workspace.openTab(context.id, custom.kind)}
                                        >
                                            <Icon name="puzzle" size={14} />
                                            <span>{custom.label}</span>
                                        </button>
                                    {/each}
                                {/if}
                            {/each}
                        {/if}
                    {/if}
                {/if}
            {/each}
        </div>
    {/if}
</div>

<style>
    .context {
        --indent: 26px;
    }

    .head {
        display: flex;
        align-items: center;
        height: var(--row-h);
        border-radius: var(--radius-sm);
        position: relative;
    }

    .head:hover {
        background: var(--bg-hover);
    }

    .selected .head {
        background: var(--ctx-tint);
    }

    /* A colour bar on the selected context, matching its tabs. */
    .selected .head::before {
        content: '';
        position: absolute;
        left: 0;
        top: 4px;
        bottom: 4px;
        width: 2px;
        border-radius: 2px;
        background: var(--ctx-color);
    }

    /* The reveal flash. It sits over the row rather than changing its
       background so that it works the same whether the context is selected or
       not, and it is the context's own colour so the row that lights up is
       recognisably the one whose tab was clicked. The global reduced-motion
       rule already collapses this to nothing. */
    .flash::after {
        content: '';
        position: absolute;
        inset: 0;
        border-radius: var(--radius-sm);
        background: var(--ctx-color);
        opacity: 0;
        pointer-events: none;
        animation: reveal-flash 700ms ease-out;
    }

    @keyframes reveal-flash {
        from {
            opacity: 0.4;
        }
        to {
            opacity: 0;
        }
    }

    .twisty {
        display: grid;
        place-items: center;
        width: var(--indent);
        height: 100%;
        color: var(--text-faint);
        flex: 0 0 auto;
    }

    .twisty:hover {
        color: var(--text);
    }

    .label {
        display: flex;
        align-items: center;
        gap: 7px;
        flex: 1 1 auto;
        min-width: 0;
        height: 100%;
        padding-right: 8px;
        text-align: left;
    }

    .swatch {
        width: 9px;
        height: 9px;
        border-radius: 2px;
        flex: 0 0 auto;
        box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.35);
    }

    .name {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        color: var(--text-dim);
    }

    .selected .name {
        color: var(--text);
    }

    .sections {
        display: grid;
        place-items: center;
        width: 18px;
        height: 18px;
        flex: 0 0 auto;
        border-radius: 3px;
        color: var(--text-faint);
    }

    .sections:hover {
        background: var(--bg-active);
        color: var(--text);
    }

    /* Shown on hover and focus only: forty rows each carrying a cross is a
       sidebar that looks like it wants everything gone. */
    .remove {
        display: grid;
        place-items: center;
        width: 18px;
        height: 18px;
        flex: 0 0 auto;
        border-radius: 3px;
        color: var(--text-faint);
        opacity: 0;
    }

    .head:hover .remove,
    .remove:focus-visible {
        opacity: 1;
    }

    .remove.disconnect:hover {
        color: var(--warn);
    }

    .remove:hover {
        background: var(--bg-active);
        color: var(--error);
    }

    .status {
        display: grid;
        place-items: center;
        width: 16px;
        flex: 0 0 auto;
        margin-right: 6px;
    }

    .status.dot::before {
        content: '';
        width: 7px;
        height: 7px;
        border-radius: 50%;
    }

    .status.dot.connected::before {
        /* A faint halo, so green reads as live rather than as another swatch. */
        background: var(--ok);
        box-shadow: 0 0 0 2px color-mix(in srgb, var(--ok) 22%, transparent);
    }

    .status.dot.checking::before {
        background: var(--accent);
        animation: pulse 1.1s ease-in-out infinite;
    }

    @keyframes pulse {
        50% {
            opacity: 0.25;
        }
    }

    /* A triangle rather than a red dot: in a list of twenty clusters a broken
       one has to be findable by shape, not only by colour. */
    .status.broken {
        color: var(--error);
    }

    .badge {
        flex: 0 0 auto;
        font-size: 9px;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--ctx-color);
        border: 1px solid var(--ctx-color);
        border-radius: 3px;
        padding: 0 4px;
        line-height: 14px;
        opacity: 0.85;
    }

    .tree {
        padding: 2px 0 6px;
    }

    /* The heading and the definitions section's refresh sit on one line. */
    .group-row {
        display: flex;
        align-items: center;
        margin: 8px 0 2px;
    }

    .group-row .group {
        margin: 0;
    }

    .group {
        display: flex;
        align-items: center;
        gap: 4px;
        width: 100%;
        margin: 8px 0 2px;
        /* One chevron width less than the items, so the arrow lines up with
           where the group's contents begin rather than indenting past them. */
        padding-left: calc(var(--indent) - 15px);
        padding-right: 8px;
        /* Sized to be read rather than merely noticed: at 10px these were the
           smallest text in the app while being the thing you navigate by. */
        font-size: 11.5px;
        letter-spacing: 0.05em;
        text-transform: uppercase;
        font-weight: 600;
        color: var(--text-dim);
        text-align: left;
        border-radius: var(--radius-sm);
        min-height: 22px;
    }

    .group:hover {
        background: var(--bg-hover);
        color: var(--text-dim);
    }

    .group span:first-of-type {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    /* A group this cluster has been set differently from the rest. Without a
       mark, a sidebar that disagrees with every other cluster looks like a bug
       rather than something the user asked for. */
    .group.local span:first-of-type::after {
        content: '';
        display: inline-block;
        width: 4px;
        height: 4px;
        margin-left: 5px;
        vertical-align: middle;
        border-radius: 50%;
        background: var(--ctx-color);
    }

    /* How much is hidden, so a folded group is not a dead end. */
    .tally {
        margin-left: auto;
        font-size: 9px;
        letter-spacing: 0;
        color: var(--text-faint);
        background: var(--bg-raised);
        border-radius: 7px;
        padding: 0 5px;
        line-height: 13px;
    }

    .item {
        display: flex;
        align-items: center;
        /* So the reveal flash can be drawn over it. */
        position: relative;
        gap: 8px;
        width: 100%;
        height: 26px;
        padding-left: var(--indent);
        padding-right: 8px;
        color: var(--text-dim);
        border-radius: var(--radius-sm);
        text-align: left;
    }

    /* One API group inside the definitions section: a heading, but a level in
       rather than a peer of the section headings, so it is indented and
       lower-key than they are. */
    .reload {
        display: grid;
        place-items: center;
        width: 20px;
        height: 20px;
        flex: 0 0 auto;
        margin-right: 4px;
        border-radius: 3px;
        color: var(--text-faint);
    }

    .reload:hover:not(:disabled) {
        background: var(--bg-active);
        color: var(--text);
    }

    .reload:disabled {
        cursor: default;
    }

    .reload.spinning {
        animation: spin 900ms linear infinite;
        color: var(--accent);
    }

    @keyframes spin {
        to {
            transform: rotate(360deg);
        }
    }

    .api {
        display: flex;
        align-items: center;
        gap: 5px;
        width: 100%;
        min-height: 24px;
        padding-left: var(--indent);
        padding-right: 8px;
        color: var(--text-dim);
        border-radius: var(--radius-sm);
        text-align: left;
        font-family: var(--mono);
        font-size: 11px;
    }

    .api:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .api span:first-of-type {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    /* A definition sits one level in from its API group, and a plugin's view
       one level in from its plugin. */
    .item.nested {
        padding-left: calc(var(--indent) + 16px);
    }

    /* One forward, under the network heading: where it is listening, what it
       reaches, and the one button that stops or restarts it. Indented like a
       nested item, because it belongs to the section rather than being one of
       the section's own rows. */
    .tunnel {
        display: flex;
        align-items: center;
        gap: 2px;
        padding-right: 4px;
    }

    .tunnel-label {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 1 1 auto;
        min-width: 0;
        height: 24px;
        padding-left: calc(var(--indent) + 16px);
        padding-right: 4px;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
        font-size: 11.5px;
        text-align: left;
    }

    .tunnel-label:hover {
        background: var(--bg-hover);
    }

    /* Only a live one is a link; the rest are rows that describe themselves. */
    .tunnel-label.active {
        cursor: pointer;
    }

    .tunnel-label .dot {
        flex: 0 0 auto;
        width: 6px;
        height: 6px;
        border-radius: 50%;
        background: var(--text-faint);
    }

    .tunnel-label.active .dot {
        background: var(--ok);
    }

    .tunnel-label.connecting .dot {
        background: var(--warn);
    }

    .tunnel-label.error .dot {
        background: var(--error);
    }

    .tunnel-label .local {
        flex: 0 0 auto;
        font-family: var(--mono);
        color: var(--text-dim);
    }

    .tunnel-label .target {
        flex: 1 1 auto;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .tunnel-act {
        display: grid;
        place-items: center;
        flex: 0 0 auto;
        width: 20px;
        height: 20px;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .tunnel-act:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    /* One plugin: a heading like an API group, but with the solution's own icon
       and a word about whether this cluster has it. */
    .plugin {
        display: flex;
        align-items: center;
        gap: 6px;
        width: 100%;
        min-height: 26px;
        padding-left: var(--indent);
        padding-right: 8px;
        color: var(--text-dim);
        border-radius: var(--radius-sm);
        text-align: left;
    }

    .plugin:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .plugin span:not(.missing) {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    /* Dimmed rather than hidden or disabled. The plugin is installed on this
       machine and its rows are real; it is this cluster that has nothing to
       show, and the overview is where that gets explained. Hiding the row
       instead would leave someone who installed a plugin wondering where it
       went. */
    .plugin.absent {
        color: var(--text-faint);
    }

    /* Named for what it says rather than for the row's state, which the row
       itself already carries as .absent -- the two sharing a class meant the
       badge's background was drawn across the whole row. */
    .missing {
        margin-left: auto;
        flex: 0 0 auto;
        font-size: 9px;
        letter-spacing: 0.02em;
        color: var(--text-faint);
        background: var(--bg-raised);
        border-radius: 7px;
        padding: 0 5px;
        line-height: 13px;
    }

    .note {
        margin: 2px 0 4px;
        padding-left: var(--indent);
        font-size: 11px;
        color: var(--text-faint);
    }

    /* A plugin this machine could have for what the cluster runs: drawn like
       a plugin row, but faint and with the accent on its badge, so it reads as
       an offer rather than as something installed. */
    .suggestion {
        position: relative;
        display: flex;
        align-items: center;
    }

    .plugin.suggested {
        color: var(--text-faint);
        padding-right: 28px;
    }

    .plugin.suggested:hover {
        color: var(--text);
    }

    .missing.get {
        color: var(--accent);
        background: color-mix(in srgb, var(--accent) 14%, transparent);
    }

    .suggestion .hide {
        position: absolute;
        right: 6px;
        display: grid;
        place-items: center;
        width: 16px;
        height: 16px;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
        opacity: 0;
    }

    .suggestion:hover .hide,
    .suggestion .hide:focus-visible {
        opacity: 1;
    }

    .suggestion .hide:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .note.failed {
        color: var(--error);
    }

    .item:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .item.open {
        background: var(--ctx-tint);
        color: var(--text);
    }

    .item span {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
</style>
