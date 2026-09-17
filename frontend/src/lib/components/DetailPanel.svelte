<!--
  The describe panel: what a tab whose view is `details` renders.

  It used to dock to an edge of the window with a size, a resize handle and
  three buttons of its own, all of which were a second layout system for one
  panel. It is a tab now, so where it sits is which pane holds it, how big it is
  is that pane's size, and it is moved by the same drag that moves everything
  else. What is left here is the report and what can be done to the object.
-->
<script lang="ts">
    import { HELM_RELEASES, SECRETS, singularFor } from '../catalogue';
    import MetricsPanel from '../charts/MetricsPanel.svelte';
    import VirtualMachine from '../kubevirt/VirtualMachine.svelte';
    import ResourceBudget from '../budget/ResourceBudget.svelte';
    import { alpha } from '../colors';
    import { actions } from '../state/actions.svelte';
    import { changes } from '../state/changes.svelte';
    import { workspace } from '../state/workspace.svelte';
    import ContainerPills from './ContainerPills.svelte';
    import ErrorState from './ErrorState.svelte';
    import HelmRelease from './HelmRelease.svelte';
    import Icon from './Icon.svelte';
    import ObjectActions from './ObjectActions.svelte';
    import ObjectLinks from './ObjectLinks.svelte';
    import PluginFrame from './PluginFrame.svelte';
    import { detail } from '../state/detail.svelte';
    import { notices } from '../state/notices.svelte';
    import { copyText } from '../clipboard';

    let target = $derived(detail.target);
    /**
     * The kinds that own a resource budget of their own.
     *
     * A node holds hardware, a namespace holds a quota, and a pod holds its own
     * limits -- which is the denominator that matters in front of a pod being
     * throttled, since the kernel stops it at its limit whatever the node has
     * spare. Everything else is accounted for inside one of those three, so a
     * budget on it would either repeat the parent's numbers or invent a
     * denominator.
     */
    const BUDGET_SCOPES: Record<string, string> = { nodes: 'node', namespaces: 'namespace', pods: 'pod' };
    let budgetScope = $derived(target ? (BUDGET_SCOPES[target.kind] ?? '') : '');
    let color = $derived(target ? workspace.colorOf(target.contextId) : 'var(--accent)');
    /**
     * Keeps the report level with the object.
     *
     * The object can be written from somewhere else while this panel is open --
     * the editor in the dock is the usual place, and it is often open on
     * exactly what is being described here. Reading the object's revision is
     * the subscription; when it has moved past the one the report was read at,
     * the panel reads again.
     */
    $effect(() => {
        if (target && changes.revision(target) !== detail.revision) {
            void detail.refresh();
        }
    });

    /**
     * Whether this is a Helm release, which is described by its own record
     * rather than by a report read off an object. See HelmRelease.svelte.
     */
    let isRelease = $derived(target?.kind === HELM_RELEASES);
    /**
     * The KubeVirt kinds that lay out as facts and tables rather than as YAML.
     *
     * Named here rather than asked of the backend because it decides what to
     * render before anything is fetched, and the list is the same on both
     * sides -- kube.IsKubeVirtDetailKind. A kind gaining a panel is a change to
     * both, which is what a test pins.
     */
    const KUBEVIRT_DETAIL = [
        'crd:virtualmachines.kubevirt.io',
        'crd:virtualmachineinstances.kubevirt.io',
        'crd:virtualmachineinstancemigrations.kubevirt.io',
    ];
    let isMachine = $derived(!!target && KUBEVIRT_DETAIL.includes(target.kind));

    /**
     * The object's containers, read by the action bar below and shown again
     * here. Empty for everything that is not a pod.
     */
    let containers = $derived(target ? actions.stateOf(target).containers : []);

    /**
     * Whether the report is one with a plain YAML body -- which is what both
     * the search and the reveal button act on. A release and a virtual machine
     * render their own panels above, and neither is text to search.
     */
    let hasReport = $derived(!isRelease && !detail.loading && !detail.error);

    /** Only a Secret has anything to reveal. */
    let isSecret = $derived(target?.kind === SECRETS);

    /**
     * What to find in the report.
     *
     * Cleared when the panel moves to another object: a query typed against one
     * object's report says nothing about the next, and leaving it would open
     * the next object already filtered down to nothing.
     */
    let query = $state('');
    $effect(() => {
        target?.name;
        query = '';
    });

    /**
     * The report split into the parts that match and the parts that do not.
     *
     * Highlighting rather than filtering to matching lines: this is a YAML
     * report, and a line out of its block says much less than a line in one --
     * `name: web` means nothing without the block above it. So every line stays
     * and the matches are marked.
     *
     * Rendered as text nodes rather than as markup, so a report that happens to
     * contain angle brackets -- a container command, an annotation holding
     * HTML -- is drawn rather than interpreted.
     */
    interface Part {
        text: string;
        hit: boolean;
    }

    let parts = $derived.by((): Part[] => {
        const needle = query.trim().toLowerCase();
        if (!needle) return [{ text: detail.text, hit: false }];

        const out: Part[] = [];
        const haystack = detail.text.toLowerCase();
        let at = 0;
        for (;;) {
            const found = haystack.indexOf(needle, at);
            if (found === -1) break;
            if (found > at) out.push({ text: detail.text.slice(at, found), hit: false });
            out.push({ text: detail.text.slice(found, found + needle.length), hit: true });
            at = found + needle.length;
        }
        if (at < detail.text.length) out.push({ text: detail.text.slice(at), hit: false });
        return out;
    });

    let hits = $derived(parts.filter((p) => p.hit).length);

    /**
     * Whether the report was just copied, which the button says for a moment:
     * a copy leaves nothing else on screen to show it happened.
     */
    let copied = $state(false);
    let copiedTimer: ReturnType<typeof setTimeout> | undefined;

    /**
     * Copies the whole report as it is shown -- a Secret's values decoded
     * only if they are decoded on screen. A part of it is copied the usual
     * way, by selecting it.
     */
    async function copyReport(): Promise<void> {
        if (!(await copyText(detail.text))) {
            notices.inform('Could not copy the report to the clipboard');
            return;
        }
        copied = true;
        clearTimeout(copiedTimer);
        copiedTimer = setTimeout(() => (copied = false), 1500);
    }

    $effect(() => {
        target?.name;
        copied = false;
    });

