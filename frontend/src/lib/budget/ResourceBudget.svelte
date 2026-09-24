<!--
  What a cluster, a node or a namespace has, what has been promised out of it,
  and what is actually being used.

  The five numbers are kept apart on purpose. Capacity and allocatable say what
  exists, requests and limits say what the scheduler has booked and what those
  pods may grow into, and usage says what is really happening. A cluster can be
  full by requests and idle by usage at the same time, and that gap is the whole
  reason for putting them on one screen.

  Usage is the only part that needs anything installed. Where neither
  metrics-server nor Prometheus answered, the used bar is left out and the
  reason is written under the heading — a bar sitting at zero would read as a
  cluster doing nothing.
-->
<script lang="ts">
    import { everyWhileVisible } from '../visibility';
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import { formatValue, type Unit } from '../charts/format';
    import { adoptBudget, adoptDelay, barsFor, ceilingOf, delayTone, type Bar, type Budget, type CPUDelay } from './adopt';

    interface Props {
        contextId: string;
        /** `cluster`, `node`, `namespace` or `pod`. */
        scope: string;
        /** The node, namespace or pod. Ignored for a cluster. */
        name?: string;
        /** Where the pod is. Only a pod needs it. */
        namespace?: string;
        title?: string;
        /** Narrow layout for the detail panel. */
        compact?: boolean;
    }

    let { contextId, scope, name = '', namespace = '', title = 'Resources', compact = false }: Props = $props();

    /** Requests move when anything is scheduled, so this does not sit stale. */
    const REFRESH_MS = 30_000;

    let budget = $state<Budget | null>(null);
    let delay = $state<CPUDelay | null>(null);
    let error = $state<string | null>(null);
    let firstLoad = $state(true);

    /**
     * The throttling reading, or null where it could not be taken at all.
     *
     * Never allowed to fail the panel: the bars are the point of it, and a
     * reading the kubelets would not give up is a line missing rather than a
     * screen missing. A cluster that refused the request says so through the
     * reading's own error field; anything worse than that is simply left out.
     */
    async function delayReading(): Promise<CPUDelay | null> {
        try {
            return adoptDelay(await ResourceService.CPUDelay(contextId, scope, namespace, name));
        } catch {
            return null;
        }
    }

    async function load(): Promise<void> {
        // Two calls rather than one field on the budget: the delay reading goes
        // to the kubelets, which is slower than anything the budget does and
        // needs a permission the budget does not. Started together so a cluster
        // that refuses it still draws its bars at the usual speed.
        const bars = ResourceService.Budget(contextId, scope, namespace, name);
        const throttling = delayReading();

        try {
            budget = adoptBudget(await bars);
            error = null;
        } catch (err: unknown) {
            error = err instanceof Error ? err.message : String(err);
        } finally {
            firstLoad = false;
        }

        delay = await throttling;
    }

    $effect(() => {
        // Named so the effect re-runs when any of them change.
        contextId;
        scope;
        name;
        namespace;

        let live = true;
        void (async () => {
            if (live) await load();
        })();

        // Not while the window is hidden; see visibility.ts.
        const stop = everyWhileVisible(REFRESH_MS, () => {
            if (live) void load();
        });

        return () => {
            live = false;
            stop();
        };
    });

    let amounts = $derived(budget?.amounts ?? []);
    let usage = $derived(budget?.usage ?? null);

    /**
     * The line under the throttling bar.
     *
     * Built here rather than in the markup so the sentence is one string: an
     * inline `{#if}` in the middle of a paragraph puts the source file's own
     * line breaks into the middle of a word.
     */
    function delayNote(d: CPUDelay): string {
        const parts = [`${formatValue(d.stalled, 'seconds')} of CPU time lost per second to limits`];
        if (d.hasPressure) parts.push(`CPU pressure ${(d.pressure * 100).toFixed(0)}%`);
        // A cluster wider than the sample cap gets a reading off some of its
        // nodes, and saying so is the difference between a total and a sample.
        if (d.sampled < d.nodes) parts.push(`sampled from ${d.sampled} of ${d.nodes} nodes`);
        parts.push('from the kubelet');
        return parts.join(' · ');
    }

    /** The ceiling written out, for the line beside each dimension's name. */
    function ceilingText(amount: (typeof amounts)[number], bars: Bar[]): string {
        const ceiling = ceilingOf(amount);
        const bounded = scope === 'pod' ? 'no limit' : 'no quota';
        if (ceiling <= 0) {
            // Say which it is. A namespace with no quota — or a pod with no
            // limit — is not capped at zero, and the bars beside this text are
            // proportions of each other rather than of anything it owns.
            return bars.some((b) => b.track) ? `${bounded} — bars are relative` : bounded;
        }

        const unit = amount.unit as Unit;
        const written = formatValue(ceiling, unit);
        // A pod's ceiling is its own limit, which is not a thing anybody
        // "allocates" to it. Calling it allocatable would read as the node's.
        if (scope === 'pod') {
            return `${written} limit`;
        }
        // Where the two differ, saying so is the point: the gap is what the
        // kubelet keeps back and it is otherwise invisible.
        if (amount.capacity > 0 && amount.allocatable > 0 && amount.capacity !== amount.allocatable) {
            return `${written} allocatable of ${formatValue(amount.capacity, unit)}`;
        }
        return `${written} allocatable`;
    }
