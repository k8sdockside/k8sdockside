<!--
  The namespace filter: a dropdown of checkboxes rather than a select, so a
  table can show two namespaces side by side -- a team's and kube-system, say
  -- which a single choice cannot. Nothing ticked means every namespace, the way
  the old picker's "All namespaces" did: a filter that showed nothing until
  something was ticked would read as an empty cluster.

  Built the way the title bar's menus are: a trigger, a panel under it that closes on a
  click anywhere else or on Escape, and arrow keys between the rows. Unlike a
  menu it stays open while rows are ticked, since ticking three is the point.
-->
<script lang="ts">
    import Icon from './Icon.svelte';

    interface Props {
        /** Every namespace the cluster has. */
        namespaces: string[];
        /** The ones ticked; empty means all of them. */
        selected: string[];
        onchange: (next: string[]) => void;
    }

    let { namespaces, selected, onchange }: Props = $props();

    let open = $state(false);
    let query = $state('');
    let menuEl = $state<HTMLElement | null>(null);
    let buttonEl = $state<HTMLButtonElement | null>(null);
    let searchEl = $state<HTMLInputElement | null>(null);

    /** Enough rows that a filter box earns its place. */
    const SEARCHABLE = 8;

    /**
     * The rows: the cluster's namespaces, plus any ticked one the cluster no
     * longer has -- deleted since it was ticked -- so it can still be unticked
     * rather than filtering the table from somewhere it cannot be seen.
     */
    let rows = $derived.by(() => {
        const known = new Set(namespaces);
        const gone = selected.filter((ns) => !known.has(ns)).sort();
        return [...namespaces, ...gone];
    });

    let shown = $derived.by(() => {
        const needle = query.trim().toLowerCase();
        return needle ? rows.filter((ns) => ns.toLowerCase().includes(needle)) : rows;
    });

    /** What the trigger reads: the choice, in as few words as it takes. */
    let summary = $derived(
        selected.length === 0
            ? 'All namespaces'
            : selected.length <= 2
              ? selected.join(', ')
              : `${selected.length} namespaces`,
    );

    function toggle(ns: string): void {
        onchange(selected.includes(ns) ? selected.filter((s) => s !== ns) : [...selected, ns].sort());
    }

    function close(): void {
        open = false;
        query = '';
    }

    function onKeyDown(event: KeyboardEvent): void {
        if (event.key === 'Escape') {
            event.stopPropagation();
            close();
            buttonEl?.focus();
            return;
        }
        if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;

        event.preventDefault();
        const items = [...(menuEl?.querySelectorAll<HTMLElement>('input, button') ?? [])];
        const at = items.indexOf(document.activeElement as HTMLElement);
        const next = (at + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
        items[next]?.focus();
    }

    // Focus goes to the filter box when there is one, and to the first row
    // otherwise, so the panel is usable without the mouse that opened it.
    $effect(() => {
        if (!open || !menuEl) return;
        (searchEl ?? menuEl.querySelector<HTMLElement>('button'))?.focus();
    });
</script>

<svelte:window onclick={close} onresize={close} />

<!-- The click that opens the panel must not reach the window handler that
     closes it again, and neither must a click on a row inside it. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="host" onclick={(e) => e.stopPropagation()}>
    <button
        class="trigger"
        bind:this={buttonEl}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Namespaces: {summary}"
        title="Which namespaces to show. Nothing ticked shows them all."
        onclick={() => (open = !open)}
    >
        <span class="lead">Namespace</span>
        <span class="value">{summary}</span>
        <Icon name="chevron-down" size={12} />
    </button>

    {#if open}
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <!-- svelte-ignore a11y_interactive_supports_focus -->
        <div class="menu" role="menu" aria-label="Namespaces" bind:this={menuEl} onkeydown={onKeyDown}>
            {#if rows.length > SEARCHABLE}
                <input
                    bind:this={searchEl}
                    bind:value={query}
                    type="search"
                    placeholder="Filter namespaces"
                    aria-label="Filter namespaces"
                    spellcheck="false"
                />
            {/if}

            <button role="menuitemcheckbox" aria-checked={selected.length === 0} onclick={() => onchange([])}>
                <span class="tick">{#if selected.length === 0}<Icon name="check" size={13} />{/if}</span>
                <span class="label">All namespaces</span>
            </button>

            <hr />

            <div class="list">
                {#each shown as ns (ns)}
                    {@const on = selected.includes(ns)}
                    <button role="menuitemcheckbox" aria-checked={on} onclick={() => toggle(ns)}>
                        <span class="tick">{#if on}<Icon name="check" size={13} />{/if}</span>
                        <span class="label">{ns}</span>
                    </button>
                {:else}
                    <p class="none">No namespace matches “{query}”.</p>
                {/each}
            </div>
        </div>
    {/if}
</div>

<style>
    .host {
        position: relative;
        display: flex;
        align-items: center;
        z-index: 5;
    }

    .trigger {
        display: flex;
        align-items: center;
        gap: 6px;
        height: 24px;
        padding: 0 6px 0 0;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-dim);
        max-width: 280px;
    }

    .trigger:hover,
    .trigger[aria-expanded='true'] {
        color: var(--text);
    }

    .lead {
        flex: 0 0 auto;
    }

    /* Drawn like the select it replaces, so the toolbar reads as before. */
    .value {
        min-width: 0;
        padding: 3px 6px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg);
        font-size: 12px;
        color: var(--text);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .trigger[aria-expanded='true'] .value {
        border-color: var(--accent);
    }

    .menu {
        position: absolute;
        top: calc(100% + 4px);
        left: 0;
        min-width: 240px;
        padding: 4px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
    }

    .menu input {
        width: 100%;
        margin-bottom: 4px;
        padding: 4px 8px;
        font: inherit;
        font-size: 12px;
        color: var(--text);
        background: var(--bg);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        outline: none;
    }

    .menu input:focus {
        border-color: var(--accent);
    }

    .menu button {
        display: flex;
        align-items: center;
        gap: 8px;
        width: 100%;
        height: 26px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text);
        text-align: left;
    }

    .menu button:hover,
    .menu button:focus-visible {
        background: var(--bg-hover);
    }

    /* A fixed column for the tick, so the names line up whether or not one is
       there and nothing shifts sideways as a row is ticked. */
    .tick {
        display: grid;
        place-items: center;
        width: 14px;
        flex: 0 0 auto;
        color: var(--accent);
    }

    .label {
        flex: 1 1 auto;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-family: var(--mono);
    }

    /* Long lists scroll inside the panel rather than growing past the window. */
    .list {
        max-height: 280px;
        overflow: auto;
    }

    .none {
        margin: 0;
        padding: 8px;
        font-size: 12px;
        color: var(--text-faint);
    }

    hr {
        height: 1px;
        margin: 4px 6px;
        border: 0;
        background: var(--border-soft);
    }
</style>
