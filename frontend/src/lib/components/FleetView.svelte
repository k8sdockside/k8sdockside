<!--
  Every cluster's health on one page.

  The dashboard answers "how is this cluster"; with fifteen of them that is
  fifteen tabs to open to find the one that is not fine. This is the one page
  that asks all of them the few questions worth asking on a timer -- are the
  nodes up, which pods are in trouble, how much has it been complaining, when
  do its credentials run out -- and lists the answers side by side, worst
  first. A row opens that cluster's dashboard, which is where to go next.

  Only the clusters connected in this window are read on their own; the rest
  are listed and left alone until somebody asks, because connecting can mean
  a login. See fleet.svelte.ts.
-->
<script lang="ts">
    import { formatDate, formatTime } from '../datetime.svelte';
    import { DASHBOARD } from '../catalogue';
    import { credentialTone, inDays, troubleOf } from '../fleet/alerts';
    import { fleet, FLEET_POLL_MS, type FleetEntry } from '../state/fleet.svelte';
    import { workspace } from '../state/workspace.svelte';
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import Icon from './Icon.svelte';

    let checkingAll = $state(false);
    let checkingNow = $state(false);

    interface Row {
        id: string;
        name: string;
        color: string;
        connected: boolean;
        entry: FleetEntry;
    }

    /** How bad a row is, for putting the worst at the top. */
    function rank(row: Row): number {
        if (!row.connected && !row.entry.health) return 0;
        if (row.entry.health?.error) return 4;
        const trouble = troubleOf(row.entry.health, row.entry.credentials);
        if (!trouble) return 1;
        return trouble.tone === 'error' ? 3 : 2;
    }

    let rows = $derived.by(() => {
        const all: Row[] = workspace.contexts.map((c) => ({
            id: c.id,
            name: workspace.displayName(c),
            color: workspace.colorOf(c.id),
            connected: workspace.isConnected(c.id),
            entry: fleet.of(c.id),
        }));
        // Stable within a rank, so the sidebar's order is kept among equals.
        return all
            .map((row, index) => ({ row, index }))
            .sort((a, b) => rank(b.row) - rank(a.row) || a.index - b.index)
            .map(({ row }) => row);
    });

    let watched = $derived(rows.filter((r) => r.connected).length);
    let unwatched = $derived(rows.length - watched);

    async function checkNow(): Promise<void> {
        checkingNow = true;
        try {
            await fleet.tick();
        } finally {
            checkingNow = false;
        }
    }

    async function checkAll(): Promise<void> {
        checkingAll = true;
        try {
            await fleet.checkAll();
        } finally {
            checkingAll = false;
        }
    }

    function open(row: Row): void {
        workspace.openTab(row.id, DASHBOARD);
    }

    /** The credential that runs out first and has a date, if any does. */
    function soonest(entry: FleetEntry): kube.Credential | null {
        return entry.credentials?.items.find((item) => item.notAfter) ?? null;
    }

    function credentialsTitle(entry: FleetEntry): string {
        const items = entry.credentials?.items ?? [];
        if (entry.credentials?.error) return entry.credentials.error;
        return items
            .map((item) => {
                const what = item.subject ? `${item.kind} — ${item.subject}` : item.kind;
                if (item.error) return `${what}: ${item.error}`;
                if (!item.notAfter) return `${what}: ${item.note}`;
                return `${what}: ${formatDate(item.notAfter)} (${inDays(item.daysLeft)})`;
            })
            .join('\n');
    }

    function checkedAgo(at: number | null): string {
        if (at === null) return '';
        const seconds = Math.round((Date.now() - at) / 1000);
        if (seconds < 45) return 'just now';
        const minutes = Math.round(seconds / 60);
        if (minutes < 60) return `${minutes}m ago`;
        return formatTime(at);
    }

    /** A count, coloured when it is not zero. */
    function tone(n: number, when: 'error' | 'warn'): string {
        return n > 0 ? when : 'zero';
    }

    // Redrawn now and then so "just now" becomes "2m ago" without a reading.
    let clock = $state(0);
    $effect(() => {
        const timer = setInterval(() => clock++, 30_000);
        return () => clearInterval(timer);
    });
