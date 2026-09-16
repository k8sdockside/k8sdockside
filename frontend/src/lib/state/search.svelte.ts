// The search in the title bar: what is being looked for, where, and what has
// turned up so far.
//
// A search runs in the backend, several clusters at once, and reports as it
// goes -- see SearchService. This store holds one search at a time. Starting
// another calls off the last, and an update for a search that is no longer the
// current one is dropped, so a slow cluster answering an old query cannot put
// its rows among the new one's.
//
// It also tells the sidebar what it learns on the way: a cluster that answered
// a search is connected, and one that could not be reached says why, exactly
// as if it had been probed.

import { Events } from '@wailsio/runtime';
import { SearchService } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/kube/models.js';
import type * as services from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services/models.js';
import { clusters } from './health.svelte';

export type SearchHit = kube.SearchHit;

/** Which contexts a search covers: the ones open already, or every one there is. */
export type Scope = 'connected' | 'all';

/** Where one cluster's part of a search has got. `waiting` is before the backend has reached it. */
export type ClusterPhase = 'waiting' | 'connecting' | 'searching' | 'done' | 'error';

const PHASES: readonly string[] = ['waiting', 'connecting', 'searching', 'done', 'error'];

export interface ClusterProgress {
    contextId: string;
    phase: ClusterPhase;
    /** Kinds listed so far, of how many, and how many of them could not be. */
    done: number;
    total: number;
    failed: number;
    error: string;
    /** Hits this cluster has contributed. */
    found: number;
}

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Search {
    /** What is in the box. */
    query = $state('');
    /** Kinds the options narrow every search to; none is every kind. */
    kinds = $state<string[]>([]);
    scope = $state<Scope>('connected');
    /** Whether the panel under the box is showing. */
    open = $state(false);
    /**
     * Counts requests to put the cursor in the box, which the box watches:
     * the menu bar asks for it, and only the box can take the focus.
     */
    focusRequests = $state(0);

    /** The search on screen, running or finished; null before the first. */
    current = $state<string | null>(null);
    running = $state(false);
    /** The query the results answer, which the box may have moved on from since. */
    answered = $state('');
    /** Raw: a thousand hits never change once they arrive, and need no proxies. */
    hits = $state.raw<SearchHit[]>([]);
    progress = $state<ClusterProgress[]>([]);
    /** The search stopped at the backend's limit rather than at the end. */
    truncated = $state(false);
    /** Why the search could not start at all. */
    error = $state('');

    private count = 0;

    /** Asks the search box to take the focus and show its panel, as ⌘K does. */
    requestFocus(): void {
        this.focusRequests++;
    }

    /** Starts a search of these contexts, calling off whatever was running. */
    async start(query: string, contextIds: string[]): Promise<void> {
        this.cancel();
        const id = `search-${++this.count}-${Date.now()}`;
        this.current = id;
        this.answered = query;
        this.hits = [];
        this.truncated = false;
        this.error = '';
        this.progress = contextIds.map((contextId) => ({
            contextId,
            phase: 'waiting',
            done: 0,
            total: 0,
            failed: 0,
            error: '',
            found: 0,
        }));
        if (contextIds.length === 0) {
            this.running = false;
            this.error = 'There is no context open to search. Open one in the sidebar, or search all contexts.';
            return;
        }

        this.running = true;
        try {
            await SearchService.Start({ id, query, kinds: [...this.kinds], contextIds });
        } catch (err) {
            if (this.current !== id) return;
            this.running = false;
            this.progress = [];
            this.error = message(err);
        }
    }

    /**
     * Calls the running search off. What it found stays on screen, and the
     * clusters still going report themselves done as they stop.
     */
    cancel(): void {
        if (this.current && this.running) {
            SearchService.Cancel(this.current).catch(() => {
                // Already finished on its own; nothing to stop.
            });
        }
        this.running = false;
    }

    /** Forgets the search and its results, as emptying the box does. */
    clear(): void {
        this.cancel();
        this.current = null;
        this.answered = '';
        this.hits = [];
        this.progress = [];
        this.truncated = false;
        this.error = '';
    }

    /** Takes one update from the backend. */
    receive(update: services.SearchUpdate | null | undefined): void {
        if (!update || update.searchId !== this.current) return;
        if (update.finished) {
            this.running = false;
            this.truncated = update.truncated;
            return;
        }

        const row = this.progress.find((p) => p.contextId === update.contextId);
        if (!row) return;
        const found = update.hits ?? [];
        if (PHASES.includes(update.phase)) row.phase = update.phase as ClusterPhase;
        row.done = update.step.done;
        row.total = update.step.total;
        row.failed = update.step.failed;
        row.found += found.length;
        if (update.error) row.error = update.error;
        if (found.length > 0) this.hits = [...this.hits, ...found];

        // A cluster that has said what kinds it serves has answered; one that
        // could not be searched at all is as unreachable as a failed probe.
        if (update.phase === 'error') {
            clusters.report(update.contextId, 'error', update.error);
        } else if (update.phase === 'searching' && clusters.of(update.contextId).status !== 'connected') {
            clusters.report(update.contextId, 'connected');
        }
    }
}

export const search = new Search();

Events.On('search:update', (event) => search.receive(event.data));
