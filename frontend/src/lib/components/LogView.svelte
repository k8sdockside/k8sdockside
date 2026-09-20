<!--
  A log view: one object's containers, followed.

  What is on screen is a window onto a stream the backend holds open. Choosing
  containers and turning following on and off change that stream rather than
  filtering what has already arrived -- holding every container's lines and
  showing a few of them would cost memory to show less.

  The lines live in the logs store, not here: this component is destroyed and
  rebuilt every time you switch dock tabs, and scrollback you have been reading
  must not go with it.
-->
<script lang="ts">
    import { tick } from 'svelte';
    import { singularFor } from '../catalogue';
    import { alpha } from '../colors';
    import { logs } from '../state/logs.svelte';
    import { workspace, type DockTab } from '../state/workspace.svelte';
    import ErrorState from './ErrorState.svelte';
    import Icon from './Icon.svelte';

    interface Props {
        tab: DockTab;
    }

    let { tab }: Props = $props();

    let doc = $derived(logs.doc(tab.id));
    let color = $derived(workspace.colorOf(tab.contextId));
    let query = $state('');

    let shown = $derived(
        query.trim() === ''
            ? doc.lines
            : doc.lines.filter((l) => l.text.toLowerCase().includes(query.trim().toLowerCase())),
    );

    /**
     * Whether more than one pod is in the view, which is what decides whether a
     * line needs to say which pod it came from. For one pod the prefix would be
     * the same on every line and only take room from the log.
     */
    let manyPods = $derived(new Set(doc.containers.map((c) => c.pod)).size > 1);

    let body = $state<HTMLElement | null>(null);
    /**
     * Whether new lines scroll the view. Following the tail is what you want
     * until you scroll up to read something, at which point having the ground
     * move under you is the opposite of what you want.
     */
    let pinned = $state(true);

    // Opens the view the first time this tab is shown. A view already in the
    // store is left as it was, which is what makes switching dock tabs safe.
    $effect(() => {
        void logs.open(tab.id, tab);
    });

    // Follow the tail as lines arrive, but only while the reader is at it.
    $effect(() => {
        shown.length;
        if (!pinned) return;
        void tick().then(() => {
            if (body) body.scrollTop = body.scrollHeight;
        });
    });

    /** Re-pins when the reader comes back to the bottom of their own accord. */
    function onScroll(): void {
        if (!body) return;
        // A few pixels of slack: a scroll rarely lands exactly on the end.
        pinned = body.scrollHeight - body.scrollTop - body.clientHeight < 24;
    }

    /** Adds or removes one container from what is being followed. */
    function toggle(name: string): void {
        const next = doc.selected.includes(name)
            ? doc.selected.filter((c) => c !== name)
            : [...doc.selected, name];
        // Following nothing would be an empty view with no way back to a full
        // one, so the last container cannot be turned off.
        if (next.length === 0) return;
        void logs.choose(tab.id, tab, next);
    }

</script>

