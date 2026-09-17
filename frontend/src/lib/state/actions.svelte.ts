// Doing things to objects: deleting, scaling, restarting, cordoning, draining.
//
// The catalogue in ../actions.ts decides which buttons a kind gets; this runs
// what they do. Split that way because the two change for different reasons --
// one when a kind gains an action, the other when an action gains a step -- and
// because the catalogue is then a table anyone can read without a cluster.
//
// Like the editors store, nothing here knows about the workspace. An action
// needs the four fields that name an object; whoever pressed the button owns
// telling the user how it went.

import { Events } from '@wailsio/runtime';
import { ActionService } from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/kube/models.js';
import { changes, type ObjectRef } from './changes.svelte';
import { VM_KINDS } from '../actions';

/** What the bar knows about an object beyond its name. */
export interface ObjectState {
    scalable: boolean;
    replicas: number;
    cordoned: boolean;
    /** A pod's containers, as the same squares the table draws. */
    containers: kube.Pill[];
    /** A Job's or CronJob's spec.suspend. */
    suspended: boolean;
    /** A Deployment's spec.paused. */
    paused: boolean;
    /** A certificate signing request nobody has answered yet. */
    pending: boolean;
    /**
     * A virtual machine's lifecycle state, for the bar that offers Start or
     * Stop but never both. `isMachine` is false for every other kind, which is
     * what keeps a Deployment from growing VM buttons.
     */
    vm: { isMachine: boolean; running: boolean; paused: boolean; migratable: boolean; status: string };
}

/** A pod a drain would not move, and why. */
export interface Refusal {
    pod: { namespace: string; name: string };
    reason: string;
    /** The option that would have moved it: "force" or "deleteEmptyDirData". */
    option: string;
}

/**
 * What a drain is told beyond `kubectl drain`'s defaults: the flags k9s's drain
 * dialog asks for, and kubectl's --pod-selector and --disable-eviction.
 */
export interface DrainOptions {
    /** Seconds each pod gets to stop, instead of its own. Null keeps its own. */
    gracePeriodSeconds: number | null;
    /** Seconds before the drain gives up. Zero waits as long as budgets take. */
    timeoutSeconds: number;
    /** Move pods holding emptyDir data, losing the data. */
    deleteEmptyDirData: boolean;
    /** Move pods nothing manages, deleting them for good. */
    force: boolean;
    /** Delete rather than evict, so no disruption budget is asked. */
    disableEviction: boolean;
    /** Only the pods carrying these labels. Empty is every pod. */
    podSelector: string;
}

/** `kubectl drain` with no flags. */
export const DRAIN_DEFAULTS: DrainOptions = {
    gracePeriodSeconds: null,
    timeoutSeconds: 0,
    deleteEmptyDirData: false,
    force: false,
    disableEviction: false,
    podSelector: '',
};

/** A drain in flight, or the last one this node had. */
export interface DrainState {
    id: string;
    /** "cordoning", "planning", "evicting", "done" or "failed". */
    phase: string;
    evicted: number;
    total: number;
    refused: Refusal[];
    error: string;
    done: boolean;
}

/** One entry in a workload's rollout history. */
export interface RolloutRevision {
    revision: number;
    images: string[];
    changeCause: string;
    age: string;
    current: boolean;
}

/** One object of a kind, as a table row names it. */
export interface RowRef {
    namespace: string;
    name: string;
}

/** What a merge patch typed as YAML comes to, or what is wrong with it. */
export interface PatchPreview {
    /** The compact JSON each object would receive. */
    json: string;
    /** A document holding nothing yet: blank, or comments only. */
    empty: boolean;
    error: string;
    /** The line the parser stopped at, 1-based; 0 when it named none. */
    line: number;
}

/** How a bulk action went: how many objects took it, and which ones did not. */
export interface BulkReport {
    done: number;
    failures: { namespace: string; name: string; error: string }[];
}

const NOT_A_MACHINE = { isMachine: false, running: false, paused: false, migratable: false, status: '' };
const UNKNOWN: ObjectState = {
    scalable: false,
    replicas: 0,
    cordoned: false,
    containers: [],
    suspended: false,
    paused: false,
    pending: false,
    vm: NOT_A_MACHINE,
};

function key(ref: ObjectRef): string {
    return `${ref.contextId}#${ref.kind}#${ref.namespace}#${ref.name}`;
}

function message(err: unknown): Error {
    return err instanceof Error ? err : new Error(String(err));
}

/** A drain that has only just been asked for, before the first report lands. */
function starting(id: string): DrainState {
    return { id, phase: 'cordoning', evicted: 0, total: 0, refused: [], error: '', done: false };
}

class Actions {
    private states = $state<Record<string, ObjectState>>({});
    private drains = $state<Record<string, DrainState>>({});
    /**
     * Which node each drain ID belongs to. Reports are routed by the ID the
     * backend handed back rather than by the node name in the payload, so two
     * nodes draining at once cannot be confused for each other.
     */
    private draining = new Map<string, string>();

