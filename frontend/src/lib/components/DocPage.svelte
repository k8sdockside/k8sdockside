<!--
  A documentation page: Help, or the Kubernetes primer. One component for
  both, drawing a page of sections from data -- see docs/types.ts.

  Laid out like Settings, a rail and one section at a time, because the two
  are opened the same way and sit in the same strip; a reader who has learnt
  one has learnt the other. The section you are on survives leaving the tab
  and coming back, for the reason the settings rail's does: the shell rebuilds
  the view on every return.
-->
<script lang="ts" module>
    // Where each page was, keyed by title, so the two pages remember
    // separately and neither survives a restart.
    const remembered = new Map<string, string>();
</script>

<script lang="ts">
    import { inline } from '../docs/inline';
    import { forMode } from '../docs/mode';
    import { onExternalClick } from '../links';
    import type { Action, Page } from '../docs/types';
    import { session } from '../state/session.svelte';
    import { workspace } from '../state/workspace.svelte';
    import Icon from './Icon.svelte';
    import { rememberSection } from './settings/section.svelte';

    interface Props {
        page: Page;
    }

    let { page }: Props = $props();

    // The page as this version of the app tells it: the web version leaves out
    // what only the desktop app can do, and the other way round. It changes
    // once, when the window learns which version it is.
    let shown = $derived(forMode(page, session.server));

    // The initial value only, on purpose: the page a tab draws never changes.
    // svelte-ignore state_referenced_locally
    let chosen = $state(remembered.get(page.title) ?? '');
    // The section on show: the one chosen, while this version of the page has
    // it, and otherwise the first -- so a section only one version has is never
    // left highlighted in the other.
    let active = $derived(shown.sections.some((s) => s.id === chosen) ? chosen : (shown.sections[0]?.id ?? ''));
    let section = $derived(shown.sections.find((s) => s.id === active));
    let panel = $state<HTMLElement | null>(null);

    function show(id: string): void {
        chosen = id;
        remembered.set(page.title, id);
        panel?.scrollTo({ top: 0 });
    }

    // Up and down move along the rail, as in the settings tab. Left and right
    // are left to the tab strip.
    function onRailKey(event: KeyboardEvent): void {
        const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
        if (step === 0) return;
        event.preventDefault();
        const sections = shown.sections;
        const at = sections.findIndex((s) => s.id === active);
        const next = sections[(at + step + sections.length) % sections.length];
        show(next.id);
        document.getElementById(`doc-nav-${next.id}`)?.focus();
    }

    /** The cluster a "show me" opens in: the one selected in the sidebar. */
    let context = $derived(workspace.selectedContext);

    function run(action: Action): void {
        switch (action.kind) {
            case 'show':
                if (context) workspace.openTab(context.id, action.resource);
                return;
            case 'settings':
                rememberSection(action.section);
                workspace.openSettings();
                return;
            case 'page':
                if (action.page === 'help') workspace.openHelp();
                else workspace.openKubernetesPrimer();
                return;
        }
    }

    function showTitle(): string {
        return context
            ? `Opens the list in ${workspace.displayName(context)}`
            : 'Select a context in the sidebar first, and this opens the list there';
    }
</script>

