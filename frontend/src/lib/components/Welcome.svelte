<!--
  The start page: what the main pane shows when nothing is open in it.

  With clusters to hand it is a way in to them -- one card each, saying whether
  it answers and which products it runs, with the views most often wanted a
  click away. Without any it is the first three steps. Either way it sits on
  the start page's picture, which changes on its own and says what it is in
  the corner.
-->
<script lang="ts">
    import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
    import { backdrop, moodName } from '../backgrounds/backdrop.svelte';
    import { DASHBOARD, iconFor, labelFor } from '../catalogue';
    import { forwards } from '../state/forwards.svelte';
    import { clusters } from '../state/health.svelte';
    import { search } from '../state/search.svelte';
    import { session } from '../state/session.svelte';
    import { workspace } from '../state/workspace.svelte';
    import Backdrop from './Backdrop.svelte';
    import Icon from './Icon.svelte';
    import PluginMark from './PluginMark.svelte';

    /** How many cluster cards are shown before "show all". */
    const FIRST = 6;

    /** The views a card offers straight from the start page. */
    const QUICK = ['pods', 'deployments', 'nodes', 'events'];

    const MOD = typeof navigator !== 'undefined' && navigator.platform.startsWith('Mac') ? '⌘' : 'Ctrl';

    let showAll = $state(false);

    /**
     * Connected clusters first, because they are the ones you were working
     * in; the rest after, in the sidebar's own order.
     */
    let ordered = $derived.by(() => {
        const all = workspace.orderContexts(workspace.contexts);
        const live = (c: kube.Context) => {
            const status = clusters.of(c.id).status;
            return status === 'connected' || status === 'checking' ? 0 : 1;
        };
        return [...all].sort((a, b) => live(a) - live(b));
    });
    let shown = $derived(showAll ? ordered : ordered.slice(0, FIRST));
    let connected = $derived(workspace.connectedContexts.length);
    let tunnels = $derived(session.server ? 0 : forwards.list.filter((f) => f.state === 'active').length);

    let picture = $derived(backdrop.current(workspace.background));
    let rotating = $derived(
        !workspace.background.pinned && backdrop.picturesFor(workspace.background.source).length > 1,
    );

    function greeting(): string {
        const hour = new Date().getHours();
        const who = session.server && session.displayName ? `, ${session.displayName.split(' ')[0]}` : '';
        if (hour >= 5 && hour < 12) return `Good morning${who}`;
        if (hour >= 12 && hour < 17) return `Good afternoon${who}`;
        if (hour >= 17 && hour < 23) return `Good evening${who}`;
        return `Working late${who}?`;
    }

    /** The API server's host, which is what tells two clusters apart at a glance. */
    function hostOf(server: string): string {
        if (!server) return '';
        try {
            return new URL(server).host;
        } catch {
            return server;
        }
    }

    function status(contextId: string): { key: string; label: string; title: string } {
        const health = clusters.of(contextId);
        switch (health.status) {
            case 'connected':
                return { key: 'connected', label: 'Connected', title: 'This cluster is answering' };
            case 'checking':
                return { key: 'checking', label: 'Connecting…', title: 'Checking whether this cluster answers' };
            case 'error':
                return { key: 'error', label: 'Unreachable', title: health.message };
            default:
                return { key: 'idle', label: 'Not connected', title: 'Open it to connect' };
        }
    }

    /**
     * The plugins this cluster is known to run a product for. Only a known yes
     * is drawn -- a card is not the place to guess -- and a plugin that needs
     * nothing is at home everywhere, so it says nothing about this cluster.
     */
    function productsIn(contextId: string) {
        return workspace.enabledPlugins
            .filter((p) => p.requires.some((req) => !req.optional) && workspace.pluginInstalledIn(contextId, p) === true)
            .slice(0, 6);
    }

    function open(contextId: string, kind: string): void {
        workspace.selectContext(contextId);
        workspace.openTab(contextId, kind);
    }
</script>

