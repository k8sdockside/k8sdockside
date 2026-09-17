<!--
  The menus in the title bar: File, Clusters, View and Help.

  They began as one View menu, which existed because every panel can be hidden
  and one of them -- the cluster tree -- is how everything else gets opened. A
  keyboard shortcut is not an answer to "it has disappeared and I do not know
  why": the way back has to be something you can see. That is still the job of
  View; the rest moved out to menus named for what they act on, so nothing has
  to be looked for under a heading it does not belong to.

  They sit at the left of the bar, where desktop apps keep their menus, and
  clear of every platform's window buttons: after the traffic lights on macOS,
  and away from the right-hand end, where Windows and most Linux desktops put
  minimise, maximise and close.

  Drawn by the app rather than by the platform: this window wears its own title
  bar, so a native menu would be a second strip of chrome above it on Windows
  and Linux with a different look from everything below. What is here instead
  renders the same everywhere and takes the user's theme.
-->
<script lang="ts">
    import { search } from '../state/search.svelte';
    import { session } from '../state/session.svelte';
    import { updates } from '../state/updates.svelte';
    import { PANE_LABELS, workspace, type PaneId } from '../state/workspace.svelte';
    import { rememberSection } from './settings/section.svelte';
    import Icon from './Icon.svelte';

    interface Item {
        label: string;
        run: () => void;
        shortcut?: string;
        disabled?: boolean;
        /** Why it is disabled, or what it does when the label does not say. */
        title?: string;
        /** Set for a toggle: whether it is on. */
        checked?: boolean;
    }
    type Entry = Item | 'separator';

    interface Menu {
        label: string;
        entries: Entry[];
    }

    /** The modifier as this platform writes it, since the point is to be read. */
    const MOD = navigator.platform.startsWith('Mac') ? '⌘' : 'Ctrl+';

    /** The panels, in the order they appear on screen from left to right. */
    const PANELS: { pane: PaneId; shortcut?: string }[] = [
        { pane: 'left', shortcut: `${MOD}B` },
        { pane: 'right' },
        { pane: 'bottom' },
    ];

    /**
     * What a panel is called here.
     *
     * "(Explorer)" after the panel holding the cluster tree, because that is
     * what somebody looking for the missing thing is looking for -- and it
     * stops being true the moment the tree is dragged elsewhere.
     */
    function labelFor(pane: PaneId): string {
        const holdsTree = workspace.paneOf(workspace.CLUSTERS_TAB_ID) === pane;
        return holdsTree ? `${PANE_LABELS[pane]} (Explorer)` : PANE_LABELS[pane];
    }

    function openSettings(section?: string): void {
        if (section) rememberSection(section);
        workspace.openSettings();
    }

    let menus = $derived.by((): Menu[] => {
        const connected = workspace.connectedContexts;
        const desktopAdmin = !session.server && session.admin;
        const webAdmin = session.server && session.admin && !!session.clustersUrl;

        const file: Entry[] = [];
        if (desktopAdmin) {
            file.push(
                { label: 'Add kubeconfig files…', run: () => void workspace.addFile() },
                { label: 'Watch a folder of kubeconfigs…', run: () => void workspace.addFolder() },
                'separator',
            );
        }
        if (webAdmin) {
            file.push({ label: 'Manage clusters…', run: () => window.location.assign(session.clustersUrl) }, 'separator');
        }
        file.push({ label: 'Settings…', shortcut: `${MOD},`, run: () => openSettings() });

        const clusters: Entry[] = [
            { label: 'Search every cluster…', shortcut: `${MOD}K`, run: () => search.requestFocus() },
            {
                label: 'Rescan kubeconfigs',
                run: () => void workspace.sync(),
                disabled: workspace.syncing,
                title: 'Rescan the kubeconfig files and folders, and recheck the clusters already looked at',
            },
            'separator',
            { label: 'Expand all contexts', run: () => workspace.expandAll() },
            { label: 'Collapse all contexts', run: () => workspace.collapseAll() },
            'separator',
            ...connected.map(
                (context): Item => ({
                    label: `Disconnect ${workspace.displayName(context)}`,
                    run: () => void workspace.disconnect(context.id),
                    title: 'Close its tabs and stop talking to the cluster; open it again to reconnect',
                }),
            ),
            {
                label: 'Disconnect all',
                run: () => void workspace.disconnectAll(),
                disabled: connected.length === 0,
                title: connected.length === 0 ? 'No context is connected' : undefined,
            },
        ];

        const view: Entry[] = [
            ...PANELS.map((panel): Item => {
                const empty = workspace.panes[panel.pane].tabs.length === 0;
                return {
                    label: labelFor(panel.pane),
                    shortcut: panel.shortcut,
                    checked: workspace.isPaneOpen(panel.pane),
                    // A panel with nothing in it has nothing to show.
                    disabled: empty,
                    title: empty ? 'Nothing is open in this panel' : undefined,
                    run: () => workspace.togglePane(panel.pane),
                };
            }),
            { label: 'Reset layout', run: () => workspace.resetLayout() },
            'separator',
            { label: 'Zoom in', shortcut: `${MOD}+`, run: () => workspace.zoomIn(), disabled: workspace.zoom >= workspace.maxZoom },
            { label: 'Zoom out', shortcut: `${MOD}−`, run: () => workspace.zoomOut(), disabled: workspace.zoom <= workspace.minZoom },
            {
                label: `Actual size (${Math.round(workspace.zoom * 100)}%)`,
                shortcut: `${MOD}0`,
                run: () => workspace.resetZoom(),
                disabled: workspace.zoom === 1,
            },
        ];

        const help: Entry[] = [
            { label: 'Help', shortcut: 'F1', run: () => workspace.openHelp() },
            { label: 'Kubernetes primer', run: () => workspace.openKubernetesPrimer() },
            'separator',
        ];
        if (!session.server) {
            help.push({ label: 'Check for updates', run: () => void updates.check(), disabled: updates.checking });
        }
        help.push({ label: 'About K8s Dockside', run: () => openSettings('about') });

        return [
            { label: 'File', entries: file },
            { label: 'Clusters', entries: clusters },
            { label: 'View', entries: view },
            { label: 'Help', entries: help },
        ];
    });

    /** The menu showing, by label; null when none is. */
    let openLabel = $state<string | null>(null);
    let barEl = $state<HTMLElement | null>(null);

    function close(): void {
        openLabel = null;
    }

    function triggerOf(label: string): HTMLButtonElement | null {
        return barEl?.querySelector<HTMLButtonElement>(`button[data-menu="${label}"]`) ?? null;
    }

    function run(label: string, item: Item): void {
        close();
        // Back to the button, so the keyboard is where it was before the menu.
        triggerOf(label)?.focus();
        item.run();
    }

    /** Opens the menu so many places along from the one showing, wrapping. */
    function step(by: number): void {
        const at = menus.findIndex((m) => m.label === openLabel);
        if (at < 0) return;
        openLabel = menus[(at + by + menus.length) % menus.length]!.label;
    }

    function onKeyDown(event: KeyboardEvent): void {
        switch (event.key) {
            case 'Escape': {
                event.stopPropagation();
                const label = openLabel;
                close();
                if (label) triggerOf(label)?.focus();
                return;
            }
            case 'ArrowLeft':
            case 'ArrowRight':
                event.preventDefault();
                step(event.key === 'ArrowRight' ? 1 : -1);
                return;
            case 'ArrowDown':
            case 'ArrowUp': {
                event.preventDefault();
                const menu = barEl?.querySelector('[role="menu"]');
                const items = [...(menu?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])];
                if (items.length === 0) return;
                const at = items.indexOf(document.activeElement as HTMLButtonElement);
                const next = (at + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
                items[next]?.focus();
                return;
            }
        }
    }

    /** What a trigger does with a click: open its menu, or close it if it is the one showing. */
    function toggle(label: string): void {
        openLabel = openLabel === label ? null : label;
    }

    // Focus the first item as a menu opens, so it is usable without the mouse
    // that opened it -- and so the arrow keys have somewhere to start from.
    $effect(() => {
        if (!openLabel || !barEl) return;
        barEl.querySelector<HTMLButtonElement>('[role="menu"] button:not(:disabled)')?.focus();
    });

    /** How close a menu may come to the window's right edge. */
    const EDGE = 8;

    /**
     * Moves an opened menu left by as much as it would otherwise run past the
     * window's right edge, and no further than the window's left edge.
     *
     * A menu grows rightwards from its button, and in a narrow window -- or on
     * macOS, where the traffic lights push every button along -- the last ones
     * run out of room. It is moved, measured and moved again rather than moved
     * once, because the app is zoomed with CSS zoom, and whether a measured
     * pixel is a zoomed one differs between the engines this runs in; the
     * second measurement says what a pixel of offset actually came to.
     */
    function keepInside(node: HTMLElement): void {
        const box = node.getBoundingClientRect();
        const over = Math.min(box.right - (document.documentElement.clientWidth - EDGE), box.left - EDGE);
        if (over <= 0) return;

        node.style.left = `${-over}px`;
        const moved = box.left - node.getBoundingClientRect().left;
        if (moved > 0 && Math.abs(moved - over) > 0.5) {
            node.style.left = `${-over * (over / moved)}px`;
        }
    }

    /** Separators only between items: none first, last or twice in a row. */
    function tidy(entries: Entry[]): Entry[] {
        const out: Entry[] = [];
        for (const entry of entries) {
            if (entry === 'separator' && (out.length === 0 || out.at(-1) === 'separator')) continue;
            out.push(entry);
        }
        if (out.at(-1) === 'separator') out.pop();
        return out;
    }
