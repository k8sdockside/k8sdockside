<!--
  The port forwards open against one cluster.

  A forward is a thing with a lifetime rather than a thing in the cluster, so
  this is not a resource table: the rows come from the app's own list, they
  change when a tunnel comes up or drops rather than when the cluster changes,
  and each carries the two buttons that are the whole point -- disconnect, and
  connect again.

  Forwards from other clusters are deliberately not here. A tunnel goes to one
  cluster, and a list mixing several would be a list you had to read the fine
  print of before clicking anything in it.

  The web version has none: localhost there is the server. Nothing in it opens
  this view, but a tab for it can still arrive from a layout saved before, and
  it says why it is empty rather than offering buttons the backend refuses.
-->
<script lang="ts">
    import { singularFor } from '../catalogue';
    import { forwards, type Forward } from '../state/forwards.svelte';
    import { session } from '../state/session.svelte';
    import { workspace } from '../state/workspace.svelte';
    import Icon from './Icon.svelte';
    import { notices } from '../state/notices.svelte';

    interface Props {
        contextId: string;
    }

    let { contextId }: Props = $props();

    let rows = $derived(forwards.forContext(contextId));
    let cluster = $derived.by(() => {
        const context = workspace.contexts.find((c) => c.id === contextId);
        return context ? workspace.displayName(context) : contextId;
    });
    /** Which row is waiting on a call, so its buttons can be held. */
    let busy = $state<string | null>(null);

    async function reconnect(forward: Forward): Promise<void> {
        busy = forward.id;
        try {
            await forwards.reconnect(forward.id);
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            busy = null;
        }
    }

    async function open(forward: Forward): Promise<void> {
        try {
            await forwards.open(forward.id);
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        }
    }

    async function forget(forward: Forward): Promise<void> {
        busy = forward.id;
        try {
            await forwards.forget(forward.id);
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            busy = null;
        }
    }

    function stateLabel(forward: Forward): string {
        switch (forward.state) {
            case 'active':
                return 'Connected';
            case 'connecting':
                return 'Connecting…';
            case 'error':
                return 'Failed';
            default:
                return 'Disconnected';
        }
    }

    /** What the row is forwarding to, in the words the user chose it by. */
    function subject(forward: Forward): string {
        const what = `${singularFor(forward.kind)} ${forward.name}`;
        return forward.namespace ? `${what} in ${forward.namespace}` : what;
    }
</script>

<div class="forwards">
    {#if session.server}
        <header>
            <h2>Port forwards</h2>
            <p>
                Port forwards are part of the desktop app. A forward is a port on the machine running K8s Dockside,
                and here that machine is the server rather than yours.
            </p>
        </header>
    {:else}
    <header>
        <h2>Port forwards</h2>
        <p>
            Tunnels from this machine into
            <strong>{cluster}</strong>.
            Start one from the <em>Forward</em> button in any pod or service's details panel. They are remembered
            between sessions, and reconnected when you ask rather than at launch.
        </p>
    </header>

    {#if rows.length === 0}
        <p class="empty">
            Nothing is being forwarded from this cluster. Select a service or a pod, then choose
            <em>Forward</em> in the panel that slides in.
        </p>
    {:else}
        <table>
            <thead>
                <tr>
                    <th scope="col">Local</th>
                    <th scope="col">Target</th>
                    <th scope="col">Remote port</th>
                    <th scope="col">State</th>
                    <th scope="col"><span class="sr">Actions</span></th>
                </tr>
            </thead>
            <tbody>
                {#each rows as forward (forward.id)}
                    <tr class:live={forward.state === 'active'}>
                        <td class="mono">
                            {#if forward.state === 'active' && forward.localPort}
                                <button class="link" onclick={() => open(forward)} title="Open in your browser">
                                    localhost:{forward.localPort}
                                </button>
                            {:else if forward.localPort}
                                <span class="dim">localhost:{forward.localPort}</span>
                            {:else}
                                <span class="dim">any free port</span>
                            {/if}
                        </td>

                        <td>
                            <span class="what">{subject(forward)}</span>
                            <!-- What it actually reached, which for a service is
                                 a pod nobody named. -->
                            {#if forward.pod}
                                <span class="via mono">via {forward.pod}:{forward.podPort}</span>
                            {/if}
                        </td>

                        <td class="mono">{forward.remotePort}</td>

                        <td>
                            <span class="state {forward.state}">
                                <span class="dot"></span>
                                {stateLabel(forward)}
                            </span>
                            {#if forward.error}
                                <span class="why" title={forward.error}>{forward.error}</span>
                            {:else if forward.note && forward.state !== 'active'}
                                <span class="why" title={forward.note}>{forward.note}</span>
                            {/if}
                        </td>

                        <td class="actions">
                            {#if forward.state === 'active' || forward.state === 'connecting'}
                                <button onclick={() => forwards.stop(forward.id)} disabled={busy === forward.id}>
                                    <Icon name="close" size={12} />
                                    Disconnect
                                </button>
                            {:else}
                                <button onclick={() => reconnect(forward)} disabled={busy === forward.id}>
                                    <Icon name="refresh" size={12} />
                                    Reconnect
                                </button>
                            {/if}
                            <button class="drop" onclick={() => forget(forward)} disabled={busy === forward.id}>
                                <Icon name="trash" size={12} />
                                Remove
                            </button>
                        </td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {/if}
    {/if}
</div>

<style>
    .forwards {
        height: 100%;
        overflow: auto;
        padding: 20px 24px 32px;
    }

    header {
        max-width: 760px;
        margin-bottom: 18px;
    }

    h2 {
        margin: 0 0 4px;
        font-size: 15px;
        font-weight: 600;
    }

    header p,
    .empty {
        margin: 0;
        font-size: 12px;
        line-height: 1.6;
        color: var(--text-dim);
        max-width: 72ch;
    }

    .empty {
        padding: 24px 0;
        color: var(--text-faint);
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
    }

    td {
        padding: 8px 12px 8px 0;
        border-bottom: 1px solid var(--border-soft);
        vertical-align: top;
    }

    .mono {
        font-family: var(--mono);
    }

    .dim {
        color: var(--text-faint);
    }

    .what {
        display: block;
        color: var(--text);
    }

    .via {
        display: block;
        font-size: 11px;
        color: var(--text-faint);
    }

    .link {
        color: var(--accent);
        text-decoration: underline;
        text-underline-offset: 2px;
        font: inherit;
    }

    .state {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .dot {
        width: 7px;
        height: 7px;
        border-radius: 50%;
        background: var(--text-faint);
    }

    .state.active .dot {
        background: var(--ok);
    }

    .state.connecting .dot {
        background: var(--warn);
    }

    .state.error .dot {
        background: var(--error);
    }

    .why {
        display: block;
        margin-top: 3px;
        font-size: 11px;
        color: var(--warn);
        max-width: 40ch;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .actions {
        display: flex;
        gap: 6px;
        justify-content: flex-end;
    }

    .actions button {
        display: flex;
        align-items: center;
        gap: 5px;
        height: 24px;
        padding: 0 9px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        font-size: 11.5px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .actions button:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .actions button:disabled {
        opacity: 0.5;
    }

    .actions .drop:hover:not(:disabled) {
        color: var(--error);
    }

    .sr {
        position: absolute;
        width: 1px;
        height: 1px;
        overflow: hidden;
        clip-path: inset(50%);
    }
</style>