<div class="welcome-stage">
    <Backdrop />

    <div class="welcome">
        <header class="hero">
            <img class="mark" src="/icon-ship.svg" alt="" width="52" height="52" />
            <div>
                <p class="greeting">{greeting()}</p>
                <h1>K8s Dockside</h1>
                <p class="tagline">
                    {#if workspace.contexts.length > 0}
                        Pick a cluster to open its dashboard, or jump straight to a view.
                    {:else}
                        Every cluster in your kubeconfigs, side by side.
                    {/if}
                </p>
            </div>
        </header>

        {#if workspace.contexts.length > 0}
            <div class="summary">
                <span class="stat"><Icon name="server" size={13} /><b>{workspace.contexts.length}</b> {workspace.contexts.length === 1 ? 'cluster' : 'clusters'}</span>
                <span class="stat" class:live={connected > 0}><span class="pip"></span><b>{connected}</b> connected</span>
                {#if tunnels > 0}
                    <span class="stat"><Icon name="forward" size={13} /><b>{tunnels}</b> {tunnels === 1 ? 'forward' : 'forwards'} open</span>
                {/if}
                {#if workspace.enabledPlugins.length > 0}
                    <span class="stat"><Icon name="puzzle" size={13} /><b>{workspace.enabledPlugins.length}</b> plugins</span>
                {/if}
                <button class="search" onclick={() => search.requestFocus()}>
                    <Icon name="search" size={13} />
                    Search every cluster
                    <kbd>{MOD}</kbd><kbd>K</kbd>
                </button>
            </div>

            <section class="clusters" aria-label="Clusters">
                {#each shown as context (context.id)}
                    {@const state = status(context.id)}
                    {@const products = productsIn(context.id)}
                    <article class="card" style:--ctx={workspace.colorOf(context.id)}>
                        <button
                            class="card-main"
                            onclick={() => open(context.id, DASHBOARD)}
                            title="Open the dashboard for {workspace.displayName(context)}"
                        >
                            <span class="card-head">
                                <span class="swatch"></span>
                                <span class="name">{workspace.displayName(context)}</span>
                                {#if context.current}<span class="badge">current</span>{/if}
                            </span>
                            <span class="host">{hostOf(context.server) || context.cluster}</span>
                            <span class="card-foot">
                                <span class="state {state.key}" title={state.title}><span class="pip"></span>{state.label}</span>
                                {#if products.length > 0}
                                    <span class="products">
                                        {#each products as plugin (plugin.id)}
                                            <span title={plugin.name}>
                                                <PluginMark id={plugin.id} icon={plugin.icon} logo={plugin.logo} size={15} />
                                            </span>
                                        {/each}
                                    </span>
                                {/if}
                            </span>
                        </button>
                        <div class="quick">
                            {#each QUICK as kind (kind)}
                                <button onclick={() => open(context.id, kind)} title="Open {labelFor(kind)} in {workspace.displayName(context)}">
                                    <Icon name={iconFor(kind)} size={13} />
                                    <span>{labelFor(kind)}</span>
                                </button>
                            {/each}
                        </div>
                    </article>
                {/each}
            </section>

            {#if ordered.length > FIRST}
                <button class="more" onclick={() => (showAll = !showAll)}>
                    {showAll ? 'Show fewer' : `Show all ${ordered.length} clusters`}
                </button>
            {/if}
        {:else if workspace.loaded && session.server}
            <!-- The web version has no disk of the user's to look on:
                 clusters are kubeconfigs an administrator uploads on the
                 gateway's own page. -->
            <div class="empty">
                <p>No clusters yet.</p>
                <p class="hint">An administrator adds clusters under Administration → Clusters. They appear here as soon as one has.</p>
                {#if session.admin && session.clustersUrl}
                    <a class="step-action" href={session.clustersUrl}><Icon name="server" size={13} /> Manage clusters</a>
                {/if}
            </div>
        {:else if workspace.loaded}
            <p class="hint lead">
                Nothing turned up in <code>~/.kube</code> or <code>$KUBECONFIG</code>. Three ways to get going:
            </p>
            <section class="steps" aria-label="Getting started">
                <article class="step">
                    <span class="num">1</span>
                    <h2>Add a kubeconfig</h2>
                    <p>Pick one or more files. Every context in them joins the sidebar.</p>
                    <button class="step-action" onclick={() => void workspace.addFile()}><Icon name="plus" size={13} /> Add files…</button>
                </article>
                <article class="step">
                    <span class="num">2</span>
                    <h2>Watch a folder</h2>
                    <p>Every kubeconfig in it is picked up, whatever it is named — now and whenever one is added.</p>
                    <button class="step-action" onclick={() => void workspace.addFolder()}><Icon name="folder-plus" size={13} /> Choose a folder…</button>
                </article>
                <article class="step">
                    <span class="num">3</span>
                    <h2>New to Kubernetes?</h2>
                    <p>A short primer on pods, deployments and the rest, as this app shows them.</p>
                    <button class="step-action" onclick={() => workspace.openKubernetesPrimer()}><Icon name="book" size={13} /> Read the primer</button>
                </article>
            </section>
        {:else}
            <p class="hint lead">Looking for kubeconfig files…</p>
        {/if}

        <nav class="links" aria-label="More">
            <button onclick={() => workspace.openHelp()}><Icon name="help" size={13} /> How to use K8s Dockside <kbd>F1</kbd></button>
            {#if workspace.contexts.length > 0}
                <button onclick={() => workspace.openKubernetesPrimer()}><Icon name="book" size={13} /> New to Kubernetes?</button>
            {/if}
            <button onclick={() => workspace.openSettings()}><Icon name="settings" size={13} /> Settings <kbd>{MOD}</kbd><kbd>,</kbd></button>
        </nav>
    </div>

    <!-- What the picture is, and the two things worth doing about it. -->
    <div class="picture">
        {#if picture}
            <Icon name="image" size={12} />
            <span class="picture-name" title={picture.kind === 'scene' ? picture.blurb : picture.name}>
                {picture.name}{#if moodName(picture)}<span class="mood">{` · ${moodName(picture)}`}</span>{/if}
            </span>
            {#if rotating}
                <button onclick={() => backdrop.next()} title="Show the next picture" aria-label="Show the next picture">
                    <Icon name="refresh" size={12} />
                </button>
            {/if}
        {/if}
        <button
            onclick={() => workspace.openBackgroundSettings()}
            title="Choose the start page's picture"
            aria-label="Choose the start page's picture"
        >
            <Icon name="sliders" size={12} />
        </button>
    </div>
</div>

<style>
    .welcome-stage {
        position: relative;
        display: flex;
        flex-direction: column;
        height: 100%;
        overflow: hidden;
    }

    .welcome {
        position: relative;
        z-index: 1;
        width: 100%;
        max-width: 980px;
        margin: 0 auto;
        padding: clamp(28px, 7vh, 72px) 32px 56px;
        overflow-y: auto;
        min-height: 0;
    }

    /* ----- the heading ---------------------------------------------------- */

    .hero {
        display: flex;
        align-items: center;
        gap: 18px;
        margin-bottom: 22px;
    }

    .mark {
        flex: 0 0 auto;
        filter: drop-shadow(0 6px 18px color-mix(in srgb, var(--accent) 45%, transparent));
    }

    .greeting {
        margin: 0 0 2px;
        font-size: 12px;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: var(--accent);
        font-weight: 600;
    }

    h1 {
        margin: 0;
        font-size: 30px;
        font-weight: 650;
        letter-spacing: -0.01em;
        color: var(--text);
        text-shadow: 0 2px 18px color-mix(in srgb, var(--bg) 70%, transparent);
    }

    .tagline {
        margin: 4px 0 0;
        color: var(--text-dim);
        font-size: 13.5px;
    }

    /* ----- the line of counts, and search -------------------------------- */

    .summary {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px;
        margin-bottom: 18px;
    }

    .stat,
    .search {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        height: 28px;
        padding: 0 11px;
        border-radius: 14px;
        font-size: 12px;
        color: var(--text-dim);
        background: color-mix(in srgb, var(--bg-panel) 72%, transparent);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        -webkit-backdrop-filter: blur(10px);
        backdrop-filter: blur(10px);
    }

    .stat b {
        color: var(--text);
        font-weight: 600;
    }

    .stat .pip {
        width: 7px;
        height: 7px;
        border-radius: 50%;
        background: var(--text-faint);
    }

    .stat.live .pip {
        background: var(--ok);
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent);
    }

    .search {
        margin-left: auto;
        color: var(--text);
        cursor: pointer;
    }

    .search:hover {
        background: color-mix(in srgb, var(--bg-raised) 85%, transparent);
        box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 55%, transparent);
    }

    kbd {
        font-family: var(--mono);
        font-size: 10px;
        min-width: 16px;
        padding: 1px 4px;
        border-radius: 4px;
        text-align: center;
        color: var(--text-dim);
        background: var(--bg-raised);
        box-shadow: inset 0 -1px 0 var(--border-soft);
    }

    /* ----- the cluster cards --------------------------------------------- */

    .clusters {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
        gap: 12px;
    }

    .card {
        position: relative;
        display: flex;
        flex-direction: column;
        border-radius: 10px;
        overflow: hidden;
        background: color-mix(in srgb, var(--bg-panel) 76%, transparent);
        box-shadow:
            inset 0 0 0 1px var(--border-soft),
            0 10px 30px -18px rgba(0, 0, 0, 0.6);
        -webkit-backdrop-filter: blur(14px) saturate(1.2);
        backdrop-filter: blur(14px) saturate(1.2);
        transition:
            transform 160ms ease,
            box-shadow 160ms ease;
    }

    /* The context's own colour, down the left edge, as its tabs wear it. */
    .card::before {
        content: '';
        position: absolute;
        left: 0;
        top: 0;
        bottom: 0;
        width: 3px;
        background: var(--ctx);
    }

    .card:hover {
        transform: translateY(-2px);
        box-shadow:
            inset 0 0 0 1px color-mix(in srgb, var(--ctx) 55%, transparent),
            0 14px 34px -16px color-mix(in srgb, var(--ctx) 45%, rgba(0, 0, 0, 0.6));
    }

    @media (prefers-reduced-motion: reduce) {
        .card,
        .card:hover {
            transition: none;
            transform: none;
        }
    }

    .card-main {
        display: flex;
        flex-direction: column;
        gap: 6px;
        padding: 13px 14px 11px 17px;
        text-align: left;
        min-width: 0;
    }

    .card-head {
        display: flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
    }

    .swatch {
        width: 10px;
        height: 10px;
        border-radius: 3px;
        flex: 0 0 auto;
        background: var(--ctx);
        box-shadow: 0 0 10px color-mix(in srgb, var(--ctx) 60%, transparent);
    }

    .name {
        font-size: 14px;
        font-weight: 600;
        color: var(--text);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .badge {
        flex: 0 0 auto;
        font-size: 9px;
        letter-spacing: 0.04em;
        text-transform: uppercase;
        color: var(--ctx);
        border: 1px solid var(--ctx);
        border-radius: 3px;
        padding: 0 4px;
        line-height: 14px;
    }

    .host {
        font-family: var(--mono);
        font-size: 11px;
        color: var(--text-faint);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .card-foot {
        display: flex;
        align-items: center;
        gap: 8px;
        min-height: 18px;
    }

    .state {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        font-size: 11.5px;
        color: var(--text-dim);
    }

    .state .pip {
        width: 7px;
        height: 7px;
        border-radius: 50%;
        background: var(--text-faint);
    }

    .state.connected .pip {
        background: var(--ok);
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent);
    }

    .state.checking .pip {
        background: var(--accent);
        animation: pulse 1.1s ease-in-out infinite;
    }

    .state.error {
        color: var(--error);
    }

    .state.error .pip {
        background: var(--error);
    }

    @keyframes pulse {
        50% {
            opacity: 0.25;
        }
    }

    .products {
        display: inline-flex;
        gap: 5px;
        margin-left: auto;
    }

    .quick {
        display: flex;
        border-top: 1px solid var(--border-soft);
    }

    .quick button {
        flex: 1 1 0;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: 5px;
        min-width: 0;
        height: 30px;
        font-size: 11px;
        color: var(--text-dim);
    }

    .quick button + button {
        border-left: 1px solid var(--border-soft);
    }

    .quick button span {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .quick button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .more {
        display: block;
        margin: 12px auto 0;
        padding: 5px 12px;
        border-radius: 14px;
        font-size: 12px;
        color: var(--text-dim);
        background: color-mix(in srgb, var(--bg-panel) 70%, transparent);
        box-shadow: inset 0 0 0 1px var(--border-soft);
    }

    .more:hover {
        color: var(--text);
    }

    /* ----- no clusters yet ----------------------------------------------- */

    .lead {
        margin: 0 0 16px;
    }

    .hint {
        font-size: 12.5px;
        color: var(--text-dim);
        line-height: 1.7;
    }

    .hint code {
        font-family: var(--mono);
        font-size: 11px;
        background: var(--bg-raised);
        border-radius: 3px;
        padding: 1px 5px;
    }

    .steps {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        gap: 12px;
    }

    .step {
        position: relative;
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding: 18px 18px 16px;
        border-radius: 10px;
        background: color-mix(in srgb, var(--bg-panel) 76%, transparent);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        -webkit-backdrop-filter: blur(14px);
        backdrop-filter: blur(14px);
    }

    .num {
        display: grid;
        place-items: center;
        width: 24px;
        height: 24px;
        border-radius: 50%;
        font-size: 12px;
        font-weight: 700;
        color: var(--accent-text);
        background: var(--accent);
    }

    .step h2 {
        margin: 2px 0 0;
        font-size: 14px;
        color: var(--text);
    }

    .step p {
        margin: 0;
        flex: 1 1 auto;
        font-size: 12px;
        line-height: 1.6;
        color: var(--text-dim);
    }

    .step-action {
        align-self: flex-start;
        display: inline-flex;
        align-items: center;
        gap: 6px;
        margin-top: 4px;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        font-size: 12.5px;
        color: var(--accent-text);
        background: var(--accent);
        text-decoration: none;
    }

    .step-action:hover {
        filter: brightness(1.1);
    }

    .empty {
        max-width: 520px;
    }

    .empty p {
        margin: 0 0 8px;
        color: var(--text-dim);
    }

    /* ----- the links at the foot ----------------------------------------- */

    .links {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-top: 28px;
    }

    .links button {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        font-size: 12.5px;
        color: var(--text-dim);
        background: color-mix(in srgb, var(--bg-raised) 75%, transparent);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        -webkit-backdrop-filter: blur(8px);
        backdrop-filter: blur(8px);
    }

    .links button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    /* ----- the picture's caption ----------------------------------------- */

    .picture {
        position: absolute;
        right: 12px;
        bottom: 10px;
        z-index: 2;
        display: flex;
        align-items: center;
        gap: 6px;
        height: 26px;
        padding: 0 4px 0 10px;
        border-radius: 13px;
        font-size: 11px;
        color: var(--text-faint);
        background: color-mix(in srgb, var(--bg-panel) 60%, transparent);
        -webkit-backdrop-filter: blur(8px);
        backdrop-filter: blur(8px);
        opacity: 0.75;
        transition: opacity 160ms ease;
    }

    .picture:hover,
    .picture:focus-within {
        opacity: 1;
    }

    .picture-name .mood {
        color: var(--text-faint);
        opacity: 0.8;
    }

    .picture-name {
        max-width: 240px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .picture button {
        display: grid;
        place-items: center;
        width: 20px;
        height: 20px;
        border-radius: 50%;
        color: var(--text-dim);
    }

    .picture button:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    @media (max-width: 560px) {
        .welcome {
            padding: 24px 16px 56px;
        }

        .hero {
            gap: 12px;
        }

        h1 {
            font-size: 24px;
        }

        .search {
            margin-left: 0;
        }
    }
</style>
