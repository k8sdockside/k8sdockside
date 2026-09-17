<!--
  The summary sections above an object's report: its conditions, the objects
  it names or is owned by, and the pods it selects.

  All of it is in the YAML below already, but as strings in fields -- a claim
  name, a node name, an owner -- and reading one meant leaving the panel to go
  and find the object. Here each is a link that opens it in the same panel.

  Draws nothing until the cluster has answered, and nothing at all for an
  object with none of the three: the report is still the panel's main thing,
  and three empty headings above it would only push it down.
-->
<script lang="ts">
    import { ResourceService } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
    import type * as kube from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/kube/models.js';
    import { PODS, singularFor } from '../catalogue';
    import { detail, type DetailTarget } from '../state/detail.svelte';
    import ContainerPills from './ContainerPills.svelte';

    interface Props {
        object: DetailTarget;
        /** Bumped when the object is written, so the sections read again. */
        revision: number;
        /**
         * Whether to show the conditions. A virtual machine's own panel shows
         * them already, laid out for a machine.
         */
        conditions?: boolean;
    }

    let { object, revision, conditions = true }: Props = $props();

    let links = $state<kube.ObjectLinks | null>(null);
    /** Every read takes a number, and only the newest may land. */
    let load = 0;

    async function read(ref: DetailTarget): Promise<void> {
        const attempt = ++load;
        try {
            const got = await ResourceService.ObjectLinks(ref.contextId, ref.kind, ref.namespace, ref.name);
            if (attempt === load) links = got;
        } catch {
            // The report below says what is wrong with the object; a summary
            // that could not be read is simply not drawn.
            if (attempt === load) links = null;
        }
    }

    /** Which object the sections on screen belong to. */
    let shownFor = '';

    // Read for every new object, and again when the object is written. A new
    // object starts empty rather than showing the last one's links while its
    // own are read; a write to the same one reads again quietly.
    $effect(() => {
        const ref = { ...object };
        void revision;
        const key = `${ref.contextId}#${ref.kind}#${ref.namespace}#${ref.name}`;
        if (key !== shownFor) {
            shownFor = key;
            links = null;
        }
        void read(ref);
    });

    let shownConditions = $derived(conditions ? (links?.conditions ?? []) : []);
    let references = $derived(links?.references ?? []);
    /** Null for a kind that selects no pods, empty for one that selects none now. */
    let pods = $derived(links?.pods ?? null);

    function open(kind: string, namespace: string, name: string): void {
        void detail.open({ contextId: object.contextId, kind, namespace, name });
    }

    /** What a reference is, in a word: its API kind, or the app's name for it. */
    function kindWord(ref: kube.Reference): string {
        return ref.apiKind || (ref.kind ? singularFor(ref.kind) : '');
    }
</script>

