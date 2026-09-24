<!--
  A cluster's events drawn against time.

  The events table sorts by when, but it reads as a list: forty BackOff rows
  and one NodeNotReady between them look alike. Drawn on a time axis, one row
  per reason, the shape shows -- the node went, then everything on it
  complained -- and that shape is usually the answer to "what happened here".

  An event that repeated is a bar from its first time to its last, thicker the
  more often it happened; one that happened once is a dot. Warnings are drawn
  in the warning colour and put at the top. A mark opens the event's report.
-->
<script lang="ts">
    import { formatDateTime, formatDay, formatTime } from '../datetime.svelte';
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import { adoptTimeline, type Timeline } from '../state/adopt';
    import { detail } from '../state/detail.svelte';

    interface Props {
        contextId: string;
        /** The namespaces to offer in the filter. */
        namespaces: string[];
        /** Bumped by the dashboard's own refresh, to read again with it. */
        refreshKey?: number;
    }

    let { contextId, namespaces, refreshKey = 0 }: Props = $props();

    /** The windows offered, in minutes. */
    const WINDOWS = [
        { minutes: 15, label: '15m' },
        { minutes: 60, label: '1h' },
        { minutes: 360, label: '6h' },
        { minutes: 1440, label: '24h' },
    ];
    /** How many reasons get a row of their own; the rest share one. */
    const LANES = 10;
    const LANE_H = 22;
    const LABEL_W = 170;
    const AXIS_H = 20;
    const PAD_R = 14;

    let minutes = $state(60);
    let namespace = $state('');
    let warningsOnly = $state(false);
    let timeline = $state<Timeline | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(false);
    let width = $state(800);

    $effect(() => {
        const id = contextId;
        const ns = namespace;
        const window = minutes;
        refreshKey;
        let live = true;
        loading = true;
        (async () => {
            try {
                const result = adoptTimeline(await ResourceService.EventTimeline(id, ns, window));
                if (!live) return;
                timeline = result;
                error = result.error || null;
            } catch (err) {
                if (!live) return;
                error = (err instanceof Error ? err.message : String(err)) || 'The events could not be read.';
            } finally {
                if (live) loading = false;
            }
        })();
        return () => {
            live = false;
        };
    });

    let events = $derived((timeline?.events ?? []).filter((e) => !warningsOnly || e.type === 'Warning'));

    interface Lane {
        reason: string;
        warning: boolean;
        count: number;
        events: kube.TimelineEvent[];
    }

    /** One row per reason, warnings first, busiest first; the tail folded into one. */
    let lanes = $derived.by((): Lane[] => {
        const by = new Map<string, Lane>();
        for (const e of events) {
            const reason = e.reason || '(no reason)';
            let lane = by.get(reason);
            if (!lane) {
                lane = { reason, warning: false, count: 0, events: [] };
                by.set(reason, lane);
            }
            lane.events.push(e);
            lane.count += e.count;
            if (e.type === 'Warning') lane.warning = true;
        }
        const sorted = [...by.values()].sort(
            (a, b) => Number(b.warning) - Number(a.warning) || b.count - a.count || a.reason.localeCompare(b.reason),
        );
        if (sorted.length <= LANES) return sorted;
        const kept = sorted.slice(0, LANES - 1);
        const rest = sorted.slice(LANES - 1);
        kept.push({
            reason: `${rest.length} other reasons`,
            warning: rest.some((l) => l.warning),
            count: rest.reduce((n, l) => n + l.count, 0),
            events: rest.flatMap((l) => l.events),
        });
        return kept;
    });

    let from = $derived(timeline ? new Date(timeline.from).getTime() : Date.now() - minutes * 60_000);
    let to = $derived(timeline ? new Date(timeline.to).getTime() : Date.now());
    let plotW = $derived(Math.max(100, width - LABEL_W - PAD_R));
    let height = $derived(lanes.length * LANE_H + AXIS_H + 6);

    function x(at: string): number {
        const t = new Date(at).getTime();
        const f = (Math.min(Math.max(t, from), to) - from) / Math.max(1, to - from);
        return LABEL_W + f * plotW;
    }

    /** How thick a mark is: more repeats, a bigger mark, within reason. */
    function radius(count: number): number {
        return Math.min(7, 3 + Math.log2(count));
    }

    /** Ticks along the axis, on round times for the window. */
    let ticks = $derived.by(() => {
        const step = minutes <= 15 ? 5 : minutes <= 60 ? 15 : minutes <= 360 ? 60 : 240;
        const stepMs = step * 60_000;
        const out: { x: number; label: string }[] = [];
        for (let t = Math.ceil(from / stepMs) * stepMs; t <= to; t += stepMs) {
            const at = new Date(t);
            out.push({
                x: LABEL_W + ((t - from) / Math.max(1, to - from)) * plotW,
                label: minutes > 1440 ? formatDay(at) : formatTime(at),
            });
        }
        return out;
    });

    function describe(e: kube.TimelineEvent): string {
        const when =
            e.first !== e.at
                ? `${formatDateTime(e.first, { seconds: true })} – ${formatTime(e.at, { seconds: true })}, ${e.count} times`
                : formatDateTime(e.at, { seconds: true });
        const where = e.objectNamespace ? `${e.objectNamespace}/` : '';
        return `${e.type} ${e.reason} — ${where}${e.object}\n${when}\n${e.message}`;
    }

    function open(e: kube.TimelineEvent): void {
        void detail.open({ contextId, kind: 'events', namespace: e.namespace, name: e.name });
    }

    function onKey(event: KeyboardEvent, e: kube.TimelineEvent): void {
        if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            open(e);
        }
    }
