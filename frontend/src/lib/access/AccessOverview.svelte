<!--
  The access overview: who may do what in one cluster.

  Three ways in, because there are three questions people bring to RBAC:

    - Map: "show me how it fits together" -- every subject, binding and role as
      a graph, with the chain through any one of them lit on hover, and what it
      amounts to in words beside it.
    - Who can…: "who is able to do this?" -- answered from the roles and
      bindings, with the route each answer takes.
    - My access: "why can't I…?" -- asked of the API server for the person
      using the app, which works even when they may read no roles at all.

  Read once when the tab opens rather than watched: RBAC changes rarely, five
  informers for a page that is looked at is poor value, and the Refresh button
  is honest about it.
-->
<script lang="ts" module>
    import { DEFAULT_FILTER, type GraphFilter } from './model';

    type Mode = 'map' | 'whocan' | 'mine';

    /**
     * How each cluster's overview was left, for the session. The pane rebuilds
     * the view every time its tab comes forward, and a filter that resets on
     * every glance at another tab is a filter nobody will use.
     */
    const remembered = new Map<string, { mode: Mode; filter: GraphFilter; selected: string | null }>();

    /**
     * How wide the detail panel was dragged, for the session and whichever
     * cluster is looked at: 0 is the stylesheet's own width.
     */
    let keptSideWidth = 0;
</script>