</script>

<svelte:window onclick={close} onresize={close} />

<!-- The click that opens a menu must not reach the window handler that closes
     it again, and neither must a click on an item inside it. This wrapper is a
     bystander in that: everything inside it is a button, and the keys it
     listens for belong to the menus it holds. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="bar" role="menubar" tabindex="-1" bind:this={barEl} onclick={(e) => e.stopPropagation()} onkeydown={onKeyDown}>
    {#each menus as menu (menu.label)}
        <div class="host">
            <button
                class="trigger"
                data-menu={menu.label}
                aria-haspopup="menu"
                aria-expanded={openLabel === menu.label}
                onclick={() => toggle(menu.label)}
                onmouseenter={() => {
                    // Once one menu is open, pointing at another opens that
                    // one instead, as a menu bar does.
                    if (openLabel !== null && openLabel !== menu.label) openLabel = menu.label;
                }}
            >
                {menu.label}
            </button>

            {#if openLabel === menu.label}
                <div class="menu" role="menu" aria-label={menu.label} use:keepInside>
                    {#each tidy(menu.entries) as entry, i (i)}
                        {#if entry === 'separator'}
                            <hr />
                        {:else}
                            <button
                                role={entry.checked === undefined ? 'menuitem' : 'menuitemcheckbox'}
                                aria-checked={entry.checked}
                                disabled={entry.disabled}
                                title={entry.title}
                                onclick={() => run(menu.label, entry)}
                            >
                                <span class="tick">{#if entry.checked}<Icon name="check" size={13} />{/if}</span>
                                <span class="label">{entry.label}</span>
                                {#if entry.shortcut}<span class="key">{entry.shortcut}</span>{/if}
                            </button>
                        {/if}
                    {/each}
                </div>
            {/if}
        </div>
    {/each}
</div>

<style>
    .bar {
        display: flex;
        align-items: center;
        gap: 1px;
        min-width: 0;
        /* The title bar drags the window; its menus must not. */
        --wails-draggable: no-drag;
    }

    .bar:focus {
        outline: none;
    }

    .host {
        position: relative;
        display: flex;
        align-items: center;
        z-index: 5;
    }

    .trigger {
        display: flex;
        align-items: center;
        height: 24px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text-dim);
        white-space: nowrap;
    }

    .trigger:hover,
    .trigger[aria-expanded='true'] {
        background: var(--bg-hover);
        color: var(--text);
    }

    .menu {
        position: absolute;
        top: calc(100% + 4px);
        /* Growing rightwards from its button: the bar is at the left of the
           window, so there is room that way and none the other -- until the
           window is narrow, when keepInside moves it back. */
        left: 0;
        min-width: 240px;
        max-width: min(360px, 90vw);
        max-height: calc(100vh - 80px);
        overflow-y: auto;
        padding: 4px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
    }

    .menu button {
        display: flex;
        align-items: center;
        gap: 8px;
        width: 100%;
        height: 26px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text);
        text-align: left;
    }

    .menu button:hover:not(:disabled),
    .menu button:focus-visible {
        background: var(--bg-hover);
    }

    .menu button:disabled {
        opacity: 0.4;
        cursor: default;
    }

    /* A fixed column for the tick, so the labels line up whether or not one is
       there and nothing shifts sideways as a panel is toggled. */
    .tick {
        display: grid;
        place-items: center;
        width: 14px;
        flex: 0 0 auto;
        color: var(--accent);
    }

    .label {
        flex: 1 1 auto;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .key {
        flex: 0 0 auto;
        font-size: 11px;
        color: var(--text-faint);
    }

    hr {
        height: 1px;
        margin: 4px 6px;
        border: 0;
        background: var(--border-soft);
    }
</style>
