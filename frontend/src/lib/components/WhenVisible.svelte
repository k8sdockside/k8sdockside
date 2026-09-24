<!--
  Draws what it holds only once it comes near the visible part of the page.

  The dashboard's charts ask Prometheus a dozen questions and its event
  timeline reads every event in the cluster; a plugin's panel in a detail view
  is a frame of its own that loads a page and starts polling. All of them
  usually sit below the fold. Drawn at once, they make the part that is on
  screen wait for work nobody has scrolled to yet; drawn here, they start when
  they are about to be seen, and stay once they have been.

  Until then it holds the room they will take, roughly, so the page does not
  jump as they arrive.
-->
<script lang="ts">
    import type { Snippet } from 'svelte';

    interface Props {
        /** Roughly how tall the contents will be, held until they are drawn. */
        height?: number;
        /** How far outside the view to start drawing, so it is ready as it arrives. */
        margin?: string;
        children: Snippet;
    }

    let { height = 200, margin = '300px', children }: Props = $props();

    let seen = $state(false);
    let holder = $state<HTMLElement | null>(null);

    $effect(() => {
        if (seen || !holder) return;
        // Without an observer -- an old engine, a test without layout -- there
        // is nothing to wait for.
        if (typeof IntersectionObserver === 'undefined') {
            seen = true;
            return;
        }
        const observer = new IntersectionObserver(
            (entries) => {
                if (entries.some((e) => e.isIntersecting)) {
                    seen = true;
                    observer.disconnect();
                }
            },
            { rootMargin: margin },
        );
        observer.observe(holder);
        return () => observer.disconnect();
    });
</script>

{#if seen}
    {@render children()}
{:else}
    <div class="holder" bind:this={holder} style:min-height="{height}px" aria-hidden="true"></div>
{/if}
