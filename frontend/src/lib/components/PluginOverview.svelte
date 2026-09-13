<!--
  A solution plugin's landing page for one cluster.

  It exists to answer, in this order: is this thing even installed here, what is
  missing if it is not, and how is what it manages doing. That order is the
  whole design. A plugin is installed on *this machine* and the solution it
  describes is installed in a *cluster*, and those come apart constantly -- you
  keep the Argo CD plugin and open a cluster that has never heard of it. Without
  this page that state is four tabs that each open onto "the argoproj.io API is
  not installed", which is the same sentence four times and never the one the
  reader wanted, which is "Argo CD is not in this cluster".
-->
<script lang="ts">
    import { PluginService } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
    import { pluginKindFor } from '../catalogue';
    import { classify } from '../errors';
    import { adoptPluginSummary } from '../plugins/adopt';
    import { standingOf } from '../plugins/credit';
    import PluginCredit from './PluginCredit.svelte';
    import type { CardResult, Plugin, PluginSummary } from '../plugins/types';
    import { workspace } from '../state/workspace.svelte';
    import { clusters } from '../state/health.svelte';
    import MetricsPanel from '../charts/MetricsPanel.svelte';
    import ErrorState from './ErrorState.svelte';
    import Icon from './Icon.svelte';
    import { onExternalClick } from '../links';

    interface Props {
        contextId: string;
        /** The `plugin:<id>/overview` kind this tab was opened with. */
        kind: string;
    }

    let { contextId, kind }: Props = $props();

    let summary = $state<PluginSummary | null>(null);
    let error = $state<string | null>(null);
    let loading = $state(true);
    /** Bumped by the retry button; the loading effect reads it as a dependency. */
    let attempt = $state(0);

    let plugin = $derived<Plugin | null>(workspace.pluginFor(kind));
    let color = $derived(workspace.colorOf(contextId));
    let context = $derived(workspace.contexts.find((c) => c.id === contextId) ?? null);

    $effect(() => {
        const id = contextId;
        const current = plugin?.id;
        attempt;
        if (!current) return;

        let cancelled = false;
        loading = true;
        error = null;

        PluginService.Summary(id, current)
            .then((result) => {
                if (cancelled) return;
                summary = adoptPluginSummary(result);
                // A summary that came back is evidence the cluster answered,
                // whatever it said about the plugin.
                if (summary.checked) clusters.report(id, 'connected');
            })
            .catch((err: unknown) => {
                if (cancelled) return;
                error = err instanceof Error ? err.message : String(err);
            })
            .finally(() => {
                if (!cancelled) loading = false;
            });

        return () => {
            cancelled = true;
        };
    });

    /** The requirements that are actually missing, which is what to lead with. */
    let missing = $derived(
        (summary?.requirements ?? []).filter((req) => !req.served && !req.optional && req.error === ''),
    );

    /** A card's biggest bucket is drawn as a bar; this is its share. */
    function share(card: CardResult, count: number): number {
        if (card.total <= 0) return 0;
        return Math.round((count / card.total) * 100);
    }

    /**
     * The ring's slices: each bucket's share of the circumference and where
     * it starts. The circle is drawn with a circumference of 100, so a share
     * is a dash length and a start is a dash offset, and there is no
     * trigonometry to get wrong.
     */
    function slices(card: CardResult): { value: string; tone: string; length: number; start: number }[] {
        if (card.total <= 0) return [];
        let start = 0;
        return card.buckets.map((bucket) => {
            const length = (bucket.count / card.total) * 100;
            const slice = { value: bucket.value, tone: bucket.tone || 'plain', length, start };
            start += length;
            return slice;
        });
    }

    /** The view listing what a card counts, so the card can open it. */
    function viewFor(card: CardResult): string | null {
        const view = plugin?.views.find((v) => v.kind === card.kind);
        return view && plugin ? pluginKindFor(plugin.id, view.id) : null;
    }

    /** How a value with no name reads. An absent field is not the value "". */
    function bucketLabel(value: string): string {
        return value === '' ? 'no status yet' : value;
    }
</script>