    constructor() {
        Events.On('node:drain', (event: { data: kube.DrainProgress }) => {
            const progress = event.data;
            const at = progress?.drainId ? this.draining.get(progress.drainId) : undefined;
            if (!at) return;

            this.drains[at] = {
                id: progress.drainId,
                phase: progress.phase,
                evicted: progress.evicted,
                total: progress.total,
                refused: progress.refused ?? [],
                error: progress.error,
                done: progress.done,
            };
            if (progress.done) this.draining.delete(progress.drainId);
        });
    }

    /** What is known about an object. Never null: an unread one reads as plain. */
    stateOf(ref: ObjectRef): ObjectState {
        return this.states[key(ref)] ?? UNKNOWN;
    }

    /**
     * Reads the object for what its buttons need to say.
     *
     * A failure is swallowed rather than raised: Edit and Delete need nothing
     * from this call, and a cluster that will not answer must still leave a
     * usable bar rather than an error where the buttons were.
     */
    async load(ref: ObjectRef): Promise<void> {
        try {
            const state = await ActionService.ObjectState(ref.contextId, ref.kind, ref.namespace, ref.name);
            this.states[key(ref)] = {
                scalable: state.scalable,
                replicas: state.replicas,
                cordoned: state.cordoned,
                // Null rather than empty is what Go sends for a kind with none.
                containers: state.containers ?? [],
                suspended: state.suspended ?? false,
                paused: state.paused ?? false,
                pending: state.pending ?? false,
                // Only asked for the two KubeVirt kinds, and only after the
                // first call has answered: every other object pays nothing for
                // a question that is not about it.
                vm: VM_KINDS.includes(ref.kind) ? await this.vmState(ref) : NOT_A_MACHINE,
            };
        } catch {
            // Deliberately silent -- see above.
        }
    }

    /**
     * A machine's lifecycle state, or "not a machine" when the cluster will not
     * say.
     *
     * Swallowed like the call above it and for the same reason: a KubeVirt the
     * user can read but not act on should still show the panel, with the
     * buttons simply absent rather than an error where they would be.
     */
    private async vmState(ref: ObjectRef): Promise<ObjectState['vm']> {
        try {
            const state = await ActionService.VMState(ref.contextId, ref.kind, ref.namespace, ref.name);
            return {
                isMachine: state.isMachine,
                running: state.running,
                paused: state.paused,
                migratable: state.migratable,
                status: state.status ?? '',
            };
        } catch {
            return NOT_A_MACHINE;
        }
    }

    /** Drops what is held about an object, with the panel that showed it. */
    forget(ref: ObjectRef): void {
        const at = key(ref);
        delete this.states[at];
        const drain = this.drains[at];
        if (drain) this.draining.delete(drain.id);
        delete this.drains[at];
    }

    /**
     * Deletes the object.
     *
     * Named `remove` because `delete` is a reserved word, and the alternative
     * -- a method you can only call through a string index -- reads worse than
     * the rename.
     *
     * It does not signal a change, unlike every other action here: there is no
     * object left to re-read, and asking the describe panel to try would show
     * the user a 404 where their object used to be.
     */
    async remove(ref: ObjectRef): Promise<void> {
        try {
            await ActionService.Delete(ref.contextId, ref.kind, ref.namespace, ref.name);
        } catch (err) {
            throw message(err);
        }
    }

    /**
     * Deletes several objects of one kind in one call.
     *
     * The report says how many went and names the ones that did not, in the
     * API server's words. Like `remove`, nothing is signalled as changed:
     * what changed is gone.
     */
    async removeMany(contextId: string, kind: string, refs: RowRef[]): Promise<BulkReport> {
        try {
            const report = await ActionService.DeleteMany(contextId, kind, refs);
            // Null rather than empty is what Go sends for none.
            return { done: report.done, failures: report.failures ?? [] };
        } catch (err) {
            throw message(err);
        }
    }

    /**
     * Applies one merge patch to several objects of one kind, and says that
     * each object that took it changed -- so a panel or editor showing one of
     * them re-reads rather than going on showing what it held a moment ago.
     * The patch travels as text, the form's JSON or the editor's YAML, and
     * the backend reads it either way.
     */
    async patchMany(contextId: string, kind: string, refs: RowRef[], patch: string): Promise<BulkReport> {
        let report: BulkReport;
        try {
            const raw = await ActionService.PatchMany(contextId, kind, refs, patch);
            report = { done: raw.done, failures: raw.failures ?? [] };
        } catch (err) {
            throw message(err);
        }
        const refused = new Set(report.failures.map((f) => `${f.namespace}/${f.name}`));
        for (const ref of refs) {
            if (!refused.has(`${ref.namespace}/${ref.name}`)) changes.changed({ contextId, kind, ...ref });
        }
        return report;
    }

    /**
     * Reads a merge patch typed as YAML for the form's preview: the JSON it
     * comes to, or the parser's complaint and its line. A call into Go rather
     * than a parser here, for the reason the editor's check is one: the
     * parser that answers is the one that will read the patch when it is
     * applied.
     */
    async previewPatch(text: string): Promise<PatchPreview> {
        try {
            const p = await ActionService.PreviewPatch(text);
            return { json: p.json, empty: p.empty, error: p.error, line: p.line };
        } catch (err) {
            throw message(err);
        }
    }

