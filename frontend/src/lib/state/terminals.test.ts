import { beforeEach, describe, expect, test, vi } from 'vitest';

const Containers = vi.fn();
const Open = vi.fn();
const OpenNode = vi.fn();
const Send = vi.fn();
const Resize = vi.fn();
const Close = vi.fn();
const OpenDebug = vi.fn();
const DeleteDebugPod = vi.fn();
const OpenLocal = vi.fn();
const OpenTool = vi.fn();
const OpenToolInteractive = vi.fn();

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
    TerminalService: {
        Containers,
        Open,
        OpenNode,
        Send,
        Resize,
        Close,
        OpenDebug,
        DeleteDebugPod,
        OpenLocal,
        OpenTool,
        OpenToolInteractive,
    },
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

describe('debug sessions', () => {
    const DEBUG_TAB = 'shell:cfg::prod#debug##worker-01';
    const DEBUG = { contextId: 'cfg::prod', kind: 'debug', namespace: '', name: 'worker-01' };

    beforeEach(() => {
        terminals.forget(DEBUG_TAB);
        Containers.mockReset();
        OpenDebug.mockReset().mockResolvedValue({
            id: 'term-9',
            namespace: 'debugging',
            pod: 'k8sdockside-debug-worker-01-abcde',
            container: 'debugger-abcde',
            node: 'worker-01',
            created: 'k8sdockside-debug-worker-01-abcde',
            kept: true,
        });
        DeleteDebugPod.mockReset().mockResolvedValue(undefined);
    });

    test('a primed debug tab opens what it was asked for, with no container list', async () => {
        const { blankDebugRequest } = await import('./debug.svelte');
        const request = blankDebugRequest('node', '', 'worker-01');
        await terminals.primeDebug(DEBUG_TAB, DEBUG, request);
        await terminals.open(DEBUG_TAB, DEBUG);

        expect(OpenDebug).toHaveBeenCalledWith('cfg::prod', request);
        expect(Containers).not.toHaveBeenCalled();
        const doc = terminals.doc(DEBUG_TAB);
        expect(doc.status).toBe('running');
        expect(doc.created).toBe('k8sdockside-debug-worker-01-abcde');
        expect(doc.kept).toBe(true);
        expect(doc.namespace).toBe('debugging');
    });

    test('asking again while the tab is open runs the new request', async () => {
        const { blankDebugRequest } = await import('./debug.svelte');
        await terminals.primeDebug(DEBUG_TAB, DEBUG, blankDebugRequest('node', '', 'worker-01'));
        await terminals.open(DEBUG_TAB, DEBUG);
        const second = { ...blankDebugRequest('node', '', 'worker-01'), image: 'alpine' };
        await terminals.primeDebug(DEBUG_TAB, DEBUG, second);

        expect(OpenDebug).toHaveBeenCalledTimes(2);
        expect(OpenDebug).toHaveBeenLastCalledWith('cfg::prod', second);
    });

    test('a debug tab with no request does not guess', async () => {
        await terminals.open(DEBUG_TAB, DEBUG);

        expect(OpenDebug).not.toHaveBeenCalled();
        expect(terminals.doc(DEBUG_TAB).status).toBe('error');
    });

    test('a kept pod is deleted from the namespace the session reported', async () => {
        const { blankDebugRequest } = await import('./debug.svelte');
        await terminals.primeDebug(DEBUG_TAB, DEBUG, blankDebugRequest('node', '', 'worker-01'));
        await terminals.open(DEBUG_TAB, DEBUG);
        await terminals.removeDebugPod(DEBUG_TAB, 'cfg::prod');

        expect(DeleteDebugPod).toHaveBeenCalledWith('cfg::prod', 'debugging', 'k8sdockside-debug-worker-01-abcde');
        expect(terminals.doc(DEBUG_TAB).created).toBe('');
    });
});