</script>

<section class="timeline">
    <div class="bar">
        <h2>Event timeline</h2>
        <div class="controls">
            <div class="windows" role="group" aria-label="How far back">
                {#each WINDOWS as w (w.minutes)}
                    <button class:on={minutes === w.minutes} onclick={() => (minutes = w.minutes)} aria-pressed={minutes === w.minutes}>
                        {w.label}
                    </button>
                {/each}
            </div>
            <select bind:value={namespace} aria-label="Namespace">
                <option value="">All namespaces</option>
                {#each namespaces as ns (ns)}
                    <option value={ns}>{ns}</option>
                {/each}
            </select>
            <label class="only">
                <input type="checkbox" bind:checked={warningsOnly} />
                Warnings only
            </label>
        </div>
    </div>

    {#if error}
        <p class="note error">{error}</p>
    {:else if timeline && events.length === 0}
        <p class="note">{loading ? 'Reading…' : 'No events in this window.'}</p>
    {:else if timeline}
        <div class="plot" bind:clientWidth={width}>
            <svg {width} {height} role="img" aria-label="{events.length} events over the last {minutes} minutes, grouped by reason">
                {#each ticks as tick (tick.x)}
                    <line class="grid" x1={tick.x} x2={tick.x} y1="0" y2={lanes.length * LANE_H} />
                    <text class="tick" x={tick.x} y={lanes.length * LANE_H + 14} text-anchor="middle">{tick.label}</text>
                {/each}

                {#each lanes as lane, i (lane.reason)}
                    {@const y = i * LANE_H + LANE_H / 2}
                    <line class="lane" x1={LABEL_W} x2={LABEL_W + plotW} y1={y} y2={y} />
                    <text class="reason" class:warning={lane.warning} x={LABEL_W - 10} y={y + 4} text-anchor="end">
                        {lane.reason.length > 22 ? lane.reason.slice(0, 21) + '…' : lane.reason}
                        <title>{lane.reason}: {lane.count}</title>
                    </text>
                    {#each lane.events as e (e.namespace + '/' + e.name)}
                        {@const x1 = x(e.first)}
                        {@const x2 = x(e.at)}
                        {@const r = radius(e.count)}
                        <g
                            class="mark"
                            class:warning={e.type === 'Warning'}
                            role="button"
                            tabindex="0"
                            aria-label="{e.reason}: {e.object}"
                            onclick={() => open(e)}
                            onkeydown={(event) => onKey(event, e)}
                        >
                            <title>{describe(e)}</title>
                            {#if x2 - x1 > 2}
                                <line class="span" x1={x1} x2={x2} y1={y} y2={y} stroke-width={r * 1.2} />
                            {/if}
                            <circle cx={x2} cy={y} {r} />
                        </g>
                    {/each}
                {/each}
            </svg>
        </div>
        {#if timeline.truncated}
            <p class="note">Only the newest {timeline.events.length} events in this window are drawn.</p>
        {/if}
    {:else}
        <p class="note">Reading…</p>
    {/if}
</section>

<style>
    .timeline {
        margin-bottom: 26px;
    }

    .bar {
        display: flex;
        align-items: center;
        flex-wrap: wrap;
        gap: 8px 12px;
        margin-bottom: 10px;
    }

    h2 {
        margin: 0;
        font-size: 11px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
        font-weight: 600;
    }

    .controls {
        margin-left: auto;
        display: flex;
        align-items: center;
        gap: 10px;
    }

    .windows {
        display: flex;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        overflow: hidden;
    }

    .windows button {
        padding: 2px 8px;
        font: inherit;
        font-size: 11px;
        color: var(--text-dim);
        background: transparent;
        border: 0;
        cursor: pointer;
    }

    .windows button + button {
        border-left: 1px solid var(--border);
    }

    .windows button.on {
        background: var(--bg-active);
        color: var(--text);
    }

    select {
        height: 22px;
        font: inherit;
        font-size: 11px;
        color: var(--text);
        background: var(--bg-panel);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
    }

    .only {
        display: flex;
        align-items: center;
        gap: 5px;
        font-size: 11px;
        color: var(--text-dim);
    }

    .plot {
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-panel);
        padding: 6px 0 0;
        overflow: hidden;
    }

    svg {
        display: block;
    }

    .grid {
        stroke: var(--border-soft);
    }

    .lane {
        stroke: var(--border-soft);
        stroke-dasharray: 2 3;
    }

    .tick {
        font-size: 10px;
        fill: var(--text-faint);
    }

    .reason {
        font-size: 11px;
        fill: var(--text-dim);
    }

    .reason.warning {
        fill: var(--warn);
    }

    .mark {
        cursor: pointer;
        outline: none;
    }

    .mark circle {
        fill: var(--text-faint);
        stroke: var(--bg-panel);
        stroke-width: 1;
    }

    .mark .span {
        stroke: color-mix(in srgb, var(--text-faint) 45%, transparent);
        stroke-linecap: round;
    }

    .mark.warning circle {
        fill: var(--warn);
    }

    .mark.warning .span {
        stroke: color-mix(in srgb, var(--warn) 40%, transparent);
    }

    .mark:hover circle,
    .mark:focus-visible circle {
        stroke: var(--text);
        stroke-width: 1.5;
    }

    .note {
        margin: 0;
        padding: 8px 0;
        font-size: 12px;
        color: var(--text-dim);
    }

    .note.error {
        color: var(--error);
    }
</style>
