<!--
  A strip of tabs: the one on top of the view, and the one at the foot of the
  window that the dock hangs off. Both are the same object -- coloured by the
  cluster they belong to, dragged into order, scrolled when they no longer fit,
  and right-clicked for what to close -- so both are this component, given a
  different list.

  What it deliberately does not know is what a tab *is*. The caller hands it
  colours and titles already resolved and gets back "the user asked for this
  one", which is what lets one pane's tabs be objects while another's are
  resource kinds.

  It does know which pane it belongs to, and only for the drag: a tab dragged
  out of one strip and into another is a move rather than a reorder, and the
  strip under the pointer has to be able to tell which it is looking at.
-->
<script lang="ts" module>
    /** One tab, as the strip needs it. */
    export interface StripTab {
        id: string;
        title: string;
        /** Smaller type after the title, usually the cluster. Omitted when it would only repeat. */
        subtitle?: string;
        icon: string;
        /** The colour the tab is painted with: its context's. */
        color: string;
        /** The tooltip. */
        hint?: string;
        /** Draws an unsaved-changes marker in place of the close button. */
        modified?: boolean;
        /**
         * Whether the tab has a close button. Default true.
         *
         * A tab without one is a tab the user cannot strand themselves by
         * closing -- the cluster tree, which is how everything else gets
         * opened. It can still be moved to another pane, and its pane can
         * still be hidden.
         */
        closable?: boolean;
    }
</script>