</script>

<svelte:window
    onkeydown={(e) => {
        if (e.key === 'Escape' && detail.target) detail.close();
    }}
/>

{#if target}
    <section
        class="panel"
        style:--ctx-color={color}
        style:--ctx-tint={alpha(color, 0.12)}
        aria-label="{singularFor(target.kind)} details"
    >
        <header>
            <div class="ident">
                <span class="kind">{singularFor(target.kind)}</span>
                <h2 class="selectable">{target.name}</h2>
                {#if target.namespace}
                    <span class="ns">in {target.namespace}</span>
                {/if}
                {#if containers.length > 0}
                    <!-- The same squares the table draws, so a pod reads the
                         same wherever you meet it. -->
                    <span class="containers">
                        <ContainerPills pills={containers} />
                    </span>
                {/if}
            </div>
        </header>

        <!-- What can be done to the object, under the line that says what it
             is. Editing lives here too: it is an action like the rest. -->
        <ObjectActions object={target} />

        <div class="body">
            <!-- Above the describe report, not below it: what a pod is *doing*
                 is what someone opening this panel mid-incident came for, and
                 the report is long enough that anything under it is out of
                 sight. Draws nothing at all unless an installed plugin has
                 charts for this kind and the cluster has a Prometheus. -->
            <!-- Above the charts for a node or a namespace: how full it is
                 right now is what someone opening this came to find out, and
                 unlike the charts it needs nothing installed to answer. -->
            {#if budgetScope}
                <ResourceBudget
                    contextId={target.contextId}
                    scope={budgetScope}
                    namespace={target.namespace}
                    name={target.name}
                    title="Resources"
                    compact
                />
            {/if}

            <MetricsPanel
                contextId={target.contextId}
                attach={target.kind}
                namespace={target.namespace}
                name={target.name}
                compact
            />

            {#if isMachine && target}
                <!-- Above the report rather than instead of it: the summary is
                     what somebody opened the panel for, and the whole object is
                     what they scroll to when it does not answer them. -->
                <VirtualMachine
                    contextId={target.contextId}
                    kind={target.kind}
                    namespace={target.namespace}
                    name={target.name}
                />
            {/if}

            {#if !isRelease}
                <!-- What the object's status says, what it names, and the
                     pods it selects: each a link to open in this panel. -->
                <ObjectLinks object={target} revision={detail.revision} conditions={!isMachine} />
            {/if}

            <!-- Panels plugins bring for this kind: a page of the plugin's own
                 in a sandboxed frame, told which object it is drawn for. After
                 the app's own summary, before the report. -->
            {#each workspace.pluginSectionsFor(target.kind) as entry (entry.plugin.id + '/' + entry.section.id)}
                <div class="plugin-section">
                    <h3>{entry.section.label} <span>· {entry.plugin.name}</span></h3>
                    <PluginFrame
                        contextId={target.contextId}
                        section={{ pluginId: entry.plugin.id, sectionId: entry.section.id, object: target }}
                    />
                </div>
            {/each}

            {#if isRelease}
                <HelmRelease
                    release={{
                        contextId: target.contextId,
                        namespace: target.namespace,
                        name: target.name,
                    }}
                    numbers={workspace.showLineNumbers}
                />
            {:else if detail.loading}
                <p class="status">Describing {target.name}…</p>
            {:else if detail.error}
                <ErrorState message={detail.error} compact />
            {:else}
                <div class="tools">
                    <label class="find">
                        <Icon name="search" size={12} />
                        <input
                            placeholder="Find in {singularFor(target.kind)}"
                            aria-label="Find in this report"
                            bind:value={query}
                        />
                    </label>
                    {#if query.trim()}
                        <!-- A count rather than nothing, because a match can be
                             far enough down the report to be off screen: "3
                             matches" is what says to keep scrolling, and "no
                             matches" is what stops you looking. -->
                        <span class="hits" class:none={hits === 0} aria-live="polite">
                            {hits === 0 ? 'No matches' : `${hits} match${hits === 1 ? '' : 'es'}`}
                        </span>
                    {/if}

                    <button
                        class="toggle"
                        class:done={copied}
                        title="Copy the whole report{isSecret && !detail.revealed ? ', values still encoded' : ''}"
                        aria-label="Copy the report"
                        onclick={() => void copyReport()}
                    >
                        <Icon name={copied ? 'tick' : 'copy'} size={13} />
                        <span aria-live="polite">{copied ? 'Copied' : 'Copy'}</span>
                    </button>

                    {#if isSecret}
                        <button
                            class="toggle"
                            class:on={detail.revealed}
                            aria-pressed={detail.revealed}
                            title={detail.revealed
                                ? 'Show the values base64-encoded again'
                                : 'Decode the values and show them in plain text'}
                            onclick={() => void detail.reveal(!detail.revealed)}
                        >
                            <Icon name={detail.revealed ? 'lock' : 'unlock'} size={13} />
                            {detail.revealed ? 'Hide values' : 'Show values'}
                        </button>
                    {/if}
                </div>

                <!-- Written on one line so that no whitespace from the template
                     itself lands inside the pre. -->
                <pre class="selectable">{#each parts as part, i (i)}{#if part.hit}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}</pre>
            {/if}
        </div>
    </section>
{/if}

<style>
    .panel {
        display: flex;
        flex-direction: column;
        min-width: 0;
        min-height: 0;
        height: 100%;
        background: var(--bg-panel);
    }

    header {
        display: flex;
        align-items: flex-start;
        gap: 12px;
        padding: 10px 12px 10px 14px;
        border-bottom: 1px solid var(--border);
        background: var(--ctx-tint);
        border-left: 3px solid var(--ctx-color);
        flex: 0 0 auto;
    }

    .ident {
        min-width: 0;
        flex: 1 1 auto;
    }

    .kind {
        display: block;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    h2 {
        margin: 2px 0 0;
        font-size: 14px;
        font-weight: 600;
        overflow-wrap: anywhere;
    }

    .ns {
        font-size: 11px;
        color: var(--text-dim);
    }

    .containers {
        display: block;
        margin-top: 5px;
    }

    .body {
        flex: 1 1 auto;
        overflow: auto;
        min-height: 0;
    }

    .plugin-section {
        margin: 12px 12px 16px;
    }

    .plugin-section h3 {
        margin: 0 0 6px;
        font-size: 11px;
        font-weight: 500;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .plugin-section h3 span {
        text-transform: none;
        letter-spacing: 0;
    }

    pre {
        margin: 0;
        padding: 14px 16px;
        font-family: var(--mono);
        font-size: 11.5px;
        line-height: 1.65;
        color: var(--text-dim);
        white-space: pre;
    }

    .status {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 18px 16px;
        color: var(--text-dim);
    }

    /* Sticky, because the report is long and a search box that scrolls away is
       one you have to scroll back for to change what you were looking for. */
    .tools {
        position: sticky;
        top: 0;
        z-index: 1;
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 8px 12px 8px 16px;
        background: var(--bg-panel);
        border-bottom: 1px solid var(--border-soft);
    }

    .find {
        display: flex;
        align-items: center;
        gap: 6px;
        flex: 1 1 auto;
        min-width: 0;
        padding: 3px 8px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg);
        color: var(--text-faint);
    }

    .find:focus-within {
        border-color: var(--accent);
    }

    .find input {
        flex: 1 1 auto;
        min-width: 0;
        border: none;
        background: none;
        padding: 0;
        color: var(--text);
        font: inherit;
        font-size: 11.5px;
    }

    .find input:focus {
        outline: none;
    }

    .hits {
        flex: 0 0 auto;
        font-size: 11px;
        color: var(--text-faint);
        white-space: nowrap;
    }

    .hits.none {
        color: var(--warn);
    }

    .toggle {
        flex: 0 0 auto;
        display: flex;
        align-items: center;
        gap: 5px;
        padding: 4px 8px;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        color: var(--text-dim);
        font-size: 11.5px;
        white-space: nowrap;
    }

    .toggle:hover {
        color: var(--text);
        background: var(--bg-hover);
    }

    .toggle.done {
        color: var(--ok);
    }

    /* Revealed reads as on rather than as ordinary: values in plain text on
       screen is a state worth being able to see at a glance. */
    .toggle.on {
        border-color: var(--accent);
        color: var(--accent);
    }

    mark {
        background: color-mix(in oklab, var(--accent) 45%, transparent);
        color: var(--text);
        border-radius: 2px;
    }
</style>
