<!--
  A plugin's mark, wherever the app names one.

  Three sources, in the order of how much each says about this particular
  plugin: the logo file it ships, then the mark the app carries for it, then
  the generic icon from its manifest. The fallbacks matter as much as the
  first case -- an offer is a plugin with no files on this machine, and a
  plugin someone wrote themselves has no mark here -- so all three come out
  the same size and sit on the same baseline as an Icon.

  Both marks are drawn through <img>, never inlined. Several appear on one
  page, and an SVG in an <img> is parsed as a document of its own: gradient
  ids and <style> rules inside one logo then cannot reach into another, which
  inlining them would allow. It also keeps anything a plugin ships out of the
  app's own DOM, and keeps drawings the size of Vitistack's 74 KB lighthouse
  out of the bundle -- they are fetched only when one is on screen.
-->
<script lang="ts">
    import Icon from './Icon.svelte';
    import { markFor } from '../plugins/marks';

    interface Props {
        /** The plugin's id: both where its files are served from and how a bundled mark is found. */
        id: string;
        /** The manifest's icon: the last fallback, and what a plugin without a mark has always used. */
        icon: string;
        /** The logo file the plugin ships, relative to its ui folder. Absent on an offer, which is not installed. */
        logo?: string;
        size?: number;
    }

    let { id, icon, logo = '', size = 16 }: Props = $props();

    // The plugin's own file is served by the app itself, from the same ui
    // folder its pages come from.
    let own = $derived(logo ? `/plugin-ui/${encodeURIComponent(id)}/${logo}` : '');

    // A logo that will not load falls through to the next source rather than
    // leaving a broken image in the sidebar. Held as the address that failed,
    // so replacing the file -- or drawing a different plugin -- tries again.
    let broken = $state('');
    let src = $derived(own && own !== broken ? own : markFor(id));
</script>

{#if src}
    <img
        class="mark"
        {src}
        width={size}
        height={size}
        alt=""
        aria-hidden="true"
        onerror={() => (broken = own)}
    />
{:else}
    <Icon name={icon} {size} />
{/if}

<style>
    .mark {
        display: block;
        flex: 0 0 auto;
        /* Not every project's mark is square -- Flannel's glyph is tall -- and
           a logo must never be stretched to fit the box it is given. */
        object-fit: contain;
    }
</style>
