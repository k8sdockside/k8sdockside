import { beforeEach, describe, expect, test, vi } from 'vitest';

const List = vi.fn();
const Ports = vi.fn();
const Start = vi.fn();
const Reconnect = vi.fn();
const Stop = vi.fn();
const Forget = vi.fn();
const Open = vi.fn();

const events = vi.hoisted(() => ({ handler: (_e: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', () => ({
    Events: {
        On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
            if (name === 'portforward:changed') events.handler = handler;
        }),
    },
}));
vi.mock('../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services', () => ({
    HelmService: {
        Releases: vi.fn().mockResolvedValue({ kind: 'helmreleases', columns: [], rows: [], namespaced: true, error: '' }),
        Detail: vi.fn().mockResolvedValue({
            name: '', namespace: '', revision: 1, status: 'deployed',
            chart: '', chartName: '', chartVersion: '', appVersion: '',
            description: '', firstDeployed: '', updated: '', notes: '',
            values: '', userValues: '', resources: [], revisions: [],
        }),
        Tool: vi.fn().mockResolvedValue({ found: true, path: '/usr/bin/helm', version: 'v3.16.2', configured: false, reason: '' }),
        Upgrade: vi.fn().mockResolvedValue(''),
        Rollback: vi.fn().mockResolvedValue(''),
        Uninstall: vi.fn().mockResolvedValue(''),
        ChartVersions: vi.fn().mockResolvedValue([]),
    },
    PortForwardService: { List, Ports, Start, Reconnect, Stop, Forget, Open },
}));

const { forwards } = await import('./forwards.svelte');
const { session } = await import('./session.svelte');

const TARGET = { contextId: 'cfg::prod', kind: 'services', namespace: 'web', name: 'api' };

/** One forward as the backend sends it. */
function record(over: Record<string, unknown> = {}) {
    return {
        id: 'pf-1',
        contextId: 'cfg::prod',
        kind: 'services',
        namespace: 'web',
        name: 'api',
        remotePort: 80,
        localPort: 51234,
        random: true,
        browser: false,
        state: 'active',
        error: '',
        pod: 'api-7f9',
        podPort: 8080,
        note: '',
        ...over,
    };
}

beforeEach(async () => {
    List.mockReset().mockResolvedValue([]);
    Ports.mockReset().mockResolvedValue([]);
    Start.mockReset().mockResolvedValue(record());
    Reconnect.mockReset().mockResolvedValue(record());
    Stop.mockReset();
    Forget.mockReset().mockResolvedValue(undefined);
    Open.mockReset().mockResolvedValue(undefined);
    await forwards.load();
});

describe('the list', () => {
    test('comes back from the backend, including what was remembered', async () => {
        List.mockResolvedValueOnce([record({ state: 'stopped', pod: '', podPort: 0 })]);
        await forwards.load();

        expect(forwards.list).toHaveLength(1);
        // Remembered forwards come back disconnected: nothing dials a cluster
        // at launch.
        expect(forwards.list[0].state).toBe('stopped');
    });

    test('a null list from a backend with nothing in it reads as empty', async () => {
        List.mockResolvedValueOnce(null as unknown as []);
        await forwards.load();

        expect(forwards.list).toEqual([]);
        expect(forwards.loaded).toBe(true);
    });

    test('is filtered by cluster, because a tunnel goes to one', async () => {
        List.mockResolvedValueOnce([record(), record({ id: 'pf-2', contextId: 'cfg::dev' })]);
        await forwards.load();

        expect(forwards.forContext('cfg::prod').map((f) => f.id)).toEqual(['pf-1']);
        expect(forwards.activeIn('cfg::prod')).toBe(1);
        expect(forwards.activeIn('cfg::staging')).toBe(0);
    });
});

