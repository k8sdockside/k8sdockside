<!-- The overview tab: what the cluster is, how much of it is healthy, and what has gone wrong lately. -->
<script lang="ts">
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import { adoptOverview, type Overview } from '../state/adopt';
    import { workspace } from '../state/workspace.svelte';
    import { clusters } from '../state/health.svelte';
    import MetricsPanel from '../charts/MetricsPanel.svelte';
    import ResourceBudget from '../budget/ResourceBudget.svelte';
    import ErrorState from './ErrorState.svelte';
    import SortableTable from './SortableTable.svelte';
    import { detail } from '../state/detail.svelte';

    interface Props {
        contextId: string;
    }

    let { contextId }: Props = $props();

    let overview = $state<Overview | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(true);
    /** Bumped by the retry button; the loading effect reads it as a dependency. */
    let attempt = $state(0);

    let color = $derived(workspace.colorOf(contextId));
    let context = $derived(workspace.contexts.find((c) => c.id === contextId) ?? null);

    $effect(() => {
        const id = contextId;
        attempt;
        let cancelled = false;
        loading = true;
        error = null;

        ResourceService.Overview(id)
            .then((result) => {
                if (cancelled) return;
                overview = adoptOverview(result);
                // A dashboard that loaded is better evidence than any ping, so
                // it settles the sidebar indicator for this context.
                clusters.report(id, 'connected');
            })
            .catch((err: unknown) => {
                if (cancelled) return;
                error = err instanceof Error ? err.message : String(err);
                clusters.report(id, 'error', error);
            })
            .finally(() => {
                if (!cancelled) loading = false;
            });

        return () => {
            cancelled = true;
        };
    });

    function statTone(stat: kube.Stat): string {
        if (stat.total === 0) return 'muted';
        return stat.ready === stat.total ? 'ok' : 'warn';
    }
</script>

<div class="dashboard" style:--ctx-color={color}>
    {#if loading && !overview}
        <p class="status">Loading cluster overview…</p>
    {:else if error}
        <ErrorState message={error} {context} onRetry={() => attempt++} />
    {:else if overview}
        <header class="head" style:--ctx-color={color}>
            <h1>{overview.context}</h1>
            <dl>
                {#if overview.server}<div><dt>Server</dt><dd class="selectable">{overview.server}</dd></div>{/if}
                <div><dt>Version</dt><dd>{overview.version}</dd></div>
                <div><dt>Distribution</dt><dd>{overview.distribution}</dd></div>
            </dl>
        </header>

        <!-- Each counter is the way into the list it counts: the number says
             something is not ready, and the list is where to find out what. -->
        <section class="stats">
            {#each overview.stats as stat (stat.label)}
                <button
                    class="stat {statTone(stat)}"
                    onclick={() => workspace.openTab(contextId, stat.kind)}
                    title="Open {stat.label}"
                >
                    <p class="value">
                        {stat.ready}<span class="of">/{stat.total}</span>
                    </p>
                    <p class="label">{stat.label}</p>
                </button>
            {/each}
        </section>

        <!-- Capacity, allocatable, requests, limits and live usage, all
             against each other. Reads the API server for everything but the
             last, so it works on a cluster with no monitoring at all. -->
        <ResourceBudget {contextId} scope="cluster" title="Resources" />

        <!-- Between the counters and the events: the counters say what the
             cluster is made of, the charts say what it has been doing, and the
             events say what went wrong. That is the order someone reads them
             in. -->
        <MetricsPanel {contextId} attach="dashboard" title="Metrics" />

        <section class="events">
            <h2>
                <button class="link" onclick={() => workspace.openTab(contextId, 'events')} title="Open every event">
                    Recent events
                </button>
            </h2>
            {#if overview.events.error}
                <p class="status quiet">{overview.events.error}</p>
            {:else}
                <div class="events-table">
                    <!-- A row opens the event's own report, as a row in the
                         Events list does. -->
                    <SortableTable
                        columns={overview.events.columns}
                        rows={overview.events.rows}
                        empty="Nothing to report."
                        onselect={(row) =>
                            void detail.open({ contextId, kind: 'events', namespace: row.namespace, name: row.name })}
                    />
                </div>
            {/if}
        </section>
    {/if}
</div>

<style>
    .dashboard {
        height: 100%;
        overflow: auto;
        padding: 20px 24px 32px;
    }

    .status {
        display: flex;
        align-items: center;
        gap: 8px;
        color: var(--text-dim);
        padding: 24px 0;
    }

    .status.quiet {
        padding: 8px 0;
        font-size: 12px;
    }

    .head {
        border-left: 3px solid var(--ctx-color);
        padding-left: 14px;
        margin-bottom: 22px;
    }

    h1 {
        margin: 0 0 8px;
        font-size: 20px;
        font-weight: 600;
    }

    h2 {
        margin: 0 0 10px;
        font-size: 11px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
        font-weight: 600;
    }

    .head dl {
        display: flex;
        flex-wrap: wrap;
        gap: 4px 22px;
        margin: 0;
        font-size: 12px;
    }

    .head dl > div {
        display: flex;
        gap: 7px;
    }

    dt {
        color: var(--text-faint);
    }

    dd {
        margin: 0;
        color: var(--text-dim);
        font-family: var(--mono);
        font-size: 11.5px;
    }

    .stats {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
        gap: 10px;
        margin-bottom: 26px;
    }

    .stat {
        background: var(--bg-panel);
        border: 1px solid var(--border);
        border-radius: var(--radius);
        padding: 14px 16px;
        text-align: left;
        cursor: pointer;
        font: inherit;
        color: inherit;
    }

    .stat:hover {
        border-color: var(--ctx-color, var(--accent));
        background: var(--bg-hover);
    }

    /* The heading is the link into the full list, kept looking like the
       heading it is until pointed at. */
    h2 .link {
        font: inherit;
        letter-spacing: inherit;
        text-transform: inherit;
        color: inherit;
        padding: 0;
        cursor: pointer;
    }

    h2 .link:hover {
        color: var(--accent);
    }

    .value {
        margin: 0;
        font-size: 26px;
        font-weight: 600;
        line-height: 1.1;
        font-variant-numeric: tabular-nums;
    }

    .stat.ok .value {
        color: var(--ok);
    }

    .stat.warn .value {
        color: var(--warn);
    }

    .stat.muted .value {
        color: var(--text-dim);
    }

    .of {
        font-size: 15px;
        font-weight: 400;
        color: var(--text-faint);
    }

    .label {
        margin: 4px 0 0;
        font-size: 12px;
        color: var(--text-dim);
    }

    /* The events panel is the shared table; it only needs a frame and a bound
       on its height, since the dashboard scrolls as a whole. */
    .events-table {
        border: 1px solid var(--border);
        border-radius: var(--radius);
        overflow: auto;
        max-height: 340px;
    }
</style>
