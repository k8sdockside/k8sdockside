// The shells open in the dock.
//
// One per dock tab, keyed by the tab's id and outliving the component that
// renders it -- the same arrangement the logs and editors stores use, and here
// it is not merely convenient but required: switching dock tabs destroys the
// component, and a shell is a *conversation*. A half-typed command, a directory
// you have cd'd into, an editor you have open in it -- none of that could be
// rebuilt by reopening, because none of it is anywhere but in the shell itself.
//
// So the xterm instance lives here, and the component only borrows it: on mount
// it asks for the terminal and attaches it to its own element, and on destroy
// it lets go without closing anything.

import { Events } from '@wailsio/runtime';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { TerminalService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

/** What a terminal is open on. */
export interface ShellTarget {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

/** One open terminal, as the view around it sees it. */
export interface ShellDoc {
    status: 'idle' | 'opening' | 'running' | 'ended' | 'error';
    error: string;
    /** The backend's handle for the session, empty until it is open. */
    sessionId: string;
    /** What it actually attached to, which for a workload is one of its pods. */
    pod: string;
    container: string;
    /** Set for a node shell, naming the machine rather than the pod into it. */
    node: string;
    /** Every container that could be attached to, for the picker. */
    containers: kube.ContainerRef[];
}

const BLANK: ShellDoc = {
    status: 'idle',
    error: '',
    sessionId: '',
    pod: '',
    container: '',
    node: '',
    containers: [],
};

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

/**
 * Base64 in both directions, because a terminal carries bytes and the bridge
 * to the backend carries JSON strings.
 *
 * Going out, what xterm hands us is a string that may hold anything a keyboard
 * or a paste can produce, so it is encoded as UTF-8 first. Coming back, the
 * bytes are given to xterm as bytes: it decodes UTF-8 itself and, crucially,
 * carries a partial sequence across chunk boundaries -- which is where a naive
 * `atob` into a string would put a replacement character in the middle of
 * somebody's output.
 */
function encode(text: string): string {
    const bytes = new TextEncoder().encode(text);
    let binary = '';
    for (const byte of bytes) binary += String.fromCharCode(byte);
    return btoa(binary);
}

function decode(data: string): Uint8Array {
    const binary = atob(data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
}

/** One terminal's xterm instance and the addon that sizes it. */
interface Attached {
    term: XTerm;
    fit: FitAddon;
}

class Terminals {
    private docs = $state<Record<string, ShellDoc>>({});
    /** The xterm instances, which are not state: nothing re-renders on them. */
    private terms = new Map<string, Attached>();
    /** Which tab each session belongs to, so output is routed by its own id. */
    private routes = new Map<string, string>();
    private fontSize = 12;
    private scrollback = 5000;

    constructor() {
        Events.On('terminal:data', (event: { data: kube.TerminalChunk }) => {
            const chunk = event.data;
            const tab = chunk?.sessionId ? this.routes.get(chunk.sessionId) : undefined;
            if (!tab) return;

            const attached = this.terms.get(tab);
            if (chunk.data && attached) {
                attached.term.write(decode(chunk.data));
            }

            if (!chunk.done) return;

            this.routes.delete(chunk.sessionId);
            const doc = this.docs[tab];
            if (!doc) return;
            if (chunk.error) {
                doc.status = 'error';
                doc.error = chunk.error;
                attached?.term.write(`\r\n\x1b[31m${chunk.error}\x1b[0m\r\n`);
            } else {
                doc.status = 'ended';
                attached?.term.write('\r\n\x1b[2mThe session has ended.\x1b[0m\r\n');
            }
        });
    }

    /** The view for a tab. Never null: an unopened tab reads as empty. */
    doc(id: string): ShellDoc {
        return this.docs[id] ?? BLANK;
    }

    /**
     * The colours and type size a new terminal is built with, and that the open
     * ones are moved to when the theme changes.
     *
     * A terminal is the one part of the app that cannot inherit its appearance
     * from CSS: xterm draws to a canvas, so the palette has to be handed to it
     * as values. These are read from the theme's own tokens, so a terminal in
     * Catppuccin is a Catppuccin terminal rather than a black rectangle in the
     * middle of one.
     */
    dress(fontSize: number, scrollback: number): void {
        this.fontSize = fontSize;
        this.scrollback = scrollback;
        for (const [id, { term }] of this.terms) {
            term.options.theme = this.palette();
            term.options.fontSize = fontSize;
            term.options.scrollback = scrollback;
            // A different type size is a different number of columns in the
            // same element, and nothing else would notice: the element has not
            // changed size, so the observer that usually refits stays quiet.
            this.resize(id);
        }
    }

    /**
     * The palette, read from the theme that is currently applied.
     *
     * The tokens are on the document root, written there by applyTheme, so this
     * follows a theme change without knowing what themes are or that they can
     * change. The sixteen ANSI colours are mapped onto the handful of meanings
     * the app already has names for -- red is the colour of a failure here and
     * in the tables -- rather than to a second palette nobody chose.
     */
    private palette(): Record<string, string> {
        const style = getComputedStyle(document.documentElement);
        const token = (name: string, fallback: string): string =>
            style.getPropertyValue(`--${name}`).trim() || fallback;

        const bg = token('bg', '#10151c');
        const text = token('text', '#e8eef7');
        const accent = token('accent', '#4a86ff');
        const ok = token('ok', '#5fd39b');
        const warn = token('warn', '#efb567');
        const error = token('error', '#f4787f');
        const dim = token('text-dim', '#a9b6c6');
        const faint = token('text-faint', '#8593a3');
        const magenta = token('chart-4', '#c792ea');
        const cyan = token('chart-2', '#66d9e8');

        return {
            background: bg,
            foreground: text,
            cursor: accent,
            cursorAccent: bg,
            selectionBackground: token('bg-active', 'rgba(255, 255, 255, 0.13)'),
            black: bg,
            red: error,
            green: ok,
            yellow: warn,
            blue: accent,
            magenta,
            cyan,
            white: dim,
            brightBlack: faint,
            brightRed: error,
            brightGreen: ok,
            brightYellow: warn,
            brightBlue: accent,
            brightMagenta: magenta,
            brightCyan: cyan,
            brightWhite: text,
        };
    }

    /**
     * The xterm instance for a tab, made on first ask.
     *
     * It is made here rather than in the component so that it survives the
     * component: everything a shell knows is in this object, and rebuilding it
     * on every dock-tab switch would mean a new shell every time you looked
     * away.
     */
    private terminal(id: string): Attached {
        const existing = this.terms.get(id);
        if (existing) return existing;

        const term = new XTerm({
            fontFamily:
                getComputedStyle(document.documentElement).getPropertyValue('--mono').trim() ||
                'monospace',
            fontSize: this.fontSize,
            scrollback: this.scrollback,
            theme: this.palette(),
            cursorBlink: true,
            // Off, so that a copy out of the terminal is the text as it was
            // typed rather than as it was wrapped to this window's width.
            convertEol: false,
            allowProposedApi: true,
        });
        const fit = new FitAddon();
        term.loadAddon(fit);

        // Keystrokes go to the shell, and the shell is told how big the window
        // is. Both are registered once, here, so that reattaching the terminal
        // to a new element does not double them up.
        term.onData((data) => {
            const session = this.docs[id]?.sessionId;
            if (session) void TerminalService.Send(session, encode(data));
        });
        term.onResize(({ cols, rows }) => {
            const session = this.docs[id]?.sessionId;
            if (session) void TerminalService.Resize(session, cols, rows);
        });

        const attached = { term, fit };
        this.terms.set(id, attached);
        return attached;
    }

    /**
     * Puts a tab's terminal into an element and sizes it to fit.
     *
     * Called every time the view mounts, including the second and third times:
     * xterm can be moved to another element, and moving it is exactly what
     * switching dock tabs and coming back amounts to.
     */
    attach(id: string, host: HTMLElement): Attached {
        const attached = this.terminal(id);
        if (attached.term.element?.parentElement !== host) {
            attached.term.open(host);
        }
        this.resize(id);

        // The shell assumes 80x24 until it is told otherwise, and the session
        // may well have opened before there was an element to measure. Fitting
        // only reports a size when it *changes* one, so the size is sent here
        // as well: a terminal that is already the right shape still has to say
        // what shape that is.
        const session = this.docs[id]?.sessionId;
        if (session) void TerminalService.Resize(session, attached.term.cols, attached.term.rows);
        return attached;
    }

    /** Refits a terminal to the room it now has. */
    resize(id: string): void {
        const attached = this.terms.get(id);
        if (!attached?.term.element) return;
        try {
            attached.fit.fit();
        } catch {
            // fit() measures the DOM, and a terminal in a panel that is being
            // folded away has nothing to measure. There is nothing to do about
            // it and nothing worth saying: the next fit, when it is on screen,
            // is the one that matters.
        }
    }

    /** Puts the cursor in a terminal, so typing goes to the shell. */
    focus(id: string): void {
        this.terms.get(id)?.term.focus();
    }

    /**
     * Opens a shell for a tab, or leaves an open one exactly as it is.
     *
     * Called every time the component mounts, for the reason the logs store's
     * open() is: a session already running must not be restarted by looking
     * away and back.
     */
    async open(id: string, target: ShellTarget): Promise<void> {
        if (this.docs[id]) return;
        this.docs[id] = { ...BLANK, status: 'opening' };

        // The picker's contents, which a node shell does not have: a node's
        // "containers" are the one this app is about to create, and it is not
        // something to choose between.
        if (target.kind !== 'nodes') {
            try {
                const containers = await TerminalService.Containers(
                    target.contextId,
                    target.kind,
                    target.namespace,
                    target.name,
                );
                const doc = this.docs[id];
                if (doc) doc.containers = containers ?? [];
            } catch {
                // A picker that could not be filled is not a reason to refuse
                // to open a shell: the default container is still reachable,
                // and the failure will say so again if it is the real problem.
            }
        }

        await this.start(id, target, '', '');
    }

    /** Attaches to a different container, which is a new session. */
    async choose(id: string, target: ShellTarget, pod: string, container: string): Promise<void> {
        const doc = this.docs[id];
        if (!doc) return;
        if (doc.pod === pod && doc.container === container && doc.status === 'running') return;
        await this.start(id, target, pod, container);
    }

    /** Opens a session again after it has ended, in the terminal it was in. */
    async restart(id: string, target: ShellTarget): Promise<void> {
        const doc = this.docs[id];
        await this.start(id, target, doc?.pod ?? '', doc?.container ?? '');
    }

    /** Drops a terminal, with the tab it belonged to, and closes its session. */
    forget(id: string): void {
        this.stop(id);
        this.terms.get(id)?.term.dispose();
        this.terms.delete(id);
        delete this.docs[id];
    }

    /**
     * Replaces whatever session a tab is on.
     *
     * The screen is cleared with it. What is on it came from a shell that has
     * hung up, and leaving it above a fresh prompt would read as one session
     * where there are two.
     */
    private async start(id: string, target: ShellTarget, pod: string, container: string): Promise<void> {
        const doc = this.docs[id];
        if (!doc) return;

        this.stop(id);
        doc.status = 'opening';
        doc.error = '';

        const attached = this.terms.get(id);
        attached?.term.reset();

        try {
            const session =
                target.kind === 'nodes'
                    ? await TerminalService.OpenNode(target.contextId, target.name)
                    : await TerminalService.Open(
                          target.contextId,
                          target.kind,
                          target.namespace,
                          target.name,
                          pod,
                          container,
                      );

            const current = this.docs[id];
            if (!current) {
                TerminalService.Close(session.id);
                return;
            }
            current.sessionId = session.id;
            current.pod = session.pod;
            current.container = session.container;
            current.node = session.node;
            current.status = 'running';
            this.routes.set(session.id, id);

            // The shell has no idea how big the window is until it is told, and
            // the size it assumes -- 80x24 -- is almost never right.
            const term = this.terms.get(id)?.term;
            if (term) void TerminalService.Resize(session.id, term.cols, term.rows);
            this.focus(id);
        } catch (err) {
            const current = this.docs[id];
            if (current) {
                current.status = 'error';
                current.error = message(err);
            }
        }
    }

    /** Closes whatever session a tab is on, if any. */
    private stop(id: string): void {
        const doc = this.docs[id];
        if (!doc?.sessionId) return;
        this.routes.delete(doc.sessionId);
        TerminalService.Close(doc.sessionId);
        doc.sessionId = '';
    }
}

export const terminals = new Terminals();
