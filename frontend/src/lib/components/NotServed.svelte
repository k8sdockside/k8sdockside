<!--
  What a view shows when the cluster does not serve its kind at all.

  Not an error, and deliberately not drawn as one: the Gateway API, the newer
  admission policies and every CRD are optional, and a cluster without them is
  healthy. A red alert and "something went wrong" would send the reader looking
  for a fault that is not there. So this says plainly that the API is not
  installed, how to install it where there is a known answer, and offers a
  second look for the reader who has just done that.
-->
<script lang="ts">
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import { labelFor } from '../catalogue';
    import { notServedGroup } from '../errors';
    import { onExternalClick } from '../links';
    import Icon from './Icon.svelte';

    interface Props {
        /** The kind the view was opened for. */
        kind: string;
        /** The error as it came off the wire; it names the API group. */
        message: string;
        context?: kube.Context | null;
        onRetry?: () => void;
    }

    let { kind, message, context = null, onRetry }: Props = $props();

    /**
     * The optional APIs worth naming and pointing at, by group. Anything else
     * is described by its group alone, which is still more use than nothing.
     */
    const KNOWN: Record<string, { name: string; href: string }> = {
        'gateway.networking.k8s.io': {
            name: 'The Gateway API',
            href: 'https://gateway-api.sigs.k8s.io/guides/getting-started/',
        },
    };

    /**
     * The groups that ship with Kubernetes itself but whose newer kinds are
     * alpha or beta, and served only where the API server has them switched
     * on. "Not installed" is the wrong word for those: there is nothing to
     * install, and the fix is a flag on the control plane.
     */
    const BUILT_IN = new Set([
        'admissionregistration.k8s.io',
        'certificates.k8s.io',
        'coordination.k8s.io',
        'internal.apiserver.k8s.io',
        'lifecycle.k8s.io',
        'networking.k8s.io',
        'resource.k8s.io',
        'scheduling.k8s.io',
        'storage.k8s.io',
        'storagemigration.k8s.io',
    ]);
    const ENABLING = 'https://kubernetes.io/docs/tasks/administer-cluster/enable-disable-api/';

    let group = $derived(notServedGroup(message));
    let known = $derived(KNOWN[group] ?? null);
    let builtIn = $derived(BUILT_IN.has(group));
    let cluster = $derived(context?.name ?? 'This cluster');
</script>

<div class="not-served">
    <div class="mark"><Icon name="puzzle" size={26} /></div>

    {#if known}
        <h2>{known.name} is not installed on this cluster</h2>
        <p class="hint">
            It is optional, and {cluster} does not have it, so there are no {labelFor(kind).toLowerCase()} to show.
            A Gateway controller usually installs it for you.
        </p>
        <a class="docs" href={known.href} target="_blank" rel="noreferrer" onclick={onExternalClick(known.href)}>
            How to install it
        </a>
    {:else if builtIn}
        <h2>{labelFor(kind)} are switched off on this cluster</h2>
        <p class="hint">
            They are part of Kubernetes, but in the {group} API at a version {cluster} does not serve — too new
            for its Kubernetes, or alpha or beta and not enabled on its API server. Nothing is wrong with the
            connection.
        </p>
        <a class="docs" href={ENABLING} target="_blank" rel="noreferrer" onclick={onExternalClick(ENABLING)}>
            How API versions are enabled
        </a>
    {:else}
        <h2>This cluster does not serve {labelFor(kind)}</h2>
        <p class="hint">
            {#if group}
                The {group} API is not installed on {cluster}, or is older than this kind.
            {:else}
                {cluster} does not have this kind.
            {/if}
            Nothing is wrong with the connection.
        </p>
    {/if}

    <p class="raw selectable">{message}</p>

    {#if onRetry}
        <button class="retry" onclick={onRetry}>
            <Icon name="refresh" size={14} /> Check again
        </button>
    {/if}
</div>

<style>
    .not-served {
        max-width: 580px;
        margin: 0 auto;
        padding: 56px 24px 32px;
        text-align: center;
    }

    .mark {
        display: flex;
        justify-content: center;
        color: var(--text-faint);
        margin-bottom: 12px;
    }

    h2 {
        margin: 0 0 8px;
        font-size: 17px;
        font-weight: 600;
        color: var(--text);
        letter-spacing: normal;
        text-transform: none;
    }

    .hint {
        margin: 0 0 14px;
        font-size: 13px;
        line-height: 1.6;
        color: var(--text-dim);
    }

    .docs {
        display: inline-block;
        margin-bottom: 18px;
        font-size: 13px;
        color: var(--accent);
        text-decoration: underline;
        text-underline-offset: 2px;
    }

    /* Kept, but quiet: it is the backend's own sentence, worth copying and not
       worth leading with. */
    .raw {
        margin: 0;
        font-family: var(--mono);
        font-size: 11.5px;
        color: var(--text-faint);
        overflow-wrap: anywhere;
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
</style>
