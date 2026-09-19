<!--
  A KubeVirt object's detail panel: what the machine is, where it is, and what
  it is made of.

  Every other kind describes as YAML, which is the right default -- it is
  complete for a kind nobody compiled in. A virtual machine is where that
  default is worst: what somebody wants to know about one is spread across the
  VirtualMachine, its instance and the pod running it, and buried under a
  hundred lines of spec. The backend does that gathering (internal/kube/
  kubevirt.go); this lays the answer out.

  The YAML is still underneath. This does not replace the report, it goes above
  it -- the same arrangement the charts already have, and for the same reason:
  what a thing is doing is what somebody opened the panel for, and the full
  object is what they scroll to when the summary does not answer it.
-->
<script lang="ts">
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import { adoptKubeVirtDetail, type Fact, type KubeVirtDetail } from './adopt';
    import { workspace } from '../state/workspace.svelte';
    import Icon from '../components/Icon.svelte';
    import { detail as describe } from '../state/detail.svelte';

    interface Props {
        contextId: string;
        kind: string;
        namespace: string;
        name: string;
    }

    let { contextId, kind, namespace, name }: Props = $props();

    let detail = $state<KubeVirtDetail | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(true);

    $effect(() => {
        // Named so the effect re-runs when the panel is pointed at another
        // object; the panel is not rebuilt for a new selection.
        const target = { contextId, kind, namespace, name };
        let live = true;
        loading = true;

        void (async () => {
            try {
                const got = adoptKubeVirtDetail(
                    await ResourceService.KubeVirtDetail(target.contextId, target.kind, target.namespace, target.name),
                );
                if (!live) return;
                detail = got;
                error = got.error || null;
            } catch (err: unknown) {
                if (!live) return;
                detail = null;
                error = err instanceof Error ? err.message : String(err);
            } finally {
                if (live) loading = false;
            }
        })();

        return () => {
            live = false;
        };
    });

    /** Opens what a value names: a node, the instance, the launcher pod. */
    function open(fact: Fact): void {
        if (!fact.ref) return;
        describe.open({
            contextId,
            kind: fact.ref.kind,
            namespace: fact.ref.namespace,
            name: fact.ref.name,
        });
    }
</script>

<section class="kubevirt">
    {#if loading && !detail}
        <p class="status">Reading {name}…</p>
    {:else if error}
        <!-- Carried rather than fatal: the charts and the report below are
             still worth having when this one call was refused. -->
        <p class="status failed">Could not read this machine — {error}</p>
    {:else if detail}
        {#each detail.sections as section (section.title)}
            <article class="block">
                <h3>{section.title}</h3>

                {#if section.facts.length > 0}
                    <dl class="facts">
                        {#each section.facts as fact (fact.label)}
                            <dt>
                                {fact.label}
                                {#if fact.note}
                                    <span class="why" title={fact.note}><Icon name="info" size={11} /></span>
                                {/if}
                            </dt>
                            <dd class={fact.tone}>
                                {#if fact.ref}
                                    <button class="link" onclick={() => open(fact)} title="Describe {fact.ref.name}">
                                        {fact.value}
                                    </button>
                                {:else}
                                    {fact.value || '—'}
                                {/if}
                            </dd>
                        {/each}
                    </dl>
                {/if}

                {#if section.rows.length > 0}
                    <div class="scroll">
                        <table>
                            <thead>
                                <tr>
                                    <!-- Unkeyed, by position: two columns can
                                         share a name, and a keyed block throws
                                         on the repeat. -->
                                    {#each section.columns as column}
                                        <th>{column}</th>
                                    {/each}
                                </tr>
                            </thead>
                            <tbody>
                                {#each section.rows as row, at (at)}
                                    <tr>
                                        {#each row as cell, index (index)}
                                            <td class={cell.tone}>
                                                {#if cell.ref}
                                                    <button class="link" onclick={() => open(cell)} title="Describe {cell.ref.name}">
                                                        {cell.value}
                                                    </button>
                                                {:else}
                                                    {cell.value || '—'}
                                                {/if}
                                            </td>
                                        {/each}
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    </div>
                {:else if section.empty}
                    <p class="none">{section.empty}</p>
                {/if}
            </article>
        {/each}
    {/if}
</section>

<style>
    .kubevirt {
        display: flex;
        flex-direction: column;
        gap: 14px;
        margin-bottom: 16px;
    }

    .block {
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        padding: 10px 12px;
    }

    h3 {
        margin: 0 0 8px;
        font-size: 11px;
        font-weight: 500;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    /* Label and value in two columns, so the values line up down the block and
       a reader can scan them without reading the labels again. */
    .facts {
        display: grid;
        grid-template-columns: minmax(90px, max-content) 1fr;
        gap: 3px 14px;
        margin: 0;
        font-size: 12px;
    }

    dt {
        display: flex;
        align-items: center;
        gap: 4px;
        color: var(--text-dim);
    }

    dd {
        margin: 0;
        min-width: 0;
        overflow-wrap: anywhere;
        font-family: var(--mono);
    }

    .why {
        display: inline-grid;
        place-items: center;
        color: var(--text-faint);
        cursor: help;
    }

    /* Wide tables scroll inside the block rather than widening the panel, which
       is docked to a pane somebody has already chosen the width of. */
    .scroll {
        overflow-x: auto;
    }

    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 12px;
    }

    th {
        text-align: left;
        font-weight: 500;
        font-size: 11px;
        color: var(--text-faint);
        padding: 0 10px 4px 0;
        white-space: nowrap;
    }

    td {
        padding: 3px 10px 3px 0;
        vertical-align: top;
        font-family: var(--mono);
        overflow-wrap: anywhere;
    }

    tbody tr + tr td {
        border-top: 1px solid var(--border-soft);
    }

    .link {
        font: inherit;
        font-family: var(--mono);
        color: var(--accent);
        text-align: left;
    }

    .link:hover {
        text-decoration: underline;
    }

    .ok {
        color: var(--ok);
    }

    .warn {
        color: var(--warn);
    }

    .error {
        color: var(--error);
    }

    .info {
        color: var(--text-dim);
    }

    .status,
    .none {
        margin: 0;
        font-size: 12px;
        color: var(--text-faint);
    }

    .status.failed {
        color: var(--error);
    }
</style>
