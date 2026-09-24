<!--
  One object in two clusters, and what differs.

  Both sides are printed the same way with what always differs taken out --
  uid, resourceVersion, status, the last-applied annotation -- so what is left
  is what somebody actually set differently. A Secret's values are compared by
  digest and never shown. See kube/compare.go.

  Usually opened from an object ("Compare with another cluster…"), which fills
  the left side and keeps whichever cluster was on the right last time; it can
  also be opened empty from the Clusters menu and filled in by hand.
-->
<script lang="ts">
    import { onMount, untrack } from 'svelte';
    import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
    import { NAV_GROUPS, PORT_FORWARDS, ACCESS_OVERVIEW, labelFor } from '../catalogue';
    import { adoptComparison, type Comparison } from '../state/adopt';
    import { compare } from '../state/compare.svelte';
    import { workspace } from '../state/workspace.svelte';
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import Icon from './Icon.svelte';

    /** Unchanged lines kept either side of a change; longer runs fold. */
    const CONTEXT_LINES = 3;

    let leftContext = $state(compare.left.contextId);
    let rightContext = $state(compare.right.contextId);
    let kind = $state(compare.left.kind);
    let namespace = $state(compare.left.namespace);
    let name = $state(compare.left.name);
    /** The right side's own namespace and name, when it differs from the left's. */
    let rightNamespace = $state('');
    let rightName = $state('');
    let differentName = $state(false);

    let result = $state<Comparison | null>(null);
    let running = $state(false);
    let error = $state<string | null>(null);
    let showAll = $state(false);
    /** Folded runs the reader opened, by their first line's index. */
    let opened = $state(new Set<number>());

    /** The kinds offered in the list: the sidebar's, less the views that are not objects. */
    const KINDS = NAV_GROUPS.flatMap((g) => g.items)
        .map((i) => i.kind)
        .filter((k) => k !== PORT_FORWARDS && k !== ACCESS_OVERVIEW);

    let ready = $derived(leftContext !== '' && rightContext !== '' && kind.trim() !== '' && name.trim() !== '');

    function sides(): [kube.CompareSide, kube.CompareSide] {
        const left = { contextId: leftContext, kind: kind.trim(), namespace: namespace.trim(), name: name.trim() };
        const right = {
            contextId: rightContext,
            kind: kind.trim(),
            namespace: differentName && rightNamespace.trim() ? rightNamespace.trim() : left.namespace,
            name: differentName && rightName.trim() ? rightName.trim() : left.name,
        };
        return [left, right];
    }

    async function run(): Promise<void> {
        if (!ready || running) return;
        const [left, right] = sides();
        compare.left = { ...left };
        compare.right = { ...right };
        running = true;
        error = null;
        try {
            result = adoptComparison(await ResourceService.Compare(left, right));
            opened = new Set();
        } catch (err) {
            error = err instanceof Error ? err.message : String(err);
            result = null;
        } finally {
            running = false;
        }
    }

    function swap(): void {
        [leftContext, rightContext] = [rightContext, leftContext];
        if (differentName) {
            [namespace, rightNamespace] = [rightNamespace || namespace, namespace];
            [name, rightName] = [rightName || name, name];
        }
        if (result) void run();
    }

    // Filled from outside -- an object's "Compare with…" -- after the tab was
    // already open: take the new sides, and compare straight away when both
    // clusters are known.
    let seen = untrack(() => compare.request);
    $effect(() => {
        const request = compare.request;
        if (request === seen) return;
        seen = request;
        untrack(() => {
            leftContext = compare.left.contextId;
            rightContext = compare.right.contextId;
            kind = compare.left.kind;
            namespace = compare.left.namespace;
            name = compare.left.name;
            differentName = false;
            result = null;
            if (rightContext) void run();
        });
    });

    // Opened already filled in, with both clusters: compare once, at once.
    onMount(() => {
        if (compare.request > 0 && ready) void run();
    });

    function nameOf(contextId: string): string {
        const context = workspace.contexts.find((c) => c.id === contextId);
        return context ? workspace.displayName(context) : contextId;
    }

    type Block =
        | { type: 'line'; line: kube.DiffLine }
        | { type: 'fold'; start: number; count: number };

    /**
     * The diff as it is drawn: every changed line, CONTEXT_LINES of unchanged
     * ones around each, and the rest folded into a row that says how many.
     */
    let blocks = $derived.by((): Block[] => {
        const lines = result?.lines ?? [];
        if (showAll) return lines.map((line) => ({ type: 'line', line }));
        const near = new Array<boolean>(lines.length).fill(false);
        lines.forEach((line, i) => {
            if (line.op === ' ') return;
            for (let j = Math.max(0, i - CONTEXT_LINES); j <= Math.min(lines.length - 1, i + CONTEXT_LINES); j++) near[j] = true;
        });
        const out: Block[] = [];
        for (let i = 0; i < lines.length; ) {
            if (near[i] || opened.has(i)) {
                out.push({ type: 'line', line: lines[i]! });
                i++;
                continue;
            }
            let j = i;
            while (j < lines.length && !near[j]) j++;
            if (j - i <= 1) {
                out.push({ type: 'line', line: lines[i]! });
                i++;
                continue;
            }
            out.push({ type: 'fold', start: i, count: j - i });
            i = j;
        }
        return out;
    });

    function unfold(start: number, count: number): void {
        const next = new Set(opened);
        for (let i = start; i < start + count; i++) next.add(i);
        opened = next;
    }
