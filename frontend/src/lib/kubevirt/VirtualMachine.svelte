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
    import PanelSection from '../components/PanelSection.svelte';
    import { detail as describe } from '../state/detail.svelte';
    import { VM_KINDS } from '../actions';

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

    /** Whether this is a machine, with a screen and a console, rather than a migration. */
    let isMachine = $derived(VM_KINDS.includes(kind));

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
    {#if isMachine}
        <!-- Buttons rather than a picture: reading the display costs a call
             to virt-api, and most people opening a machine did not come for
             it. Each opens a tab in the dock that stays up to date. -->
        <PanelSection id="kubevirt:screen" title="Console" what="the console buttons" boxed>
            <div class="consoles">
                <button
                    class="console"
                    title="Show the machine's display in the dock, kept up to date"
                    onclick={() => workspace.openVMScreen({ contextId, kind, namespace, name })}
                >
                    <Icon name="display" size={14} />
                    <span>
                        <strong>Console</strong>
                        <small>The display, as on a monitor — a Talos dashboard, a bootloader</small>
                    </span>
                </button>
                <button
                    class="console"
                    title="Open the serial console in the dock"
                    onclick={() => workspace.openVMConsole({ contextId, kind, namespace, name })}
                >
                    <Icon name="terminal" size={14} />
                    <span>
                        <strong>Serial console</strong>
                        <small>The first serial port, as a terminal you can type into</small>
                    </span>
                </button>
            </div>
        </PanelSection>
    {/if}
    {#if loading && !detail}
        <p class="status">Reading {name}…</p>
    {:else if error}
        <!-- Carried rather than fatal: the charts and the report below are
             still worth having when this one call was refused. -->
        <p class="status failed">Could not read this machine — {error}</p>
    {:else if detail}
        {#each detail.sections as section (section.title)}
            <PanelSection
                id={`kubevirt:${section.title.toLowerCase()}`}
                title={section.title}
                count={section.rows.length > 0 ? section.rows.length : undefined}
                boxed
            >
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
            </PanelSection>
        {/each}
    {/if}
</section>

<style>
    .kubevirt {
        display: flex;
        flex-direction: column;
        margin-bottom: 4px;
    }

    .consoles {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
        gap: 8px;
    }

    .console {
        display: flex;
        align-items: flex-start;
        gap: 10px;
        padding: 8px 10px;
        text-align: left;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg-panel);
        color: var(--text-dim);
    }

    .console:hover {
        border-color: var(--accent);
        color: var(--text);
    }

    .console :global(svg) {
        flex: 0 0 auto;
        margin-top: 2px;
        color: var(--accent);
    }

    .console span {
        display: flex;
        flex-direction: column;
        gap: 2px;
        min-width: 0;
    }

    .console strong {
        font-size: 12px;
        font-weight: 600;
        color: var(--text);
    }

    .console small {
        font-size: 11px;
        color: var(--text-faint);
    }

    .status {
        margin: 10px 16px;
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
