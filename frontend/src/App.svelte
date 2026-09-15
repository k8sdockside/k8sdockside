<!--
  The application shell: the four panes the user's views are arranged into, and
  nothing else. All of the state it renders lives in the workspace store.

  The panes are fixed places rather than a tree of splits, and what goes in each
  of them is the user's choice -- see lib/state/panes.ts. Main fills what the
  others leave; the side panels are there when they hold something; the bottom
  one is always there, because a place has to be visible before anything can be
  put in it.

  Both of the things that used to have a place of their own here are tabs now:
  the cluster tree, which cannot be closed but can be moved and hidden with
  Cmd/Ctrl+B, and the describe panel, which follows the selection into whichever
  pane it was last dragged to.
-->
<script lang="ts">
    import { onMount } from 'svelte';
    import Icon from './lib/components/Icon.svelte';
    import Pane from './lib/components/Pane.svelte';
    import TopBar from './lib/components/TopBar.svelte';
    import { workspace } from './lib/state/workspace.svelte';
    import { notices } from './lib/state/notices.svelte';
    import { session } from './lib/state/session.svelte';
    import { rowMetrics } from './lib/density';
    import { applyTheme } from './lib/theme/apply';

    onMount(() => {
        // Which version this is -- the desktop app or the web one -- and who
        // is using it. Not waited for: the window is the desktop app until it
        // hears otherwise, and the parts that differ follow the answer.
        void session.load();
        workspace.load();
    });

    // Zoom is applied as CSS on the app's own element, not through the window.
    //
    // Wails' native zoom cannot do this job on macOS: it clamps the scale to a
    // minimum of 1.0, so zooming out is silently discarded, and what it does
    // apply is -[WKWebView setMagnification:], which scales the rendered
    // surface without reflowing -- so the page keeps its full layout width and
    // spills out of the window with its own scrollbars, outside anything CSS
    // can say about overflow.
    //
    // CSS zoom reflows instead. Everything sized in pixels scales together, the
    // sidebar included, and the regions that scroll go on scrolling. It is set
    // on #app rather than the root because a percentage height under a zoomed
    // root ignores that zoom -- see the note in public/style.css.
    $effect(() => {
        const scale = workspace.zoom;
        document.documentElement.style.setProperty('--app-zoom', String(scale));

        // The title bar is the one thing that must not scale away. It is drawn
        // here in CSS pixels, while the macOS traffic lights over it keep their
        // real size -- so zooming out has to make it proportionally taller to
        // go on containing them. Zooming in it grows on its own.
        document.documentElement.style.setProperty('--topbar-h', `${Math.max(44, 44 / scale)}px`);
    });

    // The appearance preferences are applied to the document root rather than
    // scoped to a component, because they are what every component's own tokens
    // resolve against.
    //
    // The theme is a whole palette written onto the root by applyTheme, not a
    // class or an attribute a stylesheet keys off. That is what lets a theme be
    // a file rather than a code change: there is no rule anywhere in the app
    // that names one, so adding a thirteenth costs nothing but the file, and a
    // theme somebody wrote this morning goes through the identical path.
    $effect(() => {
        const theme = workspace.activeTheme;
        if (theme) applyTheme(theme);
    });

    // The density preference, written onto the root as the two custom
    // properties every table and the sidebar read. See lib/density.ts for what
    // each one is worth and why the pair is decided in one place.
    $effect(() => {
        const root = document.documentElement;
        const metrics = rowMetrics(workspace.density);
        root.style.setProperty('--row-h', metrics.height);
        root.style.setProperty('--cell-pad-y', metrics.padding);
    });

    function onZoomKey(event: KeyboardEvent): void {
        // The one unmodified key: help, where every desktop app keeps it.
        if (event.key === 'F1') {
            event.preventDefault();
            workspace.openHelp();
            return;
        }
        if (!event.metaKey && !event.ctrlKey) return;
        // `code` as well as `key`, so the numeric keypad works and so that a
        // layout where + needs shift is still recognised.
        switch (event.key) {
            case '+':
            case '=':
                event.preventDefault();
                workspace.zoomIn();
                return;
            case '-':
            case '_':
                event.preventDefault();
                workspace.zoomOut();
                return;
            case '0':
                event.preventDefault();
                workspace.resetZoom();
                return;
        }
        if (event.key === ',') {
            event.preventDefault();
            workspace.openSettings();
            return;
        }
        // The same key VS Code hides its sidebar with, and the reversible
        // version of the close button the cluster tree deliberately does not
        // have.
        if (event.key === 'b' || event.key === 'B') {
            event.preventDefault();
            workspace.toggleClusters();
            return;
        }
        if (event.code === 'NumpadAdd') {
            event.preventDefault();
            workspace.zoomIn();
        } else if (event.code === 'NumpadSubtract') {
            event.preventDefault();
            workspace.zoomOut();
        }
    }

    // Notices are informational; they should not need dismissing by hand.
    $effect(() => {
        if (!notices.current) return;
        const timer = setTimeout(() => notices.dismiss(), 6000);
        return () => clearTimeout(timer);
    });

