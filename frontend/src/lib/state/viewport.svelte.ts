// How the window is laid out on a small screen.
//
// The shell is four panes side by side and one above the other, which is what a
// desktop window has room for and a phone does not: at 375px the cluster tree,
// the view and the details panel each get a sliver and none of them is usable.
// Below the breakpoint the panes are therefore shown one at a time, full size,
// with a bar at the foot to switch between them -- see PaneNav.svelte.
//
// Nothing about the layout itself changes. Which tabs are in which pane, their
// sizes and whether they are open are the user's, are saved to the settings
// file, and come back exactly as they were on a wide screen. All this adds is
// which one pane a narrow screen is looking at, and that is never saved.

import { PANE_IDS, type PaneId } from './panes';

/**
 * Where the window stops being a desktop layout. The same width the gateway's
 * own pages switch at (internal/gateway/web/static/gateway.css), so signing in
 * and the app agree about what counts as a small screen.
 */
export const COMPACT_QUERY = '(max-width: 760px)';

/** Each pane's active tab id, as the shell last saw them. */
export type ActiveTabs = Record<PaneId, string | null>;

/**
 * Which pane to show after the panes' active tabs have moved.
 *
 * The pane that just gained a tab -- or switched to another one -- is the one
 * the user asked to see: picking a cluster in the tree opens its dashboard in
 * the main pane, tapping a row puts its details wherever that tab lives, and
 * Logs or Shell opens in the dock. Following that is what makes one pane at a
 * time workable, since the alternative is a tap that seems to do nothing.
 *
 * When several moved at once the one furthest from the tree wins, because the
 * tree is where the tap came from rather than where it was going. And when the
 * pane being shown has nothing left to show, the main pane takes over.
 */
export function followFocus(
    current: PaneId,
    before: ActiveTabs,
    after: ActiveTabs,
    hasTabs: (pane: PaneId) => boolean,
): PaneId {
    const moved = PANE_IDS.filter((pane) => after[pane] !== null && after[pane] !== before[pane]);
    const order: PaneId[] = ['right', 'bottom', 'main', 'left'];
    const target = order.find((pane) => moved.includes(pane));
    if (target) return target;
    if (current !== 'main' && !hasTabs(current)) return 'main';
    return current;
}

class Viewport {
    /** Whether the screen is narrow enough to show one pane at a time. */
    compact = $state(false);
    /** The pane a compact screen is showing. Meaningless on a wide one. */
    focus = $state<PaneId>('main');

    private query: MediaQueryList | null = null;

    /** Starts listening to the window's width. Returns the way to stop. */
    start(): () => void {
        if (typeof window === 'undefined' || !window.matchMedia) return () => {};
        this.query = window.matchMedia(COMPACT_QUERY);
        const update = () => {
            this.compact = this.query?.matches ?? false;
        };
        update();
        this.query.addEventListener('change', update);
        return () => this.query?.removeEventListener('change', update);
    }

    show(pane: PaneId): void {
        this.focus = pane;
    }
}

export const viewport = new Viewport();
