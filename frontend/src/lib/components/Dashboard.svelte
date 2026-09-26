<!-- The overview tab: what the cluster is, how much of it is healthy, and what has gone wrong lately. -->
<script lang="ts">
    import { everyWhileVisible } from '../visibility';
    import { formatDate, formatTime } from '../datetime.svelte';
    import { KubeconfigService, ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import { adoptCredentials, adoptOverview, type Credentials, type Overview } from '../state/adopt';
    import { workspace } from '../state/workspace.svelte';
    import { clusters } from '../state/health.svelte';
    import MetricsPanel from '../charts/MetricsPanel.svelte';
    import ResourceBudget from '../budget/ResourceBudget.svelte';
    import ErrorState from './ErrorState.svelte';
    import Icon from './Icon.svelte';
    import SortableTable from './SortableTable.svelte';
    import EventTimeline from './EventTimeline.svelte';
    import WhenVisible from './WhenVisible.svelte';
    import LoadingState from './LoadingState.svelte';
    import { detail } from '../state/detail.svelte';
    import { changes } from '../state/changes.svelte';
    import { actions } from '../state/actions.svelte';
    import { notices } from '../state/notices.svelte';
    import { credentialTone, inDays } from '../fleet/alerts';
    import { session } from '../state/session.svelte';
    import { message } from '../state/workspace/helpers';
    import { untrack } from 'svelte';

    interface Props {
        contextId: string;
    }

    let { contextId }: Props = $props();

    let overview = $state<Overview | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(true);
    /** Bumped by the retry button; the loading effect reads it as a dependency. */
    let attempt = $state(0);
    /** Whether a refresh is in flight, for the button and its spinner. */
    let refreshing = $state(false);
    /** When the counters last changed, for the line under them. */
    let updated = $state<Date | null>(null);
    /**
     * A refresh that failed while a good dashboard is on screen. Kept apart
     * from `error`, which replaces the whole page: a cluster that blinked is
     * not a reason to throw away the numbers and make the reader press a
     * button to get them back.
     */
    let stale = $state<string | null>(null);

    /**
     * How often the counters are read again.
     *
     * The same half-minute the budget and the metrics panels on this page use,
     * so the whole dashboard moves at one speed rather than three. It is a
     * poll rather than a watch because the counters are five list calls and
     * holding five informers open for four numbers would cost far more than
     * asking every thirty seconds does.
     */
    const REFRESH_MS = 30_000;

    let color = $derived(workspace.colorOf(contextId));
    let context = $derived(workspace.contexts.find((c) => c.id === contextId) ?? null);

    // A different cluster starts from nothing rather than from the last one's
    // numbers: keeping what is on screen is right for a refresh of the same
    // context and wrong for a move to another. Deliberately not keyed on
    // `attempt`, so pressing Refresh leaves the numbers up while it reads.
    $effect(() => {
        contextId;
        overview = null;
        updated = null;
        stale = null;
        loading = true;
    });

    $effect(() => {
        const id = contextId;
        attempt;

        let live = true;

        /**
         * Reads the overview once. A refresh keeps what is on screen until the
         * new numbers arrive -- clearing first would make the page flash empty
         * twice a minute -- so only the first read of a context shows Loading.
         */
        async function load(): Promise<void> {
            refreshing = true;
            try {
                const result = await ResourceService.Overview(id);
                if (!live) return;
                overview = adoptOverview(result);
                updated = new Date();
                error = null;
                stale = null;
                // A dashboard that loaded is better evidence than any ping, so
                // it settles the sidebar indicator for this context.
                clusters.report(id, 'connected');
            } catch (err: unknown) {
                if (!live) return;
                const text = err instanceof Error ? err.message : String(err);
                // Nothing on screen yet: the failure is the page. Otherwise it
                // is a note over numbers that are still worth reading.
                if (overview) stale = text;
                else error = text;
                clusters.report(id, 'error', text);
            } finally {
                if (live) {
                    loading = false;
                    refreshing = false;
                }
            }
        }

        void load();
        // Not while the window is hidden; see visibility.ts.
        const stop = everyWhileVisible(REFRESH_MS, () => {
            if (live) void load();
        });

        return () => {
            live = false;
            stop();
        };
    });

    /**
     * How long after a write to read once more. A deleted pod that was
     * running takes its grace period to go, so the read straight after the
     * delete can still count it; the second one is what settles.
     */
    const SETTLE_MS = 5_000;

    // A delete, an eviction or an edit made from this app re-reads the page
    // at once rather than leaving the counts wrong until the next poll --
    // deleting the evicted pods the panel lists should make it say so. Only
    // for this cluster, and not on arriving at a cluster: that is the load
    // above's to do.
    let seen = untrack(() => ({ id: contextId, writes: changes.writes(contextId) }));
    $effect(() => {
        const id = contextId;
        const writes = changes.writes(id);
        if (id !== seen.id || writes === seen.writes) {
            seen = { id, writes };
            return;
        }
        seen = { id, writes };
        untrack(refresh);
        const later = setTimeout(refresh, SETTLE_MS);
        return () => clearTimeout(later);
    });

    /** Reads the counters again now, for the refresh button. */
    function refresh(): void {
        attempt++;
    }

    /**
     * When this context's credentials run out. Read once per cluster rather
     * than on every refresh: certificates are dated in days.
     */
    let credentials = $state<Credentials | null>(null);
    $effect(() => {
        const id = contextId;
        credentials = null;
        let live = true;
        (async () => {
            try {
                const result = adoptCredentials(await ResourceService.Credentials(id, true));
                if (live) credentials = result;
            } catch {
                // The header simply goes without the line.
            }
        })();
        return () => {
            live = false;
        };
    });

    /** The credential that runs out first, of those that say when. */
    let soonest = $derived(credentials?.items.find((item) => item.notAfter) ?? null);

    /** Shows the kubeconfig this context was read from, selected, in the file manager. */
    async function revealKubeconfig(): Promise<void> {
        try {
            await KubeconfigService.RevealFile(contextId);
        } catch (err) {
            notices.fail(`Could not show the kubeconfig: ${message(err)}`);
        }
    }

    function credentialsTitle(items: kube.Credential[]): string {
        return items
            .map((item) => {
                const what = item.subject ? `${item.kind} — ${item.subject}` : item.kind;
                if (item.error) return `${what}: ${item.error}`;
                if (!item.notAfter) return `${what}: ${item.note}`;
                return `${what}: ${formatDate(item.notAfter)} (${inDays(item.daysLeft)})`;
            })
            .join('\n');
    }

    /**
     * Whether the evicted pods' clean-up is asking to be confirmed, and
     * whether it is under way.
     */
    let askingCleanup = $state(false);
    let cleaning = $state(false);

    /**
     * Deletes every evicted pod in the cluster. The list is read fresh rather
     * than taken from the panel, which names only the worst few; the delete
     * goes through the same bulk delete the pods list uses, so the report and
     * the refusals read the same.
     */
    async function deleteEvicted(): Promise<void> {
        if (cleaning) return;
        cleaning = true;
        try {
            const refs = (await ResourceService.EvictedPods(contextId)) ?? [];
            if (refs.length === 0) {
                notices.inform('No evicted pods left to delete');
                return;
            }
            const report = await actions.removeMany(contextId, 'pods', refs);
            if (report.failures.length === 0) {
                notices.inform(`${plural(report.done, 'evicted pod')} deleted`);
            } else {
                const first = report.failures[0]!;
                notices.fail(
                    `${report.done} of ${refs.length} evicted pods deleted; ${first.namespace}/${first.name}: ${first.error}`,
                );
            }
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            cleaning = false;
            askingCleanup = false;
        }
    }

    /** A pod's last stop, as the attention list says it: why, and how long ago. */
    function lastStop(pod: kube.PodIssue): string {
        if (!pod.lastTermination) return '';
        return pod.lastRestart ? `${pod.lastTermination} · ${pod.lastRestart} ago` : pod.lastTermination;
    }

    /** "just now", or the clock time the counters were last read. */
    function updatedAt(at: Date): string {
        const seconds = Math.round((Date.now() - at.getTime()) / 1000);
        if (seconds < 45) return 'just now';
        return `at ${formatTime(at, { seconds: true })}`;
    }

    /**
     * How loudly the pods panel speaks: pods that are gone or will not start
     * are an error, restarts on their own a warning, and nothing at all hides
     * the panel.
     */
    let podTone = $derived.by(() => {
        const pods = overview?.pods;
        if (!pods) return null;
        if (pods.evicted + pods.failed + pods.crashLooping > 0) return 'error';
        if (pods.restarting > 0) return 'warn';
        return null;
    });

    function plural(n: number, one: string, many: string = one + 's'): string {
        return `${n} ${n === 1 ? one : many}`;
    }

    function statTone(stat: kube.Stat): string {
        if (stat.total === 0) return 'muted';
        return stat.ready === stat.total ? 'ok' : 'warn';
    }
</script>

<div class="dashboard" style:--ctx-color={color}>
    {#if loading && !overview}
        {#key attempt}
            <LoadingState
                what="the overview"
                cluster={context ? workspace.displayName(context) : contextId}
                phase="reading"
                steps={false}
                onRetry={() => attempt++}
            />
        {/key}
    {:else if error}
        <ErrorState message={error} {context} onRetry={() => attempt++} />
    {:else if overview}
        <header class="head" style:--ctx-color={color}>
            <div class="title">
                <!-- The name the user gave the context, as the tab and the
                     sidebar show it; the kubeconfig's own name goes below. -->
                <h1>{context ? workspace.displayName(context) : overview.context}</h1>
                <button class="refresh" onclick={refresh} disabled={refreshing} title="Read the cluster again now">
                    <Icon name="refresh" size={13} />
                    {refreshing ? 'Reading…' : 'Refresh'}
                </button>
            </div>
            <dl>
                {#if context && workspace.displayName(context) !== overview.context}
                    <div><dt>Context</dt><dd class="selectable">{overview.context}</dd></div>
                {/if}
                {#if overview.server}<div><dt>Server</dt><dd class="selectable">{overview.server}</dd></div>{/if}
                <div><dt>Version</dt><dd>{overview.version}</dd></div>
                {#if overview.distribution}<div><dt>Distribution</dt><dd>{overview.distribution}</dd></div>{/if}
                <!-- The first of this context's credentials to run out: an
                     admin certificate that expires takes every tab with it,
                     and nothing else warns. The rest are in the tooltip. -->
                {#if soonest && credentials}
                    {@const tone = credentialTone(soonest.daysLeft, soonest.notAfter)}
                    <div class="cred {tone ?? ''}" title={credentialsTitle(credentials.items)}>
                        <dt>{soonest.kind}</dt>
                        <dd>
                            {soonest.daysLeft < 0 ? 'expired' : 'expires'}
                            {inDays(soonest.daysLeft)}
                            {#if tone}<Icon name="alert" size={11} />{/if}
                            <!-- Where the credential lives, and so where it is
                                 replaced. The web version has no files to show. -->
                            {#if context?.file && !session.server}
                                <button
                                    class="reveal"
                                    onclick={revealKubeconfig}
                                    title="Show {context.file} in the file manager"
                                >
                                    <Icon name="file" size={11} />
                                    Show kubeconfig
                                </button>
                            {/if}
                        </dd>
                    </div>
                {/if}
            </dl>
        </header>

        <!-- Each counter is the way into the list it counts: the number says
             something is not ready, and the list is where to find out what. -->
        {#if stale}
            <p class="stale" title={stale}>
                These numbers are from {updated ? updatedAt(updated) : 'the last read'} — the cluster did not answer the
                last refresh.
            </p>
        {/if}

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

        <!-- The Pods tile says how many are not running; this says why, and
             points at them. Evicted pods in particular linger until someone
             deletes them, so a pile of them is news the counter hides. -->
        {#if podTone}
            {@const pods = overview.pods}
            <section class="attention {podTone}" aria-label="Pods needing attention">
                <h2>
                    <Icon name="alert" size={13} />
                    Pods need attention
                </h2>
                <div class="chips">
                    {#if pods.evicted > 0}
                        <button
                            class="chip error"
                            onclick={() => workspace.showPodsMatching(contextId, 'Evicted')}
                            title="Show the evicted pods"
                        >
                            <span class="n">{pods.evicted}</span> evicted
                        </button>
                    {/if}
                    {#if pods.failed > 0}
                        <button
                            class="chip error"
                            onclick={() => workspace.showPodsMatching(contextId, '')}
                            title="Open Pods"
                        >
                            <span class="n">{pods.failed}</span> failed
                        </button>
                    {/if}
                    {#if pods.crashLooping > 0}
                        <button
                            class="chip error"
                            onclick={() => workspace.showPodsMatching(contextId, '')}
                            title="Open Pods"
                        >
                            <span class="n">{pods.crashLooping}</span> not starting
                        </button>
                    {/if}
                    {#if pods.restartedRecently > 0}
                        <button
                            class="chip warn"
                            onclick={() => workspace.showPodsMatching(contextId, '')}
                            title="Pods that restarted a container in the last hour — failing now, rather than once some time ago"
                        >
                            <span class="n">{pods.restartedRecently}</span> restarted in the last hour
                        </button>
                    {/if}
                    {#if pods.restarting > 0}
                        <button
                            class="chip quiet"
                            onclick={() => workspace.showPodsMatching(contextId, '')}
                            title="Open Pods and sort by Restarts to see them"
                        >
                            <span class="n">{pods.restarting}</span>
                            {pods.restarting === 1 ? 'pod' : 'pods'} ever restarted · {plural(pods.restarts, 'restart')}
                        </button>
                    {/if}
                </div>
                <!-- Evicted pods are records of pods that are already gone:
                     the kubelet stopped them and keeps the object only so
                     someone can read why. Deleting them loses that record and
                     nothing else, which is why one button does all of them. -->
                {#if pods.evicted > 0}
                    <div class="cleanup">
                        {#if askingCleanup}
                            <p class="question">
                                Delete all {plural(pods.evicted, 'evicted pod')}? They are not running; this removes the
                                records of why they were evicted.
                            </p>
                            <button class="plain" onclick={() => (askingCleanup = false)} disabled={cleaning}>Cancel</button>
                            <button class="danger" onclick={deleteEvicted} disabled={cleaning}>
                                {cleaning ? 'Deleting…' : 'Delete'}
                            </button>
                        {:else}
                            <button class="plain" onclick={() => (askingCleanup = true)}>
                                <Icon name="trash" size={12} />
                                Delete {plural(pods.evicted, 'evicted pod')}
                            </button>
                        {/if}
                    </div>
                {/if}
                {#if pods.worst.length > 0}
                    <ul class="worst">
                        {#each pods.worst as pod (pod.namespace + '/' + pod.name)}
                            <li>
                                <button
                                    onclick={() =>
                                        void detail.open({
                                            contextId,
                                            kind: 'pods',
                                            namespace: pod.namespace,
                                            name: pod.name,
                                        })}
                                    title="Open {pod.name}"
                                >
                                    <span
                                        class="reason {pod.trouble === 'restarting' || pod.trouble === 'restarted' ? 'warn' : 'error'}"
                                        class:faded={pod.trouble === 'restarted'}>{pod.reason}</span
                                    >
                                    <span class="name">{pod.name}</span>
                                    <span class="ns">{pod.namespace}</span>
                                    {#if pod.restarts > 0}
                                        <span class="restarts">{plural(pod.restarts, 'restart')}</span>
                                    {:else}
                                        <span class="restarts"></span>
                                    {/if}
                                    {#if lastStop(pod) || pod.message}
                                        <span class="why" title={pod.message || lastStop(pod)}>
                                            {lastStop(pod) || pod.message}
                                        </span>
                                    {/if}
                                </button>
                            </li>
                        {/each}
                    </ul>
                {/if}
            </section>
        {/if}

        <!-- Capacity, allocatable, requests, limits and live usage, all
             against each other. Reads the API server for everything but the
             last, so it works on a cluster with no monitoring at all. -->
        <ResourceBudget {contextId} scope="cluster" title="Resources" />

        <!-- Between the counters and the events: the counters say what the
             cluster is made of, the charts say what it has been doing, and the
             events say what went wrong. That is the order someone reads them
             in. -->
        <!-- Below the fold on most screens, and a dozen Prometheus queries:
             drawn as it comes into view rather than with the counters. -->
        <WhenVisible height={320}>
            <MetricsPanel {contextId} attach="dashboard" title="Metrics" />
        </WhenVisible>

        <!-- The events again, drawn against time, which is what shows the
             order things went wrong in. The table under it is the newest few
             as rows. -->
        <WhenVisible height={260}>
            <EventTimeline {contextId} namespaces={overview.namespaces} refreshKey={attempt} />
        </WhenVisible>

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

    .title {
        display: flex;
        align-items: baseline;
        gap: 12px;
    }

    .refresh {
        margin-left: auto;
        display: inline-flex;
        align-items: center;
        gap: 5px;
        height: 24px;
        padding: 0 9px;
        font-size: 11px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--text-dim);
        cursor: pointer;
        flex: none;
    }

    .refresh:hover:not(:disabled) {
        color: var(--text);
        border-color: var(--ctx-color);
    }

    .refresh:disabled {
        cursor: default;
        opacity: 0.6;
    }

    /* Said over the numbers rather than instead of them: counters half a
       minute old are worth far more than an empty page. */
    .stale {
        margin: 0 0 8px;
        font-size: 11px;
        color: var(--warn);
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

    /* Coloured by its worst news, so it is the first thing on the page read
       as trouble -- and absent entirely on a cluster with none. */
    .attention {
        --tone: var(--warn);
        border: 1px solid color-mix(in srgb, var(--tone) 45%, var(--border));
        border-left: 3px solid var(--tone);
        background: color-mix(in srgb, var(--tone) 7%, var(--bg-panel));
        border-radius: var(--radius);
        padding: 12px 14px;
        margin: 0 0 26px;
    }

    .attention.error {
        --tone: var(--error);
    }

    .attention h2 {
        display: flex;
        align-items: center;
        gap: 6px;
        color: var(--tone);
        margin-bottom: 10px;
    }

    .chips {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
    }

    .chip {
        display: inline-flex;
        align-items: baseline;
        gap: 5px;
        padding: 4px 10px;
        font: inherit;
        font-size: 12px;
        color: var(--text-dim);
        background: var(--bg-panel);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        cursor: pointer;
    }

    .chip:hover {
        border-color: var(--ctx-color, var(--accent));
        color: var(--text);
    }

    .chip .n {
        font-size: 15px;
        font-weight: 600;
        font-variant-numeric: tabular-nums;
    }

    .chip.error .n {
        color: var(--error);
    }

    .chip.warn .n {
        color: var(--warn);
    }

    .worst {
        list-style: none;
        margin: 10px 0 0;
        padding: 0;
        display: grid;
        gap: 1px;
    }

    .worst button {
        display: grid;
        grid-template-columns: 130px minmax(0, 1fr) auto auto;
        grid-auto-rows: auto;
        gap: 12px;
        align-items: baseline;
        width: 100%;
        padding: 3px 6px;
        font: inherit;
        font-size: 12px;
        text-align: left;
        color: var(--text);
        background: transparent;
        border: 0;
        border-radius: var(--radius-sm);
        cursor: pointer;
    }

    .worst button:hover {
        background: var(--bg-hover);
    }

    .worst .reason {
        font-weight: 600;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .worst .reason.error {
        color: var(--error);
    }

    .worst .reason.warn {
        color: var(--warn);
    }

    .worst .name {
        font-family: var(--mono);
        font-size: 11.5px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .worst .ns,
    .worst .restarts {
        color: var(--text-faint);
        white-space: nowrap;
    }

    .worst .restarts {
        font-variant-numeric: tabular-nums;
    }

    /* Why it last stopped, under the name: the restart count says how often,
       this says what to fix. */
    .worst .why {
        grid-column: 2 / -1;
        margin-top: -2px;
        font-size: 11px;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .worst .reason.faded {
        opacity: 0.7;
        font-weight: 500;
    }

    .chip.quiet .n {
        color: var(--text-dim);
    }

    .cleanup {
        display: flex;
        align-items: center;
        flex-wrap: wrap;
        gap: 8px;
        margin-top: 10px;
    }

    .cleanup .question {
        margin: 0;
        font-size: 12px;
        color: var(--text);
        flex-basis: 100%;
    }

    .cleanup button {
        display: inline-flex;
        align-items: center;
        gap: 5px;
        height: 24px;
        padding: 0 10px;
        font: inherit;
        font-size: 11.5px;
        border-radius: var(--radius-sm);
        cursor: pointer;
    }

    .cleanup .plain {
        color: var(--text-dim);
        background: var(--bg-panel);
        border: 1px solid var(--border);
    }

    .cleanup .plain:hover:not(:disabled) {
        color: var(--text);
    }

    .cleanup .danger {
        color: #fff;
        background: var(--error);
        border: 1px solid var(--error);
    }

    .cleanup button:disabled {
        opacity: 0.6;
        cursor: default;
    }

    .head .cred.warn dd {
        color: var(--warn);
        font-weight: 600;
    }

    .head .cred.error dd {
        color: var(--error);
        font-weight: 600;
    }

    .head .cred dd {
        display: inline-flex;
        align-items: center;
        gap: 4px;
    }

    .head .cred .reveal {
        display: inline-flex;
        align-items: center;
        gap: 4px;
        margin-left: 6px;
        padding: 1px 6px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: transparent;
        color: var(--text-dim);
        font: inherit;
        font-weight: 400;
        cursor: pointer;
    }

    .head .cred .reveal:hover {
        color: var(--text);
        border-color: var(--text-faint);
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