</script>

<svelte:window onkeydown={onZoomKey} />

<div class="shell">
    <TopBar />

    <div class="body">
        <!-- Outside <main> so it keeps its full height: the bottom panel spans
             the view and whatever is beside it, and shortening the cluster list
             by however tall a log stream happens to be is not a trade the tree
             should have to make. -->
        <Pane pane="left" />

        <main>
            <div class="upper">
                <Pane pane="main" empty={welcome} />
                <Pane pane="right" />
            </div>

            <!-- Under the view rather than beside the sidebar: it keeps the
                 sidebar whole-height, so the context list is never shortened
                 by whatever is open at the foot of the window. -->
            <Pane pane="bottom" />
        </main>
    </div>

    {#snippet welcome()}
        <div class="welcome-stage">
            <div class="welcome">
                <h1>K8s Dockside</h1>
                {#if workspace.contexts.length > 0}
                    <p>Pick a context in the sidebar, then choose a view to open it as a tab.</p>
                    <p class="hint">
                        Tabs take the colour of their context, so you always know which cluster you are
                        looking at. Drag them to reorder, or into another panel to keep two views side by
                        side.
                    </p>
                {:else if workspace.loaded && session.server}
                    <!-- The web version has no disk of the user's to look
                         on: clusters are kubeconfigs an administrator
                         uploads on the gateway's own page. -->
                    <p>No clusters yet.</p>
                    <p class="hint">
                        An administrator adds clusters under Administration → Clusters. They appear here as
                        soon as one has.
                    </p>
                {:else if workspace.loaded}
                    <p>No kubeconfig contexts yet.</p>
                    <p class="hint">
                        Nothing turned up in <code>~/.kube</code> or <code>$KUBECONFIG</code>. Use the
                        sidebar to add kubeconfig files, or point it at a folder and it will take every
                        one in there, whatever they are named.
                    </p>
                {:else}
                    <p>Looking for kubeconfig files…</p>
                {/if}
                <p class="welcome-links">
                    {#if workspace.loaded && workspace.contexts.length === 0 && session.admin && session.clustersUrl}
                        <!-- A page of the same site, so an ordinary link. -->
                        <a href={session.clustersUrl}><Icon name="server" size={13} /> Manage clusters</a>
                    {/if}
                    <button onclick={() => workspace.openHelp()}><Icon name="help" size={13} /> How to use K8s Dockside</button>
                    <button onclick={() => workspace.openKubernetesPrimer()}><Icon name="book" size={13} /> New to Kubernetes?</button>
                </p>
            </div>
        </div>
    {/snippet}

    <footer class="statusbar">
        {#if notices.current}
            <span class="notice" class:error={notices.current.tone === 'error'}>
                {#if notices.current.tone === 'error'}<Icon name="alert" size={12} />{/if}
                {notices.current.text}
            </span>
            <button class="dismiss" onclick={() => notices.dismiss()} aria-label="Dismiss message">
                <Icon name="close" size={11} />
            </button>
        {:else if workspace.selectedContext}
            <span class="dot" style:background={workspace.colorOf(workspace.selectedContext.id)}></span>
            <span>{workspace.displayName(workspace.selectedContext)}</span>
            {#if workspace.selectedContext.server}
                <span class="dim mono">{workspace.selectedContext.server}</span>
            {/if}
        {/if}

        <span class="spacer"></span>
        <span class="dim">{workspace.contexts.length} contexts · {workspace.files.length} files</span>
        <!-- A path on the server in the web version, which is nothing the
             user can open or needs to know. -->
        {#if workspace.configPath && !session.server}
            <span class="dim mono" title="Where your context names, colours and layout are stored">
                {workspace.configPath}
            </span>
        {/if}
    </footer>
</div>

<style>
    .shell {
        display: flex;
        flex-direction: column;
        height: 100%;
    }

    .body {
        display: flex;
        flex: 1 1 auto;
        min-height: 0;
    }

    main {
        display: flex;
        flex-direction: column;
        /* Basis 0 rather than auto: what is in the view must not decide how
           much of the window the view asks for. With `auto` the basis is the
           content's own width, so one wide table made the whole row overflow
           and the sidebar paid for it. */
        flex: 1 1 0;
        min-width: 0;
        min-height: 0;
    }

    /* Main and the right panel share the room above the bottom one. */
    .upper {
        display: flex;
        flex: 1 1 auto;
        min-height: 0;
        min-width: 0;
    }

    /* The harbour behind the idle screen. It fills the empty content area
       rather than sitting at a fixed size, so the app looks like itself when
       nothing is open -- and goes with the panel the moment a tab is, rather
       than sitting behind a table of pod names. */
    .welcome-stage {
        position: relative;
        display: flex;
        align-items: center;
        justify-content: center;
        height: 100%;
        overflow-y: auto;
    }

    /* The image lives on a pseudo-element so it alone can be faded. Setting
       opacity on the container would take the text down with it, and the text
       is the part that has to stay readable -- which is the whole difference
       between a background and a picture.

       `contain`-style sizing keeps the mark whole at any window size, and it is
       kept out of the way of the pointer so nothing here is selectable. */
    .welcome-links {
        display: flex;
        gap: 8px;
        justify-content: center;
        flex-wrap: wrap;
        margin-top: 18px;
    }

    .welcome-links button,
    .welcome-links a {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 6px 11px;
        border-radius: var(--radius-sm);
        background: var(--bg-raised);
        box-shadow: inset 0 0 0 1px var(--border-soft);
        font-size: 12.5px;
        color: var(--text-dim);
        text-decoration: none;
    }

    .welcome-links button:hover,
    .welcome-links a:hover {
        background: var(--bg-hover);
        color: var(--text);
    }

    .welcome-stage::before {
        content: '';
        position: absolute;
        inset: 0;
        /* The neon harbour by night: drawn for the dark themes, and faded just
           enough that the theme's own ground shows through it and the heading
           stays legible against the sky. `cover` keeps the ship in frame at
           any pane shape; what goes is sky or sea at the edges, never the
           middle. See frontend/artwork/neon_harbour.py for how it is made. */
        background-image: url('/k8s_dockside_neon_harbour.svg');
        background-repeat: no-repeat;
        background-position: center;
        background-size: cover;
        opacity: 0.72;
        pointer-events: none;
    }

    /* A light theme gets the same harbour by day. The night picture faded
       over a pale window only ever reads as grey; the day one is drawn for
       it, so it can be shown almost whole. */
    :global([data-theme-base='light']) .welcome-stage::before {
        background-image: url('/k8s_dockside_neon_harbour_light.svg');
        opacity: 0.85;
    }

    .welcome {
        /* Above the watermark, not through it. */
        position: relative;
        z-index: 1;
        max-width: 520px;
        padding: 64px 32px;
        margin: 0 auto;
        text-align: center;
    }

    .welcome h1 {
        margin: 0 0 12px;
        font-size: 22px;
        font-weight: 600;
    }

    .welcome p {
        margin: 0 0 10px;
        color: var(--text-dim);
    }

    .welcome .hint {
        font-size: 12px;
        color: var(--text-faint);
        line-height: 1.7;
    }

    .welcome code {
        font-family: var(--mono);
        font-size: 11px;
        background: var(--bg-raised);
        border-radius: 3px;
        padding: 1px 5px;
    }

    .statusbar {
        display: flex;
        align-items: center;
        gap: 10px;
        height: 24px;
        padding: 0 12px;
        flex: 0 0 auto;
        background: var(--bg-sidebar);
        border-top: 1px solid var(--border);
        font-size: 11px;
        color: var(--text-dim);
        white-space: nowrap;
        overflow: hidden;
    }

    .spacer {
        flex: 1 1 auto;
    }

    .dot {
        width: 8px;
        height: 8px;
        border-radius: 2px;
        flex: 0 0 auto;
    }

    .dim {
        color: var(--text-faint);
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .mono {
        font-family: var(--mono);
        font-size: 10px;
    }

    .notice {
        display: flex;
        align-items: center;
        gap: 6px;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .notice.error {
        color: var(--error);
    }

    .dismiss {
        display: grid;
        place-items: center;
        width: 16px;
        height: 16px;
        border-radius: 3px;
        color: var(--text-faint);
    }

    .dismiss:hover {
        background: var(--bg-hover);
        color: var(--text);
    }
</style>
