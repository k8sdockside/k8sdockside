<!--
  The bar at the foot of a small screen, which picks the one pane it shows.

  On a wide window the four panes are all on screen and this does not exist.
  On a phone they are shown one at a time -- see state/viewport.svelte.ts -- and
  this is how you get from the cluster tree to the view it opened and back, the
  way a tab bar at the foot of a mobile app does.
-->
<script lang="ts">
    import { viewport } from '../state/viewport.svelte';
    import { workspace, type PaneId } from '../state/workspace.svelte';
    import Icon from './Icon.svelte';

    /** Short names: four of these share 375px, and each has an icon beside it. */
    const ITEMS: { pane: PaneId; label: string; icon: string }[] = [
        { pane: 'left', label: 'Left', icon: 'dock-left' },
        { pane: 'main', label: 'Main', icon: 'dashboard' },
        { pane: 'right', label: 'Right', icon: 'dock-right' },
        { pane: 'bottom', label: 'Dock', icon: 'dock-bottom' },
    ];

    /** What a pane is showing, so a button says more than where it is. */
    function nameOf(pane: PaneId): string {
        return workspace.activeTabIn(pane)?.title ?? '';
    }

    function choose(pane: PaneId): void {
        // The dock folds to its strip on a wide screen; asked for here, it is
        // being asked to show what it holds.
        if (pane === 'bottom') workspace.setPaneOpen('bottom', true);
        viewport.show(pane);
    }
</script>

<nav class="panenav" aria-label="Panels">
    {#each ITEMS as item (item.pane)}
        {@const count = workspace.panes[item.pane].tabs.length}
        <button
            class:current={viewport.focus === item.pane}
            aria-current={viewport.focus === item.pane ? 'page' : undefined}
            disabled={item.pane !== 'main' && count === 0}
            title={nameOf(item.pane) || item.label}
            onclick={() => choose(item.pane)}
        >
            <span class="icon">
                <Icon name={item.icon} size={18} />
                {#if count > 1}<span class="count">{count}</span>{/if}
            </span>
            <span class="label">{nameOf(item.pane) || item.label}</span>
        </button>
    {/each}
</nav>

<style>
    .panenav {
        display: grid;
        grid-template-columns: repeat(4, minmax(0, 1fr));
        flex: 0 0 auto;
        background: var(--bg-sidebar);
        border-top: 1px solid var(--border);
        /* Clear of the home indicator on phones that have one. */
        padding-bottom: env(safe-area-inset-bottom, 0px);
    }

    button {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 2px;
        min-width: 0;
        height: 52px;
        padding: 0 4px;
        color: var(--text-faint);
        font-size: 10.5px;
    }

    button.current {
        color: var(--accent);
        box-shadow: inset 0 2px 0 var(--accent);
    }

    button:disabled {
        opacity: 0.35;
        cursor: default;
    }

    .icon {
        position: relative;
        display: grid;
        place-items: center;
    }

    .count {
        position: absolute;
        top: -5px;
        right: -11px;
        min-width: 15px;
        height: 15px;
        padding: 0 3px;
        border-radius: 8px;
        background: var(--bg-raised);
        color: var(--text-dim);
        font-size: 9.5px;
        line-height: 15px;
        text-align: center;
    }

    .label {
        max-width: 100%;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
</style>
