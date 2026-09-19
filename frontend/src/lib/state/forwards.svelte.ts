// The port forwards this app is holding open.
//
// One list for the whole window rather than one per view: a forward belongs to
// the app, not to the tab it was started from. It goes on carrying traffic
// after that tab is closed, after the context is deselected, and -- as a row
// waiting to be reconnected -- after the app itself is restarted.
//
// The backend owns the truth. Everything here either asks it a question or
// listens to what it says: a forward's state changes when a listener comes up,
// when a pod behind it goes away, and when a connection drops hours later, and
// none of those are things this side could work out for itself.
//
// There are none in the web version. A forward is a port on localhost, and
// there localhost is the server: a tunnel opened there would be reachable by
// everything on that machine and by nobody in the browser. The backend refuses
// them, and this list stays empty rather than asking.

import { Events } from '@wailsio/runtime';
import { PortForwardService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';
import type * as main from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/models.js';
import { session } from './session.svelte';

/** One forward, live or waiting to be reconnected. */
export type Forward = main.Forward;

/** A port that could be forwarded, as offered by the picker. */
export type PortOption = kube.PortOption;

/** What a forward is opened against. */
export interface ForwardTarget {
    contextId: string;
    kind: string;
    namespace: string;
    name: string;
}

function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

class Forwards {
    /** Every forward, in the order they were made. */
    list = $state<Forward[]>([]);
    /** True once the list has been read from the backend at least once. */
    loaded = $state(false);

    constructor() {
        Events.On('portforward:changed', (event: { data: Forward }) => {
            const record = event.data;
            if (!record?.id) return;

            const at = this.list.findIndex((f) => f.id === record.id);
            if (at === -1) {
                this.list = [...this.list, record];
            } else {
                // Replaced whole rather than patched: the backend sends the
                // record it has, and merging fields would be this side deciding
                // which of two versions of the truth to believe.
                this.list = this.list.map((f) => (f.id === record.id ? record : f));
            }
        });
    }

    /** Reads the list, including the forwards remembered from last session. */
    async load(): Promise<void> {
        await session.load();
        if (session.server) {
            this.loaded = true;
            return;
        }
        try {
            this.list = (await PortForwardService.List()) ?? [];
        } finally {
            this.loaded = true;
        }
    }

    /** The forwards belonging to one cluster. */
    forContext(contextId: string): Forward[] {
        return this.list.filter((f) => f.contextId === contextId);
    }

    /** How many of a cluster's forwards are carrying traffic right now. */
    activeIn(contextId: string): number {
        return this.list.filter((f) => f.contextId === contextId && f.state === 'active').length;
    }

    /** Whether one object already has a forward on a given port. */
    on(target: ForwardTarget, remotePort: number): Forward | null {
        return (
            this.list.find(
                (f) =>
                    f.contextId === target.contextId &&
                    f.kind === target.kind &&
                    f.namespace === target.namespace &&
                    f.name === target.name &&
                    f.remotePort === remotePort,
            ) ?? null
        );
    }

    /** What could be forwarded from an object, for the picker. */
    async ports(target: ForwardTarget): Promise<PortOption[]> {
        return (
            (await PortForwardService.Ports(
                target.contextId,
                target.kind,
                target.namespace,
                target.name,
            )) ?? []
        );
    }

    /**
     * Opens a forward and answers with it.
     *
     * A local port of 0 means "any free one". Waiting for the answer is the
     * point: which port it got is what the link beside it will use, and a
     * browser cannot be opened on a port that has not been decided.
     */
    async start(
        target: ForwardTarget,
        remotePort: number,
        localPort: number,
        browser: boolean,
    ): Promise<Forward> {
        const record = await PortForwardService.Start(
            target.contextId,
            target.kind,
            target.namespace,
            target.name,
            remotePort,
            localPort,
            browser,
        );
        this.adopt(record);
        if (browser) {
            // Failing to open a browser must not read as a failed forward: the
            // tunnel is up either way, and the row says so with a link.
            try {
                await PortForwardService.Open(record.id);
            } catch (err) {
                throw new Error(`The forward is open, but the browser would not start: ${message(err)}`);
            }
        }
        return record;
    }

    /** Opens a forward that is not currently up. */
    async reconnect(id: string): Promise<Forward> {
        const record = await PortForwardService.Reconnect(id);
        this.adopt(record);
        return record;
    }

    /** Closes a forward's tunnel, leaving the row where it is. */
    stop(id: string): void {
        void PortForwardService.Stop(id);
    }

    /** Closes a forward and drops it from the list for good. */
    async forget(id: string): Promise<void> {
        await PortForwardService.Forget(id);
        this.list = this.list.filter((f) => f.id !== id);
    }

    /** Sends a live forward to the browser. */
    async open(id: string): Promise<void> {
        await PortForwardService.Open(id);
    }

    /** Where a live forward can be reached, empty when it is not up. */
    url(forward: Forward): string {
        if (forward.state !== 'active' || !forward.localPort) return '';
        const secure = [443, 8443, 6443, 9443].includes(forward.remotePort);
        return `${secure ? 'https' : 'http'}://localhost:${forward.localPort}`;
    }

    /** Files a record the backend answered with, in case its event beat it. */
    private adopt(record: Forward): void {
        if (!record?.id) return;
        this.list = this.list.some((f) => f.id === record.id)
            ? this.list.map((f) => (f.id === record.id ? record : f))
            : [...this.list, record];
    }
}

export const forwards = new Forwards();
