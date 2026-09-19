import { beforeEach, describe, expect, test, vi } from 'vitest';

const Containers = vi.fn();
const Open = vi.fn();
const OpenNode = vi.fn();
const Send = vi.fn();
const Resize = vi.fn();
const Close = vi.fn();

const events = vi.hoisted(() => ({ handler: (_e: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', () => ({
    Events: {
        On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
            if (name === 'terminal:data') events.handler = handler;
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
    TerminalService: { Containers, Open, OpenNode, Send, Resize, Close },
}));

// xterm draws to a canvas, which jsdom does not have. Nothing here attaches a
// terminal to an element, so the instance is never built -- but the modules are
// still imported, and standing them in keeps that honest.
const written = vi.hoisted(() => ({ chunks: [] as string[] }));
vi.mock('@xterm/xterm', () => ({
    Terminal: class {
        cols = 80;
        rows = 24;
        options = {};
        write(data: Uint8Array | string) {
            written.chunks.push(typeof data === 'string' ? data : new TextDecoder().decode(data));
        }
        reset() {}
        focus() {}
        dispose() {}
        onData() {}
        onResize() {}
        loadAddon() {}
    },
}));
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit() {} } }));

const { terminals } = await import('./terminals.svelte');

const TAB = 'shell:cfg::prod#pods#default#web';
const TARGET = { contextId: 'cfg::prod', kind: 'pods', namespace: 'default', name: 'web' };
const NODE_TAB = 'shell:cfg::prod#nodes##worker-01';
const NODE = { contextId: 'cfg::prod', kind: 'nodes', namespace: '', name: 'worker-01' };

const CONTAINERS = [
    { pod: 'web', container: 'app', init: false },
    { pod: 'web', container: 'sidecar', init: false },
];

const SESSION = { id: 'term-1', namespace: 'default', pod: 'web', container: 'app', node: '' };

beforeEach(() => {
    terminals.forget(TAB);
    terminals.forget(NODE_TAB);
    written.chunks = [];
    Containers.mockReset().mockResolvedValue(CONTAINERS);
    Open.mockReset().mockResolvedValue(SESSION);
    OpenNode.mockReset().mockResolvedValue({ ...SESSION, id: 'term-2', pod: '', container: '', node: 'worker-01' });
    Send.mockReset();
    Resize.mockReset();
    Close.mockReset();
});

describe('opening a shell', () => {
    test('an unopened tab reads as empty rather than as null', () => {
        const doc = terminals.doc(TAB);
        expect(doc.status).toBe('idle');
        expect(doc.containers).toEqual([]);
    });

    test('finds the containers and attaches to the default one', async () => {
        await terminals.open(TAB, TARGET);

        expect(Containers).toHaveBeenCalledWith('cfg::prod', 'pods', 'default', 'web');
        // Empty pod and container: the backend resolves both, because for a
        // workload neither is anything the user named.
        expect(Open).toHaveBeenCalledWith('cfg::prod', 'pods', 'default', 'web', '', '');

        const doc = terminals.doc(TAB);
        expect(doc.status).toBe('running');
        expect(doc.containers).toHaveLength(2);
        expect(doc.pod).toBe('web');
        expect(doc.container).toBe('app');
    });

    test('a session already open is left exactly as it was', async () => {
        await terminals.open(TAB, TARGET);
        await terminals.open(TAB, TARGET);

        // Switching dock tabs and back must not restart the shell: everything
        // it knows is in the session.
        expect(Open).toHaveBeenCalledTimes(1);
    });

    test('a node opens a node shell and asks for no container list', async () => {
        await terminals.open(NODE_TAB, NODE);

        expect(Containers).not.toHaveBeenCalled();
        expect(OpenNode).toHaveBeenCalledWith('cfg::prod', 'worker-01');
        expect(terminals.doc(NODE_TAB).node).toBe('worker-01');
    });

    test('a picker that could not be filled still opens a shell', async () => {
        Containers.mockRejectedValueOnce(new Error('forbidden'));
        await terminals.open(TAB, TARGET);

        expect(Open).toHaveBeenCalled();
        expect(terminals.doc(TAB).status).toBe('running');
    });

    test('a refused exec is reported rather than left spinning', async () => {
        Open.mockRejectedValueOnce(new Error('cannot create resource "pods/exec"'));
        await terminals.open(TAB, TARGET);

        const doc = terminals.doc(TAB);
        expect(doc.status).toBe('error');
        expect(doc.error).toMatch(/pods\/exec/);
    });
});

describe('what the backend sends', () => {
    test('a session that ends is said to have ended', async () => {
        await terminals.open(TAB, TARGET);
        events.handler({ data: { sessionId: 'term-1', data: '', error: '', done: true } });

        expect(terminals.doc(TAB).status).toBe('ended');
    });

    test('a session that broke says why', async () => {
        await terminals.open(TAB, TARGET);
        events.handler({
            data: { sessionId: 'term-1', data: '', error: 'container is not running', done: true },
        });

        const doc = terminals.doc(TAB);
        expect(doc.status).toBe('error');
        expect(doc.error).toBe('container is not running');
    });

    test('output for a session nobody is holding is dropped', async () => {
        await terminals.open(TAB, TARGET);
        events.handler({ data: { sessionId: 'term-99', data: btoa('hello'), error: '', done: false } });

        expect(terminals.doc(TAB).status).toBe('running');
    });
});

describe('changing container', () => {
    test('closes the session it was on and opens another', async () => {
        await terminals.open(TAB, TARGET);
        Open.mockResolvedValueOnce({ ...SESSION, id: 'term-3', container: 'sidecar' });

        await terminals.choose(TAB, TARGET, 'web', 'sidecar');

        expect(Close).toHaveBeenCalledWith('term-1');
        expect(Open).toHaveBeenLastCalledWith('cfg::prod', 'pods', 'default', 'web', 'web', 'sidecar');
        expect(terminals.doc(TAB).container).toBe('sidecar');
    });

    test('choosing the container it is already on changes nothing', async () => {
        await terminals.open(TAB, TARGET);
        await terminals.choose(TAB, TARGET, 'web', 'app');

        expect(Open).toHaveBeenCalledTimes(1);
        expect(Close).not.toHaveBeenCalled();
    });
});

describe('reconnecting and closing', () => {
    test('a reconnect goes back to the same container', async () => {
        await terminals.open(TAB, TARGET);
        events.handler({ data: { sessionId: 'term-1', data: '', error: '', done: true } });

        await terminals.restart(TAB, TARGET);

        expect(Open).toHaveBeenLastCalledWith('cfg::prod', 'pods', 'default', 'web', 'web', 'app');
        expect(terminals.doc(TAB).status).toBe('running');
    });

    test('forgetting a tab closes its session', async () => {
        await terminals.open(TAB, TARGET);
        terminals.forget(TAB);

        expect(Close).toHaveBeenCalledWith('term-1');
        expect(terminals.doc(TAB).status).toBe('idle');
    });
});
