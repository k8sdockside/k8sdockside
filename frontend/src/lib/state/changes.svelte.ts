// What has been written, per object.
//
// An object can be on screen in more than one place at once -- the describe
// panel and an editor in the dock -- and only one of those places is the one
// doing the writing. Rather than have the writer know who else is looking, it
// says only that an object changed; whoever is showing that object notices and
// re-reads.
//
// A revision rather than a callback because the readers are Svelte components:
// reading `revision(ref)` inside an effect is the subscription, and there is
// nothing to unregister when the component goes.
//
// This is about our own writes. Changes made elsewhere -- kubectl, a controller
// -- do not pass through here; catching those means watching the object in the
// backend, which is a larger thing than this.

/** One object, by the four fields that name it. */
export interface ObjectRef {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

/**
 * The key one object's revision is held under. A name alone is unique in
 * nothing: not across namespaces, kinds, or clusters.
 */
function key(ref: ObjectRef): string {
    return `${ref.contextId}#${ref.kind}#${ref.namespace}#${ref.name}`;
}

class Changes {
    private revs = $state<Record<string, number>>({});
    /** Writes per cluster, deletes included -- see writes. */
    private clusters = $state<Record<string, number>>({});

    /**
     * How many times this object has been written since the app started. The
     * number itself means nothing; that it has moved is the whole signal.
     */
    revision(ref: ObjectRef | null): number {
        return ref ? (this.revs[key(ref)] ?? 0) : 0;
    }

    /** Says an object has been written. Anything showing it re-reads. */
    changed(ref: ObjectRef): void {
        const k = key(ref);
        this.revs[k] = (this.revs[k] ?? 0) + 1;
        this.touched(ref.contextId);
    }

    /**
     * How many times anything in this cluster has been written or deleted
     * from here. For views that summarise a whole cluster rather than show
     * one object -- the dashboard, whose counts go stale the moment a pod is
     * deleted and would otherwise wait for the next poll to say so.
     */
    writes(contextId: string): number {
        return this.clusters[contextId] ?? 0;
    }

    /**
     * Says something in a cluster changed without naming an object to
     * re-read: a delete or an eviction, where the object is gone.
     */
    touched(contextId: string): void {
        this.clusters[contextId] = (this.clusters[contextId] ?? 0) + 1;
    }
}

export const changes = new Changes();