{#snippet runs(text: string)}
    {#each inline(text) as run, i (i)}
        {#if run.kind === 'bold'}<strong>{run.text}</strong>{:else if run.kind === 'code'}<code>{run.text}</code>{:else}{run.text}{/if}
    {/each}
{/snippet}

<div class="doc">
    <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
    <nav class="rail" role="tablist" aria-orientation="vertical" aria-label="{page.title} sections" onkeydown={onRailKey}>
        <p class="rail-heading">{page.title}</p>
        {#each shown.sections as s (s.id)}
            <button
                id="doc-nav-{s.id}"
                role="tab"
                class="rail-item"
                class:active={active === s.id}
                aria-selected={active === s.id}
                aria-controls="doc-panel-{s.id}"
                tabindex={active === s.id ? 0 : -1}
                onclick={() => show(s.id)}
            >
                <Icon name={s.icon} size={15} />
                <span>{s.label}</span>
            </button>
        {/each}
    </nav>

    {#if section}
        <div class="panel" id="doc-panel-{section.id}" role="tabpanel" aria-labelledby="doc-nav-{section.id}" tabindex="-1" bind:this={panel}>
            <article>
                <h1>{section.label}</h1>
                {#if section.lede}<p class="lede">{@render runs(section.lede)}</p>{/if}

                {#each section.blocks as block, i (i)}
                    {#if block.type === 'p'}
                        <p>{@render runs(block.text)}</p>
                    {:else if block.type === 'h3'}
                        <h3>{block.text}</h3>
                    {:else if block.type === 'note'}
                        <p class="note"><Icon name="info" size={13} /><span>{@render runs(block.text)}</span></p>
                    {:else if block.type === 'list'}
                        <ul>
                            {#each block.items as item, j (j)}<li>{@render runs(item)}</li>{/each}
                        </ul>
                    {:else if block.type === 'steps'}
                        <ol>
                            {#each block.items as item, j (j)}<li>{@render runs(item)}</li>{/each}
                        </ol>
                    {:else if block.type === 'code'}
                        <figure>
                            <pre class="selectable">{block.text}</pre>
                            {#if block.caption}<figcaption>{@render runs(block.caption)}</figcaption>{/if}
                        </figure>
                    {:else if block.type === 'table'}
                        <div class="table-scroll">
                            <table>
                                <thead>
                                    <tr>{#each block.head as cell, j (j)}<th>{cell}</th>{/each}</tr>
                                </thead>
                                <tbody>
                                    {#each block.rows as row, j (j)}
                                        <tr>{#each row as cell, k (k)}<td>{@render runs(cell)}</td>{/each}</tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {:else if block.type === 'terms'}
                        <dl class="terms">
                            {#each block.terms as term (term.term)}
                                <div class="term">
                                    <dt>{term.term}</dt>
                                    <dd>
                                        <span>{@render runs(term.meaning)}</span>
                                        <span class="term-links">
                                            {#if term.resource}
                                                <button
                                                    class="show"
                                                    disabled={!context}
                                                    title={showTitle()}
                                                    onclick={() => run({ kind: 'show', resource: term.resource ?? '', label: '' })}
                                                >
                                                    <Icon name="chevron-right" size={11} /> show me
                                                </button>
                                            {/if}
                                            {#if term.href}
                                                <a
                                                    href={term.href}
                                                    target="_blank"
                                                    rel="noreferrer noopener"
                                                    onclick={onExternalClick(term.href)}>docs</a>
                                            {/if}
                                        </span>
                                    </dd>
                                </div>
                            {/each}
                        </dl>
                    {:else if block.type === 'links'}
                        <ul class="links">
                            {#each block.links as link (link.href)}
                                <li>
                                    <a
                                        href={link.href}
                                        target="_blank"
                                        rel="noreferrer noopener"
                                        onclick={onExternalClick(link.href)}>{link.label}</a>
                                    {#if link.note}<span class="link-note">{@render runs(link.note)}</span>{/if}
                                </li>
                            {/each}
                        </ul>
                    {:else if block.type === 'actions'}
                        <div class="actions">
                            {#each block.actions as action (action.label)}
                                {#if action.kind === 'show'}
                                    <button class="action primary" disabled={!context} title={showTitle()} onclick={() => run(action)}>
                                        <Icon name="chevron-right" size={12} />
                                        {action.label}
                                    </button>
                                {:else}
                                    <button class="action" onclick={() => run(action)}>
                                        <Icon name={action.kind === 'settings' ? 'settings' : action.page === 'help' ? 'help' : 'book'} size={12} />
                                        {action.label}
                                    </button>
                                {/if}
                            {/each}
                        </div>
                    {/if}
                {/each}
            </article>
        </div>
    {/if}
</div>

<style>
    .doc {
        display: flex;
        height: 100%;
        min-height: 0;
    }

    .rail {
        display: flex;
        flex-direction: column;
        gap: 2px;
        flex: 0 0 auto;
        width: 200px;
        padding: 16px 10px;
        border-right: 1px solid var(--border);
        background: var(--bg-sidebar);
        overflow-y: auto;
    }

    .rail-heading {
        margin: 0 0 10px 8px;
        font-size: 10px;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .rail-item {
        display: flex;
        align-items: center;
        gap: 9px;
        width: 100%;
        padding: 7px 9px;
        border-radius: var(--radius-sm);
        font-size: 12.5px;
        text-align: left;
        color: var(--text-dim);
    }

    .rail-item:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .rail-item.active {
        background: var(--bg-active);
        color: var(--text);
        font-weight: 500;
    }

    .rail-item:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: -2px;
    }

    .panel {
        flex: 1 1 auto;
        min-width: 0;
        overflow-y: auto;
        padding: 24px 28px 48px;
    }

    .panel:focus {
        outline: none;
    }

    /* Prose measure: wide enough for a table, narrow enough to read. */
    article {
        max-width: 78ch;
        line-height: 1.65;
        font-size: 13px;
    }

    h1 {
        margin: 0 0 6px;
        font-size: 20px;
        font-weight: 600;
    }

    .lede {
        margin: 0 0 18px;
        color: var(--text-dim);
        font-size: 13.5px;
    }

    h3 {
        margin: 22px 0 6px;
        font-size: 11px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    p {
        margin: 0 0 12px;
    }

    ul,
    ol {
        margin: 0 0 12px;
        padding-left: 22px;
    }

    li {
        margin-bottom: 4px;
    }

    code {
        font-family: var(--mono);
        font-size: 11.5px;
        padding: 1px 4px;
        border-radius: 3px;
        background: var(--bg-raised);
    }

    strong {
        font-weight: 600;
        color: var(--text);
    }

    figure {
        margin: 0 0 14px;
    }

    pre {
        margin: 0;
        padding: 10px 12px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        font-family: var(--mono);
        font-size: 11.5px;
        line-height: 1.5;
        overflow-x: auto;
    }

    figcaption {
        margin-top: 5px;
        font-size: 11.5px;
        color: var(--text-faint);
    }

    .note {
        display: flex;
        gap: 8px;
        padding: 8px 11px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        color: var(--text-dim);
        font-size: 12.5px;
    }

    .note :global(svg) {
        flex: 0 0 auto;
        margin-top: 3px;
        color: var(--accent);
    }

    .table-scroll {
        overflow-x: auto;
        margin: 0 0 14px;
    }

    table {
        border-collapse: collapse;
        width: 100%;
        font-size: 12.5px;
    }

    th,
    td {
        text-align: left;
        vertical-align: top;
        padding: 6px 10px 6px 0;
        border-bottom: 1px solid var(--border-soft);
    }

    th {
        font-size: 11px;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--text-faint);
        font-weight: 600;
    }

    td:first-child {
        white-space: nowrap;
        color: var(--text);
    }

    .terms {
        margin: 0 0 14px;
    }

    .term {
        display: grid;
        grid-template-columns: minmax(120px, 170px) 1fr;
        gap: 4px 14px;
        padding: 7px 0;
        border-bottom: 1px solid var(--border-soft);
    }

    dt {
        font-weight: 600;
        color: var(--text);
    }

    dd {
        margin: 0;
        color: var(--text-dim);
    }

    .term-links {
        display: inline-flex;
        gap: 10px;
        margin-left: 8px;
        white-space: nowrap;
    }

    .show {
        display: inline-flex;
        align-items: center;
        gap: 2px;
        font-size: 11.5px;
        color: var(--accent);
    }

    .show:disabled {
        color: var(--text-faint);
        cursor: default;
    }

    a {
        color: var(--accent);
    }

    .links {
        list-style: none;
        padding: 0;
    }

    .links li {
        margin-bottom: 6px;
    }

    .link-note {
        margin-left: 8px;
        color: var(--text-faint);
        font-size: 12px;
    }

    .actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin: 2px 0 16px;
    }

    .action {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        font-size: 12.5px;
        color: var(--text-dim);
    }

    .action:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .action.primary:not(:disabled) {
        background: var(--accent);
        color: var(--accent-text);
        box-shadow: none;
    }

    .action:disabled {
        opacity: 0.55;
        cursor: default;
    }
</style>
