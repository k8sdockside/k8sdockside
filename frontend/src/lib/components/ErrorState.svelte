<!--
  What a view shows when it could not reach the cluster.

  The shape is deliberate: a plain reading of the failure first, because that is
  what decides what the reader does next; then which cluster it was, because
  with a dozen contexts open the tab title is not enough; then the raw error,
  in full and selectable, because for a Kubernetes tool that text is usually the
  thing you actually want to copy somewhere.
-->
<script lang="ts">
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import { classify } from '../errors';
    import Icon from './Icon.svelte';

    interface Props {
        /** The raw error, as it came off the wire. */
        message: string;
        /** The cluster it happened against, when the caller knows it. */
        context?: kube.Context | null;
        /** Omitted when there is nothing for a retry to re-run. */
        onRetry?: () => void;
        /** For the detail panel, which has far less room than a tab. */
        compact?: boolean;
    }

    let { message, context = null, onRetry, compact = false }: Props = $props();

    let explanation = $derived(classify(message));
</script>

<div class="error-state" class:compact>
    <div class="mark"><Icon name="alert" size={compact ? 18 : 26} /></div>

    <h2>{explanation.headline}</h2>
    <p class="hint">{explanation.hint}</p>

    {#if context && !compact}
        <dl>
            <div><dt>Context</dt><dd class="selectable">{context.name}</dd></div>
            {#if context.server}
                <div><dt>Server</dt><dd class="selectable">{context.server}</dd></div>
            {/if}
            {#if context.file}
                <div><dt>Kubeconfig</dt><dd class="selectable">{context.file}</dd></div>
            {/if}
        </dl>
    {/if}

    <pre class="raw selectable">{message}</pre>

    {#if onRetry}
        <button class="retry" onclick={onRetry}>
            <Icon name="refresh" size={14} /> Try again
        </button>
    {/if}
</div>

<style>
    .error-state {
        max-width: 580px;
        margin: 0 auto;
        padding: 56px 24px 32px;
        text-align: center;
    }

    /* The icon is a block-level svg, so it needs centring of its own -- the
       container's text-align does not reach it. */
    .mark {
        display: flex;
        justify-content: center;
        color: var(--error);
        margin-bottom: 12px;
    }

    h2 {
        margin: 0 0 8px;
        font-size: 17px;
        font-weight: 600;
        color: var(--text);
        /* Overrides the uppercase section headings the views use elsewhere:
           this is a sentence, not a label. */
        letter-spacing: normal;
        text-transform: none;
    }

    .hint {
        margin: 0 0 20px;
        font-size: 13px;
        line-height: 1.6;
        color: var(--text-dim);
    }

    dl {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 4px 14px;
        margin: 0 0 18px;
        padding: 12px 14px;
        text-align: left;
        background: var(--bg-panel);
        border: 1px solid var(--border);
        border-radius: var(--radius);
        font-size: 12px;
    }

    dl > div {
        display: contents;
    }

    dt {
        color: var(--text-faint);
    }

    dd {
        margin: 0;
        min-width: 0;
        overflow-wrap: anywhere;
        color: var(--text-dim);
        font-family: var(--mono);
        font-size: 11.5px;
    }

    /* The raw error wraps rather than scrolls: these messages are long and
       nesting a scrollbar inside an error page hides the end of the sentence. */
    .raw {
        margin: 0;
        padding: 11px 13px;
        text-align: left;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
        background: var(--bg);
        border: 1px solid var(--border);
        border-left: 2px solid var(--error);
        border-radius: var(--radius-sm);
        font-family: var(--mono);
        font-size: 11.5px;
        line-height: 1.65;
        color: var(--text-dim);
    }

    .retry {
        display: inline-flex;
        align-items: center;
        gap: 7px;
        margin-top: 18px;
        padding: 7px 14px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        color: var(--text);
    }

    .retry:hover {
        background: var(--bg-active);
    }

    /* The detail panel is a narrow column beside a table, so the same content
       loses its margins and its centring rather than its parts. */
    .compact {
        padding: 20px 4px;
        text-align: left;
    }

    .compact h2 {
        font-size: 14px;
    }

    .compact .hint {
        margin-bottom: 12px;
        font-size: 12px;
    }

    .compact .mark {
        justify-content: flex-start;
        margin-bottom: 8px;
    }
</style>