<script lang="ts">
    import type { Snippet } from 'svelte';
    import { alpha, textOn } from '../colors';
    import type { PaneId } from '../state/panes';
    import { beginTabDrag, currentTabDrag, endTabDrag } from '../state/tabdrag.svelte';
    import Icon from './Icon.svelte';

    interface Props {
        tabs: StripTab[];
        activeId: string | null;
        /** Names the strip for a screen reader: "Open views", "Dock". */
        label: string;
        /** Which pane this strip belongs to, so a drag knows where it started. */
        pane: PaneId;
        onactivate: (id: string) => void;
        onclose: (id: string) => void;
        /** Both indices are positions in `tabs`. */
        onmove: (from: number, to: number) => void;
        /**
         * A tab dragged in from another pane, dropped at this position. Left
         * out by a strip that will not accept them.
         */
        onadopt?: (id: string, from: PaneId, index: number) => void;
        /** The right-click menu's items. `dismiss` closes the menu. */
        menu?: Snippet<[StripTab, () => void]>;
        /** Controls pinned to the end of the strip, past the tabs. */
        trailing?: Snippet;
        /** Shown in place of the tabs while there are none. */
        empty?: string;
        /**
         * Which edge carries the strip's dividing line: the one facing what it
         * belongs to. The strip above a view rules below itself; the dock's,
         * at the foot of the window, rules above.
         */
        rule?: 'above' | 'below';
    }

    let {
        tabs,
        activeId,
        label,
        pane,
        onactivate,
        onclose,
        onmove,
        onadopt,
        menu,
        trailing,
        empty,
        rule = 'below',
    }: Props = $props();

    /**
     * Index of the tab being dragged, or null when the tab in the air is not
     * one of these.
     *
     * Derived from the drag rather than noted at dragstart, because a tab
     * dropped into another pane leaves this list without this strip hearing
     * a dragend for it -- the source is gone from the document by then, and
     * a removed source fires none. A remembered index would outlive the tab
     * and reorder whatever came to sit at that position on the next drag.
     */
    let dragIndex = $derived.by(() => {
        const drag = currentTabDrag();
        if (!drag || drag.from !== pane) return null;
        const at = tabs.findIndex((t) => t.id === drag.id);
        return at === -1 ? null : at;
    });
    /**
     * Where a tab from another pane would land, or null when nothing is hovering
     * over this strip from outside it.
     *
     * A foreign tab cannot be reordered into place the way a local one is --
     * it is not in this list yet -- so it gets an insertion marker instead, and
     * the move happens on drop.
     */
    let adoptAt = $state<number | null>(null);

    function startDrag(event: DragEvent, index: number): void {
        beginTabDrag({ id: tabs[index].id, from: pane });
        event.dataTransfer?.setData('text/plain', tabs[index].id);
        if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
    }

    function stopDrag(): void {
        adoptAt = null;
        stopEdge();
        endTabDrag();
    }

    // The marker goes with the drag, wherever it ended. A tab hovered over
    // this strip and then dropped on another leaves here without a drop, and
    // the dragleave for it is not guaranteed.
    $effect(() => {
        if (currentTabDrag() === null) {
            adoptAt = null;
            stopEdge();
        }
    });

    /** Whether the tab in the air came from somewhere this strip can take it from. */
    function foreign(): boolean {
        const drag = currentTabDrag();
        return onadopt !== undefined && drag !== null && drag.from !== pane;
    }

    /**
     * Where in the strip a horizontal position falls: 0 before the first tab,
     * tabs.length past the last, and past a tab's midpoint counts as after it.
     *
     * Reading the position rather than asking which tab the pointer happens to
     * be over is what makes every position reachable. The strip is not a row of
     * tabs and nothing else: the scroll arrows are drawn over its ends, there is
     * padding at each end and a gap between every pair, and the controls sit
     * past them all. A drag over any of that used to be a drag over nothing,
     * which is why a tab could not be taken to the end of a strip that
     * scrolled -- the end of one is mostly not tab.
     */
    function insertionAt(clientX: number): number | null {
        if (!stripEl) return null;
        const boxes = stripEl.querySelectorAll<HTMLElement>('.tab');
        if (boxes.length === 0) return null;

        for (let i = 0; i < boxes.length; i++) {
            const box = boxes[i].getBoundingClientRect();
            if (clientX < box.left + box.width / 2) return i;
        }
        return boxes.length;
    }

    /** The tab a position is inside, if it is inside one at all. */
    function tabAt(clientX: number): number | null {
        if (!stripEl) return null;
        const boxes = stripEl.querySelectorAll<HTMLElement>('.tab');
        for (let i = 0; i < boxes.length; i++) {
            const box = boxes[i].getBoundingClientRect();
            if (clientX >= box.left && clientX < box.right) return i;
        }
        return null;
    }

    /** Takes in a tab dragged from another pane. A local drag has already landed. */
    function drop(event: DragEvent): void {
        const drag = currentTabDrag();
        if (!drag) return;
        if (!foreign()) {
            // A local reorder happened on the way over, so there is nothing to
            // do but end it. The drop is still taken rather than left to the
            // browser, which would otherwise read the payload as dropped text.
            event.preventDefault();
            stopDrag();
            return;
        }
        event.preventDefault();
        event.stopPropagation();
        onadopt?.(drag.id, drag.from, adoptAt ?? tabs.length);
        stopDrag();
    }

    /**
     * Anywhere in the strip that is not a tab takes a foreign tab at the end:
     * the room past the last tab, the hint an empty strip shows, the controls
     * pinned after them. The strip is a thin target at the best of times, and
     * the parts of it that would refuse a drop are not distinguishable from
     * the parts that would take one.
     */
    function dragOverStrip(event: DragEvent): void {
        if (currentTabDrag() === null) return;
        // Held near an end, this is also how the tabs beyond it are asked for.
        edgeScroll(event);

        event.preventDefault();
        if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';

        const at = insertionAt(event.clientX);
        if (foreign()) {
            // A tab from another pane is not in this list yet, so it gets a
            // marker where it would land rather than being reordered into place.
            adoptAt = at ?? tabs.length;
            return;
        }
        if (dragIndex === null) return;

        // Reaching a neighbour is enough to displace it; the midpoint only
        // decides where a position between tabs belongs. Asking for the
        // midpoint everywhere would mean dragging a tab as far as half the
        // neighbour before anything happened -- and on a strip that scrolls
        // that point is often off screen, so the swap could not be asked for
        // at all. Reordering as the pointer moves, rather than on drop, is what
        // shows the result directly instead of a marker.
        const over = tabAt(event.clientX);
        // An insertion point runs one past the end; a position in the list
        // does not.
        const target = over ?? (at === null ? null : Math.min(at, tabs.length - 1));
        if (target !== null && target !== dragIndex) onmove(dragIndex, target);
    }

    let rootEl = $state<HTMLElement | null>(null);

    /**
     * Clears the marker on the way out of the strip -- and only then. A
     * dragleave also fires on the way from the strip into one of its own
     * tabs, and clearing it there makes the marker flicker with every tab
     * the pointer crosses.
     */
    function dragLeaveStrip(event: DragEvent): void {
        const into = event.relatedTarget;
        if (into instanceof Node && rootEl?.contains(into)) return;
        adoptAt = null;
        stopEdge();
    }

    /** Alt+Arrow moves the focused tab, so reordering is not drag-only. */
    function onKeyDown(event: KeyboardEvent, index: number): void {
        if (event.altKey && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
            event.preventDefault();
            onmove(index, index + (event.key === 'ArrowLeft' ? -1 : 1));
            return;
        }
        if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            onactivate(tabs[index].id);
            return;
        }
        if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
            event.preventDefault();
            openMenuFromKeyboard(event, tabs[index]);
        }
    }

    function onPointerDown(event: MouseEvent, tab: StripTab): void {
        if (event.button === 1 && tab.closable !== false) {
            event.preventDefault();
            onclose(tab.id);
        }
    }

    // ----- scrolling ---------------------------------------------------
    //
    // Tabs keep their width and the strip scrolls, rather than compressing to
    // fit: a title carries the cluster and what is open, which is the pair you
    // are reading the strip to tell apart, and neither survives being squeezed
    // to a few characters.

    let stripEl = $state<HTMLElement | null>(null);
    let canLeft = $state(false);
    let canRight = $state(false);

    function measure(): void {
        if (!stripEl) return;
        const { scrollLeft, scrollWidth, clientWidth } = stripEl;
        // A pixel of slack: fractional widths mean scrollLeft rarely reaches
        // the exact end, which would leave the arrow lit with nowhere to go.
        canLeft = scrollLeft > 1;
        canRight = scrollLeft + clientWidth < scrollWidth - 1;
    }

    function nudge(direction: -1 | 1): void {
        if (!stripEl) return;
        stripEl.scrollBy({ left: direction * Math.max(140, stripEl.clientWidth * 0.6), behavior: 'smooth' });
    }

    /**
     * A vertical wheel scrolls the strip sideways, which is what a one-line
     * horizontal scroller should do and what browsers do not do for it.
     * Genuine horizontal input is left alone to scroll natively.
     */
    function onWheel(event: WheelEvent): void {
        if (!stripEl || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return;
        event.preventDefault();
        stripEl.scrollLeft += event.deltaY;
    }

    // Attached by hand rather than as an attribute so the listener is
    // explicitly non-passive; a passive one cannot preventDefault, and the
    // page behind would scroll instead of the strip.
    $effect(() => {
        const el = stripEl;
        if (!el) return;
        el.addEventListener('wheel', onWheel, { passive: false });
        return () => el.removeEventListener('wheel', onWheel);
    });

    // Re-measure when the window or the sidebar changes the strip's width.
    $effect(() => {
        const el = stripEl;
        if (!el) return;
        const observer = new ResizeObserver(measure);
        observer.observe(el);
        return () => observer.disconnect();
    });

    // ...and when tabs are added or removed, which changes the content width
    // without changing the container's.
    $effect(() => {
        tabs.length;
        measure();
    });

    // ----- dragging past the edge ---------------------------------------
    //
    // Nothing scrolls an overflowing element while a drag is in flight: the
    // browser scrolls the *page* near the window's edge and leaves a scroller
    // inside it alone. So with more tabs than fit, everything outside the
    // visible window was unreachable -- a tab could not be dragged to the end
    // past a last tab it could not reach, and the tabs scrolled off could not
    // be passed at all. Holding the pointer at either edge scrolls the strip
    // under it, which is what makes the whole order reachable again.

    /** How close to an edge starts the scroll, and how fast it goes there. */
    const EDGE_ZONE = 48;
    const EDGE_SLOWEST = 4;
    const EDGE_FASTEST = 16;

    let edgeStep = 0;
    let edgeFrame: number | null = null;

    /** Scrolls one frame's worth, and stops at the end rather than spinning. */
    function stepEdge(): void {
        edgeFrame = null;
        if (!stripEl || edgeStep === 0) return;
        // `instant` rather than a write to scrollLeft: the strip sets
        // scroll-behavior: smooth for the arrows and for scrolling a tab into
        // view, and a smooth animation restarted every frame crawls.
        stripEl.scrollBy({ left: edgeStep, behavior: 'instant' });
        measure();
        if ((edgeStep < 0 && !canLeft) || (edgeStep > 0 && !canRight)) {
            edgeStep = 0;
            return;
        }
        edgeFrame = requestAnimationFrame(stepEdge);
    }

    /**
     * Starts, steers or stops the edge scroll from where the pointer is.
     *
     * Speed rises with how far into the edge the pointer has gone, so resting
     * just inside it creeps along tab by tab and pushing right up against it
     * covers a long strip quickly -- the same gesture doing both jobs.
     */
    function edgeScroll(event: DragEvent): void {
        if (!stripEl) return;
        const box = stripEl.getBoundingClientRect();

        let depth = 0;
        let direction = 0;
        if (event.clientX < box.left + EDGE_ZONE) {
            direction = -1;
            depth = (box.left + EDGE_ZONE - event.clientX) / EDGE_ZONE;
        } else if (event.clientX > box.right - EDGE_ZONE) {
            direction = 1;
            depth = (event.clientX - (box.right - EDGE_ZONE)) / EDGE_ZONE;
        }

        if (direction === 0) {
            stopEdge();
            return;
        }
        const speed = EDGE_SLOWEST + (EDGE_FASTEST - EDGE_SLOWEST) * Math.min(1, Math.max(0, depth));
        edgeStep = direction * speed;
        if (edgeFrame === null) edgeFrame = requestAnimationFrame(stepEdge);
    }

    function stopEdge(): void {
        edgeStep = 0;
        if (edgeFrame !== null) {
            cancelAnimationFrame(edgeFrame);
            edgeFrame = null;
        }
    }

    // A strip taken off screen mid-drag -- a pane closed, a layout changed --
    // would otherwise leave a frame queued against an element nobody holds.
    $effect(() => stopEdge);

    /**
     * Keep the active tab on screen. Reordering, restoring a session or closing
     * a neighbour can all leave it scrolled out of sight, and a tab you cannot
     * see is a tab you cannot tell is selected.
     */
    $effect(() => {
        const active = activeId;
        if (!active || !stripEl) return;

        const tab = stripEl.querySelector<HTMLElement>(`[data-tab-id="${CSS.escape(active)}"]`);
        // `block: nearest` keeps this from scrolling the page vertically as a
        // side effect of nudging the strip sideways.
        tab?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' });
    });

    /** The tab the context menu was opened on, and where to draw it. */
    interface OpenMenu {
        tab: StripTab;
        /** The tab's own box, which the menu hangs off. */
        anchor: { left: number; top: number; bottom: number };
        x: number;
        y: number;
    }

    /** The breathing room kept between the menu and the window's edges. */
    const MENU_GAP = 8;

    let openMenuAt = $state<OpenMenu | null>(null);
    let menuEl = $state<HTMLElement | null>(null);

    /**
     * Brings a tab forward so the menu is never opened on one you cannot see:
     * acting on hidden contents is how the wrong document gets closed.
     *
     * Only when it is not already the one showing. Asking for a menu is not a
     * click on the tab, and for the dock a click on the active tab is a toggle
     * that folds the whole thing away -- which is not what a right-click meant.
     */
    function revealForMenu(tab: StripTab): void {
        if (activeId !== tab.id) onactivate(tab.id);
    }

    /**
     * The scale the app is drawn at, measured off an element inside it.
     *
     * Zoom is CSS `zoom` on the app's own element, and a position:fixed box
     * inside a zoomed one resolves against the window in that zoomed space. A
     * menu placed at a viewport coordinate therefore lands at coordinate ×
     * zoom: exact at the top-left corner and further out the further down the
     * window you go. Everything below works in the app's own coordinates and
     * divides what it measures of the window by this.
     */
    function appScale(el: HTMLElement): number {
        const width = el.offsetWidth;
        return width > 0 ? el.getBoundingClientRect().width / width : 1;
    }

    /**
     * Hangs the menu off the tab it was asked for.
     *
     * Off the tab rather than at the pointer so that both strips read the same.
     * The dock sits at the foot of the window, where a menu dropping from the
     * pointer belongs to nothing and covers the document it is about, while the
     * identical placement in the strip along the top reads as part of the tab.
     * Which side of the tab it ends up on is settled once it has a size.
     */
    function anchorMenu(el: HTMLElement, tab: StripTab): void {
        const scale = appScale(el);
        const box = el.getBoundingClientRect();
        const anchor = { left: box.left / scale, top: box.top / scale, bottom: box.bottom / scale };
        openMenuAt = { tab, anchor, x: anchor.left, y: anchor.bottom };
    }

    /** Opens the menu on a tab, bringing it forward first if it is not. */
    function openMenu(event: MouseEvent, tab: StripTab): void {
        if (!menu) return;
        event.preventDefault();
        // The window handler below dismisses on any right-click; without this
        // the same event would close the menu we are opening.
        event.stopPropagation();
        revealForMenu(tab);
        anchorMenu(event.currentTarget as HTMLElement, tab);
    }

    /** Shift+F10 and the Menu key open it from the keyboard, at the tab itself. */
    function openMenuFromKeyboard(event: KeyboardEvent, tab: StripTab): void {
        if (!menu) return;
        revealForMenu(tab);
        anchorMenu(event.currentTarget as HTMLElement, tab);
    }

    function closeMenu(): void {
        openMenuAt = null;
    }

    /**
     * Settles which side of the tab the menu rests on, once it has a size.
     *
     * The rule already says which end of the window the strip is at, and the
     * menu opens away from that end: down from a strip along the top, up from
     * the dock at the foot. It is the same relationship both times -- a menu
     * resting on its tab, growing into the room there is -- which is what makes
     * the dock's read as part of its tab rather than as something dropped over
     * the document it is about.
     *
     * Whether it fits only decides the fallback. Sideways it is merely nudged:
     * a tab near the right edge would otherwise take its menu off-screen, where
     * it can be neither read nor dismissed.
     */
    $effect(() => {
        const at = openMenuAt;
        if (!at || !menuEl) return;

        const scale = appScale(menuEl);
        const box = menuEl.getBoundingClientRect();
        const height = box.height / scale;
        const width = box.width / scale;
        const bottomEdge = window.innerHeight / scale - MENU_GAP;
        const rightEdge = window.innerWidth / scale - MENU_GAP;

        const fits = (top: number): boolean => top >= MENU_GAP && top + height <= bottomEdge;
        const away = rule === 'above' ? at.anchor.top - height : at.anchor.bottom;
        const back = rule === 'above' ? at.anchor.bottom : at.anchor.top - height;

        const y = fits(away) ? away : fits(back) ? back : away;
        const x = Math.max(MENU_GAP, Math.min(at.anchor.left, rightEdge - width));

        // Guarded so that settling on a position does not re-trigger this.
        if (at.x !== x) at.x = x;
        if (at.y !== y) at.y = y;
    });

    // Focus the first item so the menu is usable without the mouse that opened it.
    $effect(() => {
        if (openMenuAt && menuEl) menuEl.querySelector('button')?.focus();
    });

    function onMenuKeyDown(event: KeyboardEvent): void {
        if (event.key === 'Escape') {
            event.stopPropagation();
            closeMenu();
            return;
        }
        if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;

        event.preventDefault();
        const items = [...(menuEl?.querySelectorAll('button') ?? [])];
        const at = items.indexOf(document.activeElement as HTMLButtonElement);
        const next = (at + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
        items[next]?.focus();
    }
</script>

<svelte:window
    onclick={closeMenu}
    oncontextmenu={closeMenu}
    onresize={closeMenu}
    onkeydown={(e) => e.key === 'Escape' && closeMenu()}
/>

<!-- The drag handlers are what make a strip a drop target for a tab from
     another pane. On the strip as a whole rather than on the tab list inside
     it, so the hint and the controls take a drop as readily as the tabs. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
    class="strip {rule}"
    class:in-drag={currentTabDrag() !== null}
    bind:this={rootEl}
    ondragover={dragOverStrip}
    ondrop={drop}
    ondragleave={dragLeaveStrip}
>
    <div class="scroller">
        <!-- Before the tab list rather than inside it: a tablist holding a
             paragraph is one a screen reader announces as a tab nobody can
             reach, and an empty list would take the width and squeeze it out. -->
        {#if tabs.length === 0 && empty}
            <p class="empty">{empty}</p>
        {/if}

        <!-- A tablist is not focusable itself -- its tabs are -- so the rule
             that wants a tabindex here does not apply. -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <!-- svelte-ignore a11y_interactive_supports_focus -->
        <div class="tabbar" role="tablist" aria-label={label} bind:this={stripEl} onscroll={measure}>
            {#each tabs as tab, index (tab.id)}
                {@const active = activeId === tab.id}
                <div
                    class="tab"
                    data-tab-id={tab.id}
                    class:active
                    class:dragging={dragIndex === index}
                    class:adopt-before={adoptAt === index}
                    class:adopt-after={adoptAt === tabs.length && index === tabs.length - 1}
                    role="tab"
                    tabindex={active ? 0 : -1}
                    aria-selected={active}
                    title={tab.hint ?? tab.title}
                    draggable="true"
                    style:--tab-bg={active ? tab.color : alpha(tab.color, 0.18)}
                    style:--tab-fg={active ? textOn(tab.color) : 'var(--text-dim)'}
                    style:--tab-rule={tab.color}
                    onclick={() => onactivate(tab.id)}
                    onmousedown={(e) => onPointerDown(e, tab)}
                    oncontextmenu={(e) => openMenu(e, tab)}
                    onkeydown={(e) => onKeyDown(e, index)}
                    ondragstart={(e) => startDrag(e, index)}
                    ondragend={stopDrag}
                >
                    <Icon name={tab.icon} size={14} />
                    <span class="title">{tab.title}</span>
                    {#if tab.subtitle}
                        <span class="context">{tab.subtitle}</span>
                    {/if}
                    {#if tab.closable !== false}
                    <button
                        class="close"
                        class:modified={tab.modified}
                        aria-label={tab.modified
                            ? `Close ${tab.title} — unsaved changes`
                            : `Close ${tab.title}`}
                        onclick={(e) => {
                            e.stopPropagation();
                            onclose(tab.id);
                        }}
                    >
                        <!-- The unsaved marker is the close button wearing a
                             dot: one control in one place, which turns back
                             into a cross under the pointer rather than moving
                             out from under it. Both are drawn and one hidden,
                             because swapping the glyph on :hover cannot be
                             done from script. -->
                        <span class="mark"><Icon name="dot" size={13} /></span>
                        <span class="cross"><Icon name="close" size={12} /></span>
                    </button>
                    {/if}
                </div>
            {/each}
        </div>

        <!-- Overlaid rather than laid out beside the tabs, so appearing and
             disappearing does not shift them sideways under the pointer. -->
        <button class="nudge left" class:shown={canLeft} aria-label="Scroll tabs left" onclick={() => nudge(-1)}>
            <Icon name="chevron-left" size={14} />
        </button>
        <button class="nudge right" class:shown={canRight} aria-label="Scroll tabs right" onclick={() => nudge(1)}>
            <Icon name="chevron-right" size={14} />
        </button>
    </div>

    {#if trailing}
        <div class="trailing">{@render trailing()}</div>
    {/if}
</div>

{#if openMenuAt && menu}
    <!-- Stops the window handler from closing the menu on the click that
         reaches an item, and keeps a right-click inside it from opening the
         browser's own menu. -->
    <div
        class="menu"
        role="menu"
        tabindex="-1"
        bind:this={menuEl}
        style:left="{openMenuAt.x}px"
        style:top="{openMenuAt.y}px"
        onclick={(e) => e.stopPropagation()}
        oncontextmenu={(e) => e.preventDefault()}
        onkeydown={onMenuKeyDown}
    >
        {@render menu(openMenuAt.tab, closeMenu)}
    </div>
{/if}

<style>
    .menu {
        position: fixed;
        z-index: 50;
        min-width: 170px;
        padding: 4px;
        display: flex;
        flex-direction: column;
        background: var(--bg-sidebar);
        border: 1px solid var(--border);
        border-radius: var(--radius);
        box-shadow: 0 10px 28px rgb(0 0 0 / 0.45);
    }

    /* The items come from the caller as a snippet, so they are styled from
       here rather than scoped to their own component. */
    .menu :global(button) {
        font: inherit;
        font-size: 12px;
        text-align: left;
        padding: 6px 10px;
        border-radius: var(--radius-sm);
        color: var(--text);
        white-space: nowrap;
    }

    .menu :global(button:hover),
    .menu :global(button:focus-visible) {
        background: var(--bg-hover);
        outline: none;
    }

    .menu :global(hr) {
        border: 0;
        border-top: 1px solid var(--border);
        margin: 4px 6px;
    }

    /* The strip owns the width and the edge affordances; .tabbar inside it is
       purely the scrolling viewport. */
    .strip {
        display: flex;
        flex: 0 0 auto;
        min-width: 0;
        background: var(--bg-sidebar);
    }

    .strip.below {
        border-bottom: 1px solid var(--border);
    }

    .strip.above {
        border-top: 1px solid var(--border);
    }

    /* The scrolling region and its two edge buttons, which are positioned
       against it rather than against the strip -- otherwise the right-hand one
       would sit over whatever the caller pinned to the end. */
    .scroller {
        position: relative;
        display: flex;
        flex: 1 1 auto;
        min-width: 0;
    }

    .tabbar {
        display: flex;
        align-items: flex-end;
        gap: 2px;
        height: 38px;
        padding: 0 6px;
        flex: 1 1 auto;
        min-width: 0;
        overflow-x: auto;
        overflow-y: hidden;
        scrollbar-width: none;
        scroll-behavior: smooth;
        /* So scrolling a tab into view stops at the strip's padding rather than
           at the tab's own edge, which would leave it flush against the arrow
           and the scroll offset a few pixels short of the true end. */
        scroll-padding: 0 6px;
    }

    .trailing {
        display: flex;
        align-items: center;
        gap: 2px;
        padding: 0 6px;
        flex: 0 0 auto;
        z-index: 3;
    }

    /* A strip with nothing in it says so rather than looking broken. */
    .empty {
        margin: 0;
        align-self: center;
        padding: 0 6px;
        flex: 0 1 auto;
        font-size: 11.5px;
        color: var(--text-faint);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .nudge {
        position: absolute;
        top: 0;
        bottom: 1px;
        width: 30px;
        display: grid;
        place-items: center;
        color: var(--text-dim);
        /* visibility, not opacity alone: a transparent button is still in the
           accessibility tree and still a click target. */
        visibility: hidden;
        opacity: 0;
        transition:
            opacity 120ms ease,
            visibility 120ms ease;
        z-index: 2;
    }

    .nudge.shown {
        visibility: visible;
        opacity: 1;
    }

    /* An arrow is drawn over the end of the strip, which while a tab is in the
       air would be thirty pixels of tab that cannot be dropped on or reordered
       past. Nothing needs clicking mid-drag, so the arrows stand aside and the
       tab underneath takes the drag -- the edge still scrolls, because that is
       decided by where the pointer is rather than by what it is over. */
    .strip.in-drag .nudge {
        pointer-events: none;
    }

    .nudge:hover {
        color: var(--text);
    }

    /* The fade is on the button itself, so there is one element per edge and
       nothing to keep in sync when it appears. */
    .nudge.left {
        left: 0;
        justify-items: start;
        padding-left: 4px;
        background: linear-gradient(to right, var(--bg-sidebar) 55%, transparent);
    }

    .nudge.right {
        right: 0;
        justify-items: end;
        padding-right: 4px;
        background: linear-gradient(to left, var(--bg-sidebar) 55%, transparent);
    }

    .tabbar::-webkit-scrollbar {
        height: 0;
    }

    .tab {
        display: flex;
        align-items: center;
        gap: 7px;
        height: 30px;
        max-width: 240px;
        padding: 0 6px 0 10px;
        flex: 0 0 auto;
        border-radius: var(--radius) var(--radius) 0 0;
        background: var(--tab-bg);
        color: var(--tab-fg);
        cursor: default;
        position: relative;
        transition:
            background 110ms ease,
            color 110ms ease;
    }

    /* A full-strength rule along the top edge keeps an inactive tab's context
       identifiable even though its body is only tinted. */
    .tab::before {
        content: '';
        position: absolute;
        inset: 0 0 auto 0;
        height: 2px;
        border-radius: 2px 2px 0 0;
        background: var(--tab-rule);
    }

    .tab:not(.active):hover {
        background: color-mix(in srgb, var(--tab-rule) 32%, transparent);
        color: var(--text);
    }

    /* Where a tab dragged in from another pane would land. A rule rather than
       a gap: the strip scrolls, and opening a hole in it shifts every tab
       sideways under the pointer that is trying to aim at one. */
    .tab.adopt-before::before,
    .tab.adopt-after::after {
        content: '';
        position: absolute;
        top: 3px;
        bottom: 3px;
        width: 2px;
        background: var(--accent);
        border-radius: 1px;
    }

    .tab.adopt-before::before {
        left: -1px;
    }

    .tab.adopt-after::after {
        right: -1px;
    }

    .tab.dragging {
        opacity: 0.55;
    }

    .title {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .context {
        font-size: 11px;
        opacity: 0.72;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        max-width: 90px;
    }

    .close {
        display: grid;
        place-items: center;
        width: 18px;
        height: 18px;
        border-radius: 3px;
        color: inherit;
        opacity: 0.6;
        flex: 0 0 auto;
    }

    .close:hover {
        opacity: 1;
        background: rgba(0, 0, 0, 0.25);
    }

    /* The two glyphs share one cell, so swapping them moves nothing. */
    .close .mark,
    .close .cross {
        grid-area: 1 / 1;
        display: grid;
        place-items: center;
    }

    .close .mark {
        display: none;
    }

    /* An unsaved document's marker is full strength -- it is information
       rather than an affordance -- and gives way to the cross the moment the
       button is aimed at. */
    .close.modified {
        opacity: 1;
    }

    .close.modified .mark {
        display: grid;
    }

    .close.modified .cross {
        display: none;
    }

    .close.modified:hover .mark {
        display: none;
    }

    .close.modified:hover .cross {
        display: grid;
    }

    /* Fingers rather than a pointer: an 18px cross is a target a thumb misses. */
    @media (pointer: coarse) {
        .close {
            width: 26px;
            height: 26px;
        }
    }
</style>