<script lang="ts">
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import ErrorState from '../components/ErrorState.svelte';
    import Icon from '../components/Icon.svelte';
    import { clusters } from '../state/health.svelte';
    import { workspace } from '../state/workspace.svelte';
    import AccessGraph from './AccessGraph.svelte';
    import MyAccess from './MyAccess.svelte';
    import NodeDetail from './NodeDetail.svelte';
    import WhoCan from './WhoCan.svelte';
    import {
        adoptAccess,
        assessRules,
        buildGraph,
        index,
        isSystem,
        roleKey,
        roleKeyOf,
        subjectKey,
        type Access,
    } from './model';

    interface Props {
        contextId: string;
    }

    let { contextId }: Props = $props();

    // svelte-ignore state_referenced_locally
    const saved = remembered.get(contextId);
    let mode = $state<Mode>(saved?.mode ?? 'map');
    let filter = $state<GraphFilter>({ ...(saved?.filter ?? DEFAULT_FILTER) });
    let selected = $state<string | null>(saved?.selected ?? null);

    $effect(() => {
        remembered.set(contextId, { mode, filter: { ...filter }, selected });
    });

    // ----- the detail panel's width ------------------------------------------

    /** The narrowest the detail panel goes, and what it always leaves the graph. */
    const MIN_SIDE = 280;
    const GRAPH_ROOM = 320;

    let mapEl = $state<HTMLElement | null>(null);
    let sideEl = $state<HTMLElement | null>(null);
    /** 0 is the stylesheet's own width. */
    let sideWidth = $state(keptSideWidth);
    let sideDrag = $state<{ x: number; from: number } | null>(null);

    $effect(() => {
        keptSideWidth = sideWidth;
    });

    function fitSide(px: number): number {
        const room = (mapEl?.clientWidth ?? window.innerWidth) - GRAPH_ROOM;
        return Math.round(Math.max(MIN_SIDE, Math.min(px, room)));
    }

    function startSideResize(event: PointerEvent): void {
        if (event.button !== 0 || !sideEl) return;
        event.preventDefault();
        sideDrag = { x: event.clientX, from: sideEl.getBoundingClientRect().width };
        (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    }

    function onSideResize(event: PointerEvent): void {
        // The panel is on the right: it grows as the pointer goes left.
        if (sideDrag) sideWidth = fitSide(sideDrag.from + sideDrag.x - event.clientX);
    }

    function endSideResize(event: PointerEvent): void {
        if (!sideDrag) return;
        sideDrag = null;
        (event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId);
    }

    /** Arrow keys resize the panel for anyone not using a pointer. */
    function onSideHandleKey(event: KeyboardEvent): void {
        const step = event.shiftKey ? 48 : 16;
        const now = sideWidth || sideEl?.getBoundingClientRect().width || 400;
        if (event.key === 'ArrowLeft') sideWidth = fitSide(now + step);
        else if (event.key === 'ArrowRight') sideWidth = fitSide(now - step);
        else return;
        event.preventDefault();
    }

    let access = $state<Access | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(true);
    let attempt = $state(0);

    let color = $derived(workspace.colorOf(contextId));
    let context = $derived(workspace.contexts.find((c) => c.id === contextId) ?? null);

    $effect(() => {
        const id = contextId;
        attempt;
        let cancelled = false;
        loading = true;
        error = null;

        ResourceService.Access(id)
            .then((raw) => {
                if (cancelled) return;
                access = adoptAccess(raw);
                clusters.report(id, 'connected');
            })
            .catch((err: unknown) => {
                if (cancelled) return;
                error = err instanceof Error ? err.message : String(err);
            })
            .finally(() => {
                if (!cancelled) loading = false;
            });

        return () => {
            cancelled = true;
        };
    });

    let idx = $derived(access ? index(access) : null);
    let graph = $derived(access && idx ? buildGraph(access, idx, filter) : null);

    let namespaces = $derived.by(() => {
        if (!access) return [];
        const out = new Set<string>();
        for (const r of access.roles) if (r.namespace) out.add(r.namespace);
        for (const b of access.bindings) if (b.namespace) out.add(b.namespace);
        for (const a of access.serviceAccounts) out.add(a.namespace);
        return [...out].sort();
    });

    /** The headline numbers, over what a person set up rather than the cluster's own plumbing. */
    let stats = $derived.by(() => {
        if (!access || !idx) return null;
        const byRole = idx.bindingsByRole;
        const roles = idx.roles;
        const own = access.bindings.filter((b) => !isSystem(b));
        const subjects = new Set(own.flatMap((b) => b.subjects.map(subjectKey)));
        const risky = access.roles.filter((r) => {
            if (isSystem(r) || !byRole.has(roleKey(r))) return false;
            const level = assessRules(r.rules).level;
            return level === 'critical' || level === 'high';
        }).length;
        const dangling = own.filter((b) => !roles.has(roleKeyOf(b))).length;
        const admins = access.bindings
            .filter((b) => b.kind === 'ClusterRoleBinding' && b.roleKind === 'ClusterRole' && b.roleName === 'cluster-admin')
            .flatMap((b) => b.subjects).length;
        return {
            subjects: subjects.size,
            bindings: own.length,
            roles: access.roles.filter((r) => !isSystem(r)).length,
            risky,
            dangling,
            admins,
        };
    });

    /** Jump from anywhere to a node on the map, making sure the filters let it be seen. */
    function show(id: string): void {
        mode = 'map';
        selected = id;
        filter.query = '';
        filter.onlyMe = false;
        if (id.includes('system:')) filter.showSystem = true;
    }

    function showRiskyRoles(): void {
        mode = 'map';
        filter = { ...DEFAULT_FILTER, namespace: filter.namespace, riskyOnly: true };
        selected = null;
    }

    function showClusterAdmins(): void {
        mode = 'map';
        filter = { ...DEFAULT_FILTER, query: 'cluster-admin', showSystem: true };
        selected = 'r:ClusterRole//cluster-admin';
    }
</script>

<div class="access" style:--ctx-color={color}>
    {#if loading && !access}
        <p class="status">Reading roles and bindings…</p>
    {:else if error && !access}
        <ErrorState message={error} {context} onRetry={() => attempt++} />
    {:else if access && idx && graph && stats}
        <header class="head">
            <div class="title">
                <h1>Access</h1>
                <button class="refresh" onclick={() => attempt++} title="Read the roles and bindings again" disabled={loading}>
                    <Icon name="refresh" size={14} />
                    {loading ? 'Refreshing…' : 'Refresh'}
                </button>
            </div>
            <p class="lede">
                Who may do what in {context ? workspace.displayName(context) : 'this cluster'}.
                {#if access.me.username}
                    You are signed in as <strong class="selectable">{access.me.username}</strong>.
                {/if}
            </p>

            <details class="primer">
                <summary>How Kubernetes permissions work</summary>
                <div class="flow">
                    <div class="step">
                        <Icon name="users" size={16} />
                        <strong>Subject</strong>
                        <span>A <em>user</em>, a <em>group</em>, or a <em>service account</em> (the identity pods run as).</span>
                    </div>
                    <span class="arrow">→</span>
                    <div class="step">
                        <Icon name="link" size={16} />
                        <strong>Binding</strong>
                        <span>
                            Hands a role to subjects. A <em>RoleBinding</em> grants inside one namespace; a
                            <em>ClusterRoleBinding</em> grants everywhere.
                        </span>
                    </div>
                    <span class="arrow">→</span>
                    <div class="step">
                        <Icon name="policy" size={16} />
                        <strong>Role</strong>
                        <span>
                            A list of rules: <em>verbs</em> (get, list, create, delete…) on <em>resources</em> (pods,
                            secrets…). A <em>Role</em> lives in a namespace; a <em>ClusterRole</em> can be reused anywhere.
                        </span>
                    </div>
                </div>
                <p class="fine">
                    Permissions only ever add up — there is no "deny". If nothing grants something, it is not allowed.
                </p>
            </details>
        </header>

        {#if access.unreadable.length}
            <div class="partial">
                <Icon name="alert" size={14} />
                <div>
                    <strong>Part of the picture is missing.</strong> The cluster would not let you read:
                    <ul>
                        {#each access.unreadable as u (u)}<li class="selectable">{u}</li>{/each}
                    </ul>
                    <button class="linkish" onclick={() => (mode = 'mine')}>See what you can do →</button>
                </div>
            </div>
        {/if}

        <section class="stats">
            <div class="stat"><p class="value">{stats.subjects}</p><p class="label">Subjects granted something</p></div>
            <div class="stat"><p class="value">{stats.bindings}</p><p class="label">Bindings</p></div>
            <div class="stat"><p class="value">{stats.roles}</p><p class="label">Roles</p></div>
            <button class="stat" class:alert={stats.admins > 0} onclick={showClusterAdmins} title="Show who holds cluster-admin">
                <p class="value">{stats.admins}</p>
                <p class="label">Cluster-admin holders</p>
            </button>
            <button class="stat" class:warn={stats.risky > 0} onclick={showRiskyRoles} title="Show only the bindings to high-risk roles">
                <p class="value">{stats.risky}</p>
                <p class="label">High-risk roles in use</p>
            </button>
            {#if stats.dangling > 0}
                <div class="stat warn"><p class="value">{stats.dangling}</p><p class="label">Bindings to missing roles</p></div>
            {/if}
        </section>

        <nav class="modes" aria-label="Access views">
            <button class:on={mode === 'map'} onclick={() => (mode = 'map')}><Icon name="graph" size={14} /> Map</button>
            <button class:on={mode === 'whocan'} onclick={() => (mode = 'whocan')}><Icon name="search" size={14} /> Who can…?</button>
            <button class:on={mode === 'mine'} onclick={() => (mode = 'mine')}><Icon name="user" size={14} /> My access</button>
        </nav>

        {#if mode === 'map'}
            <div class="filters">
                <select bind:value={filter.namespace} aria-label="Namespace">
                    <option value="">All namespaces</option>
                    {#each namespaces as ns (ns)}<option value={ns}>{ns}</option>{/each}
                </select>
                <input type="search" placeholder="Find a user, group, account, binding or role…" bind:value={filter.query} />
                <label><input type="checkbox" bind:checked={filter.showSystem} /> System objects</label>
                <label title="Only bindings to roles that can read Secrets, run code, or change permissions">
                    <input type="checkbox" bind:checked={filter.riskyOnly} /> Risky only
                </label>
                <label title="Roles that no binding names: defined, but granting nothing">
                    <input type="checkbox" bind:checked={filter.showUnbound} /> Unused roles
                </label>
                {#if access.me.username}
                    <label><input type="checkbox" bind:checked={filter.onlyMe} /> Only mine</label>
                {/if}
                {#if filter.namespace}
                    <label title="ClusterRoleBindings apply in every namespace, this one included">
                        <input type="checkbox" bind:checked={filter.hideClusterWide} /> Hide cluster-wide
                    </label>
                {/if}
            </div>

            <div class="legend">
                <span><i class="sw none"></i> Read-only</span>
                <span><i class="sw low"></i> Can change things</span>
                <span><i class="sw high"></i> High risk</span>
                <span><i class="sw critical"></i> Critical</span>
                <span><i class="sw me"></i> You</span>
                <span><i class="sw missing"></i> Missing</span>
                <span class="muted">Point at anything to trace it; click to pin and explain.</span>
            </div>

            <div class="map" class:with-detail={selected !== null} bind:this={mapEl}>
                <div class="canvas">
                    {#if graph.bindings.length === 0 && graph.roles.length === 0}
                        <div class="empty">
                            <p>Nothing to draw with these filters.</p>
                            {#if !filter.showSystem}
                                <button class="linkish" onclick={() => (filter.showSystem = true)}>
                                    Show the cluster's own system objects
                                </button>
                            {/if}
                        </div>
                    {:else}
                        <AccessGraph {graph} {selected} onselect={(id) => (selected = id)} />
                    {/if}
                </div>
                {#if selected}
                    <div class="side" bind:this={sideEl} style:--side-w={sideWidth ? `${sideWidth}px` : null}>
                        <!-- A focusable separator is the ARIA "window splitter" pattern, as
                             on the panes; the a11y rules only key off the role, which they
                             treat as static. -->
                        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                        <div
                            class="handle"
                            class:active={sideDrag !== null}
                            role="separator"
                            aria-label="Resize the detail panel"
                            aria-orientation="vertical"
                            aria-valuemin={MIN_SIDE}
                            tabindex="0"
                            title="Drag to resize · double-click to reset"
                            onpointerdown={startSideResize}
                            onpointermove={onSideResize}
                            onpointerup={endSideResize}
                            onpointercancel={endSideResize}
                            ondblclick={() => (sideWidth = 0)}
                            onkeydown={onSideHandleKey}
                        ></div>
                        <NodeDetail {contextId} {access} {idx} id={selected} onselect={(id) => (selected = id)} />
                    </div>
                {/if}
            </div>
        {:else if mode === 'whocan'}
            <WhoCan {access} {idx} {namespaces} onshow={show} />
        {:else}
            <MyAccess {contextId} me={access.me} {namespaces} initial={filter.namespace} />
        {/if}
    {/if}
</div>

<style>
    .access {
        height: 100%;
        overflow: auto;
        padding: 20px 24px 32px;
    }

    .status {
        color: var(--text-dim);
        padding: 24px 0;
    }

    .head {
        border-left: 3px solid var(--ctx-color);
        padding-left: 14px;
        margin-bottom: 16px;
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    h1 {
        margin: 0;
        font-size: 20px;
        font-weight: 600;
    }

    .refresh {
        display: inline-flex;
        align-items: center;
        gap: 5px;
        padding: 3px 9px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .refresh:hover:not(:disabled) {
        border-color: var(--accent);
        color: var(--text);
    }

    .lede {
        margin: 6px 0 8px;
        color: var(--text-dim);
    }

    .primer summary {
        cursor: pointer;
        color: var(--accent);
        font-size: 12px;
        width: max-content;
    }

    .flow {
        display: flex;
        align-items: stretch;
        gap: 8px;
        margin-top: 10px;
        flex-wrap: wrap;
    }

    .step {
        flex: 1 1 200px;
        display: flex;
        flex-direction: column;
        gap: 4px;
        padding: 10px 12px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-panel);
        font-size: 12px;
        color: var(--text-dim);
        line-height: 1.5;
    }

    .step strong {
        color: var(--text);
        font-size: 13px;
    }

    .step em {
        color: var(--text);
        font-style: normal;
        font-weight: 600;
    }

    .arrow {
        align-self: center;
        color: var(--text-faint);
        font-size: 18px;
    }

    .fine {
        margin: 8px 0 0;
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .partial {
        display: flex;
        gap: 10px;
        padding: 10px 14px;
        margin-bottom: 16px;
        border-radius: var(--radius);
        background: color-mix(in srgb, var(--warn) 12%, transparent);
        border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent);
        font-size: 12px;
        color: var(--text);
    }

    .partial ul {
        margin: 4px 0 6px;
        padding-left: 18px;
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-dim);
    }

    .linkish {
        color: var(--accent);
        font-size: 12px;
    }

    .linkish:hover {
        text-decoration: underline;
    }

    .stats {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
        gap: 10px;
        margin-bottom: 18px;
    }

    .stat {
        background: var(--bg-panel);
        border: 1px solid var(--border);
        border-radius: var(--radius);
        padding: 12px 14px;
        text-align: left;
    }

    button.stat:hover {
        border-color: var(--ctx-color, var(--accent));
        background: var(--bg-hover);
    }

    .value {
        margin: 0;
        font-size: 22px;
        font-weight: 600;
        line-height: 1.1;
        font-variant-numeric: tabular-nums;
    }

    .stat.warn .value {
        color: var(--warn);
    }

    .stat.alert .value {
        color: var(--error);
    }

    .label {
        margin: 4px 0 0;
        font-size: 12px;
        color: var(--text-dim);
    }

    .modes {
        display: flex;
        gap: 2px;
        margin-bottom: 14px;
        border-bottom: 1px solid var(--border);
    }

    .modes button {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 7px 14px;
        font-size: 12.5px;
        color: var(--text-dim);
        border-bottom: 2px solid transparent;
        margin-bottom: -1px;
    }

    .modes button:hover {
        color: var(--text);
    }

    .modes button.on {
        color: var(--text);
        border-bottom-color: var(--ctx-color, var(--accent));
    }

    .filters {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 10px 14px;
        margin-bottom: 8px;
        font-size: 12px;
    }

    .filters input[type='search'] {
        width: 300px;
    }

    .filters label {
        display: inline-flex;
        align-items: center;
        gap: 5px;
        color: var(--text-dim);
        cursor: pointer;
    }

    select {
        font: inherit;
        color: var(--text);
        background: var(--bg);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        padding: 4px 6px;
    }

    .legend {
        display: flex;
        flex-wrap: wrap;
        gap: 6px 14px;
        margin-bottom: 14px;
        font-size: 11px;
        color: var(--text-dim);
    }

    .legend span {
        display: inline-flex;
        align-items: center;
        gap: 5px;
    }

    .sw {
        display: inline-block;
        width: 14px;
        height: 10px;
        border: 1px solid var(--border);
        border-left-width: 3px;
        border-radius: 2px;
        background: var(--bg-panel);
    }

    .sw.low {
        border-left-color: var(--accent);
    }

    .sw.high {
        border-left-color: var(--warn);
    }

    .sw.critical {
        border-left-color: var(--error);
    }

    .sw.me {
        background: color-mix(in srgb, var(--ok) 30%, var(--bg-panel));
    }

    .sw.missing {
        border-style: dashed;
    }

    .muted {
        color: var(--text-faint);
    }

    .map {
        display: flex;
        gap: 16px;
        align-items: flex-start;
    }

    .canvas {
        flex: 1 1 auto;
        min-width: 0;
    }

    .side {
        flex: 0 0 var(--side-w, 400px);
        /* Dragged wider, it may take all but the graph's share of the row;
           never less than it always had. */
        max-width: max(45%, calc(100% - 320px));
        position: sticky;
        top: 0;
        max-height: calc(100vh - 140px);
        display: flex;
        flex-direction: column;
    }

    /* The grab strip sits in the gap beside the panel, so the whole edge is
       grabbable rather than the one pixel of its border. */
    .handle {
        position: absolute;
        z-index: 2;
        top: 0;
        bottom: 0;
        left: -12px;
        width: 8px;
        border-radius: 4px;
        cursor: col-resize;
        touch-action: none;
        background: transparent;
        transition: background 120ms ease;
    }

    .handle:hover,
    .handle.active,
    .handle:focus-visible {
        background: var(--accent);
    }

    .empty {
        padding: 24px;
        border: 1px dashed var(--border);
        border-radius: var(--radius);
        color: var(--text-dim);
        text-align: center;
    }

    .empty p {
        margin: 0 0 6px;
    }
</style>
