<!--
  A terminal: one container, or one node, with a shell running in it -- or a
  shell on this machine (a local terminal), scoped to one cluster or to none.

  What is on screen is xterm, and xterm does not live here. It lives in the
  terminals store, because this component is destroyed and rebuilt every time
  you switch dock tabs and a shell cannot survive that: a directory you have cd'd
  into, a command half typed, an editor you have open -- none of it exists
  anywhere but in the session itself. So the store holds the terminal and this
  borrows it, attaching it to the element below on the way in and letting go on
  the way out without closing anything.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import '@xterm/xterm/css/xterm.css';
    import { singularFor } from '../catalogue';
    import { alpha } from '../colors';
    import { session } from '../state/session.svelte';
    import { DEBUG_SESSION, LOCAL_SHELL, terminals, TOOL_CONSOLE } from '../state/terminals.svelte';
    import { notices } from '../state/notices.svelte';
    import { workspace, type DockTab } from '../state/workspace.svelte';
    import ErrorState from './ErrorState.svelte';
    import { rememberSection } from './settings/section.svelte';
    import Icon from './Icon.svelte';

    interface Props {
        tab: DockTab;
    }

    let { tab }: Props = $props();

    let doc = $derived(terminals.doc(tab.id));
    let color = $derived(workspace.colorOf(tab.contextId));
    let onNode = $derived(tab.kind === 'nodes');
    /** A plugin tool's console rather than a shell: it has no container, and no other terminal to move to. */
    let onTool = $derived(tab.kind === TOOL_CONSOLE);
    /**
     * A kubectl debug session. It has no Reconnect: starting it again makes
     * something new in the cluster, which is the Debug button's question to
     * ask, not this bar's.
     */
    let onDebug = $derived(tab.kind === DEBUG_SESSION);
    /** A shell on this machine, rather than in a cluster. */
    let onLocal = $derived(tab.kind === LOCAL_SHELL);
    /** Whether the kept debug pod's delete is waiting on its confirmation. */
    let confirmingDelete = $state(false);
    let deleting = $state(false);

    let host = $state<HTMLElement | null>(null);

    /**
     * Whether more than one pod could be attached to, which is what decides
     * whether the picker has to name pods as well as containers. For one pod
     * the prefix would be the same on every button.
     */
    let manyPods = $derived(new Set(doc.containers.map((c) => c.pod)).size > 1);

    // Opens the session the first time this tab is shown. One already in the
    // store is left exactly as it was, which is what makes switching dock tabs
    // safe.
    $effect(() => {
        void terminals.open(tab.id, tab);
    });

    // The colours and type size follow the app's own. Re-run when the theme
    // changes, because xterm draws to a canvas and cannot inherit anything.
    $effect(() => {
        workspace.activeTheme;
        terminals.dress(workspace.terminalFontSize, workspace.terminalScrollback);
    });

    onMount(() => {
        if (!host) return;
        terminals.attach(tab.id, host);
        terminals.focus(tab.id);

        // The dock is resized by dragging, by folding, and by the window
        // itself. A terminal that is not refitted keeps drawing at the size it
        // was built at, and everything full-screen inside it -- an editor, top
        // -- draws to the wrong edge.
        const watching = new ResizeObserver(() => terminals.resize(tab.id));
        watching.observe(host);
        return () => watching.disconnect();
    });

    /** Attaches to a different container, which starts a new session. */
    function choose(pod: string, container: string): void {
        void terminals.choose(tab.id, tab, pod, container);
    }

    let subject = $derived(
        onTool
            ? `${tab.name} console`
            : onLocal
              ? tab.contextId
                  ? `local terminal for ${tab.name}`
                  : 'local terminal'
              : onDebug
              ? `debugging ${tab.name}`
              : onNode
                ? `node ${tab.name}`
                : `${singularFor(tab.kind)} ${tab.name}`,
    );

    /** Deletes the pod a kept debug session made, once confirmed. */
    async function deleteDebugPod(): Promise<void> {
        const pod = doc.created;
        deleting = true;
        try {
            await terminals.removeDebugPod(tab.id, tab.contextId);
            notices.inform(`Debug pod ${pod} deleted`);
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            deleting = false;
            confirmingDelete = false;
        }
    }

    /** What the bar says about where the session actually is. */
    let where = $derived.by(() => {
        // A local terminal's "pod" is the shell it runs.
        if (onLocal) return doc.pod;
        if (doc.node) return doc.pod ? `${doc.node} — via ${doc.pod}` : doc.node;
        if (!doc.pod) return '';
        return doc.pod === tab.name ? doc.container : `${doc.pod} · ${doc.container}`;
    });
</script>

