// The object a plugin's own page has been asked to open on.
//
// A custom view that declares a `focus` can be opened on one object -- an
// Argo CD Application shown selected on the board -- and learns which from its
// own address, after the #. The request is made before the tab exists, or
// while it is already showing, so it waits here for the frame to take it.
//
// Taken, not read: the page keeps its own place in its address as the reader
// moves about, and a request still lying here would put it back on the searched
// object every time the tab was brought forward again.

export interface FocusRequest {
    /** What goes after the #, placeholders already filled. */
    hash: string;
    /** Distinct for every request, so asking for the same object twice reloads it twice. */
    nonce: number;
}

class PluginFocus {
    private pending = $state<Record<string, FocusRequest>>({});
    private count = 0;

    /** Asks the page in one tab to open on an object. */
    request(tabId: string, hash: string): void {
        this.pending[tabId] = { hash, nonce: ++this.count };
    }

    /** The request waiting for a tab, if any. Reading it in an effect tracks it. */
    peek(tabId: string): FocusRequest | null {
        return this.pending[tabId] ?? null;
    }

    /** Hands over the request waiting for a tab, and forgets it. */
    take(tabId: string): FocusRequest | null {
        const request = this.pending[tabId] ?? null;
        if (request) delete this.pending[tabId];
        return request;
    }
}

export const pluginFocus = new PluginFocus();
