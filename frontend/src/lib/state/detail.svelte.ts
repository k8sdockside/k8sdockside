// The describe tab: what it is reporting on, and the report itself.
//
// Split out of the workspace because the reading and the state are genuinely
// its own -- what is being described, whether the read landed, and which read
// wins when two are in flight. What is *not* its own is the tab: where the
// report appears, which pane it was dragged into and how it is closed are the
// workspace's business, and stay there.
//
// Those two halves meet at Presenter. Rather than reach back for the workspace
// -- which would make the pair import each other, with the initialisation order
// that implies -- this store is handed the two operations it needs and knows
// nothing else about panes.

import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { HELM_RELEASES } from '../catalogue';
import { changes } from './changes.svelte';

/** The object the detail panel is describing. */
export interface DetailTarget {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

/**
 * How the report is put on screen and taken away again, supplied by whoever
 * owns the tabs. Registered once at startup -- see connect.
 */
export interface Presenter {
    /** Puts the describe tab in front, titled with what it describes. */
    show(target: DetailTarget): void;
    /** Closes the describe tab, if it is open. */
    hide(): void;
}

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Detail {
    /** What is being described, or nothing. */
    target = $state<DetailTarget | null>(null);
    /** The report, once it has arrived. */
    text = $state('');
    loading = $state(false);
    error = $state<string | null>(null);
    /**
     * Which version of the object the report was read against, so an edit
     * landing afterwards can be seen to have superseded it.
     */
    revision = $state(0);

    /**
     * Whether a Secret's values are being shown in plain text.
     *
     * False for every object as it opens, including the next Secret after one
     * that was revealed: a panel that stayed revealed would eventually put a
     * password on screen because of a button pressed on a different object ten
     * minutes earlier. Turning it on re-reads, because the decoding happens in
     * the backend -- see ResourceService.Describe.
     */
    revealed = $state(false);

    /**
     * Every read takes a number and only the newest may land, so that a slow
     * one answering late cannot put the panel back to what it said before -- or
     * fill in a panel the user has since closed.
     */
    private load = 0;

    /** Nothing until connect is called; a store with no tabs shows nothing. */
    private stage: Presenter | null = null;

    /** Gives the store the tab operations it cannot do for itself. */
    connect(presenter: Presenter): void {
        this.stage = presenter;
    }

    /**
     * Describes one object.
     *
     * One tab for the window rather than one per object: clicking row after row
     * refills it, which is what the panel did before it was a tab and what
     * anyone reading down a list actually wants. What it costs is the ability
     * to hold two reports open at once, which is the editor's job anyway.
     */
    async open(target: DetailTarget): Promise<void> {
        this.target = target;
        this.revision = changes.revision(target);
        this.loading = true;
        this.error = null;
        // Every object opens hidden, whatever the last one was showing.
        this.revealed = false;
        this.stage?.show(target);
        await this.read(target);
    }

    /**
     * Shows or hides a Secret's values, re-reading to get them.
     *
     * Quietly, without raising `loading`: the report on screen is already the
     * right object and blanking it to "Describing…" to swap one field would
     * read as the panel reloading rather than as a value being revealed.
     */
    async reveal(on: boolean): Promise<void> {
        if (this.revealed === on || !this.target) return;
        this.revealed = on;
        await this.read(this.target);
    }

    /**
     * Re-reads what the panel is describing, after the object has been written.
     *
     * It does not go through open because it must not raise `loading`: the
     * report on screen is a moment out of date, which is better than blanking
     * the panel to "Describing…" on every save.
     */
    async refresh(): Promise<void> {
        const target = this.target;
        if (!target) return;
        this.revision = changes.revision(target);
        await this.read(target);
    }

    /**
     * Puts the describe tab away and forgets what it held.
     *
     * Called by the tab's own close button, by Escape, and by the workspace
     * when the list the report belonged to is left or closed.
     */
    close(): void {
        this.clear();
        this.stage?.hide();
    }

    /**
     * Drops the report without touching the tab.
     *
     * The half of closing the workspace's `forget` needs: the tab is already on
     * its way out by the time it is called, and going back through close from
     * there would send it round the houses to close a tab that has gone.
     */
    clear(): void {
        // Takes the number with it, so whatever is in flight has already lost.
        this.load++;
        this.target = null;
        this.text = '';
        this.error = null;
        this.revision = 0;
        this.loading = false;
        this.revealed = false;
    }

    /**
     * Whether a tab is the list the open report was opened from.
     *
     * The report belongs to the list it was opened from, and both of the rules
     * that close it turn on that belonging: leaving the list, or closing it.
     * Neither should fire for some other list that happens to be in the way.
     */
    describesTheListIn(tab: { contextId: string; kind: string }): boolean {
        const target = this.target;
        if (!target) return false;
        return tab.contextId === target.contextId && tab.kind === target.kind;
    }

    /** Reads one object's describe report into the panel. */
    private async read(target: DetailTarget): Promise<void> {
        const attempt = ++this.load;

        // A Helm release has no Kubernetes kind, so there is nothing here for
        // the REST mapper to resolve and Describe can only answer "unknown
        // resource kind: helmreleases" -- correctly, since there is no such
        // kind. The drawer renders the release's own record instead, read by
        // HelmRelease.svelte, so this leaves the report empty rather than
        // filling the panel with a complaint about a call that should not have
        // been made.
        if (target.kind === HELM_RELEASES) {
            this.text = '';
            this.error = null;
            this.loading = false;
            return;
        }

        try {
            const text = await ResourceService.Describe(
                target.contextId,
                target.kind,
                target.namespace,
                target.name,
                this.revealed,
            );
            if (this.load !== attempt) return;
            this.text = text;
            this.error = null;
        } catch (err) {
            if (this.load !== attempt) return;
            this.text = '';
            this.error = message(err);
        } finally {
            if (this.load === attempt) this.loading = false;
        }
    }
}

export const detail = new Detail();
