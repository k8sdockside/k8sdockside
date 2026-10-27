<!--
  The heading of a section in the details panel that can be folded away: the
  whole row is the button, with a chevron in a box of its own at the start, so
  it reads as something to click rather than as a label with a mark beside it.
  What it folds, and remembering that, is the caller's.
-->
<script lang="ts">
    import type { Snippet } from 'svelte';
    import Icon from './Icon.svelte';

    interface Props {
        folded: boolean;
        ontoggle: () => void;
        /** What the section is, for the title: "the report", "this section". */
        what?: string;
        children: Snippet;
    }

    let { folded, ontoggle, what = 'this section', children }: Props = $props();
</script>

<button
    class="fold"
    class:folded
    aria-expanded={!folded}
    title={folded ? `Show ${what}, for every object` : `Fold ${what} away, for every object`}
    onclick={ontoggle}
>
    <span class="chevron" aria-hidden="true"><Icon name="chevron-down" size={13} /></span>
    <span class="label">{@render children()}</span>
    {#if folded}<span class="hint">Show</span>{/if}
</button>

<style>
    .fold {
        display: flex;
        align-items: center;
        gap: 8px;
        width: 100%;
        min-height: 26px;
        margin: 0 -6px;
        padding: 3px 6px;
        box-sizing: content-box;
        border: none;
        border-radius: var(--radius-sm);
        background: none;
        font: inherit;
        font-weight: 600;
        color: var(--text-dim);
        text-transform: inherit;
        letter-spacing: inherit;
        text-align: left;
        cursor: pointer;
    }

    .fold:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .fold:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: 1px;
    }

    .chevron {
        flex: 0 0 auto;
        display: flex;
        align-items: center;
        justify-content: center;
        width: 20px;
        height: 20px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        color: var(--text-dim);
        transition: transform 120ms ease;
    }

    .fold:hover .chevron {
        border-color: var(--accent);
        color: var(--accent);
    }

    .folded .chevron {
        transform: rotate(-90deg);
    }

    .label {
        flex: 1 1 auto;
        min-width: 0;
    }

    .hint {
        flex: 0 0 auto;
        font-size: 11px;
        text-transform: none;
        letter-spacing: 0;
        color: var(--text-faint);
    }
</style>