<div class="logs" style:--ctx-color={color} style:--ctx-tint={alpha(color, 0.12)}>
    <div class="bar">
        <span class="what" title="{singularFor(tab.kind)} {tab.name}">{tab.name}</span>

        {#if doc.containers.length > 0}
            <div class="picker" role="group" aria-label="Containers to follow">
                {#each doc.containers as container (container.pod + container.container)}
                    <button
                        class:on={doc.selected.includes(container.container)}
                        aria-pressed={doc.selected.includes(container.container)}
                        title={manyPods
                            ? `${container.container} in ${container.pod}`
                            : container.container}
                        onclick={() => toggle(container.container)}
                    >
                        {container.container}
                    </button>
                {/each}
            </div>
        {/if}

        <label class="find">
            <Icon name="search" size={12} />
            <input placeholder="Filter lines" bind:value={query} aria-label="Filter lines" />
        </label>

        <button
            class="toggle"
            class:on={doc.follow}
            aria-pressed={doc.follow}
            title={doc.follow ? 'Stop following' : 'Follow'}
            onclick={() => logs.setFollow(tab.id, tab, !doc.follow)}
        >
            <Icon name={doc.follow ? 'refresh' : 'clock'} size={13} />
            Follow
        </button>

        <button
            class="toggle"
            class:nudge={doc.status === 'ended'}
            title="Open the stream again, from the beginning"
            onclick={() => void logs.reload(tab.id, tab)}
        >
            <Icon name="refresh" size={13} />
            Reload
        </button>

        <button class="toggle" title="Clear what is on screen" onclick={() => logs.clear(tab.id)}>
            Clear
        </button>
    </div>

    {#if doc.status === 'error'}
        <ErrorState message={doc.error} compact onRetry={() => void logs.reload(tab.id, tab)} />
    {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div class="body" bind:this={body} onscroll={onScroll} tabindex="0" role="log" aria-label="Log output">
            {#if doc.truncated}
                <p class="edge">Earlier lines have been dropped — this view keeps the most recent.</p>
            {/if}

            {#each shown as line, index (index)}
                <div class="line">
                    {#if manyPods}<span class="pod">{line.pod}</span>{/if}
                    <span class="container">{line.container}</span>
                    <span class="text selectable">{line.text}</span>
                </div>
            {:else}
                <p class="empty">
                    {#if doc.status === 'opening'}
                        Opening…
                    {:else if query.trim() !== ''}
                        No lines match “{query}”.
                    {:else if doc.status === 'ended'}
                        The stream ended, and nothing was logged. Reload to open it again.
                    {:else if doc.follow}
                        Nothing logged yet — this view is following, so lines appear as they are written.
                    {:else}
                        Nothing logged yet.
                    {/if}
                </p>
            {/each}
        </div>
    {/if}
</div>

<style>
    .logs {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
    }

    /* Wraps rather than overlaps. The container picker is as wide as the pod
       has containers, so on a narrow dock the buttons to its right were drawn
       over the top of it and took the clicks meant for a container. */
    .bar {
        display: flex;
        align-items: center;
        flex-wrap: wrap;
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

    /* A stream that has ended is the one moment Reload is the thing to press,
       so it is lit then and plain the rest of the time. */
    .toggle.nudge {
        border-color: var(--ctx-color);
        color: var(--ctx-color);
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

    /* A container not being followed is dimmed rather than removed: which
       containers exist should not change with what you are reading. */
    .picker button.on {
        color: var(--text);
        background: var(--bg-active);
    }

    .find {
        display: flex;
        align-items: center;
        gap: 5px;
        flex: none;
        margin-left: auto;
        padding: 0 8px;
        height: 22px;
        border-radius: var(--radius-sm);
        background: var(--bg);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text-faint);
    }

    .find input {
        width: 120px;
        border: none;
        background: none;
        color: var(--text);
        font: inherit;
        font-size: 11.5px;
        outline: none;
    }

    .toggle {
        display: flex;
        align-items: center;
        gap: 5px;
        flex: none;
        height: 22px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 11.5px;
        color: var(--text-dim);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        white-space: nowrap;
    }

    .toggle.on {
        color: var(--text);
        background: var(--bg-active);
    }

    .body {
        flex: 1 1 auto;
        overflow: auto;
        min-height: 0;
        padding: 6px 0 10px;
        font-family: var(--mono);
        font-size: 11.5px;
        line-height: 1.55;
    }

    .line {
        display: flex;
        gap: 8px;
        padding: 0 12px;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
    }

    .line:hover {
        background: var(--bg-hover);
    }

    .pod,
    .container {
        flex: 0 0 auto;
        color: var(--text-faint);
        user-select: none;
    }

    .text {
        flex: 1 1 auto;
        min-width: 0;
        color: var(--text);
    }

    .edge,
    .empty {
        margin: 0;
        padding: 10px 12px;
        color: var(--text-dim);
        font-family: var(--font);
        font-size: 12px;
    }

    .edge {
        color: var(--text-faint);
        border-bottom: 1px solid var(--border-soft);
    }
</style>
