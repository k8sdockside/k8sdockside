<!--
  A pod's containers, one small rectangle each.

  The colour is the state and the count is the number of rectangles, so a screen
  of pods can be scanned instead of read. Colour is never the only carrier: each
  rectangle names its container and says its state in words, which is what a
  tooltip shows and what a screen reader reads out.

  The same row of rectangles serves two places. In the table it is a picture,
  and a click on it belongs to the row underneath. In the detail panel it is the
  container picker for the log view, and each rectangle is a button.
-->
<script lang="ts">
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

    interface Props {
        /** Null rather than empty is what Go sends for a kind that has none. */
        pills: kube.Pill[] | null;
        /** Whether a rectangle can be pressed, which it is only in the panel. */
        selectable?: boolean;
        /** The container names currently chosen, when they can be. */
        selected?: string[];
        onchoose?: (name: string) => void;
    }

    let { pills, selectable = false, selected = [], onchoose }: Props = $props();

    /**
     * How many rectangles are drawn before the rest become a count.
     *
     * A pod with thirty containers is rare and would otherwise push everything
     * else in the row off the side of the table.
     */
    const LIMIT = 12;

    let all = $derived(pills ?? []);
    let shown = $derived(all.slice(0, LIMIT));
    let hidden = $derived(all.length - shown.length);

    /** What the rectangle says when there is no room to write it. */
    function title(pill: kube.Pill): string {
        return `${pill.label} — ${pill.detail}`;
    }
</script>

{#if all.length > 0}
    <span class="pills">
        {#each shown as pill, index (pill.label + index)}
            {#if selectable}
                <button
                    class="pill {pill.tone}"
                    class:off={!selected.includes(pill.label)}
                    aria-pressed={selected.includes(pill.label)}
                    aria-label={title(pill)}
                    title={title(pill)}
                    onclick={() => onchoose?.(pill.label)}
                ></button>
            {:else}
                <!-- role="img" rather than a bare span: it is a picture of a
                     state, and its label is the only way that state reaches
                     anyone not reading the colour. -->
                <span class="pill {pill.tone}" role="img" aria-label={title(pill)} title={title(pill)}></span>
            {/if}
        {/each}
        {#if hidden > 0}
            <span class="more" title="{hidden} more containers">+{hidden}</span>
        {/if}
    </span>
{/if}

<style>
    .pills {
        display: inline-flex;
        align-items: center;
        gap: 3px;
    }

    /* Rounded squares rather than dots: a square reads as a thing that has
       parts -- a container -- where a dot reads as a status light. Equal on
       both sides, so a row of them is a row of equals rather than a bar chart
       of nothing. */
    .pill {
        display: block;
        width: 11px;
        height: 11px;
        border-radius: 3px;
        padding: 0;
        border: none;
        /* The plain one: a container that ran and stopped, or has not started
           yet. Present, and nothing to do about it. */
        background: var(--text-faint);
    }

    .pill.ok {
        background: var(--ok);
    }

    .pill.warn {
        background: var(--warn);
    }

    .pill.error {
        background: var(--error);
    }

    button.pill {
        cursor: pointer;
        transition: opacity 120ms ease, box-shadow 120ms ease;
    }

    /* A container not being followed is dimmed rather than hidden: which
       containers exist should not change with what you are looking at. */
    button.pill.off {
        opacity: 0.3;
    }

    button.pill:hover {
        opacity: 1;
        box-shadow: 0 0 0 2px var(--bg-active);
    }

    button.pill:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: 1px;
    }

    .more {
        font-size: 10px;
        color: var(--text-faint);
        margin-left: 1px;
    }
</style>