    async scale(ref: ObjectRef, replicas: number): Promise<void> {
        await this.run(ref, () =>
            ActionService.Scale(ref.contextId, ref.kind, ref.namespace, ref.name, replicas),
        );
    }

    async restart(ref: ObjectRef): Promise<void> {
        await this.run(ref, () => ActionService.Restart(ref.contextId, ref.kind, ref.namespace, ref.name));
    }

    /** Suspends a Job or CronJob, or lets it carry on. */
    async suspend(ref: ObjectRef, on: boolean): Promise<void> {
        await this.run(ref, () => ActionService.Suspend(ref.contextId, ref.kind, ref.namespace, ref.name, on));
    }

    /** Holds a Deployment's rollout, or lets it go on. */
    async pause(ref: ObjectRef, on: boolean): Promise<void> {
        await this.run(ref, () => ActionService.PauseRollout(ref.contextId, ref.namespace, ref.name, on));
    }

    /**
     * Runs a CronJob now. Resolves with the name of the Job it started, which
     * is the one thing worth telling whoever pressed the button.
     */
    async trigger(ref: ObjectRef): Promise<string> {
        let created = '';
        await this.run(ref, async () => {
            created = await ActionService.TriggerCronJob(ref.contextId, ref.namespace, ref.name);
        });
        return created;
    }

    /**
     * Evicts a pod. Like `remove`, nothing is signalled as changed: the pod is
     * on its way out, and a panel re-reading it would soon be reading nothing.
     */
    async evict(ref: ObjectRef): Promise<void> {
        try {
            await ActionService.Evict(ref.contextId, ref.namespace, ref.name);
        } catch (err) {
            throw message(err);
        }
    }

    /** Approves or denies a certificate signing request. */
    async answer(ref: ObjectRef, approve: boolean): Promise<void> {
        await this.run(ref, () => ActionService.AnswerCSR(ref.contextId, ref.name, approve));
    }

    /** A workload's revisions, newest first. */
    async history(ref: ObjectRef): Promise<RolloutRevision[]> {
        try {
            const got = await ActionService.RolloutHistory(ref.contextId, ref.kind, ref.namespace, ref.name);
            // Null rather than empty is what Go sends for none.
            return (got ?? []).map((r) => ({ ...r, images: r.images ?? [] }));
        } catch (err) {
            throw message(err);
        }
    }

    /** Puts a workload back to an earlier revision. */
    async undo(ref: ObjectRef, revision: number): Promise<void> {
        await this.run(ref, () =>
            ActionService.RollbackWorkload(ref.contextId, ref.kind, ref.namespace, ref.name, revision),
        );
    }

    async cordon(ref: ObjectRef, on: boolean): Promise<void> {
        await this.run(ref, () => ActionService.Cordon(ref.contextId, ref.name, on));
    }

    /**
     * One virtual machine lifecycle operation: the virtctl set, minus the two
     * that are a terminal rather than a command.
     *
     * `op` is the backend's own name for it -- start, stop, restart, pause,
     * unpause, softreboot, migrate -- rather than the action id, so the two
     * sides do not have to agree on a UI string.
     */
    async vmOperation(ref: ObjectRef, op: string): Promise<void> {
        await this.run(ref, () => ActionService.VMOperation(ref.contextId, op, ref.namespace, ref.name));
    }

    /** The drain on this node, or null if it has never had one. */
    drainOf(ref: ObjectRef): DrainState | null {
        return this.drains[key(ref)] ?? null;
    }

    /**
     * Starts moving everything off a node.
     *
     * It resolves once the drain is under way, not once it is finished: a drain
     * waits on disruption budgets and can take minutes, and its progress
     * arrives as events. Options the backend cannot use are refused here,
     * before the node is touched.
     */
    async drain(ref: ObjectRef, options: DrainOptions = DRAIN_DEFAULTS): Promise<void> {
        let id: string;
        try {
            id = await ActionService.Drain(ref.contextId, ref.name, options);
        } catch (err) {
            throw message(err);
        }
        const at = key(ref);
        this.draining.set(id, at);
        // Only if nothing has already reported: the first event can arrive
        // before the call carrying the ID has come back, exactly as a
        // subscription's first snapshot can.
        this.drains[at] ??= starting(id);
    }

    /**
     * Calls off a drain in flight. The node stays cordoned -- it is half
     * emptied, and letting work back onto it is not what stopping meant.
     */
    cancelDrain(ref: ObjectRef): void {
        const drain = this.drains[key(ref)];
        if (!drain || drain.done) return;
        ActionService.CancelDrain(drain.id);
    }

    /**
     * Runs one action and, if it worked, says the object changed -- so the
     * describe panel beside the button re-reads rather than going on showing
     * what the cluster held a moment ago.
     */
    private async run(ref: ObjectRef, call: () => Promise<unknown>): Promise<void> {
        try {
            await call();
        } catch (err) {
            throw message(err);
        }
        changes.changed(ref);
        await this.load(ref);
    }
}

export const actions = new Actions();
