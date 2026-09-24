// What the Compare tab is comparing: the object on each side.
//
// A store rather than the tab's own state because the tab is often opened
// from somewhere else -- an object's "Compare with another cluster…" -- and
// the component that will show it is built afterwards, the way a pods tab
// narrowed to a node reads its filter as it mounts. Kept for the session.

/** One side of a comparison. An empty contextId is a side not chosen yet. */
export interface CompareTarget {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

function blank(): CompareTarget {
    return { contextId: '', kind: '', namespace: '', name: '' };
}

class CompareState {
    left = $state<CompareTarget>(blank());
    right = $state<CompareTarget>(blank());
    /** Bumped when a caller sets the sides, so an open tab compares again. */
    request = $state(0);

    /**
     * Compares one object with the same object elsewhere. The other side
     * keeps the kind, namespace and name, and keeps its cluster when it had
     * one other than this -- comparing staging with prod, then the next object
     * with prod again, should not need prod picked every time.
     */
    against(target: CompareTarget): void {
        this.left = { ...target };
        const cluster = this.right.contextId && this.right.contextId !== target.contextId ? this.right.contextId : '';
        this.right = { ...target, contextId: cluster };
        this.request++;
    }
}

export const compare = new CompareState();
