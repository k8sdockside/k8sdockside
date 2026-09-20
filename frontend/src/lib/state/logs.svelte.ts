// The log views open in the dock.
//
// One per dock tab, keyed by the tab's id and outliving the component that
// renders it -- the same arrangement the editors store uses, and for the same
// reason: switching dock tabs destroys the component, and scrollback you have
// been reading must survive that.
//
// A view holds a stream open. Which containers it follows, and whether it is
// following at all, are changes to that stream rather than filters over what
// has already arrived: the alternative is holding every container's lines and
// hiding most of them, which costs memory to show less.

import { Events } from '@wailsio/runtime';
import { LogService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

/** What a log view is open on. */
export interface LogTarget {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

/** One line, tagged with where it came from. */
export interface LogLine {
    pod: string;
    container: string;
    text: string;
}

/**
 * How many lines a view keeps.
 *
 * A container logging in a loop would otherwise grow this without limit until
 * the window died. Five thousand is far more than anyone scrolls back through
 * and still cheap to hold.
 */
export const KEEP = 5000;

/** One open log view. */
export interface LogDoc {
    status: 'idle' | 'opening' | 'streaming' | 'ended' | 'error';
    lines: LogLine[];
    /** Every container that could be followed, for the picker. */
    containers: kube.ContainerRef[];
    /** The containers being followed. Empty means all of them. */
    selected: string[];
    follow: boolean;
    error: string;
    /** Whether the cap has dropped lines off the front. */
    truncated: boolean;
}

const BLANK: LogDoc = {
    status: 'idle',
    lines: [],
    containers: [],
    selected: [],
    follow: true,
    error: '',
    truncated: false,
};

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Logs {
    private docs = $state<Record<string, LogDoc>>({});
    /** Which tab each stream belongs to, so a batch is routed by its own id. */
    private routes = new Map<string, string>();
    /** The stream each tab is on, so it can be closed or replaced. */
    private streams = new Map<string, string>();
    /**
     * Which start each view is on. Choosing containers twice in quick
     * succession leaves two opens in flight, and the newer one has to win --
     * the same guard the editors store puts on a reload.
     */
    private starts = new Map<string, number>();

    constructor() {
        Events.On('pod:logs', (event: { data: kube.LogBatch }) => {
            const batch = event.data;
            const tab = batch?.streamId ? this.routes.get(batch.streamId) : undefined;
            if (!tab) return;

            const doc = this.docs[tab];
            if (!doc) return;

            const arrived = batch.lines ?? [];
            if (arrived.length > 0) {
                const next = doc.lines.concat(arrived);
                if (next.length > KEEP) {
                    doc.lines = next.slice(next.length - KEEP);
                    doc.truncated = true;
                } else {
                    doc.lines = next;
                }
            }

            if (batch.done) {
                this.routes.delete(batch.streamId);
                if (batch.error) {
                    doc.status = 'error';
                    doc.error = batch.error;
                } else {
                    doc.status = 'ended';
                }
            }
        });
    }

    /** The view for a tab. Never null: an unopened tab reads as empty. */
    doc(id: string): LogDoc {
        return this.docs[id] ?? BLANK;
    }

    /**
     * Opens a view, finding what it could follow and then following all of it.
     *
     * Called every time the component mounts, so a view already open is left
     * exactly as it was rather than starting again from an empty screen.
     */
    async open(id: string, target: LogTarget): Promise<void> {
        if (this.docs[id]) return;

        this.docs[id] = { ...BLANK, status: 'opening', lines: [] };
        try {
            const containers = await LogService.Containers(
                target.contextId,
                target.kind,
                target.namespace,
                target.name,
            );
            const doc = this.docs[id];
            if (!doc) return;
            doc.containers = containers ?? [];
            // Named rather than left empty, so the picker can show every
            // rectangle lit without having to know that empty means all.
            doc.selected = [...new Set((containers ?? []).map((c) => c.container))];
        } catch (err) {
            const doc = this.docs[id];
            if (doc) {
                doc.status = 'error';
                doc.error = message(err);
            }
            return;
        }
        await this.start(id, target);
    }

    /** Follows a different set of containers. Empty means all of them. */
    async choose(id: string, target: LogTarget, containers: string[]): Promise<void> {
        const doc = this.docs[id];
        if (!doc) return;
        doc.selected = containers;
        await this.start(id, target);
    }

    /** Turns following on or off. Off still shows what is there now. */
    async setFollow(id: string, target: LogTarget, on: boolean): Promise<void> {
        const doc = this.docs[id];
        if (!doc || doc.follow === on) return;
        doc.follow = on;
        await this.start(id, target);
    }

    /**
     * Opens the stream again from the beginning.
     *
     * The backend waits for a container that is still starting, so this is not
     * how you get at a log that has not begun yet. It is for the stream that
     * ended -- a pod that restarted, a rollout that replaced it -- where what
     * is on screen is the log of something that is no longer running.
     */
    async reload(id: string, target: LogTarget): Promise<void> {
        const doc = this.docs[id];
        if (!doc) {
            await this.open(id, target);
            return;
        }
        // The containers are read again too: a pod that restarted may have a
        // different set, and the picker would otherwise name the old one.
        try {
            const containers = await LogService.Containers(
                target.contextId,
                target.kind,
                target.namespace,
                target.name,
            );
            const current = this.docs[id];
            if (!current) return;
            const found = containers ?? [];
            if (found.length > 0) {
                const names = [...new Set(found.map((c) => c.container))];
                current.containers = found;
                // Whatever was being followed and is still there stays being
                // followed; a view reloaded onto a new container set follows
                // all of it rather than nothing.
                const kept = current.selected.filter((c) => names.includes(c));
                current.selected = kept.length > 0 ? kept : names;
            }
        } catch {
            // Not fatal: the stream below says why far better than this can,
            // and the containers already named are still worth trying.
        }
        await this.start(id, target);
    }

    /** Empties the view. The stream stays open. */
    clear(id: string): void {
        const doc = this.docs[id];
        if (!doc) return;
        doc.lines = [];
        doc.truncated = false;
    }

    /** Drops a view, with the tab it belonged to, and closes its stream. */
    forget(id: string): void {
        this.stop(id);
        this.starts.delete(id);
        delete this.docs[id];
    }

    /**
     * Replaces whatever stream a view is on.
     *
     * The lines go with the old stream. What is on screen came from containers
     * that may no longer be followed, and keeping it would leave a view whose
     * contents did not match its own heading.
     */
    private async start(id: string, target: LogTarget): Promise<void> {
        const doc = this.docs[id];
        if (!doc) return;

        const attempt = (this.starts.get(id) ?? 0) + 1;
        this.starts.set(id, attempt);

        this.stop(id);
        doc.lines = [];
        doc.truncated = false;
        doc.error = '';
        doc.status = 'opening';

        // Empty is the backend's "all of them", and sending every name when
        // every name is selected would break the moment a pod gained one.
        const wanted = doc.selected.length === doc.containers.length ? [] : doc.selected;

        try {
            const stream = await LogService.Open(
                target.contextId,
                target.kind,
                target.namespace,
                target.name,
                wanted,
                doc.follow,
            );
            const current = this.docs[id];
            // Another change landed while this was opening; its own stream is
            // the one that matters, whichever of the two the backend answered
            // first. Keyed on the attempt rather than on whether a stream is
            // already set, which would have let the older one win by being
            // quicker to open.
            if (!current || this.starts.get(id) !== attempt) {
                LogService.Close(stream);
                return;
            }
            this.streams.set(id, stream);
            this.routes.set(stream, id);
            current.status = 'streaming';
        } catch (err) {
            const current = this.docs[id];
            if (current && this.starts.get(id) === attempt) {
                current.status = 'error';
                current.error = message(err);
            }
        }
    }

    /** Closes whatever stream a view is on, if any. */
    private stop(id: string): void {
        const stream = this.streams.get(id);
        if (!stream) return;
        this.streams.delete(id);
        this.routes.delete(stream);
        LogService.Close(stream);
    }
}

export const logs = new Logs();
