<!--
  The picture behind the start page, filling the empty pane.

  It changes on the interval the settings give, cross-fading from one picture
  to the next, and drifts very slowly while it is on screen so the page does
  not read as a still. Over it lies a scrim in the theme's own ground: strong
  enough that the text on the page stays readable whatever the picture, faint
  enough that the picture is still the picture.

  Nothing here takes the pointer.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import { fade } from 'svelte/transition';
    import { backdrop } from '../backgrounds/backdrop.svelte';
    import { workspace } from '../state/workspace.svelte';

    /** How often to look whether the picture is due to change. */
    const CHECK_MS = 20_000;

    let settings = $derived(workspace.background);
    let picture = $derived(backdrop.current(settings));
    let url = $derived(picture ? backdrop.urlFor(picture, workspace.activeTheme) : '');

    onMount(() => {
        if (!backdrop.folderLoaded) void backdrop.loadFolder();
        // A start page seen again after a while shows something new straight
        // away, rather than after one more full interval.
        if (!settings.pinned) backdrop.advanceIfDue(settings.minutes);
    });

    $effect(() => {
        if (settings.pinned || settings.source === 'none') return;
        const minutes = settings.minutes;
        const timer = setInterval(() => backdrop.advanceIfDue(minutes), CHECK_MS);
        return () => clearInterval(timer);
    });
</script>

<div class="backdrop" aria-hidden="true" data-picture={picture?.id ?? ''}>
    {#if url}
        {#key url}
            <div class="layer" style:background-image="url('{url}')" transition:fade={{ duration: 1400 }}></div>
        {/key}
    {/if}
    <div class="scrim" class:bare={!url}></div>
</div>

<style>
    .backdrop {
        position: absolute;
        inset: 0;
        overflow: hidden;
        pointer-events: none;
    }

    .layer {
        position: absolute;
        /* A little larger than the pane, so the drift never shows an edge. */
        inset: -4%;
        background-repeat: no-repeat;
        background-position: center;
        background-size: cover;
        animation: drift 90s ease-in-out infinite alternate;
        will-change: transform;
    }

    @keyframes drift {
        from {
            transform: scale(1) translate3d(0, 0, 0);
        }
        to {
            transform: scale(1.05) translate3d(-1.2%, -0.8%, 0);
        }
    }

    @media (prefers-reduced-motion: reduce) {
        .layer {
            animation: none;
        }
    }

    /* Lighter behind the heading, heavier towards the foot of the page where
       the cluster cards sit. */
    .scrim {
        position: absolute;
        inset: 0;
        background:
            radial-gradient(
                ellipse 90% 70% at 50% 30%,
                color-mix(in srgb, var(--bg) 18%, transparent),
                color-mix(in srgb, var(--bg) 62%, transparent)
            ),
            linear-gradient(to bottom, transparent 55%, color-mix(in srgb, var(--bg) 45%, transparent));
    }

    /* No picture: the theme's ground, with only the faintest light on it. */
    .scrim.bare {
        background: radial-gradient(
            ellipse 80% 60% at 50% 25%,
            color-mix(in srgb, var(--accent) 7%, transparent),
            transparent
        );
    }
</style>