<div class="shell" style:--ctx-color={color} style:--ctx-tint={alpha(color, 0.12)}>
    <div class="bar">
        {#if onDebug}
            <span class="tag" title="A kubectl debug session">debug</span>
        {/if}
        {#if onLocal}
            <span
                class="tag local"
                title={tab.contextId
                    ? 'A shell on this machine. Its KUBECONFIG holds this cluster’s context alone, so kubectl and helm talk to this cluster.'
                    : 'A shell on this machine'}>local</span
            >
        {/if}
        <span class="what" title={subject}>{tab.name}</span>

        {#if where}
            <span class="where" title="The session is attached to this">{where}</span>
        {/if}

        {#if doc.containers.length > 1}
            <div class="picker" role="group" aria-label="Containers to open a shell in">
                {#each doc.containers as container (container.pod + container.container)}
                    {@const on = doc.pod === container.pod && doc.container === container.container}
                    <button
                        class:on
                        aria-pressed={on}
                        title={manyPods
                            ? `Open a shell in ${container.container} in ${container.pod}`
                            : `Open a shell in ${container.container}`}
                        onclick={() => choose(container.pod, container.container)}
                    >
                        {container.container}
                    </button>
                {/each}
            </div>
        {/if}

        <span class="state {doc.status}">
            {#if doc.status === 'opening'}
                Opening…
            {:else if doc.status === 'running'}
                {onLocal ? 'Running' : 'Connected'}
            {:else if doc.status === 'ended'}
                {doc.exitCode !== null ? `Exited (${doc.exitCode})` : 'Ended'}
            {:else if doc.status === 'error'}
                Failed
            {/if}
        </span>

        {#if onDebug && doc.created && doc.kept && doc.status !== 'opening'}
            {#if confirmingDelete}
                <span class="ask">Delete pod {doc.created}?</span>
                <button class="toggle" onclick={() => (confirmingDelete = false)}>Cancel</button>
                <button class="toggle danger" disabled={deleting} onclick={deleteDebugPod}>Delete</button>
            {:else}
                <button
                    class="toggle"
                    title="Delete the pod this debug session made and kept"
                    onclick={() => (confirmingDelete = true)}
                >
                    <Icon name="trash" size={13} />
                    Delete pod
                </button>
            {/if}
        {/if}

        {#if (doc.status === 'ended' || doc.status === 'error') && !onDebug}
            <button class="toggle" onclick={() => terminals.restart(tab.id, tab)}>
                <Icon name="refresh" size={13} />
                {onLocal ? 'Restart' : 'Reconnect'}
            </button>
        {/if}

        <!-- Not in the web version: "your own terminal" would be one on the
             server, where nobody is looking. -->
        {#if !session.server && !onTool && !onDebug && !onLocal}
            <button
                class="toggle"
                title="Open this shell in your own terminal instead"
                onclick={() => workspace.openExternalShell(tab)}
            >
                <Icon name="terminal" size={13} />
                External
            </button>
        {/if}
    </div>

    {#if doc.status === 'error' && !doc.pod && !doc.node}
        <!-- A session that never opened has nothing on screen to read, so the
             failure is shown as a page rather than as a line in a terminal
             nobody can see. -->
        <ErrorState message={doc.error} compact />
        {#if onLocal}
            <div class="fix">
                <button
                    class="toggle"
                    onclick={() => {
                        rememberSection('terminal');
                        workspace.openSettings();
                    }}
                >
                    <Icon name="settings" size={13} />
                    Open Settings
                </button>
            </div>
        {/if}
    {/if}

    <div class="body" bind:this={host}></div>
</div>

<style>
    .shell {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
    }

    .bar {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 6px 10px;
        border-bottom: 1px solid var(--border);
        background: var(--ctx-tint);
        border-left: 3px solid var(--ctx-color);
        flex: 0 0 auto;
    }

    .what {
        font-size: 12px;
        font-weight: 600;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        max-width: 220px;
    }

    .where {
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-faint);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        max-width: 260px;
    }

    .picker {
        display: flex;
        gap: 3px;
        flex-wrap: wrap;
        min-width: 0;
    }

    .picker button {
        height: 20px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-faint);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        white-space: nowrap;
    }

    .picker button.on {
        color: var(--text);
        background: var(--bg-active);
    }

    .state {
        margin-left: auto;
        font-size: 11px;
        color: var(--text-faint);
        white-space: nowrap;
    }

    .state.running {
        color: var(--ok);
    }

    .state.error {
        color: var(--error);
    }

    .toggle {
        display: flex;
        align-items: center;
        gap: 5px;
        height: 22px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        color: var(--text-dim);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        white-space: nowrap;
    }

    .tag {
        font-size: 10.5px;
        padding: 1px 6px;
        border-radius: var(--radius-sm);
        color: var(--warn);
        box-shadow: inset 0 0 0 1px var(--warn);
        text-transform: uppercase;
        letter-spacing: 0.04em;
    }

    .tag.local {
        color: var(--accent);
        box-shadow: inset 0 0 0 1px var(--accent);
    }

    .fix {
        display: flex;
        justify-content: center;
        padding: 0 10px 10px;
    }

    .ask {
        font-size: 11.5px;
        color: var(--text);
        white-space: nowrap;
    }

    .toggle.danger {
        color: var(--error);
    }

    .toggle:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    /* The terminal itself. It is given the room and nothing else: xterm sizes
       its own canvas to whatever this element turns out to be, which is why the
       resize observer above matters more than any rule here. */
    .body {
        flex: 1 1 auto;
        min-height: 0;
        overflow: hidden;
        padding: 4px 0 0 8px;
        background: var(--bg);
    }

    .body :global(.xterm) {
        height: 100%;
    }

    .body :global(.xterm-viewport)::-webkit-scrollbar {
        width: 10px;
    }

    .body :global(.xterm-viewport)::-webkit-scrollbar-thumb {
        background: var(--scrollbar);
        border-radius: 5px;
    }

    .body :global(.xterm-viewport)::-webkit-scrollbar-thumb:hover {
        background: var(--scrollbar-hover);
    }
</style>