describe('local terminals', () => {
    const LOCAL_TAB = 'shell:cfg::prod#local#abc#prod';
    const LOCAL = { contextId: 'cfg::prod', kind: 'local', namespace: 'abc', name: 'prod' };
    const PLAIN_TAB = 'shell:#local#def#Terminal';
    const PLAIN = { contextId: '', kind: 'local', namespace: 'def', name: 'Terminal' };

    beforeEach(() => {
        terminals.forget(LOCAL_TAB);
        terminals.forget(PLAIN_TAB);
        Containers.mockReset();
        OpenLocal.mockReset().mockResolvedValue({ id: 'term-20', namespace: '', pod: '/bin/zsh', container: '', node: '' });
    });

    test('a cluster tab opens a local shell scoped to that context, with no container list', async () => {
        await terminals.open(LOCAL_TAB, LOCAL);

        expect(OpenLocal).toHaveBeenCalledWith('cfg::prod');
        expect(Containers).not.toHaveBeenCalled();
        const doc = terminals.doc(LOCAL_TAB);
        expect(doc.status).toBe('running');
        expect(doc.pod).toBe('/bin/zsh');
        expect(doc.exitCode).toBeNull();
    });

    test('a plain one is tied to no cluster', async () => {
        await terminals.open(PLAIN_TAB, PLAIN);
        expect(OpenLocal).toHaveBeenCalledWith('');
    });

    test('a shell that exits by itself says its status, and is not an error', async () => {
        await terminals.open(LOCAL_TAB, LOCAL);
        events.handler({ data: { sessionId: 'term-20', data: '', error: '', done: true, exited: true, exitCode: 130 } });

        const doc = terminals.doc(LOCAL_TAB);
        expect(doc.status).toBe('ended');
        expect(doc.exitCode).toBe(130);
    });

    test('restarting opens a fresh shell and forgets the old status', async () => {
        await terminals.open(LOCAL_TAB, LOCAL);
        events.handler({ data: { sessionId: 'term-20', data: '', error: '', done: true, exited: true, exitCode: 0 } });
        OpenLocal.mockResolvedValueOnce({ id: 'term-21', namespace: '', pod: '/bin/zsh', container: '', node: '' });

        await terminals.restart(LOCAL_TAB, LOCAL);

        expect(OpenLocal).toHaveBeenCalledTimes(2);
        const doc = terminals.doc(LOCAL_TAB);
        expect(doc.status).toBe('running');
        expect(doc.sessionId).toBe('term-21');
        expect(doc.exitCode).toBeNull();
    });

    test('a shell that cannot be found is reported in the tab', async () => {
        OpenLocal.mockRejectedValueOnce(
            new Error('The shell "/no/shell" was not found (tried: /no/shell). Choose another shell in Settings → Terminal → Local shell.'),
        );
        await terminals.open(LOCAL_TAB, LOCAL);

        const doc = terminals.doc(LOCAL_TAB);
        expect(doc.status).toBe('error');
        expect(doc.error).toMatch(/not found/);
        expect(doc.error).toMatch(/Settings/);
    });

    test('closing the tab closes the session', async () => {
        await terminals.open(LOCAL_TAB, LOCAL);
        terminals.forget(LOCAL_TAB);
        expect(Close).toHaveBeenCalledWith('term-20');
    });
});

describe('tool consoles', () => {
    const TOOL_TAB = 'shell:cfg::prod#tool#talos/talosctl#talosctl';
    const TOOL = { contextId: 'cfg::prod', kind: 'tool', namespace: 'talos/talosctl', name: 'talosctl' };

    beforeEach(() => {
        terminals.forget(TOOL_TAB);
        OpenTool.mockReset().mockResolvedValue({ id: 'term-30', namespace: '', pod: '', container: '', node: '' });
        OpenToolInteractive.mockReset().mockResolvedValue({ id: 'term-31', namespace: '', pod: '', container: '', node: '' });
    });

    test('an interactive command runs in the dock through its own allow-list, once', async () => {
        await terminals.primeTool(TOOL_TAB, TOOL, [], ['dashboard'], true);
        await terminals.open(TOOL_TAB, TOOL);

        expect(OpenToolInteractive).toHaveBeenCalledWith('cfg::prod', 'talos', 'talosctl', ['dashboard']);
        expect(OpenTool).not.toHaveBeenCalled();

        // Reconnecting is a plain console: the dashboard is not run twice.
        await terminals.restart(TOOL_TAB, TOOL);
        expect(OpenToolInteractive).toHaveBeenCalledTimes(1);
        expect(OpenTool).toHaveBeenCalledWith('cfg::prod', 'talos', 'talosctl', [], []);
    });

    test('a run command still goes through OpenTool', async () => {
        await terminals.primeTool(TOOL_TAB, TOOL, ['--nodes', '10.0.0.1'], ['reboot']);
        await terminals.open(TOOL_TAB, TOOL);

        expect(OpenTool).toHaveBeenCalledWith('cfg::prod', 'talos', 'talosctl', ['--nodes', '10.0.0.1'], ['reboot']);
        expect(OpenToolInteractive).not.toHaveBeenCalled();
    });
});
