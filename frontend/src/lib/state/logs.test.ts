import { beforeEach, describe, expect, test, vi } from 'vitest';

const Containers = vi.fn();
const Open = vi.fn();
const Close = vi.fn();

const events = vi.hoisted(() => ({ handler: (_e: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', () => ({
    Events: {
        On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
            if (name === 'pod:logs') events.handler = handler;
        }),
    },
}));
vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
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
    LogService: { Containers, Open, Close },
}));

const { logs, KEEP } = await import('./logs.svelte');

const TAB = 'logs:cfg::prod#pods#default#web';
const TARGET = { contextId: 'cfg::prod', kind: 'pods', namespace: 'default', name: 'web' };

const CONTAINERS = [
    { pod: 'web', container: 'app', init: false },
    { pod: 'web', container: 'sidecar', init: false },
];

/** Delivers a batch as the backend sends it. */
function deliver(streamId: string, texts: string[], over: Record<string, unknown> = {}) {
    events.handler({
        data: {
            streamId,
            lines: texts.map((text) => ({ pod: 'web', container: 'app', text })),
            error: '',
            done: false,
            ...over,
        },
    });
}

beforeEach(() => {
    logs.forget(TAB);
    Containers.mockReset().mockResolvedValue(CONTAINERS);
    Open.mockReset().mockResolvedValue('logs-1');
    Close.mockReset();
});

describe('opening a view', () => {
    test('an unopened tab reads as empty rather than as null', () => {
        const doc = logs.doc(TAB);
        expect(doc.lines).toEqual([]);
        expect(doc.status).toBe('idle');
    });

    test('opening finds the containers and follows all of them', async () => {
        await logs.open(TAB, TARGET);

        expect(Containers).toHaveBeenCalledWith('cfg::prod', 'pods', 'default', 'web');
        expect(logs.doc(TAB).containers).toHaveLength(2);
        // Everything, until you say otherwise -- which is what you want when
        // you do not yet know which container is the interesting one.
        expect(logs.doc(TAB).selected).toEqual(['app', 'sidecar']);
        expect(Open).toHaveBeenCalledWith('cfg::prod', 'pods', 'default', 'web', [], true);
    });

    test('opening an already-open tab does not start a second stream', async () => {
        await logs.open(TAB, TARGET);
        await logs.open(TAB, TARGET);

        expect(Open).toHaveBeenCalledTimes(1);
    });

    test('a view that will not open says why', async () => {
        Containers.mockRejectedValue(new Error('pods "web" not found'));

        await logs.open(TAB, TARGET);

        expect(logs.doc(TAB).status).toBe('error');
        expect(logs.doc(TAB).error).toContain('not found');
    });
});

describe('lines arriving', () => {
    test('they are kept in the order they came', async () => {
        await logs.open(TAB, TARGET);

        deliver('logs-1', ['one', 'two']);
        deliver('logs-1', ['three']);

        expect(logs.doc(TAB).lines.map((l) => l.text)).toEqual(['one', 'two', 'three']);
    });

    // Two log views can be open at once, and a batch is routed by the ID the
    // backend gave rather than by anything in the payload's prose.
    test('a batch for another view is ignored', async () => {
        await logs.open(TAB, TARGET);

        deliver('logs-9', ['not mine']);

        expect(logs.doc(TAB).lines).toHaveLength(0);
    });

    // A container logging in a loop would otherwise grow this without limit
    // until the window died.
    test('the buffer is capped, keeping the newest', async () => {
        await logs.open(TAB, TARGET);

        deliver('logs-1', Array.from({ length: KEEP + 50 }, (_, i) => `line ${i}`));

        const doc = logs.doc(TAB);
        expect(doc.lines).toHaveLength(KEEP);
        expect(doc.lines.at(-1)?.text).toBe(`line ${KEEP + 49}`);
        // And it says so, rather than quietly appearing to start at line 50.
        expect(doc.truncated).toBe(true);
    });

    test('a stream that ends is marked ended', async () => {
        await logs.open(TAB, TARGET);

        deliver('logs-1', [], { done: true });

        expect(logs.doc(TAB).status).toBe('ended');
    });

    test('a stream that breaks keeps the reason', async () => {
        await logs.open(TAB, TARGET);

        deliver('logs-1', [], { done: true, error: 'pods "web" is forbidden' });

        expect(logs.doc(TAB).status).toBe('error');
        expect(logs.doc(TAB).error).toContain('forbidden');
    });
});

describe('choosing containers', () => {
    test('choosing some restarts the stream on just those', async () => {
        await logs.open(TAB, TARGET);
        deliver('logs-1', ['from both']);

        await logs.choose(TAB, TARGET, ['app']);

        expect(Close).toHaveBeenCalledWith('logs-1');
        expect(Open).toHaveBeenLastCalledWith('cfg::prod', 'pods', 'default', 'web', ['app'], true);
        expect(logs.doc(TAB).selected).toEqual(['app']);
    });

    // What is on screen came from containers you have just stopped following,
    // so keeping it would leave a view whose contents no longer match its
    // heading.
    test('the lines from before are cleared', async () => {
        await logs.open(TAB, TARGET);
        deliver('logs-1', ['from both']);

        await logs.choose(TAB, TARGET, ['app']);

        expect(logs.doc(TAB).lines).toHaveLength(0);
    });

    test('lines from the old stream no longer land', async () => {
        await logs.open(TAB, TARGET);
        Open.mockResolvedValue('logs-2');

        await logs.choose(TAB, TARGET, ['app']);
        deliver('logs-1', ['late from the old stream']);

        expect(logs.doc(TAB).lines).toHaveLength(0);
    });

    test('choosing none follows all of them again', async () => {
        await logs.open(TAB, TARGET);

        await logs.choose(TAB, TARGET, []);

        expect(Open).toHaveBeenLastCalledWith('cfg::prod', 'pods', 'default', 'web', [], true);
    });
});

describe('following', () => {
    test('turning it off stops the stream and re-reads what is there', async () => {
        await logs.open(TAB, TARGET);

        await logs.setFollow(TAB, TARGET, false);

        expect(Close).toHaveBeenCalledWith('logs-1');
        expect(Open).toHaveBeenLastCalledWith('cfg::prod', 'pods', 'default', 'web', [], false);
        expect(logs.doc(TAB).follow).toBe(false);
    });

    test('turning it back on follows again', async () => {
        await logs.open(TAB, TARGET);
        await logs.setFollow(TAB, TARGET, false);

        await logs.setFollow(TAB, TARGET, true);

        expect(Open).toHaveBeenLastCalledWith('cfg::prod', 'pods', 'default', 'web', [], true);
        expect(logs.doc(TAB).follow).toBe(true);
    });
});

test('clearing empties the view without stopping the stream', async () => {
    await logs.open(TAB, TARGET);
    deliver('logs-1', ['one', 'two']);

    logs.clear(TAB);

    expect(logs.doc(TAB).lines).toHaveLength(0);
    expect(logs.doc(TAB).truncated).toBe(false);
    expect(Close).not.toHaveBeenCalled();
});

// Closing the dock tab is the one thing that means "I am done with this",
// and a stream nobody is reading is a connection held open for nothing.
test('forgetting a tab closes its stream', async () => {
    await logs.open(TAB, TARGET);

    logs.forget(TAB);

    expect(Close).toHaveBeenCalledWith('logs-1');
    expect(logs.doc(TAB).status).toBe('idle');
});