</script>

<div class="fleet">
    <header>
        <div class="title">
            <h2>Fleet health</h2>
            <div class="buttons">
                <button onclick={checkNow} disabled={checkingNow || watched === 0} title="Read the connected clusters again now">
                    <Icon name="refresh" size={12} />
                    {checkingNow ? 'Reading…' : 'Check now'}
                </button>
                {#if unwatched > 0}
                    <button
                        onclick={checkAll}
                        disabled={checkingAll}
                        title="Connects to every cluster, including the {unwatched} not connected yet. That can run login plugins and ask you to sign in."
                    >
                        <Icon name="gauge" size={12} />
                        {checkingAll ? 'Checking…' : `Check all ${rows.length}`}
                    </button>
                {/if}
            </div>
        </div>
        <p>
            {#if watched === 0}
                No cluster is connected in this window yet. Open one, or check them all, and its health appears here.
            {:else}
                The {watched === 1 ? 'cluster' : `${watched} clusters`} connected in this window, read every
                {Math.round(FLEET_POLL_MS / 1000)} seconds, worst first. A row opens that cluster's dashboard.
            {/if}
        </p>
    </header>

    <table>
        <thead>
            <tr>
                <th scope="col">Cluster</th>
                <th scope="col">Nodes</th>
                <th scope="col">Pods</th>
                <th scope="col" title="Pods with a container that will not come up: crash loops, image pulls">Not starting</th>
                <th scope="col">Evicted</th>
                <th scope="col">Failed</th>
                <th scope="col" title="Pods that restarted a container in the last hour">Restarted (1h)</th>
                <th scope="col" title="Warning events in the last hour, with the commonest reason">Warnings (1h)</th>
                <th scope="col">Credentials</th>
                <th scope="col">Checked</th>
            </tr>
        </thead>
        <tbody>
            {#each rows as row (row.id)}
                {@const health = row.entry.health}
                {@const cred = soonest(row.entry)}
                <!-- The whole row opens the dashboard for the pointer; the
                     name is the same thing as a button, for the keyboard. -->
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <tr class:quiet={!health} onclick={() => open(row)}>
                    <td class="cluster">
                        <span class="swatch" style:background={row.color}></span>
                        <button class="name" onclick={(e) => { e.stopPropagation(); open(row); }} title="Open {row.name}'s dashboard">
                            {row.name}
                        </button>
                    </td>
                    {#if !health}
                        <td colspan="7" class="dim">
                            {#if row.entry.checking}
                                Reading…
                            {:else if row.connected}
                                Waiting for the first reading.
                            {:else}
                                Not connected.
                                <button
                                    class="inline"
                                    onclick={(e) => {
                                        e.stopPropagation();
                                        void fleet.check(row.id, { credentials: true });
                                    }}
                                    title="Connect to this cluster and read its health"
                                >
                                    Check
                                </button>
                            {/if}
                        </td>
                    {:else if health.error}
                        <td colspan="7" class="unreachable" title={health.error}>
                            <Icon name="alert" size={12} />
                            <span>{health.error}</span>
                        </td>
                    {:else}
                        <td class="num {health.nodesReady < health.nodesTotal ? 'error' : 'ok'}"
                            title={health.notReadyNodes.length ? `Not ready: ${health.notReadyNodes.join(', ')}` : ''}>
                            {health.nodesReady}<span class="of">/{health.nodesTotal}</span>
                        </td>
                        <td class="num">{health.podsRunning}<span class="of">/{health.podsTotal}</span></td>
                        <td class="num {tone(health.pods.crashLooping, 'error')}">{health.pods.crashLooping}</td>
                        <td class="num {tone(health.pods.evicted, 'warn')}">{health.pods.evicted}</td>
                        <td class="num {tone(health.pods.failed, 'warn')}">{health.pods.failed}</td>
                        <td class="num {tone(health.pods.restartedRecently, 'warn')}"
                            title="{health.pods.restarting} pods have restarted at some point, {health.pods.restarts} restarts in all">
                            {health.pods.restartedRecently}
                        </td>
                        <td class="warnings" title={health.warningReasons.map((r) => `${r.reason}: ${r.count}`).join('\n')}>
                            <span class="num {tone(health.warnings, 'warn')}">{health.warnings}</span>
                            {#if health.warningReasons[0]}
                                <span class="reason">{health.warningReasons[0].reason}</span>
                            {/if}
                        </td>
                    {/if}
                    <td class="cred" title={credentialsTitle(row.entry)}>
                        {#if cred}
                            <span class={credentialTone(cred.daysLeft, cred.notAfter) ?? 'fine'}>
                                {cred.daysLeft < 0 ? 'expired' : inDays(cred.daysLeft)}
                            </span>
                            <span class="kind">{cred.kind}</span>
                        {:else if row.entry.credentials}
                            <span class="dim">no dates</span>
                        {/if}
                    </td>
                    <td class="dim when">
                        {#key clock}{checkedAgo(row.entry.checkedAt)}{/key}
                    </td>
                </tr>
            {/each}
        </tbody>
    </table>
</div>

<style>
    .fleet {
        height: 100%;
        overflow: auto;
        padding: 20px 24px 32px;
    }

    header {
        margin-bottom: 18px;
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
        margin-bottom: 4px;
    }

    h2 {
        margin: 0;
        font-size: 15px;
        font-weight: 600;
    }

    .buttons {
        margin-left: auto;
        display: flex;
        gap: 6px;
    }

    .buttons button {
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
    }

    .buttons button:hover:not(:disabled) {
        color: var(--text);
        border-color: var(--accent);
    }

    .buttons button:disabled {
        opacity: 0.6;
        cursor: default;
    }

    header p {
        margin: 0;
        font-size: 12px;
        line-height: 1.6;
        color: var(--text-dim);
        max-width: 80ch;
    }

    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 12px;
    }

    th {
        text-align: left;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
        font-weight: 600;
        padding: 0 12px 6px 0;
        border-bottom: 1px solid var(--border);
        white-space: nowrap;
    }

    td {
        padding: 7px 12px 7px 0;
        border-bottom: 1px solid var(--border-soft);
        vertical-align: middle;
        white-space: nowrap;
    }

    tbody tr {
        cursor: pointer;
    }

    tbody tr:hover {
        background: var(--bg-hover);
    }

    tr.quiet .name {
        color: var(--text-dim);
    }

    .cluster {
        display: flex;
        align-items: center;
        gap: 8px;
        padding-left: 4px;
    }

    .swatch {
        width: 8px;
        height: 8px;
        border-radius: 2px;
        flex: none;
    }

    .name {
        font: inherit;
        font-weight: 500;
        color: var(--text);
        background: none;
        border: 0;
        padding: 0;
        cursor: pointer;
    }

    .num {
        font-variant-numeric: tabular-nums;
        font-weight: 600;
    }

    .of {
        font-weight: 400;
        color: var(--text-faint);
    }

    .ok {
        color: var(--ok);
    }

    .warn {
        color: var(--warn);
    }

    .error {
        color: var(--error);
    }

    .zero {
        color: var(--text-faint);
        font-weight: 400;
    }

    .dim {
        color: var(--text-faint);
    }

    .unreachable {
        color: var(--error);
        max-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .unreachable span {
        margin-left: 4px;
    }

    .warnings .reason {
        margin-left: 6px;
        color: var(--text-dim);
    }

    .cred .kind {
        margin-left: 6px;
        color: var(--text-faint);
    }

    .cred .fine {
        color: var(--text-dim);
    }

    .cred .warn,
    .cred .error {
        font-weight: 600;
    }

    .inline {
        margin-left: 6px;
        font: inherit;
        font-size: 11px;
        color: var(--accent);
        background: none;
        border: 0;
        padding: 0;
        cursor: pointer;
        text-decoration: underline;
        text-underline-offset: 2px;
    }
</style>
