// Refreshing on a timer, but only while somebody can see the result.
//
// The dashboard, its charts and budget, the plugin action bar and the plugin
// recheck all read their clusters again every so often. With the window
// minimised, behind another app's full-screen window or on another desktop,
// every one of those reads is work nobody will look at -- API calls, Prometheus
// queries and redraws -- and on fifteen clusters it adds up. So a timer made
// here skips its turns while the page is hidden, and when the page is shown
// again it runs once straight away if it missed one, so what comes back into
// view is current rather than as old as the moment it was hidden.
//
// The fleet poller is deliberately not built on this: its reading is what
// raises the alerts and notifications, which matter most when the window is
// not being looked at.

type Listener = () => void;

const shown = new Set<Listener>();

function isHidden(): boolean {
    return typeof document !== 'undefined' && document.visibilityState === 'hidden';
}

if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
        if (isHidden()) return;
        for (const listener of [...shown]) listener();
    });
}

/**
 * Calls fn every ms milliseconds while the page is visible. A turn that falls
 * while it is hidden is skipped, and the first time the page is shown again
 * after a skipped turn, fn runs at once. Returns the stop.
 *
 * fn is not called straight away: the caller does its first read itself, when
 * it wants it.
 */
export function everyWhileVisible(ms: number, fn: () => void): () => void {
    let missed = false;
    const timer = setInterval(() => {
        if (isHidden()) {
            missed = true;
            return;
        }
        fn();
    }, ms);
    const onShown = () => {
        if (!missed) return;
        missed = false;
        fn();
    };
    shown.add(onShown);
    return () => {
        clearInterval(timer);
        shown.delete(onShown);
    };
}
