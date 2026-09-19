// The live side of a resource tab.
//
// A tab does not fetch its rows, it subscribes: the backend opens a watch
// against the cluster and pushes the whole current table whenever anything
// changes. One listener serves every tab -- Wails delivers each event to every
// registered handler, so routing by subscription ID here is cheaper than making
// each tab filter the traffic of all the others.

import { Events } from '@wailsio/runtime';
import { HelmService, ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { HELM_RELEASES } from '../catalogue';
import { adoptTable, type Table } from './adopt';

type Listener = (table: Table) => void;

const listeners = new Map<string, Listener>();

// A snapshot can arrive before Subscribe's promise has resolved: the backend
// starts pushing the moment the watch is open, which may be before the reply
// carrying its ID has crossed back. Holding the most recent snapshot per
// subscription means the first payload -- the one that clears the tab's loading
// state -- is never the one that gets dropped.
const pending = new Map<string, Table>();

Events.On('resource:snapshot', (event) => {
    const snapshot = event.data;
    if (!snapshot?.subscriptionId) return;

    const table = adoptTable(snapshot.table);
    const listener = listeners.get(snapshot.subscriptionId);
    if (listener) {
        listener(table);
    } else {
        pending.set(snapshot.subscriptionId, table);
    }
});

/** One tab's live view. Closing it stops the watch if no other tab shares it. */
export interface Subscription {
    /** Narrows the rows to these namespaces; none means every namespace. */
    setNamespaces(namespaces: string[]): void;
    close(): void;
}

/**
 * Opens a live view of one resource kind. Rows arrive through onTable, starting
 * with the cluster's current contents once the watch has synced; a cluster that
 * cannot be reached reports through onError instead. namespaces narrows the
 * rows, and an empty list is the whole cluster.
 */
export function subscribe(
    contextId: string,
    kind: string,
    namespaces: string[],
    onTable: Listener,
    onError: (message: string) => void,
): Subscription {
    // Helm releases go through their own service. A release is not a kind: the
    // backend watches the Secrets holding them and re-reads on each change,
    // because the payload it would otherwise cache is the half that carries
    // credentials. Everything after this line is the same for both -- the
    // snapshots arrive on one event and are routed by ID -- so a view still
    // just asks for rows and gets them.
    const helm = kind === HELM_RELEASES;
    const open = helm
        ? HelmService.Subscribe(contextId, namespaces)
        : ResourceService.Subscribe(contextId, kind, namespaces);
    const retarget = helm ? HelmService.SetNamespaces : ResourceService.SetNamespaces;
    const release = helm ? HelmService.Unsubscribe : ResourceService.Unsubscribe;

    let id: string | null = null;
    let closed = false;
    // Where the namespace filter has been moved to while we were still waiting
    // for the subscription to open, so an impatient click is not lost.
    let wanted = namespaces;
    let moved = false;

    open
        .then((subscriptionId) => {
            if (closed) {
                // A snapshot may already have been buffered under this ID by
                // the time we learn the tab has gone.
                pending.delete(subscriptionId);
                void release(subscriptionId);
                return;
            }
            id = subscriptionId;
            listeners.set(subscriptionId, onTable);

            const buffered = pending.get(subscriptionId);
            if (buffered) {
                pending.delete(subscriptionId);
                onTable(buffered);
            }
            if (moved) {
                void retarget(subscriptionId, wanted);
            }
        })
        .catch((err: unknown) => {
            if (!closed) onError(err instanceof Error ? err.message : String(err));
        });

    return {
        setNamespaces(next: string[]): void {
            wanted = next;
            moved = true;
            if (id) void retarget(id, next);
        },
        close(): void {
            closed = true;
            if (id === null) return;
            listeners.delete(id);
            pending.delete(id);
            void release(id);
            id = null;
        },
    };
}