{#if shownConditions.length > 0}
    <details class="section" open>
        <summary>Conditions <span class="count">{shownConditions.length}</span></summary>
        <table>
            <tbody>
                {#each shownConditions as condition, i (i)}
                    <tr title={condition.message}>
                        <td class="type">
                            <span class="dot tone-{condition.tone}" aria-hidden="true"></span>
                            {condition.type}
                        </td>
                        <td class="tone-{condition.tone}">{condition.status}</td>
                        <td class="dim">{condition.reason}</td>
                        <td class="dim age">{condition.age}</td>
                    </tr>
                    {#if condition.message}
                        <tr class="message">
                            <td colspan="4" class="selectable">{condition.message}</td>
                        </tr>
                    {/if}
                {/each}
            </tbody>
        </table>
    </details>
{/if}

{#if references.length > 0}
    <details class="section" open>
        <summary>Related <span class="count">{references.length}</span></summary>
        <ul class="refs">
            {#each references as ref, i (i)}
                <li>
                    <span class="role">{ref.role}</span>
                    <span class="kindword">{kindWord(ref)}</span>
                    {#if ref.kind}
                        <button
                            class="link"
                            title="Describe {kindWord(ref)} {ref.namespace ? `${ref.namespace}/` : ''}{ref.name}"
                            onclick={() => open(ref.kind, ref.namespace, ref.name)}
                        >
                            {ref.name}
                        </button>
                    {:else}
                        <span class="selectable">{ref.name}</span>
                    {/if}
                    {#if ref.namespace && ref.namespace !== object.namespace}
                        <span class="dim">in {ref.namespace}</span>
                    {/if}
                </li>
            {/each}
        </ul>
    </details>
{/if}

{#if pods}
    <details class="section" open>
        <summary>
            Pods
            <span class="count">
                {links && links.podsTotal > pods.length ? `${pods.length} of ${links.podsTotal}` : pods.length}
            </span>
            {#if links?.selector}
                <code class="selector" title="The label selector the pods were found by">{links.selector}</code>
            {/if}
        </summary>
        {#if pods.length === 0}
            <p class="none">
                {links?.selector ? 'No pods match this selector.' : 'There are no pods in this namespace.'}
            </p>
        {:else}
            <table>
                <tbody>
                    {#each pods as pod (pod.namespace + '/' + pod.name)}
                        <tr>
                            <td class="podname">
                                <button class="link" title="Describe pod {pod.name}" onclick={() => open(PODS, pod.namespace, pod.name)}>
                                    {pod.name}
                                </button>
                                {#if (pod.containers ?? []).length > 0}
                                    <ContainerPills pills={pod.containers ?? []} />
                                {/if}
                            </td>
                            <td class="tone-{pod.status.tone || 'plain'}">{pod.status.text}</td>
                            <td class="tone-{pod.ready.tone || 'plain'}" title="Ready containers">{pod.ready.text}</td>
                            <td class="dim" title="Restarts">↻ {pod.restarts.text}</td>
                            <td class="dim node" title="Node">{pod.node}</td>
                            <td class="dim age">{pod.age}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        {/if}
    </details>
{/if}

<style>
    .section {
        margin: 10px 12px 12px;
    }

    summary {
        display: flex;
        align-items: baseline;
        gap: 6px;
        margin-bottom: 6px;
        font-size: 11px;
        font-weight: 500;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--text-faint);
        cursor: pointer;
        user-select: none;
        list-style: none;
    }

    /* A flex summary loses the browser's own marker, so it draws one. */
    summary::-webkit-details-marker {
        display: none;
    }

    summary::before {
        content: '▸';
        width: 8px;
        color: var(--text-faint);
    }

    details[open] > summary::before {
        content: '▾';
    }

    .count {
        letter-spacing: 0;
        font-variant-numeric: tabular-nums;
        color: var(--text-dim);
    }

    .selector {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        text-transform: none;
        letter-spacing: 0;
        font-family: var(--mono);
        font-size: 10.5px;
    }

    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 12px;
    }

    td {
        padding: 3px 8px 3px 0;
        vertical-align: top;
        white-space: nowrap;
    }

    td:last-child {
        padding-right: 0;
    }

    .type {
        color: var(--text);
    }

    .message td {
        padding: 0 0 5px 14px;
        white-space: normal;
        overflow-wrap: anywhere;
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .age {
        text-align: right;
        font-variant-numeric: tabular-nums;
    }

    .node {
        max-width: 140px;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .podname {
        display: flex;
        align-items: center;
        gap: 8px;
        white-space: normal;
    }

    .dim {
        color: var(--text-dim);
    }

    .dot {
        display: inline-block;
        width: 7px;
        height: 7px;
        margin-right: 5px;
        border-radius: 50%;
        background: currentColor;
        vertical-align: 1px;
    }

    .tone-ok {
        color: var(--ok);
    }

    .tone-warn {
        color: var(--warn);
    }

    .tone-error {
        color: var(--error);
    }

    .tone-info,
    .tone-plain {
        color: var(--text-dim);
    }

    .refs {
        margin: 0;
        padding: 0;
        list-style: none;
        display: flex;
        flex-direction: column;
        gap: 3px;
        font-size: 12px;
    }

    .refs li {
        display: flex;
        align-items: baseline;
        gap: 6px;
        min-width: 0;
    }

    .role {
        flex: 0 0 110px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        color: var(--text-faint);
    }

    .kindword {
        flex: 0 0 auto;
        font-size: 11px;
        color: var(--text-dim);
    }

    .link {
        min-width: 0;
        padding: 0;
        background: none;
        color: var(--accent);
        font: inherit;
        text-align: left;
        overflow-wrap: anywhere;
        cursor: pointer;
    }

    .link:hover {
        text-decoration: underline;
        text-underline-offset: 2px;
    }

    .none {
        margin: 0;
        font-size: 12px;
        color: var(--text-dim);
    }
</style>