<div class="overview">
    {#if !plugin}
        <!-- The tab outlived its plugin: restored from a saved session after the
             folder it came from was dropped, or opened on another machine. -->
        <div class="gone">
            <Icon name="alert" size={18} />
            <div>
                <h1>This plugin is not installed</h1>
                <p>
                    The tab was opened on <code>{kind}</code>, and nothing by that name is in your plugin folders.
                    Add the folder it came from under Settings → Plugins, or close this tab.
                </p>
            </div>
        </div>
    {:else if loading && !summary}
        <p class="status">Checking this cluster for {plugin.name}…</p>
    {:else if error}
        <ErrorState message={error} {context} onRetry={() => attempt++} />
    {:else if summary}
        <header class="head" style:--ctx-color={color}>
            <div class="title">
                <Icon name={plugin.icon} size={22} />
                <div>
                    <h1>{plugin.name}</h1>
                    {#if plugin.tagline}<p class="tagline">{plugin.tagline}</p>{/if}
                </div>
            </div>

            {#if !summary.checked}
                <span class="badge unknown" title={summary.error}>could not check</span>
            {:else if summary.installed}
                <span class="badge ok"><Icon name="check" size={12} /> installed here</span>
            {:else}
                <span class="badge absent">not installed here</span>
            {/if}
        </header>

        {#if plugin.description}
            <p class="lede">{plugin.description}</p>
        {/if}

        {#if !summary.checked}
            <!-- Could not ask, which is not the same as asking and being told no.
                 Saying "not installed" here would send the reader to install
                 something they already have. -->
            <section class="banner warn">
                <Icon name="alert" size={15} />
                <div>
                    <h2>This cluster did not answer</h2>
                    <p>{classify(summary.error).headline}</p>
                    <p class="raw selectable">{summary.error}</p>
                </div>
            </section>
        {:else if missing.length > 0}
            <section class="banner">
                <Icon name="info" size={15} />
                <div>
                    <h2>{plugin.name} does not look installed in this cluster</h2>
                    <p>
                        The plugin is installed on this machine — it is this cluster that is missing
                        {missing.length === 1 ? 'the resource' : 'the resources'} it works on. Its views below will
                        open, and say the same thing.
                    </p>
                    {#if plugin.docs}
                        <p>
                            <a
                                href={plugin.docs}
                                target="_blank"
                                rel="noreferrer noopener"
                                onclick={onExternalClick(plugin.docs)}>Installation docs</a>
                        </p>
                    {/if}
                </div>
            </section>
        {/if}

        {#if summary.requirements.length > 0}
            <section>
                <h2>What it needs from the cluster</h2>
                <ul class="requirements">
                    {#each summary.requirements as req (req.kind)}
                        <li
                            class:met={req.served}
                            class:unknown={req.error !== ''}
                            class:optional={req.optional}
                        >
                            {#if req.error !== ''}
                                <Icon name="alert" size={13} />
                            {:else if req.served}
                                <Icon name="check" size={13} />
                            {:else}
                                <Icon name="close" size={13} />
                            {/if}
                            <span class="label">{req.label}</span>
                            <code class="selectable">{req.kind}</code>
                            {#if req.optional}<span class="optional">optional</span>{/if}
                        </li>
                    {/each}
                </ul>
            </section>
        {/if}

        {#if summary.cards.length > 0}
            <section>
                <h2>What it is managing</h2>
                <div class="cards">
                    {#each summary.cards as card (card.label + card.kind)}
                        {@const target = viewFor(card)}
                        <article class="card" class:empty={card.error !== ''}>
                            <header>
                                <!-- The card counts what one of the views lists,
                                     so its title opens that view. -->
                                {#if target}
                                    <button class="card-label link" onclick={() => workspace.openTab(contextId, target)} title="Open the list">
                                        {card.label}
                                        <Icon name="chevron-right" size={11} />
                                    </button>
                                {:else}
                                    <p class="card-label">{card.label}</p>
                                {/if}
                                {#if card.error === '' && !card.grouped}
                                    <p class="total">{card.total}</p>
                                {/if}
                            </header>

                            {#if card.error !== ''}
                                <p class="card-note">{card.error}</p>
                            {:else if card.total === 0}
                                <p class="card-note">None in this cluster</p>
                            {:else if card.grouped}
                                <!-- A ring with one slice per bucket and the
                                     total in the middle: how much of it is red
                                     is the first thing worth knowing. -->
                                <div class="grouped">
                                <div class="ring" role="img" aria-label="{card.total} {card.label}">
                                    <svg viewBox="0 0 42 42">
                                        <circle class="track" cx="21" cy="21" r="15.915" />
                                        {#each slices(card) as slice (slice.value)}
                                            <circle
                                                class="slice {slice.tone}"
                                                cx="21"
                                                cy="21"
                                                r="15.915"
                                                stroke-dasharray="{slice.length} {100 - slice.length}"
                                                stroke-dashoffset={25 - slice.start}
                                            ></circle>
                                        {/each}
                                    </svg>
                                    <span class="middle">{card.total}</span>
                                </div>
                                <ul class="buckets">
                                    {#each card.buckets as bucket (bucket.value)}
                                        <li>
                                            <span class="dot {bucket.tone || 'plain'}"></span>
                                            <span class="value" class:absent={bucket.value === ''}>
                                                {bucketLabel(bucket.value)}
                                            </span>
                                            <span class="count">{bucket.count}</span>
                                            <span class="bar" aria-hidden="true">
                                                <span
                                                    class="fill {bucket.tone || 'plain'}"
                                                    style:width="{share(card, bucket.count)}%"
                                                ></span>
                                            </span>
                                        </li>
                                    {/each}
                                </ul>
                                </div>
                            {/if}
                        </article>
                    {/each}
                </div>
            </section>
        {/if}

        <!-- The plugin's own charts, after the counts it can answer from the
             API server and before the list of views. Asked for by this tab's
             own kind: an overview is one plugin's page, and bare "overview"
             drew every plugin's charts here. -->
        <MetricsPanel {contextId} attach={kind} title="Charts" />

        {#if plugin.views.length > 0}
            <section>
                <h2>Views</h2>
                <div class="views">
                    {#each plugin.views as view (view.id)}
                        <button class="view" onclick={() => workspace.openTab(contextId, pluginKindFor(plugin.id, view.id))}>
                            <Icon name={view.icon} size={15} />
                            <span class="view-label">{view.label}</span>
                            {#if view.namespace}<span class="pin">in {view.namespace}</span>{/if}
                        </button>
                    {/each}
                </div>
            </section>
        {/if}

        <footer class="about">
            <span>
                {#if plugin.origin === 'builtin'}
                    Ships with k8sdockside
                {:else}
                    <span class="selectable">{plugin.origin}</span>
                {/if}
            </span>
            {#if plugin.version}<span>· v{plugin.version.replace(/^v/, '')}</span>{/if}
            {#if plugin.origin !== 'builtin'}
                <span>·</span>
                <PluginCredit author={plugin.author} authorUrl={plugin.authorUrl} standing={standingOf(plugin)} />
            {/if}
            <!-- What the plugin is about: its links, and its docs where the
                 links do not already carry them. -->
            {#each plugin.links ?? [] as link (link.url)}
                <a href={link.url} target="_blank" rel="noreferrer noopener" title={link.url} onclick={onExternalClick(link.url)}
                    >{link.label}</a
                >
            {/each}
            {#if plugin.docs && !(plugin.links ?? []).some((l) => l.url === plugin.docs)}
                <a
                    href={plugin.docs}
                    target="_blank"
                    rel="noreferrer noopener"
                    onclick={onExternalClick(plugin.docs)}>Documentation</a>
            {/if}
            <button class="refresh" onclick={() => attempt++} disabled={loading}>
                <Icon name="refresh" size={12} />
                {loading ? 'Checking…' : 'Check again'}
            </button>
        </footer>
    {/if}
</div>

<style>
    .overview {
        height: 100%;
        overflow-y: auto;
        padding: 22px 26px 40px;
    }

    .status {
        margin: 0;
        color: var(--text-faint);
    }

    .gone {
        display: flex;
        align-items: flex-start;
        gap: 12px;
        max-width: 60ch;
        margin: 40px auto;
    }

    .gone :global(svg) {
        flex: 0 0 auto;
        margin-top: 4px;
        color: var(--warn);
    }

    .gone h1 {
        margin: 0 0 8px;
        font-size: 17px;
        font-weight: 600;
    }

    .gone p {
        margin: 0;
        color: var(--text-dim);
        line-height: 1.7;
    }

    .head {
        display: flex;
        align-items: center;
        gap: 14px;
        padding-bottom: 14px;
        margin-bottom: 16px;
        border-bottom: 1px solid var(--border);
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
        flex: 1 1 auto;
        min-width: 0;
        color: var(--ctx-color);
    }

    .title h1 {
        margin: 0;
        font-size: 19px;
        font-weight: 600;
        color: var(--text);
    }

    .tagline {
        margin: 2px 0 0;
        font-size: 12px;
        color: var(--text-faint);
    }

    .badge {
        display: flex;
        align-items: center;
        gap: 5px;
        flex: 0 0 auto;
        padding: 3px 10px;
        border-radius: 999px;
        font-size: 11px;
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border);
        color: var(--text-dim);
    }

    .badge.ok {
        color: var(--ok);
    }

    .badge.absent,
    .badge.unknown {
        color: var(--text-faint);
    }

    .lede {
        margin: 0 0 18px;
        max-width: 78ch;
        line-height: 1.75;
        color: var(--text-dim);
    }

    .banner {
        display: flex;
        align-items: flex-start;
        gap: 10px;
        padding: 12px 14px;
        margin-bottom: 20px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .banner :global(svg) {
        flex: 0 0 auto;
        margin-top: 3px;
        color: var(--accent);
    }

    .banner.warn :global(svg) {
        color: var(--warn);
    }

    .banner h2 {
        margin: 0 0 5px;
        font-size: 13px;
        font-weight: 600;
    }

    .banner p {
        margin: 0 0 4px;
        max-width: 74ch;
        font-size: 12.5px;
        line-height: 1.7;
        color: var(--text-dim);
    }

    .banner .raw {
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-faint);
    }

    section {
        margin-bottom: 24px;
    }

    section h2 {
        margin: 0 0 10px;
        font-size: 11px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--text-faint);
    }

    .requirements {
        list-style: none;
        margin: 0;
        padding: 0;
        max-width: 70ch;
    }

    .requirements li {
        display: flex;
        align-items: center;
        gap: 9px;
        padding: 6px 2px;
        border-bottom: 1px solid var(--border-soft);
        color: var(--text-faint);
    }

    .requirements li :global(svg) {
        flex: 0 0 auto;
        color: var(--error);
    }

    .requirements li.met :global(svg) {
        color: var(--ok);
    }

    .requirements li.unknown :global(svg) {
        color: var(--warn);
    }

    /* An optional requirement that is absent is not a failure -- Argo CD
       without ApplicationSets is still Argo CD -- so it does not get the red
       cross that would send someone looking for a problem. */
    .requirements li.optional:not(.met):not(.unknown) :global(svg) {
        color: var(--text-faint);
    }

    .requirements .label {
        color: var(--text);
        flex: 0 0 auto;
    }

    .requirements code {
        flex: 1 1 auto;
        min-width: 0;
        font-family: var(--mono);
        font-size: 11px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .optional {
        flex: 0 0 auto;
        font-size: 10px;
        padding: 0 6px;
        line-height: 15px;
        border-radius: 7px;
        background: var(--bg-raised);
    }

    .cards {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
        gap: 12px;
    }

    .card {
        padding: 12px 14px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        box-shadow: inset 0 0 0 1px var(--border-soft);
    }

    .card header {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        gap: 10px;
        margin-bottom: 8px;
    }

    .card-label {
        margin: 0;
        font-size: 12px;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .card-label.link {
        display: inline-flex;
        align-items: center;
        gap: 3px;
        padding: 0;
        font: inherit;
        cursor: pointer;
    }

    .card-label.link:hover {
        color: var(--accent);
    }

    .grouped {
        display: flex;
        align-items: center;
        gap: 14px;
    }

    .grouped .buckets {
        flex: 1 1 auto;
        min-width: 0;
    }

    /* The ring. Stroke widths are in the circle's own units, so the same
       markup reads at any size the card gives it. */
    .ring {
        position: relative;
        flex: 0 0 auto;
        width: 84px;
        height: 84px;
    }

    .ring svg {
        width: 100%;
        height: 100%;
        transform: rotate(0deg);
    }

    .ring circle {
        fill: none;
        stroke-width: 5;
    }

    .ring .track {
        stroke: var(--bg-raised);
    }

    .ring .slice {
        stroke: var(--text-faint);
    }

    .ring .slice.ok {
        stroke: var(--ok);
    }

    .ring .slice.warn {
        stroke: var(--warn);
    }

    .ring .slice.error {
        stroke: var(--error);
    }

    .ring .slice.info {
        stroke: var(--accent);
    }

    .ring .middle {
        position: absolute;
        inset: 0;
        display: grid;
        place-items: center;
        font-size: 17px;
        font-weight: 600;
        font-variant-numeric: tabular-nums;
        color: var(--text);
    }

    .total {
        margin: 0;
        font-size: 20px;
        font-weight: 600;
        font-variant-numeric: tabular-nums;
        color: var(--text);
    }

    .card-note {
        margin: 0;
        font-size: 11.5px;
        line-height: 1.6;
        color: var(--text-faint);
    }

    .buckets {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 5px;
    }

    .buckets li {
        display: grid;
        grid-template-columns: 8px 1fr auto;
        grid-template-areas: 'dot value count' '. bar bar';
        align-items: center;
        gap: 2px 7px;
        font-size: 11.5px;
    }

    .dot {
        grid-area: dot;
        width: 7px;
        height: 7px;
        border-radius: 50%;
        background: var(--text-faint);
    }

    .dot.ok,
    .fill.ok {
        background: var(--ok);
    }

    .dot.warn,
    .fill.warn {
        background: var(--warn);
    }

    .dot.error,
    .fill.error {
        background: var(--error);
    }

    .dot.info,
    .fill.info {
        background: var(--accent);
    }

    .value {
        grid-area: value;
        color: var(--text-dim);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .value.absent {
        font-style: italic;
        color: var(--text-faint);
    }

    .count {
        grid-area: count;
        font-variant-numeric: tabular-nums;
        color: var(--text);
    }

    .bar {
        grid-area: bar;
        height: 3px;
        border-radius: 2px;
        background: var(--bg-raised);
        overflow: hidden;
    }

    .fill {
        display: block;
        height: 100%;
        background: var(--text-faint);
    }

    .fill.plain,
    .dot.plain {
        background: var(--text-faint);
    }

    .views {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
    }

    .view {
        display: flex;
        align-items: center;
        gap: 7px;
        padding: 7px 12px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        font-size: 12.5px;
        color: var(--text-dim);
    }

    .view:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .view-label {
        white-space: nowrap;
    }

    .pin {
        font-family: var(--mono);
        font-size: 10px;
        color: var(--text-faint);
    }

    .about {
        display: flex;
        align-items: center;
        flex-wrap: wrap;
        gap: 8px;
        padding-top: 14px;
        border-top: 1px solid var(--border-soft);
        font-size: 11px;
        color: var(--text-faint);
    }

    .about a,
    .banner a {
        color: var(--accent);
    }

    .refresh {
        display: flex;
        align-items: center;
        gap: 5px;
        margin-left: auto;
        padding: 3px 9px;
        border-radius: var(--radius-sm);
        font-size: 11px;
        color: var(--text-dim);
        box-shadow: inset 0 0 0 1px var(--border);
    }

    .refresh:hover:not(:disabled) {
        background: var(--bg-hover);
        color: var(--text);
    }

    .refresh:disabled {
        opacity: 0.5;
        cursor: default;
    }
</style>
