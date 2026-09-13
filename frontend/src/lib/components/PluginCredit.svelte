<!--
  Who a plugin is from: a badge saying whether it ships with the app, is an
  official plugin or comes from the community, and its author, linked where
  the manifest gives an address. Drawn by the app on the Settings cards, the
  generated overview and the strip under a plugin's own overview, so the
  plugin's code has no say in it.
-->
<script lang="ts">
    import { onExternalClick } from '../links';
    import { STANDING, type Standing } from '../plugins/credit';

    interface Props {
        author: string;
        authorUrl?: string;
        standing: Standing;
    }

    let { author, authorUrl = '', standing }: Props = $props();
    let info = $derived(STANDING[standing]);
</script>

<span class="credit">
    <span class="standing {standing}" title={info.title}>{info.label}</span>
    {#if author}
        <span class="by">
            by
            {#if authorUrl}
                <a href={authorUrl} target="_blank" rel="noreferrer noopener" title={authorUrl} onclick={onExternalClick(authorUrl)}
                    >{author}</a
                >
            {:else}
                <span class="who">{author}</span>
            {/if}
        </span>
    {/if}
</span>

<style>
    .credit {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        min-width: 0;
        font-size: 11px;
        color: var(--text-faint);
    }

    .standing {
        flex: none;
        padding: 0 6px;
        border-radius: 999px;
        font-size: 10px;
        line-height: 16px;
        letter-spacing: 0.02em;
        border: 1px solid var(--border-soft);
        color: var(--text-dim);
    }

    .standing.official,
    .standing.builtin {
        color: var(--accent);
        border-color: color-mix(in srgb, var(--accent) 40%, transparent);
        background: color-mix(in srgb, var(--accent) 10%, transparent);
    }

    .by {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .who {
        color: var(--text-dim);
    }

    a {
        color: var(--accent);
        text-decoration: none;
    }

    a:hover {
        text-decoration: underline;
    }
</style>