</script>

<section class="budget" class:compact>
    <header>
        <h2>{title}</h2>
        {#if usage?.source}
            <span class="source" title="Live usage is read from {usage.source}">{usage.source}</span>
        {/if}
    </header>

    {#if error}
        <p class="note failed">{error}</p>
    {:else if budget?.error}
        <p class="note failed">{budget.error}</p>
    {:else if firstLoad}
        <p class="note">Adding it up…</p>
    {:else}
        {#if usage && !usage.source && usage.error}
            <!-- Not a failure: a cluster with no metrics stack is a normal
                 cluster, and everything above still works. -->
            <p class="note quiet" title={usage.error}>
                No live usage — {usage.error}. Requests and limits come from the API server and are unaffected.
            </p>
        {/if}

        {#each amounts as amount (amount.label)}
            {@const bars = barsFor(amount)}
            <article class="amount">
                <div class="amount-head">
                    <span class="name">{amount.label}</span>
                    <span class="ceiling">{ceilingText(amount, bars)}</span>
                </div>

                {#each bars as bar (bar.label)}
                    <div class="bar">
                        <span class="bar-label">{bar.label}</span>
                        {#if bar.track}
                            <div class="track" class:relative={bar.relative}>
                                <div
                                    class="fill {bar.label.toLowerCase()}"
                                    class:over={bar.overcommitted}
                                    style:width="{bar.percent}%"
                                ></div>
                            </div>
                        {:else}
                            <span class="no-track"></span>
                        {/if}
                        <span class="bar-value">
                            {formatValue(bar.value, amount.unit as Unit)}
                            {#if bar.overcommitted}<span class="over-tag" title="More than the node can actually give out">over</span>{/if}
                        </span>
                    </div>
                {/each}

                <!-- Under the used bar, because it is the reading that explains
                     it: cores used says how much CPU went in, and this says how
                     much of the time something was standing still waiting for
                     it. Read from the kubelets, so it answers on a cluster with
                     no metrics stack at all. -->
                {#if amount.label === 'CPU' && delay}
                    {#if delay.waiting}
                        <p class="delay quiet">Measuring throttling…</p>
                    {:else if delay.error}
                        <p class="delay quiet" title={delay.error}>No throttling reading — {delay.error}</p>
                    {:else if delay.source && !delay.limited}
                        <p class="delay quiet">
                            Nothing here has a CPU limit, so nothing is throttled.
                        </p>
                    {:else if delay.source}
                        <!-- Its own track rather than another of the bars above
                             it: those are fractions of the ceiling beside the
                             heading, and this is a fraction of the kernel's
                             enforcement periods. Same shape, different
                             denominator, so it is never mistaken for one. -->
                        <div class="bar">
                            <span class="bar-label">Throttled</span>
                            <div class="delay-track">
                                <div
                                    class="delay-fill {delayTone(delay)}"
                                    style:width="{Math.min(100, delay.throttled * 100)}%"
                                ></div>
                            </div>
                            <span class="bar-value">{(delay.throttled * 100).toFixed(delay.throttled < 0.1 ? 1 : 0)}%</span>
                        </div>
                        <p class="delay quiet">{delayNote(delay)}</p>
                    {/if}
                {/if}
            </article>
        {/each}
    {/if}
</section>

<style>
    .budget {
        margin-bottom: 26px;
    }

    /* In the detail panel the section is the full width of the dock, so it
       brings its own gutters -- matching the describe report below it, which
       is what it has to line up with. */
    .budget.compact {
        padding: 14px 16px 0;
        margin-bottom: 18px;
    }

    header {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        gap: 10px;
    }

    h2 {
        margin: 0 0 10px;
        font-size: 11px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
        font-weight: 600;
    }

    .source {
        font-size: 10.5px;
        font-family: var(--mono);
        color: var(--text-faint);
    }

    .note {
        color: var(--text-dim);
        font-size: 12px;
        margin: 0 0 12px;
    }

    .note.quiet {
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .note.failed {
        color: var(--error, var(--warn));
    }

    .amount + .amount {
        margin-top: 16px;
    }

    .amount-head {
        display: flex;
        justify-content: space-between;
        align-items: baseline;
        gap: 12px;
        font-size: 12px;
        margin-bottom: 6px;
    }

    .ceiling {
        color: var(--text-faint);
        font-size: 11px;
        font-family: var(--mono);
    }

    /* Three columns so the bars line up under each other and can be read as
       one stack rather than three unrelated rows. */
    .bar {
        display: grid;
        grid-template-columns: 68px 1fr 110px;
        align-items: center;
        gap: 10px;
        margin-top: 4px;
    }

    .compact .bar {
        grid-template-columns: 60px 1fr 88px;
        gap: 8px;
    }

    .bar-label {
        font-size: 11px;
        color: var(--text-dim);
    }

    .bar-value {
        font-size: 11px;
        font-family: var(--mono);
        color: var(--text-faint);
        text-align: right;
        font-variant-numeric: tabular-nums;
    }

    .track {
        height: 6px;
        border-radius: 3px;
        background: var(--bg-raised);
        overflow: hidden;
    }

    /* Scaled against the other bars rather than a ceiling: a dashed end says
       the bar does not run out at a wall. */
    .track.relative {
        background: repeating-linear-gradient(
            90deg,
            var(--bg-raised) 0 4px,
            transparent 4px 7px
        );
    }

    .no-track {
        display: block;
    }

    .fill {
        height: 100%;
        border-radius: 3px;
        transition: width 220ms ease;
        background: var(--text-faint);
    }

    /* Requested is the booking, limits the ceiling those pods may reach, used
       what is really happening -- distinct enough to tell apart at a glance,
       and used is the one the eye should land on. */
    .fill.requested {
        background: var(--accent, #6ea8fe);
    }

    .fill.limits {
        background: var(--text-faint);
        opacity: 0.55;
    }

    .fill.used {
        background: var(--ok);
    }

    .fill.over {
        background: var(--warn) !important;
        opacity: 1;
    }

    .over-tag {
        color: var(--warn);
        margin-left: 5px;
        font-size: 10px;
        text-transform: uppercase;
        letter-spacing: 0.04em;
    }

    /* Throttling is not a share of the ceiling above it -- it is a share of the
       kernel's own enforcement periods -- so it gets a track of its own rather
       than joining the bars it sits under. Never green: a throttled container
       is never a good state, only a mild one. */
    .delay-track {
        height: 6px;
        border-radius: 3px;
        background: var(--bg-raised);
        overflow: hidden;
    }

    .delay-fill {
        height: 100%;
        border-radius: 3px;
        transition: width 220ms ease;
        background: var(--text-faint);
    }

    .delay-fill.warn {
        background: var(--warn);
    }

    .delay-fill.bad {
        background: var(--error, var(--warn));
    }

    .delay {
        margin: 2px 0 0;
        padding-left: 78px;
        font-size: 11px;
        line-height: 1.5;
        color: var(--text-faint);
    }

    .compact .delay {
        padding-left: 68px;
    }
</style>
