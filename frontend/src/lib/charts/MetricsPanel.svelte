<!--
  A surface's worth of plugin charts: the cluster dashboard, a plugin's overview,
  or the detail panel of one object.

  The range control sits once at the top and scopes every chart below it. That is
  a rule rather than a layout preference — a page where each chart carries its own
  window is a page whose numbers cannot be compared, and comparing them is the
  entire reason they are next to each other.

  Nothing is drawn at all when no installed plugin has charts for this surface,
  so a cluster with no monitoring shows no empty frame.
-->
<script lang="ts">
    import { MetricsService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import Icon from '../components/Icon.svelte';
    import { adoptPanel, type MetricsPanelData } from './adopt';
    import LineChart from './LineChart.svelte';
    import type { Unit } from './format';
    import { workspace } from '../state/workspace.svelte';

    interface Props {
        contextId: string;
        /** A resource kind, `dashboard`, or a plugin's overview tab kind, `plugin:<id>/overview`. */
        attach: string;
        /** The object being drawn for. Empty on the two cluster-wide surfaces. */
        namespace?: string;
        name?: string;
        /** Section heading. Omitted where the surrounding view supplies one. */
        title?: string;
        /** Narrow layout: one column, shorter charts. The detail panel wants this. */
        compact?: boolean;
    }

    let { contextId, attach, namespace = '', name = '', title = 'Metrics', compact = false }: Props = $props();

    /** The windows worth one click. Presets before any custom range. */
    const RANGES = [
        { minutes: 15, label: '15m' },
        { minutes: 60, label: '1h' },
        { minutes: 360, label: '6h' },
        { minutes: 1440, label: '24h' },
    ];

    /** How often a visible panel refetches. Metrics are expected to move. */
    const REFRESH_MS = 30_000;

    let panel = $state<MetricsPanelData | null>(null);
    let error = $state<string | null>(null);
    /** True only for the first load: later ones hold the previous render. */
    let firstLoad = $state(true);
    let loading = $state(false);
    let attempt = $state(0);

    let range = $derived(workspace.metricsRange);

    /**
     * The chart whose description is showing, if any.
     *
     * One at a time for the whole panel rather than a flag per card: two
     * descriptions open at once would overlap the charts between them, and only
     * one pointer can be over a card anyway.
     */
    let hint = $state<string | null>(null);

    /** Closes the hint only if it is still the one being left. */
    function closeHint(id: string): void {
        if (hint === id) hint = null;
    }

    async function load(): Promise<void> {
        loading = true;
        try {
            const result = await MetricsService.Charts(contextId, attach, namespace, name, range);
            panel = adoptPanel(result);
            error = null;
        } catch (err: unknown) {
            error = err instanceof Error ? err.message : String(err);
        } finally {
            loading = false;
            firstLoad = false;
        }
    }

    $effect(() => {
        if (!show) return;

        // Named so the effect re-runs when any of them change.
        contextId;
        attach;
        namespace;
        name;
        range;
        attempt;

        let live = true;
        void (async () => {
            if (live) await load();
        })();

        const timer = setInterval(() => {
            if (live) void load();
        }, REFRESH_MS);

        return () => {
            live = false;
            clearInterval(timer);
        };
    });

    /** Where the numbers came from, for the line under the heading. */
    let source = $derived(panel?.source ?? null);
    let charts = $derived(panel?.charts ?? []);
    /**
     * Whether to draw anything at all.
     *
     * Answered from the installed plugins rather than from the reply, so the
     * panel either exists from the first frame or never appears -- a heading
     * that shows "Looking for metrics…" and then removes itself is worse than
     * one that was never there. A plugin that *does* chart here and a cluster
     * with no Prometheus is a different case, and gets a sentence.
     */
    let show = $derived(workspace.chartsAttachTo(attach));
</script>

{#if show}
    <section class="metrics" class:compact>
        <header>
            <h2>{title}</h2>

            <!-- One row, above the charts, scoping all of them. -->
            <div class="ranges" role="group" aria-label="Time range">
                {#each RANGES as preset (preset.minutes)}
                    <button
                        class:current={range === preset.minutes}
                        aria-pressed={range === preset.minutes}
                        onclick={() => workspace.setMetricsRange(preset.minutes)}
                    >
                        {preset.label}
                    </button>
                {/each}
            </div>

            <button
                class="refresh"
                onclick={() => attempt++}
                disabled={loading}
                title={source?.describe ? `Reading from ${source.describe}` : 'Refresh'}
                aria-label="Refresh the charts"
            >
                <Icon name="refresh" size={12} />
            </button>
        </header>

        {#if error}
            <p class="note failed">{error}</p>
        {:else if firstLoad}
            <p class="note">Looking for metrics…</p>
        {:else if source && !source.available}
            <!-- The two reasons there are no charts are entirely different and
                 need different things done about them, so they are never worded
                 the same way. -->
            {#if source.error}
                <p class="note failed" title={source.error}>Could not look for a Prometheus — {source.error}</p>
            {:else}
                <p class="note">
                    No Prometheus found in this cluster. If there is one, point this context at it under
                    the cluster's settings in the sidebar.
                </p>
            {/if}
        {:else}
            <div class="grid">
                {#each charts as chart (chart.pluginId + chart.id)}
                    <article class="card">
                        <header class="card-head">
                            <h3>{chart.label}</h3>
                            {#if chart.description}
                                <!-- A real popover rather than a `title`: the
                                     webview does not draw native tooltips, so
                                     the description behind that attribute was
                                     unreachable. Hover shows it, focus shows it
                                     too, and a click pins it open for a reader
                                     who wants to keep it up. -->
                                <button
                                    type="button"
                                    class="what"
                                    aria-label="What {chart.label} measures"
                                    aria-expanded={hint === chart.id}
                                    onmouseenter={() => (hint = chart.id)}
                                    onmouseleave={() => closeHint(chart.id)}
                                    onfocus={() => (hint = chart.id)}
                                    onblur={() => closeHint(chart.id)}
                                    onclick={() => (hint = hint === chart.id ? null : chart.id)}
                                    onkeydown={(e) => {
                                        if (e.key === 'Escape') hint = null;
                                    }}
                                >
                                    <Icon name="info" size={11} />
                                </button>
                                {#if hint === chart.id}
                                    <span class="hint" role="tooltip">{chart.description}</span>
                                {/if}
                            {/if}
                        </header>
                        {#if chart.error}
                            <p class="note failed" title={chart.error}>{chart.error}</p>
                        {:else if chart.series.length === 0 && !loading}
                            <!-- Nothing at all, as opposed to a flat line: the
                                 metric is not there, which almost always means
                                 nothing is scraping it. The plugin's own
                                 description says what it needs. -->
                            <p class="note">
                                No data came back for this window.
                                {#if chart.description}{chart.description}{/if}
                            </p>
                        {:else}
                            <LineChart
                                series={chart.series}
                                unit={chart.unit as Unit}
                                spanMinutes={range}
                                height={compact ? 104 : 132}
                                stale={loading}
                            />
                        {/if}
                    </article>
                {/each}
            </div>
        {/if}
    </section>
{/if}

<style>
    .metrics {
        margin-bottom: 22px;
    }

    header {
        display: flex;
        align-items: center;
        gap: 10px;
        margin-bottom: 10px;
    }

    h2 {
        flex: 1 1 auto;
        margin: 0;
        font-size: 11px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .ranges {
        display: flex;
        gap: 2px;
        flex: 0 0 auto;
    }

    .ranges button {
        padding: 2px 7px;
        border-radius: var(--radius-sm);
        font-family: var(--mono);
        font-size: 10px;
        color: var(--text-faint);
    }

    .ranges button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .ranges button.current {
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text);
    }

    .refresh {
        display: grid;
        place-items: center;
        width: 20px;
        height: 20px;
        flex: 0 0 auto;
        border-radius: var(--radius-sm);
        color: var(--text-faint);
    }

    .refresh:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .refresh:disabled {
        opacity: 0.5;
        cursor: default;
    }

    .grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
        gap: 12px;
    }

    .compact .grid {
        grid-template-columns: 1fr;
        gap: 10px;
    }

    .card {
        padding: 10px 12px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        min-width: 0;
    }

    .card-head {
        position: relative;
        display: flex;
        align-items: center;
        gap: 5px;
        margin-bottom: 4px;
    }

    h3 {
        margin: 0;
        font-size: 12px;
        font-weight: 500;
        color: var(--text-dim);
    }

    .what {
        display: grid;
        place-items: center;
        padding: 0;
        border: 0;
        background: none;
        color: var(--text-faint);
        cursor: help;
    }

    .what:hover,
    .what:focus-visible {
        color: var(--text-dim);
    }

    /* Over the chart rather than above the card: pushing the card taller would
       move every other card on the row while a pointer rests on one of them. */
    .hint {
        position: absolute;
        z-index: 2;
        top: calc(100% + 4px);
        left: 0;
        right: 0;
        padding: 7px 9px;
        border-radius: var(--radius);
        background: var(--bg-raised, var(--bg-panel));
        box-shadow:
            inset 0 0 0 1px var(--border-soft),
            0 6px 18px rgb(0 0 0 / 0.28);
        color: var(--text-dim);
        font-size: 11.5px;
        line-height: 1.55;
    }

    .note {
        margin: 0;
        padding: 6px 0;
        font-size: 11.5px;
        line-height: 1.6;
        color: var(--text-faint);
    }

    .note.failed {
        color: var(--warn);
    }
</style>