</script>

<div class="compare">
    <header>
        <h2>Compare</h2>
        <p>
            One object in two clusters. The uid, resourceVersion, status and other bookkeeping every copy has its own of
            are left out, so what differs is what somebody set differently. Secret values are compared by digest and never
            shown.
        </p>
    </header>

    <form
        class="form"
        onsubmit={(e) => {
            e.preventDefault();
            void run();
        }}
    >
        <div class="clusters">
            <label class="field">
                <span>Left cluster</span>
                <select bind:value={leftContext}>
                    <option value="">Choose…</option>
                    {#each workspace.contexts as context (context.id)}
                        <option value={context.id}>{workspace.displayName(context)}</option>
                    {/each}
                </select>
            </label>
            <button type="button" class="swap" onclick={swap} title="Swap the two sides" aria-label="Swap the two sides">
                <Icon name="repeat" size={13} />
            </button>
            <label class="field">
                <span>Right cluster</span>
                <select bind:value={rightContext}>
                    <option value="">Choose…</option>
                    {#each workspace.contexts as context (context.id)}
                        <option value={context.id}>{workspace.displayName(context)}</option>
                    {/each}
                </select>
            </label>
        </div>

        <div class="object">
            <label class="field">
                <span>Kind</span>
                <input list="compare-kinds" bind:value={kind} placeholder="deployments" spellcheck="false" />
                <datalist id="compare-kinds">
                    {#each KINDS as k (k)}
                        <option value={k}>{labelFor(k)}</option>
                    {/each}
                </datalist>
            </label>
            <label class="field">
                <span>Namespace</span>
                <input bind:value={namespace} placeholder="none for a cluster-wide kind" spellcheck="false" />
            </label>
            <label class="field">
                <span>Name</span>
                <input bind:value={name} spellcheck="false" />
            </label>
        </div>

        <label class="check">
            <input type="checkbox" bind:checked={differentName} />
            Named differently on the right
        </label>
        {#if differentName}
            <div class="object two">
                <label class="field">
                    <span>Right namespace</span>
                    <input bind:value={rightNamespace} placeholder={namespace || 'same as the left'} spellcheck="false" />
                </label>
                <label class="field">
                    <span>Right name</span>
                    <input bind:value={rightName} placeholder={name || 'same as the left'} spellcheck="false" />
                </label>
            </div>
        {/if}

        <div class="go">
            <button type="submit" class="primary" disabled={!ready || running}>
                <Icon name="columns" size={12} />
                {running ? 'Comparing…' : 'Compare'}
            </button>
        </div>
    </form>

    {#if error}
        <p class="problem">{error}</p>
    {/if}

    {#if result}
        {@const [left, right] = sides()}
        <section class="result">
            <div class="summary">
                {#if result.leftError}
                    <p class="problem"><strong>{nameOf(left.contextId)}:</strong> {result.leftError}</p>
                {/if}
                {#if result.rightError}
                    <p class="problem"><strong>{nameOf(right.contextId)}:</strong> {result.rightError}</p>
                {/if}
                {#if result.same}
                    <p class="same"><Icon name="tick" size={13} /> The same in both clusters.</p>
                {:else if !result.leftError && !result.rightError}
                    <p class="differs">
                        {result.changes} {result.changes === 1 ? 'line differs' : 'lines differ'}
                    </p>
                {/if}
                <label class="check all">
                    <input type="checkbox" bind:checked={showAll} />
                    Show unchanged lines
                </label>
            </div>

            <div class="legend">
                <span class="del">− {nameOf(left.contextId)}</span>
                <span class="add">+ {nameOf(right.contextId)}</span>
            </div>

            <div class="diff" role="table" aria-label="Differences">
                {#each blocks as block, i (block.type === 'line' ? `l${block.line.left}:${block.line.right}:${i}` : `f${block.start}`)}
                    {#if block.type === 'fold'}
                        <button class="fold" onclick={() => unfold(block.start, block.count)}>
                            … {block.count} unchanged lines
                        </button>
                    {:else}
                        <div
                            class="row"
                            class:del={block.line.op === '-'}
                            class:add={block.line.op === '+'}
                            role="row"
                        >
                            <span class="n" role="cell">{block.line.left || ''}</span>
                            <span class="n" role="cell">{block.line.right || ''}</span>
                            <span class="op" role="cell">{block.line.op === ' ' ? '' : block.line.op === '-' ? '−' : '+'}</span>
                            <span class="text" role="cell">{block.line.text}</span>
                        </div>
                    {/if}
                {/each}
            </div>
        </section>
    {/if}
</div>

<style>
    .compare {
        height: 100%;
        overflow: auto;
        padding: 20px 24px 32px;
    }

    header {
        margin-bottom: 16px;
    }

    h2 {
        margin: 0 0 4px;
        font-size: 15px;
        font-weight: 600;
    }

    header p {
        margin: 0;
        font-size: 12px;
        line-height: 1.6;
        color: var(--text-dim);
        max-width: 80ch;
    }

    .form {
        display: flex;
        flex-direction: column;
        gap: 10px;
        max-width: 900px;
        margin-bottom: 18px;
    }

    .clusters {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
        gap: 12px;
        align-items: end;
    }

    .object {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
        gap: 12px;
    }

    .object.two {
        grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .field {
        display: flex;
        flex-direction: column;
        gap: 4px;
        font-size: 11px;
        color: var(--text-faint);
    }

    .field input,
    .field select {
        height: 28px;
        padding: 0 8px;
        font: inherit;
        font-size: 12px;
        color: var(--text);
        background: var(--bg-panel);
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
    }

    .field input {
        font-family: var(--mono);
    }

    .swap {
        height: 28px;
        width: 28px;
        display: grid;
        place-items: center;
        border: 1px solid var(--border);
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--text-dim);
        cursor: pointer;
    }

    .swap:hover {
        color: var(--text);
        border-color: var(--accent);
    }

    .check {
        display: flex;
        align-items: center;
        gap: 6px;
        font-size: 12px;
        color: var(--text-dim);
    }

    .primary {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        height: 28px;
        padding: 0 14px;
        font: inherit;
        font-size: 12px;
        border-radius: var(--radius-sm);
        border: 0;
        background: var(--accent);
        color: var(--accent-text);
        cursor: pointer;
    }

    .primary:disabled {
        opacity: 0.5;
        cursor: default;
    }

    .problem {
        margin: 0 0 8px;
        font-size: 12px;
        color: var(--error);
    }

    .summary {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 6px 16px;
        margin-bottom: 8px;
    }

    .summary p {
        margin: 0;
    }

    .same {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        color: var(--ok);
        font-weight: 600;
    }

    .differs {
        color: var(--warn);
        font-weight: 600;
    }

    .summary .all {
        margin-left: auto;
    }

    .legend {
        display: flex;
        gap: 16px;
        font-size: 11px;
        margin-bottom: 6px;
    }

    .legend .del {
        color: var(--error);
    }

    .legend .add {
        color: var(--ok);
    }

    .diff {
        border: 1px solid var(--border);
        border-radius: var(--radius);
        overflow: auto;
        font-family: var(--mono);
        font-size: 11.5px;
        line-height: 1.55;
        background: var(--bg-panel);
    }

    .row {
        display: grid;
        grid-template-columns: 44px 44px 18px minmax(0, 1fr);
        white-space: pre;
    }

    .row.del {
        background: color-mix(in srgb, var(--error) 13%, transparent);
    }

    .row.add {
        background: color-mix(in srgb, var(--ok) 13%, transparent);
    }

    .n {
        text-align: right;
        padding-right: 8px;
        color: var(--text-faint);
        user-select: none;
    }

    .op {
        color: var(--text-dim);
        user-select: none;
    }

    .row.del .op {
        color: var(--error);
    }

    .row.add .op {
        color: var(--ok);
    }

    .text {
        padding-right: 12px;
    }

    .fold {
        display: block;
        width: 100%;
        padding: 2px 0 2px 106px;
        text-align: left;
        font: inherit;
        color: var(--text-faint);
        background: var(--bg-raised);
        border: 0;
        cursor: pointer;
    }

    .fold:hover {
        color: var(--accent);
    }
</style>
