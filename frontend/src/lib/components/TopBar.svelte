<!--
  The window's title bar.

  It exists for two reasons. The obvious one is that the app should say what it
  is. The other is layout: on macOS the traffic lights are drawn over the top
  left of the web view, and without a bar of their own they land on top of the
  sidebar's heading. Giving them a full-width strip moves everything else out
  from under them.

  The title is centred on the window rather than in the space left over, so it
  does not drift as the panels are resized: the bar is three columns, and the
  two either side of the title are the same width whatever they hold. The left
  one holds the menus -- after the traffic lights, on macOS -- which are the
  one part of the window that is there whatever else has been hidden. The right
  one holds the search box, just after the title, and the bell at the far end
  -- see MenuBar.svelte, SearchBar.svelte and NotificationMenu.svelte.

  The web version adds who is signed in at the very end, where every site keeps
  it -- see AccountMenu.svelte. The desktop app has nobody to sign in, so its
  bar is exactly as it was.
-->
<script lang="ts">
    import { session } from '../state/session.svelte';
    import AccountMenu from './AccountMenu.svelte';
    import NotificationMenu from './NotificationMenu.svelte';
    import MenuBar from './MenuBar.svelte';
    import SearchBar from './SearchBar.svelte';

    /**
     * Whether the macOS traffic lights are drawn over the left of the bar:
     * in the desktop app on a Mac, and not in a browser on one.
     */
    let trafficLights = $derived(navigator.platform.startsWith('Mac') && !session.server);
</script>

<header class="topbar">
    <div class="lead" class:traffic-lights={trafficLights}>
        <MenuBar />
    </div>

    <div class="title">
        <img src="/icon-ship.svg" alt="" width="18" height="18" />
        <span>K8S Dockside</span>
    </div>

    <div class="trail">
        <SearchBar />
        <div class="menus">
            <NotificationMenu />
            {#if session.server}
                <AccountMenu />
            {/if}
        </div>
    </div>
</header>

<style>
    .topbar {
        /* The search panel is placed against this, so it hangs from the middle
           of the window rather than from the box. */
        position: relative;
        display: grid;
        /* minmax(0, …) rather than 1fr, whose minimum is its content: a wide
           search box would otherwise widen its column, and the title would
           drift off the centre line to make room. */
        grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
        align-items: center;
        flex: 0 0 auto;
        /* Set from the zoom level: the webview scales CSS pixels but the
           native traffic lights over this bar keep their real size, so the bar
           has to grow as the zoom shrinks to go on containing them. */
        height: var(--topbar-h, 44px);
        background: var(--bg-sidebar);
        border-bottom: 1px solid var(--border);
        /* Dragging the bar moves the window, as a title bar should. Wails reads
           this property off the element under the pointer. */
        --wails-draggable: drag;
    }

    .lead {
        display: flex;
        align-items: center;
        min-width: 0;
        padding-left: 8px;
    }

    /* The traffic lights sit at the left of this bar, at their real size
       whatever the zoom: 12px buttons from about 20px in, 20px apart. The
       menus start clear of them. */
    .lead.traffic-lights {
        padding-left: calc(84px / var(--app-zoom, 1));
    }

    .title {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: 13px;
        font-weight: 600;
        letter-spacing: 0.02em;
        color: var(--text);
        pointer-events: none;
        white-space: nowrap;
    }

    .title img {
        display: block;
        opacity: 0.95;
    }

    .trail {
        display: flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
        padding-left: 14px;
    }

    /* At the far right, where nothing else is. */
    .menus {
        display: flex;
        align-items: center;
        gap: 2px;
        flex: 0 0 auto;
        margin-left: auto;
        padding-right: 8px;
    }
</style>
