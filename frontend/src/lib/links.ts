// Opening a page outside the app.
//
// A plain `<a target="_blank">` does nothing here. The window is a WKWebView
// (WebView2 on Windows), and asking one for a new window only works if the host
// implements the delegate that makes one; Wails does not, so the click lands on
// nothing at all -- no new window, no error, no sign that anything happened.
// Dropping the target would be worse: the webview would then navigate *itself*
// to the page, replacing the app with a website and no way back.
//
// So every link out goes through the runtime instead, which hands the address
// to the machine's own browser. The anchors keep their href and their target:
// the href is what the address is, and the inert target is what makes a click
// this module somehow misses do nothing rather than something bad.
//
// The web version is the exception. There the window *is* a tab in the user's
// browser, and the backend handing an address to "the machine's own browser"
// would open it on the server, where nobody is looking. A new tab is exactly
// what a browser is for, so there it is asked for one directly.

import { Browser } from '@wailsio/runtime';
import { notices } from './state/notices.svelte';
import { session } from './state/session.svelte';

/**
 * Whether this is an address we are willing to hand to the browser.
 *
 * https only, and http for a link a plugin might carry to something on the
 * user's own network. Everything else -- `file:`, `javascript:`, a custom
 * scheme registered by some other app -- is refused: most of these addresses
 * are written in the app's own pages, but a plugin's `docs` comes from a file
 * on disk, and "open whatever this says" is not a thing to offer a file.
 */
function allowed(url: string): boolean {
    try {
        const scheme = new URL(url).protocol;
        return scheme === 'https:' || scheme === 'http:';
    } catch {
        return false;
    }
}

/**
 * Opens a link in the browser the user already has.
 *
 * Reports rather than throws: a link that will not open is worth a line in the
 * status bar, and nothing that calls this has anything else to do about it.
 */
export async function openExternal(url: string): Promise<void> {
    if (!allowed(url)) {
        notices.fail(`Not a web address: ${url}`);
        return;
    }
    if (session.server) {
        // noopener and noreferrer, because the page is somebody else's and
        // must not be handed a way back into this one. With noopener the
        // browser returns no window, so there is no telling from here whether
        // a popup blocker ate it -- which is also true of an ordinary link.
        window.open(url, '_blank', 'noopener,noreferrer');
        return;
    }
    try {
        await Browser.OpenURL(url);
    } catch {
        // The address is in the message on purpose: with no browser to show it
        // in, the next best thing is something the user can copy.
        notices.fail(`Could not open ${url} in your browser`);
    }
}

/**
 * The click handler for a link out. Takes the address rather than reading it
 * off the event, so the caller is the one deciding what gets opened.
 */
export function onExternalClick(url: string): (event: MouseEvent) => void {
    return (event: MouseEvent) => {
        event.preventDefault();
        void openExternal(url);
    };
}
