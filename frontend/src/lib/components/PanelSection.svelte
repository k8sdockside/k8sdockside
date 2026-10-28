<!--
  One section of the details panel: a heading that folds it away, and what is
  under it.

  Every block the panel shows is one of these -- the app's own, and the frames
  plugins bring -- so they all look the same, fold the same way and remember it
  the same way: by key, for every object, on every cluster. A plugin page draws
  the same thing with k8sdockside.ui.section (internal/plugins/sdk), and its
  folds are kept in the same list; keep the two in step.
-->
<script lang="ts">
    import type { Snippet } from 'svelte';
    import { workspace } from '../state/workspace.svelte';
    import FoldButton from './FoldButton.svelte';

    interface Props {
        /** The key the fold is remembered under. */
        id: string;
        title: string;
        /** A number or a few words after the title: how many rows, of how many. */
        count?: number | string;
        /** Whose it is, for a plugin's: " · KubeVirt". */
        by?: string;
        /** What it is, for the fold button's title: "the conditions". */
        what?: string;
        /** Drawn after the title inside the heading, for what a plain title cannot say. */
        heading?: Snippet;
        /** Buttons on the heading's row, shown while the section is open. */
        actions?: Snippet;
        /** Drawn in a bordered box, as a summary block, rather than straight onto the panel. */
        boxed?: boolean;
        children: Snippet;
    }

    let { id, title, count, by, what, heading, actions, boxed = false, children }: Props = $props();

    let folded = $derived(workspace.isSectionFolded(id));
</script>

<section class="panel-section" class:folded class:boxed>
    <div class="head">
        <h3>
            <FoldButton {folded} ontoggle={() => workspace.toggleSection(id)} what={what ?? `the ${title.toLowerCase()}`}>
                <span class="heading">
                    <span class="title">{title}</span>
                    {#if count !== undefined && count !== ''}<span class="count">{count}</span>{/if}
                    {#if by}<span class="by">· {by}</span>{/if}
                    {#if heading}{@render heading()}{/if}
                </span>
            </FoldButton>
        </h3>
        {#if actions && !folded}
            <div class="actions">{@render actions()}</div>
        {/if}
    </div>
    {#if !folded}
        <div class="content">{@render children()}</div>
    {/if}
</section>

<style>
    .panel-section {
        margin: 10px 16px 12px;
    }

    .panel-section.boxed {
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        padding: 6px 12px 10px;
    }

    .panel-section.boxed.folded {
        padding-bottom: 6px;
    }

    .head {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    h3 {
        flex: 1 1 auto;
        min-width: 0;
        margin: 0 0 6px;
        font-size: 11px;
        font-weight: 500;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--text-faint);
        user-select: none;
    }

    .folded h3 {
        margin-bottom: 0;
    }

    .heading {
        display: flex;
        align-items: baseline;
        gap: 6px;
        min-width: 0;
    }

    .count,
    .by {
        letter-spacing: 0;
        text-transform: none;
        font-variant-numeric: tabular-nums;
        color: var(--text-dim);
        font-weight: 400;
    }

    .actions {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 0 0 auto;
        margin-bottom: 6px;
    }
</style>
