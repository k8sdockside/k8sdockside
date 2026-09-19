// Whether each cluster answers, for the sidebar indicator and the error pages.
//
// Its own store rather than a corner of the workspace because it is genuinely
// separate: nothing here reads a tab, a pane or a setting, and nothing in the
// workspace reads a status except to draw it. What connects them is one call on
// load -- see prune -- and that is passed in rather than reached for.

import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';

export type HealthStatus = 'unknown' | 'checking' | 'connected' | 'error';

/**
 * What a cluster last said. `unknown` is the honest answer before anything has
 * asked, and is drawn as no indicator at all rather than as a problem.
 */
export interface Health {
    status: HealthStatus;
    /** Why, when the status is an error. Empty otherwise. */
    message: string;
}

const UNCHECKED: Health = { status: 'unknown', message: '' };

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Clusters {
    private states = $state<Record<string, Health>>({});
    /**
     * Bumped per context by forget, so a probe that set out before the
     * context was disconnected does not paint it connected when it returns.
     */
    private generations = new Map<string, number>();

    /** How a context last responded. Never probed is `unknown`, not an error. */
    of(contextId: string): Health {
        return this.states[contextId] ?? UNCHECKED;
    }

    /**
     * Records what we now know about a cluster.
     *
     * Tabs call this as well as probes, and that is the point: a dashboard that
     * has just failed to load is better evidence than any ping, and routing
     * both through one map is what stops the sidebar indicator and the error
     * page in the tab from disagreeing.
     */
    report(contextId: string, status: HealthStatus, detail = ''): void {
        this.states[contextId] = { status, message: detail };
    }

    /**
     * Checks whether a cluster answers, for the sidebar indicator.
     *
     * Probing is lazy and deliberately so: building a client can run an exec
     * credential plugin, and a kubeconfig with twenty contexts would otherwise
     * launch twenty subprocesses at startup for clusters the user never asked
     * about. So a context is probed when it is touched -- selected, expanded or
     * opened in a tab -- and not again unless something asks it to be.
     */
    async probe(contextId: string, { force = false } = {}): Promise<void> {
        const status = this.of(contextId).status;
        // A probe already in flight will report for both callers.
        if (status === 'checking') return;
        if (!force && status !== 'unknown') return;

        this.report(contextId, 'checking');
        const generation = this.generations.get(contextId) ?? 0;
        const current = () => (this.generations.get(contextId) ?? 0) === generation;
        try {
            await ResourceService.Ping(contextId);
            if (current()) this.report(contextId, 'connected');
        } catch (err) {
            if (current()) this.report(contextId, 'error', message(err));
        }
    }

    /**
     * Forgets what a context said, as if it had never been asked: the
     * sidebar draws no indicator for it, and a recheck leaves it alone. What
     * a disconnected context looks like.
     */
    forget(contextId: string): void {
        this.generations.set(contextId, (this.generations.get(contextId) ?? 0) + 1);
        const { [contextId]: _, ...rest } = this.states;
        this.states = rest;
    }

    /**
     * Forgets the status of contexts that are no longer in any kubeconfig.
     *
     * The ids are passed in rather than read off the workspace: which contexts
     * exist is the kubeconfigs' business, and this store having an opinion
     * about that is what would tie it back to everything it was split out of.
     */
    prune(known: Iterable<string>): void {
        const alive = new Set(known);
        const kept: Record<string, Health> = {};
        for (const [id, state] of Object.entries(this.states)) {
            if (alive.has(id)) kept[id] = state;
        }
        this.states = kept;
    }

    /**
     * Re-probes the contexts already carrying a status, so that the sync button
     * refreshes what is on screen. Contexts never checked stay unchecked: a
     * rescan is not a reason to start waking clusters the user has not asked
     * about.
     */
    recheck(): void {
        for (const id of Object.keys(this.states)) {
            void this.probe(id, { force: true });
        }
    }
}

export const clusters = new Clusters();