describe('what the backend says', () => {
    test('a new forward appears', () => {
        events.handler({ data: record({ id: 'pf-9' }) });

        expect(forwards.list.map((f) => f.id)).toEqual(['pf-9']);
    });

    test('a change replaces the row whole rather than merging into it', () => {
        events.handler({ data: record() });
        events.handler({ data: record({ state: 'error', error: 'lost connection to pod', pod: '', podPort: 0 }) });

        expect(forwards.list).toHaveLength(1);
        expect(forwards.list[0].state).toBe('error');
        // The pod it had reached is cleared with it: a row still naming one
        // would claim a tunnel that is no longer there.
        expect(forwards.list[0].pod).toBe('');
    });

    test('an event with no forward in it is ignored', () => {
        events.handler({ data: { id: '' } });

        expect(forwards.list).toEqual([]);
    });
});

describe('starting one', () => {
    test('passes what was chosen and files the answer', async () => {
        const opened = await forwards.start(TARGET, 80, 0, false);

        expect(Start).toHaveBeenCalledWith('cfg::prod', 'services', 'web', 'api', 80, 0, false);
        expect(opened.localPort).toBe(51234);
        expect(forwards.list.map((f) => f.id)).toEqual(['pf-1']);
    });

    test('opens a browser only when that was asked for', async () => {
        await forwards.start(TARGET, 80, 0, false);
        expect(Open).not.toHaveBeenCalled();

        await forwards.start(TARGET, 80, 0, true);
        expect(Open).toHaveBeenCalledWith('pf-1');
    });

    test('a browser that will not start is not a failed forward', async () => {
        Open.mockRejectedValueOnce(new Error('no browser'));

        await expect(forwards.start(TARGET, 80, 0, true)).rejects.toThrow(/forward is open/);
        // The tunnel is up either way, and the row says so.
        expect(forwards.list[0].state).toBe('active');
    });

    test('an event that arrived first is not duplicated by the answer', async () => {
        events.handler({ data: record() });
        await forwards.start(TARGET, 80, 0, false);

        expect(forwards.list).toHaveLength(1);
    });
});

describe('stopping and forgetting', () => {
    test('stopping leaves the row where it is', async () => {
        await forwards.start(TARGET, 80, 0, false);
        forwards.stop('pf-1');

        expect(Stop).toHaveBeenCalledWith('pf-1');
        // The backend reports the new state; the row does not remove itself.
        expect(forwards.list).toHaveLength(1);
    });

    test('forgetting drops it', async () => {
        await forwards.start(TARGET, 80, 0, false);
        await forwards.forget('pf-1');

        expect(Forget).toHaveBeenCalledWith('pf-1');
        expect(forwards.list).toEqual([]);
    });
});

describe('where a forward can be reached', () => {
    test('only while it is up', () => {
        expect(forwards.url(record())).toBe('http://localhost:51234');
        expect(forwards.url(record({ state: 'stopped' }))).toBe('');
        expect(forwards.url(record({ localPort: 0 }))).toBe('');
    });

    // A link that opens on http where the far end speaks TLS produces a blank
    // tab and no explanation.
    test('https for the ports that conventionally mean it', () => {
        expect(forwards.url(record({ remotePort: 443 }))).toBe('https://localhost:51234');
        expect(forwards.url(record({ remotePort: 8443 }))).toBe('https://localhost:51234');
        expect(forwards.url(record({ remotePort: 8080 }))).toBe('http://localhost:51234');
    });
});

// localhost in the web version is the server: a tunnel there would reach
// nobody in the browser, so the list is not even asked for.
describe('in the web version', () => {
    test('the list is empty without asking the backend', async () => {
        const desktop = session.info;
        session.info = { ...desktop, server: true };
        try {
            List.mockClear();

            await forwards.load();

            expect(List).not.toHaveBeenCalled();
            expect(forwards.list).toEqual([]);
            expect(forwards.loaded).toBe(true);
        } finally {
            session.info = desktop;
        }
    });
});

describe('finding one that already exists', () => {
    test('matches the object and the port, not one or the other', async () => {
        List.mockResolvedValueOnce([record()]);
        await forwards.load();

        expect(forwards.on(TARGET, 80)?.id).toBe('pf-1');
        expect(forwards.on(TARGET, 8080)).toBeNull();
        expect(forwards.on({ ...TARGET, name: 'other' }, 80)).toBeNull();
    });
});
