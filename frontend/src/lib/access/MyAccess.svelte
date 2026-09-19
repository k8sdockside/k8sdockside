<!--
  What the cluster lets *you* do, in one namespace.

  Asked of the API server itself (SelfSubjectRulesReview) rather than worked
  out from the roles, so it answers for somebody who may read no roles at all --
  which is usually why they are wondering -- and it includes whatever the
  authorizers besides RBAC say, when they are willing to enumerate it.
-->
<script lang="ts">
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import ErrorState from '../components/ErrorState.svelte';
    import RuleMatrix from './RuleMatrix.svelte';
    import { adoptMyRules, assessRules, type Identity, type MyRules } from './model';

    interface Props {
        contextId: string;
        me: Identity;
        namespaces: string[];
        /** The namespace to start on. */
        initial: string;
    }

    let { contextId, me, namespaces, initial }: Props = $props();

    // svelte-ignore state_referenced_locally
    let namespace = $state(initial || 'default');
    let result = $state<MyRules | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(false);
    let attempt = $state(0);

    $effect(() => {
        const id = contextId;
        const ns = namespace;
        attempt;
        let cancelled = false;
        loading = true;
        error = null;
        ResourceService.MyAccess(id, ns)
            .then((raw) => {
                if (!cancelled) result = adoptMyRules(raw);
            })
            .catch((err: unknown) => {
                if (!cancelled) error = err instanceof Error ? err.message : String(err);
            })
            .finally(() => {
                if (!cancelled) loading = false;
            });
        return () => {
            cancelled = true;
        };
    });

    let risk = $derived(result ? assessRules(result.rules) : null);
    let choices = $derived(namespaces.includes(namespace) ? namespaces : [namespace, ...namespaces]);
</script>

<section class="mine">
    <div class="who">
        {#if me.username}
            <p>
                The cluster knows you as <strong class="selectable">{me.username}</strong>{#if me.groups.length}, in
                    the groups
                    {#each me.groups as g, i (i)}<code>{g}</code>{i < me.groups.length - 1 ? ', ' : ''}{/each}{/if}.
            </p>
        {:else}
            <p class="muted">
                The cluster did not say who you are{me.error ? `: ${me.error}` : ''}. It needs Kubernetes 1.28 or
                later to answer.
            </p>
        {/if}
    </div>

    <div class="bar">
        <label>
            What can I do in
            <select bind:value={namespace}>
                {#each choices as ns (ns)}<option value={ns}>{ns}</option>{/each}
            </select>
        </label>
        {#if loading}<span class="muted">Asking the cluster…</span>{/if}
    </div>

    {#if error}
        <ErrorState message={error} onRetry={() => attempt++} />
    {:else if result}
        {#if result.incomplete}
            <p class="warn">
                The cluster says this list is incomplete — another authorizer (often a cloud IAM webhook) allows
                things it will not enumerate. You may be able to do more than shown.
            </p>
        {/if}
        {#if result.evaluationError}
            <p class="warn">Some rules could not be evaluated: {result.evaluationError}</p>
        {/if}
        {#if risk && risk.reasons.length}
            <ul class="reasons risk-{risk.level}">
                {#each risk.reasons as r (r)}<li>{r}</li>{/each}
            </ul>
        {/if}
        <p class="muted small">
            Includes what you can do cluster-wide. Ticks are allowed; ◐ is allowed only for the named objects.
        </p>
        <RuleMatrix rules={result.rules} empty="The cluster says you can do nothing here." />
    {/if}
</section>

<style>
    .mine {
        max-width: 980px;
    }

    .who p {
        margin: 0 0 12px;
        line-height: 1.6;
    }

    code {
        font-family: var(--mono);
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .bar {
        display: flex;
        align-items: center;
        gap: 12px;
        margin-bottom: 10px;
    }

    label {
        display: flex;
        align-items: center;
        gap: 8px;
        font-weight: 600;
    }

    select {
        font: inherit;
        font-weight: 400;
        color: var(--text);
        background: var(--bg);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        padding: 4px 6px;
    }

    .muted {
        color: var(--text-faint);
    }

    .small {
        font-size: 11.5px;
        margin: 6px 0;
    }

    .warn {
        margin: 8px 0;
        padding: 7px 10px;
        border-radius: var(--radius-sm);
        background: color-mix(in srgb, var(--warn) 14%, transparent);
        line-height: 1.5;
    }

    .reasons {
        margin: 6px 0 8px;
        padding-left: 18px;
        line-height: 1.6;
        color: var(--text-dim);
    }

    .reasons.risk-critical {
        color: var(--error);
    }

    .reasons.risk-high {
        color: var(--warn);
    }
</style>
