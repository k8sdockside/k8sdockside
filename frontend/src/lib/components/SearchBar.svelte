<!--
  The search box in the title bar, and the panel it drops.

  It finds objects by name across every kind each cluster serves -- see
  SearchService -- and shows them as they are found, with the clusters still
  going listed above the results. By default it looks in the contexts already
  open: one showing a tab, answering in the sidebar, or selected. "All
  contexts" reaches the rest too, connecting to each as it goes, which for a
  kubeconfig of exec-plugin clusters means running every one's credential
  plugin -- which is why it is a choice and not the default.

  A hit opens in its own kind's list, narrowed to its name, with its report in
  the details panel. Where an enabled plugin shows that kind -- a table of
  Argo CD Applications, or the application board, which can be opened on one --
  the hit carries a button for that as well.
-->
<script lang="ts">
    import { tick, untrack } from 'svelte';
    import Icon from './Icon.svelte';
    import { labelFor } from '../catalogue';
    import { detail } from '../state/detail.svelte';
    import { clusters } from '../state/health.svelte';
    import { search, type ClusterProgress, type Scope, type SearchHit } from '../state/search.svelte';
    import { views } from '../state/views';
    import { resourceTabId, workspace } from '../state/workspace.svelte';
    import { pluginFocus } from '../plugins/focus.svelte';
    import { groupHits, hitKey, pluginLinksFor, searchable, type PluginLink } from '../search/results';

    /** The kinds offered as options before the plugins add theirs. */
    const COMMON_KINDS = [
        'pods',
        'deployments',
        'statefulsets',
        'daemonsets',
        'jobs',
        'cronjobs',
        'services',
        'ingresses',
        'configmaps',
        'secrets',
        'persistentvolumeclaims',
        'namespaces',
        'nodes',
    ];

    const MAC = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent);
    const SHORTCUT = MAC ? '⌘K' : 'Ctrl K';

    /** What the panel shows before anything is typed. Each one can be clicked into the box. */
    const EXAMPLES: { text: string; says: string }[] = [
        { text: 'nginx', says: 'names containing it, in every kind the cluster serves' },
        { text: 'web prod', says: 'every word has to match' },
        { text: 'api-*-worker', says: 'a * is a wildcard, and pins the ends of the name' },
        { text: 'kube-system/core', says: 'a slash matches the namespace as well' },
        { text: 'kind:po,svc web', says: 'only these kinds, by name, plural or short name' },
        { text: 'kind:app', says: 'every Argo CD Application, or anything else called that' },
        { text: 'ns:default web', says: 'one namespace; ns:a,b for several' },
        { text: 'label:app=web', says: 'a label selector, answered by the cluster' },
    ];

    let input = $state<HTMLInputElement | null>(null);
    let list = $state<HTMLElement | null>(null);
    /** The row the arrow keys are on, as an index into `ordered`. */
    let active = $state(0);

    let everyContext = $derived(workspace.orderContexts(workspace.contexts));
    /**
     * The contexts that count as open: selected, showing a tab, or known to
     * answer. Read when a search starts rather than tracked, so a cluster
     * turning green because a search reached it does not start another one.
     */
    let openContexts = $derived.by(() => {
        const withTabs = new Set(workspace.allTabs.map((t) => t.contextId));
        return everyContext.filter(
            (c) =>
                c.id === workspace.selectedContextId || withTabs.has(c.id) || clusters.of(c.id).status === 'connected',
        );
    });
    let contextsById = $derived(new Map(workspace.contexts.map((c) => [c.id, c])));

    /** The kind options: the common built-ins, then what the enabled plugins show. */
    let kindOptions = $derived.by(() => {
        const out: { kind: string; label: string; title: string }[] = COMMON_KINDS.map((kind) => ({
            kind,
            label: labelFor(kind),
            title: kind,
        }));
        const seen = new Set(COMMON_KINDS);
        for (const plugin of workspace.enabledPlugins) {
            for (const view of plugin.views) {
                const kind = view.type === 'custom' ? (view.focus?.kind ?? '') : view.kind;
                if (!kind.startsWith('crd:') || seen.has(kind)) continue;
                seen.add(kind);
                out.push({ kind, label: `${plugin.name} · ${labelFor(kind)}`, title: kind });
            }
        }
        return out;
    });

    let groups = $derived(groupHits(search.hits, everyContext.map((c) => c.id), search.answered));
    let ordered = $derived(groups.flatMap((g) => g.hits));
    /** Whether what is in the box is worth searching for. */
    let typed = $derived(searchable(search.query));

    /** The running totals over every cluster in the search, for the status line. */
    let tally = $derived.by(() => {
        const rows = search.progress;
        const going = rows.filter((r) => r.phase === 'waiting' || r.phase === 'connecting' || r.phase === 'searching');
        return {
            contexts: rows.length,
            finished: rows.length - going.length,
            failed: rows.filter((r) => r.phase === 'error').length,
            done: rows.reduce((n, r) => n + r.done, 0),
            total: rows.reduce((n, r) => n + r.total, 0),
            withHits: rows.filter((r) => r.found > 0).length,
        };
    });

    // Searches as the query settles, and again when an option changes. Only
    // the query and the options are tracked here; which contexts to search is
    // read when the timer fires, for the reason given at openContexts.
    $effect(() => {
        const query = search.query.trim();
        void search.scope;
        void search.kinds.join(',');
        if (!searchable(query)) {
            untrack(() => search.clear());
            return;
        }
        const timer = setTimeout(run, 350);
        return () => clearTimeout(timer);
    });

    // Keeps the arrow-key row on something that exists as hits arrive.
    $effect(() => {
        if (active >= ordered.length && active !== 0) active = 0;
    });

    function targets(scope: Scope): string[] {
        return (scope === 'all' ? everyContext : openContexts).map((c) => c.id);
    }

    function run(): void {
        const query = search.query.trim();
        if (!searchable(query)) return;
        active = 0;
        void search.start(query, targets(search.scope));
    }

    function close(): void {
        search.open = false;
        input?.blur();
    }

    function nameOf(contextId: string): string {
        const context = contextsById.get(contextId);
        return context ? workspace.displayName(context) : contextId;
    }

    /**
     * Opens a hit: in its own kind's list, or in one of a plugin's views.
     *
     * A list is narrowed to the object's name before it is opened, so the row
     * is there to see when the tab comes up, and the report goes in the
     * details panel beside it. A plugin's own page is told the object in its
     * address instead, and shows it its own way.
     */
    function openHit(hit: SearchHit, link: PluginLink | null = null): void {
        if (link?.custom) {
            pluginFocus.request(resourceTabId(hit.contextId, link.tabKind), link.hash);
            workspace.openTab(hit.contextId, link.tabKind);
            close();
            return;
        }
        const tabKind = link?.tabKind ?? hit.kind;
        views.focusName(resourceTabId(hit.contextId, tabKind), hit.name, hit.namespace);
        workspace.openTab(hit.contextId, tabKind);
        void detail.open({ contextId: hit.contextId, kind: hit.kind, namespace: hit.namespace, name: hit.name });
        close();
    }

    function openYaml(hit: SearchHit): void {
        workspace.openEditor({ contextId: hit.contextId, kind: hit.kind, namespace: hit.namespace, name: hit.name });
        close();
    }

    function toggleKind(kind: string): void {
        search.kinds = search.kinds.includes(kind) ? search.kinds.filter((k) => k !== kind) : [...search.kinds, kind];
        input?.focus();
    }

    function setScope(scope: Scope): void {
        search.scope = scope;
        input?.focus();
    }

    function useExample(text: string): void {
        search.query = text;
        input?.focus();
    }

    function move(by: number): void {
        if (ordered.length === 0) return;
        active = (active + by + ordered.length) % ordered.length;
        void tick().then(() => list?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' }));
    }

    function onInputKey(event: KeyboardEvent): void {
        switch (event.key) {
            case 'Escape':
                // Not on to the window, where Escape also closes the details panel.
                event.preventDefault();
                event.stopPropagation();
                close();
                return;
            case 'ArrowDown':
                event.preventDefault();
                search.open = true;
                move(1);
                return;
            case 'ArrowUp':
                event.preventDefault();
                move(-1);
                return;
            case 'Enter': {
                event.preventDefault();
                search.open = true;
                const hit = ordered[active];
                // Enter on results that answer what is in the box opens the one
                // under the arrow keys; otherwise it searches now, without
                // waiting for the pause.
                if (hit && search.answered === search.query.trim()) openHit(hit);
                else run();
                return;
            }
        }
    }

    function onWindowKey(event: KeyboardEvent): void {
        if ((event.metaKey || event.ctrlKey) && (event.key === 'k' || event.key === 'K')) {
            event.preventDefault();
            search.open = true;
            input?.focus();
            input?.select();
        }
    }

    function clearQuery(): void {
        search.query = '';
        input?.focus();
    }

    function phaseText(p: ClusterProgress): string {
        switch (p.phase) {
            case 'waiting':
                return 'queued';
            case 'connecting':
                return 'connecting…';
            case 'searching':
                return p.total > 0 ? `${p.done}/${p.total}` : 'reading kinds…';
            case 'done':
                return `${p.found} found`;
            case 'error':
                return 'failed';
        }
    }

    function phaseTitle(p: ClusterProgress): string {
        if (p.phase === 'error') return p.error;
        const parts = [`${p.done} of ${p.total} kinds searched`];
        if (p.failed > 0) parts.push(`${p.failed} could not be listed — most likely not allowed`);
        return parts.join('; ');
    }

    function plural(n: number, one: string, many: string): string {
        return `${n.toLocaleString()} ${n === 1 ? one : many}`;
    }
</script>

<svelte:window onkeydown={onWindowKey} onclick={() => (search.open = false)} onresize={() => (search.open = false)} />

<!-- The click that opens the panel must not reach the window handler that
     closes it, and neither must a click inside it. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="host" onclick={(e) => e.stopPropagation()}>
    <div class="field" class:focused={search.open}>
        {#if search.running}
            <span class="spinner" role="status" aria-label="Searching"></span>
        {:else}
            <Icon name="search" size={13} />
        {/if}
        <input
            bind:this={input}
            bind:value={search.query}
            type="text"
            role="combobox"
            placeholder="Search every cluster"
            spellcheck="false"
            autocomplete="off"
            aria-label="Search every cluster"
            aria-expanded={search.open}
            aria-controls="search-panel"
            onfocus={() => (search.open = true)}
            onkeydown={onInputKey}
        />
        {#if search.query}
            <button class="clear" onclick={clearQuery} aria-label="Clear the search" title="Clear">
                <Icon name="close" size={10} />
            </button>
        {:else}
            <kbd class="shortcut">{SHORTCUT}</kbd>
        {/if}
    </div>

    {#if search.open}
        <div id="search-panel" class="panel" role="dialog" aria-label="Search">
            <div class="options">
                <div class="row">
                    <span class="label">Search in</span>
                    <div class="segmented">
                        <button aria-pressed={search.scope === 'connected'} onclick={() => setScope('connected')}>
                            Open contexts <span class="n">{openContexts.length}</span>
                        </button>
                        <button aria-pressed={search.scope === 'all'} onclick={() => setScope('all')}>
                            All contexts <span class="n">{everyContext.length}</span>
                        </button>
                    </div>
                    {#if search.scope === 'all'}
                        <span class="aside">Connects to the ones not open yet, running their credential plugins.</span>
                    {/if}
                </div>
                <div class="row kinds">
                    <span class="label">Kinds</span>
                    <div class="chips">
                        <button class="chip" aria-pressed={search.kinds.length === 0} onclick={() => (search.kinds = [])}>
                            All
                        </button>
                        {#each kindOptions as option (option.kind)}
                            <button
                                class="chip"
                                aria-pressed={search.kinds.includes(option.kind)}
                                title={option.title}
                                onclick={() => toggleKind(option.kind)}
                            >
                                {option.label}
                            </button>
                        {/each}
                    </div>
                </div>
            </div>

            {#if search.error}
                <p class="problem"><Icon name="alert" size={12} /> {search.error}</p>
            {:else if search.current && typed}
                <div class="status">
                    <p class="summary">
                        {#if search.running}
                            <span class="spinner small"></span>
                            Searching {tally.finished} of {plural(tally.contexts, 'context', 'contexts')}{#if tally.total > 0}
                                · {tally.done.toLocaleString()} of {tally.total.toLocaleString()} kinds{/if}
                            {#if ordered.length > 0}· {plural(ordered.length, 'hit', 'hits')} so far{/if}
                        {:else if ordered.length > 0}
                            {plural(ordered.length, 'hit', 'hits')} in {plural(tally.withHits, 'context', 'contexts')}
                        {:else}
                            Nothing called “{search.answered}” in {plural(tally.contexts, 'context', 'contexts')}.
                        {/if}
                    </p>
                    {#if search.running}
                        <button class="action" onclick={() => search.cancel()}><Icon name="stop" size={11} /> Stop</button>
                    {:else}
                        <button class="action" onclick={run}><Icon name="refresh" size={11} /> Again</button>
                    {/if}
                </div>

                <ul class="clusters">
                    {#each search.progress as p (p.contextId)}
                        <li class="cluster {p.phase}" title={phaseTitle(p)}>
                            <span class="dot" style:background={workspace.colorOf(p.contextId)}></span>
                            <span class="cluster-name">{nameOf(p.contextId)}</span>
                            <span class="cluster-state">
                                {#if p.phase === 'error'}<Icon name="alert" size={10} />{/if}
                                {phaseText(p)}
                            </span>
                            {#if p.phase === 'searching' && p.total > 0}
                                <span class="bar"><span style:width="{(p.done / p.total) * 100}%"></span></span>
                            {/if}
                        </li>
                    {/each}
                </ul>

                {#if search.truncated}
                    <p class="note">Stopped at {ordered.length.toLocaleString()} hits. Add a word, a <code>kind:</code> or an <code>ns:</code> to narrow it.</p>
                {/if}
            {/if}

            {#if typed && ordered.length > 0}
                <div class="results" bind:this={list}>
                    {#each groups as group (group.contextId)}
                        <section class="group">
                            <p class="group-head">
                                <span class="dot" style:background={workspace.colorOf(group.contextId)}></span>
                                <span>{nameOf(group.contextId)}</span>
                                <span class="count">{group.hits.length}</span>
                            </p>
                            {#each group.hits as hit, i (hitKey(hit))}
                                {@const index = group.offset + i}
                                {@const links = pluginLinksFor(hit, workspace.enabledPlugins)}
                                <div class="hit" data-active={index === active}>
                                    <button class="open" onclick={() => openHit(hit)} onmouseenter={() => (active = index)}>
                                        <span class="kind" title={hit.group ? `${hit.apiKind}.${hit.group}` : hit.apiKind}>
                                            {hit.apiKind}
                                        </span>
                                        <span class="name">
                                            {#if hit.namespace}<span class="ns">{hit.namespace}/</span>{/if}{hit.name}
                                        </span>
                                        <span class="age">{hit.age}</span>
                                    </button>
                                    {#each links as link (link.tabKind)}
                                        <button
                                            class="link"
                                            title="Open in {link.pluginName} › {link.label}"
                                            onclick={() => openHit(hit, link)}
                                        >
                                            <Icon name={link.icon} size={11} />
                                            {link.label}
                                        </button>
                                    {/each}
                                    <button class="link" title="Open the live YAML in the editor" onclick={() => openYaml(hit)}>
                                        <Icon name="edit" size={11} /> YAML
                                    </button>
                                </div>
                            {/each}
                        </section>
                    {/each}
                </div>
            {:else if !typed}
                <section class="tips">
                    <p class="heading">Find anything, in every cluster</p>
                    <ul>
                        {#each EXAMPLES as example (example.text)}
                            <li>
                                <button class="example" onclick={() => useExample(example.text)}><code>{example.text}</code></button>
                                <span>{example.says}</span>
                            </li>
                        {/each}
                    </ul>
                    <p class="keys">
                        <kbd>↑</kbd><kbd>↓</kbd> move · <kbd>Enter</kbd> open · <kbd>Esc</kbd> close · <kbd>{SHORTCUT}</kbd> search from anywhere
                    </p>
                    <p class="aside">
                        Only names, namespaces and labels are read — never what a Secret holds. Events are left out
                        unless you ask for <code>kind:events</code>.
                    </p>
                </section>
            {/if}
        </div>
    {/if}
</div>

<style>
    /* Not positioned: the panel below is placed against the title bar, so it
       can sit on the window's centre line rather than hang off the box. */
    .host {
        display: flex;
        align-items: center;
        flex: 0 1 300px;
        min-width: 0;
        /* The title bar drags the window; a control in it must not. */
        --wails-draggable: no-drag;
    }

    .field {
        position: relative;
        display: flex;
        align-items: center;
        gap: 6px;
        width: 100%;
        height: 26px;
        padding: 0 6px 0 8px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        color: var(--text-faint);
    }

    .field.focused {
        box-shadow: inset 0 0 0 1px var(--accent);
    }

    input {
        flex: 1 1 auto;
        min-width: 0;
        height: 100%;
        border: 0;
        outline: none;
        background: transparent;
        font: inherit;
        font-size: 12px;
        color: var(--text);
    }

    input::placeholder {
        color: var(--text-faint);
    }

    .clear {
        display: grid;
        place-items: center;
        width: 16px;
        height: 16px;
        border-radius: 3px;
        color: var(--text-faint);
    }

    .clear:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    kbd {
        padding: 0 4px;
        border-radius: 3px;
        box-shadow: inset 0 0 0 1px var(--border);
        font-family: inherit;
        font-size: 10px;
        color: var(--text-faint);
        white-space: nowrap;
    }

    .spinner {
        flex: 0 0 auto;
        width: 12px;
        height: 12px;
        border-radius: 50%;
        border: 2px solid color-mix(in srgb, var(--accent) 25%, transparent);
        border-top-color: var(--accent);
        animation: spin 0.8s linear infinite;
    }

    .spinner.small {
        display: inline-block;
        width: 10px;
        height: 10px;
        vertical-align: -1px;
        margin-right: 4px;
    }

    @keyframes spin {
        to {
            transform: rotate(360deg);
        }
    }

    .panel {
        position: absolute;
        top: calc(100% + 4px);
        left: 50%;
        transform: translateX(-50%);
        z-index: 50;
        display: flex;
        flex-direction: column;
        width: min(760px, calc(100% - 24px));
        max-height: 72vh;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: 0 12px 32px rgb(0 0 0 / 0.4);
        overflow: hidden;
        font-size: 12px;
        color: var(--text);
    }

    .options {
        display: flex;
        flex-direction: column;
        gap: 6px;
        padding: 8px 10px;
        border-bottom: 1px solid var(--border-soft);
        background: var(--bg-panel);
    }

    .row {
        display: flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
    }

    .row.kinds {
        align-items: flex-start;
    }

    .label {
        flex: 0 0 64px;
        padding-top: 2px;
        font-size: 10px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .segmented {
        display: inline-flex;
        padding: 2px;
        border-radius: var(--radius-sm);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .segmented button {
        padding: 3px 9px;
        border-radius: 3px;
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .segmented button[aria-pressed='true'] {
        background: var(--bg-hover);
        color: var(--text);
    }

    .n {
        margin-left: 3px;
        color: var(--text-faint);
    }

    .aside {
        min-width: 0;
        font-size: 11px;
        color: var(--text-faint);
    }

    .chips {
        display: flex;
        flex-wrap: wrap;
        gap: 4px;
        min-width: 0;
    }

    .chip {
        padding: 2px 8px;
        border-radius: 999px;
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .chip:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .chip[aria-pressed='true'] {
        background: color-mix(in srgb, var(--accent) 18%, transparent);
        box-shadow: inset 0 0 0 1px var(--accent);
        color: var(--text);
    }

    .problem {
        display: flex;
        align-items: center;
        gap: 6px;
        margin: 0;
        padding: 10px 12px;
        color: var(--error);
    }

    .status {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
        padding: 8px 10px 4px 12px;
    }

    .summary {
        margin: 0;
        min-width: 0;
        color: var(--text-dim);
    }

    .action {
        display: flex;
        align-items: center;
        gap: 5px;
        flex: 0 0 auto;
        padding: 3px 9px;
        border-radius: var(--radius-sm);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11px;
        color: var(--text-dim);
    }

    .action:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .clusters {
        display: flex;
        flex-wrap: wrap;
        gap: 4px;
        margin: 0;
        padding: 2px 10px 8px 12px;
        list-style: none;
        border-bottom: 1px solid var(--border-soft);
    }

    .cluster {
        position: relative;
        display: flex;
        align-items: center;
        gap: 5px;
        max-width: 240px;
        padding: 2px 7px 3px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        font-size: 11px;
        overflow: hidden;
    }

    .cluster-name {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .cluster-state {
        display: inline-flex;
        align-items: center;
        gap: 3px;
        flex: 0 0 auto;
        color: var(--text-faint);
        font-variant-numeric: tabular-nums;
    }

    .cluster.error .cluster-state {
        color: var(--error);
    }

    .bar {
        position: absolute;
        left: 0;
        right: 0;
        bottom: 0;
        height: 2px;
        background: var(--border-soft);
    }

    .bar span {
        display: block;
        height: 100%;
        background: var(--accent);
        transition: width 0.15s linear;
    }

    .dot {
        flex: 0 0 auto;
        width: 7px;
        height: 7px;
        border-radius: 2px;
    }

    .note {
        margin: 0;
        padding: 6px 12px;
        border-bottom: 1px solid var(--border-soft);
        font-size: 11px;
        color: var(--text-dim);
    }

    code {
        font-family: var(--mono);
        font-size: 11px;
    }

    .results {
        flex: 1 1 auto;
        min-height: 0;
        overflow-y: auto;
        padding: 4px 0 6px;
    }

    .group-head {
        position: sticky;
        top: 0;
        display: flex;
        align-items: center;
        gap: 6px;
        margin: 0;
        padding: 6px 12px 4px;
        background: var(--bg-raised);
        font-size: 11px;
        font-weight: 600;
        color: var(--text-dim);
    }

    .count {
        font-weight: 400;
        color: var(--text-faint);
    }

    .hit {
        display: flex;
        align-items: center;
        gap: 4px;
        margin: 0 6px;
        padding-right: 4px;
        border-radius: var(--radius-sm);
    }

    .hit[data-active='true'] {
        background: var(--bg-hover);
    }

    .open {
        display: flex;
        align-items: baseline;
        gap: 8px;
        flex: 1 1 auto;
        min-width: 0;
        padding: 4px 6px;
        text-align: left;
        color: var(--text);
    }

    .kind {
        flex: 0 0 auto;
        min-width: 86px;
        font-size: 10.5px;
        color: var(--text-faint);
    }

    .name {
        flex: 1 1 auto;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-family: var(--mono);
        font-size: 11.5px;
    }

    .ns {
        color: var(--text-faint);
    }

    .age {
        flex: 0 0 auto;
        font-size: 10.5px;
        color: var(--text-faint);
        font-variant-numeric: tabular-nums;
    }

    .link {
        display: flex;
        align-items: center;
        gap: 4px;
        flex: 0 0 auto;
        padding: 2px 7px;
        border-radius: var(--radius-sm);
        font-size: 10.5px;
        color: var(--text-faint);
        white-space: nowrap;
    }

    .hit:not([data-active='true']) .link {
        visibility: hidden;
    }

    .link:hover {
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
    }

    .tips {
        padding: 10px 14px 12px;
        overflow-y: auto;
    }

    .heading {
        margin: 0 0 6px;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .tips ul {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: 3px 12px;
        margin: 0 0 10px;
        padding: 0;
        list-style: none;
    }

    .tips li {
        display: contents;
    }

    .tips li span {
        align-self: center;
        color: var(--text-dim);
    }

    .example {
        justify-self: start;
        padding: 2px 6px;
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        color: var(--text);
    }

    .example:hover {
        box-shadow: inset 0 0 0 1px var(--accent);
    }

    .keys {
        margin: 0 0 6px;
        color: var(--text-dim);
    }

    .keys kbd {
        margin-right: 2px;
    }

    .tips .aside {
        margin: 0;
    }
</style>
