import { beforeEach, describe, expect, test, vi } from 'vitest';

const ObjectState = vi.fn();
const Delete = vi.fn();
const DeleteMany = vi.fn();
const PatchMany = vi.fn();
const PreviewPatch = vi.fn();
const Scale = vi.fn();
const Restart = vi.fn();
const Cordon = vi.fn();
const Drain = vi.fn();
const CancelDrain = vi.fn();
const Suspend = vi.fn();
const PauseRollout = vi.fn();
const TriggerCronJob = vi.fn();
const Evict = vi.fn();
const AnswerCSR = vi.fn();
const RolloutHistory = vi.fn();
const RollbackWorkload = vi.fn();

// The drain event handler is captured as it registers, so a test can deliver a
// report exactly as Wails would.
let deliver: (event: { data: unknown }) => void = () => {};
vi.mock('@wailsio/runtime', () => ({
    Events: {
        On: vi.fn((_name: string, handler: (event: { data: unknown }) => void) => {
            deliver = handler;
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
    LogService: {
        Containers: vi.fn().mockResolvedValue([]),
        Open: vi.fn().mockResolvedValue('logs-1'),
        Close: vi.fn(),
    },
    ActionService: {
        ObjectState,
        Delete,
        DeleteMany,
        PatchMany,
        PreviewPatch,
        Scale,
        Restart,
        Cordon,
        Drain,
        CancelDrain,
        Suspend,
        PauseRollout,
        TriggerCronJob,
        Evict,
        AnswerCSR,
        RolloutHistory,
        RollbackWorkload,
    },
}));

const { actions, DRAIN_DEFAULTS } = await import('./actions.svelte');
const { changes } = await import('./changes.svelte');

const NODE = { contextId: 'cfg::prod', kind: 'nodes', namespace: '', name: 'wrkr01' };
const DEPLOYMENT = { contextId: 'cfg::prod', kind: 'deployments', namespace: 'default', name: 'web' };

// `vm` is false for every kind that is not a KubeVirt machine, which is what
// keeps a Deployment from growing Start and Stop buttons.
const NOT_A_MACHINE = { isMachine: false, running: false, paused: false, migratable: false, status: '' };
const IDLE = {
    scalable: false,
    replicas: 0,
    cordoned: false,
    containers: [],
    suspended: false,
    paused: false,
    pending: false,
    vm: NOT_A_MACHINE,
};

/** One drain report, as the backend sends it. */
function report(over: Record<string, unknown> = {}) {
    return {
        data: {
            drainId: 'drain-1',
            node: 'wrkr01',
            phase: 'evicting',
            evicted: 0,
            total: 4,
            refused: [],
            error: '',
            done: false,
            ...over,
        },
    };
}

beforeEach(() => {
    actions.forget(NODE);
    actions.forget(DEPLOYMENT);
    ObjectState.mockReset().mockResolvedValue(IDLE);
    Delete.mockReset().mockResolvedValue(undefined);
    DeleteMany.mockReset().mockResolvedValue({ done: 0, failures: [] });
    PatchMany.mockReset().mockResolvedValue({ done: 0, failures: [] });
    PreviewPatch.mockReset().mockResolvedValue({ json: '', empty: true, error: '', line: 0 });
    Scale.mockReset().mockResolvedValue(undefined);
    Restart.mockReset().mockResolvedValue(undefined);
    Cordon.mockReset().mockResolvedValue(undefined);
    Drain.mockReset().mockResolvedValue('drain-1');
    CancelDrain.mockReset();
});

describe('what the bar knows about an object', () => {
    test('an object it has not read yet is neither scalable nor cordoned', () => {
        expect(actions.stateOf(NODE)).toEqual(IDLE);
    });

    test('loading reads the object and holds what it said', async () => {
        ObjectState.mockResolvedValue({ scalable: true, replicas: 3, cordoned: false, containers: [] });

        await actions.load(DEPLOYMENT);

        expect(ObjectState).toHaveBeenCalledWith('cfg::prod', 'deployments', 'default', 'web');
        expect(actions.stateOf(DEPLOYMENT).replicas).toBe(3);
    });

    // A cluster that will not answer must not stop the bar being drawn: Edit
    // and Delete need nothing from it.
    test('a read that fails leaves the defaults rather than throwing', async () => {
        ObjectState.mockRejectedValue(new Error('forbidden'));

        await expect(actions.load(NODE)).resolves.toBeUndefined();
        expect(actions.stateOf(NODE)).toEqual(IDLE);
    });
});

describe('the one-shot actions', () => {
    test('scale sends the replica count', async () => {
        await actions.scale(DEPLOYMENT, 5);

        expect(Scale).toHaveBeenCalledWith('cfg::prod', 'deployments', 'default', 'web', 5);
    });

    test('cordon closes a node, and uncordon reopens it', async () => {
        await actions.cordon(NODE, true);
        expect(Cordon).toHaveBeenCalledWith('cfg::prod', 'wrkr01', true);

        await actions.cordon(NODE, false);
        expect(Cordon).toHaveBeenCalledWith('cfg::prod', 'wrkr01', false);
    });

    // Whatever else is on screen showing this object is now a moment out of
    // date, which is exactly what the change signal is for.
    test('a successful action says the object changed', async () => {
        const before = changes.revision(DEPLOYMENT);

        await actions.restart(DEPLOYMENT);

        expect(changes.revision(DEPLOYMENT)).toBe(before + 1);
    });

    test('a refused action says nothing changed, and carries the reason', async () => {
        Restart.mockRejectedValue(new Error('deployments.apps is forbidden'));
        const before = changes.revision(DEPLOYMENT);

        await expect(actions.restart(DEPLOYMENT)).rejects.toThrow('forbidden');

        expect(changes.revision(DEPLOYMENT)).toBe(before);
    });

    // Delete is the exception: the object is gone, so there is nothing to
    // re-read and the panel closes instead.
    test('deleting does not ask anything to re-read a deleted object', async () => {
        const before = changes.revision(DEPLOYMENT);

        await actions.remove(DEPLOYMENT);

        expect(Delete).toHaveBeenCalledWith('cfg::prod', 'deployments', 'default', 'web');
        expect(changes.revision(DEPLOYMENT)).toBe(before);
    });
});

describe('the kind-specific actions', () => {
    const CRONJOB = { contextId: 'cfg::prod', kind: 'cronjobs', namespace: 'ops', name: 'backup' };
    const POD = { contextId: 'cfg::prod', kind: 'pods', namespace: 'default', name: 'web-1' };

    beforeEach(() => {
        for (const mock of [Suspend, PauseRollout, Evict, AnswerCSR, RollbackWorkload]) {
            mock.mockReset().mockResolvedValue(undefined);
        }
        TriggerCronJob.mockReset().mockResolvedValue('backup-manual-1');
        RolloutHistory.mockReset().mockResolvedValue([]);
        ObjectState.mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] });
    });

    test('suspend and pause send the flag they are given, and say the object changed', async () => {
        const before = changes.revision(CRONJOB);

        await actions.suspend(CRONJOB, true);
        await actions.pause(DEPLOYMENT, false);

        expect(Suspend).toHaveBeenCalledWith('cfg::prod', 'cronjobs', 'ops', 'backup', true);
        expect(PauseRollout).toHaveBeenCalledWith('cfg::prod', 'default', 'web', false);
        expect(changes.revision(CRONJOB)).toBe(before + 1);
    });

    test('run now answers with the job it started', async () => {
        await expect(actions.trigger(CRONJOB)).resolves.toBe('backup-manual-1');
        expect(TriggerCronJob).toHaveBeenCalledWith('cfg::prod', 'ops', 'backup');
    });

    // Like a delete: the pod is on its way out.
    test('an eviction does not ask anything to re-read the pod', async () => {
        const before = changes.revision(POD);

        await actions.evict(POD);

        expect(Evict).toHaveBeenCalledWith('cfg::prod', 'default', 'web-1');
        expect(changes.revision(POD)).toBe(before);
    });

    test('a refused eviction carries the reason', async () => {
        Evict.mockRejectedValue(new Error('Cannot evict pod as it would violate the pod’s disruption budget.'));

        await expect(actions.evict(POD)).rejects.toThrow('disruption budget');
    });

    // Go sends null for a revision with no images, and for no revisions.
    test('a history with nothing in it reads as empty lists', async () => {
        RolloutHistory.mockResolvedValueOnce(null);
        await expect(actions.history(DEPLOYMENT)).resolves.toEqual([]);

        RolloutHistory.mockResolvedValueOnce([{ revision: 2, images: null, changeCause: '', age: '1h', current: true }]);
        const got = await actions.history(DEPLOYMENT);
        expect(got[0].images).toEqual([]);
    });

    test('rollback sends the revision chosen', async () => {
        await actions.undo(DEPLOYMENT, 4);

        expect(RollbackWorkload).toHaveBeenCalledWith('cfg::prod', 'deployments', 'default', 'web', 4);
    });

    test('an answer to a signing request says which', async () => {
        const csr = { contextId: 'cfg::prod', kind: 'certificatesigningrequests', namespace: '', name: 'csr-1' };

        await actions.answer(csr, false);

        expect(AnswerCSR).toHaveBeenCalledWith('cfg::prod', 'csr-1', false);
    });
});

describe('draining a node', () => {
    test('nothing is draining until one is started', () => {
        expect(actions.drainOf(NODE)).toBeNull();
    });

    test('starting one shows it as under way straight away', async () => {
        await actions.drain(NODE);

        expect(Drain).toHaveBeenCalledWith('cfg::prod', 'wrkr01', DRAIN_DEFAULTS);
        expect(actions.drainOf(NODE)?.done).toBe(false);
    });

    test('the options chosen travel with it', async () => {
        const options = { ...DRAIN_DEFAULTS, force: true, gracePeriodSeconds: 30, podSelector: 'app=web' };

        await actions.drain(NODE, options);

        expect(Drain).toHaveBeenCalledWith('cfg::prod', 'wrkr01', options);
    });

    test('progress reports land on the node they belong to', async () => {
        await actions.drain(NODE);

        deliver(report({ evicted: 3, total: 4 }));

        expect(actions.drainOf(NODE)).toMatchObject({ evicted: 3, total: 4, phase: 'evicting' });
    });

    // Two nodes can drain at once. A report is routed by the ID the backend
    // gave, not by the node name, so nothing has to trust the payload's prose.
    test('a report for another drain is ignored', async () => {
        await actions.drain(NODE);

        deliver(report({ drainId: 'drain-9', evicted: 99 }));

        expect(actions.drainOf(NODE)?.evicted).toBe(0);
    });

    test('the pods it would not move are kept, with their reasons', async () => {
        await actions.drain(NODE);

        deliver(
            report({
                refused: [{ pod: { namespace: 'default', name: 'debug' }, reason: 'nothing manages it' }],
            }),
        );

        expect(actions.drainOf(NODE)?.refused).toHaveLength(1);
        expect(actions.drainOf(NODE)?.refused[0].reason).toContain('nothing manages it');
    });

    test('a finished drain is remembered as finished', async () => {
        await actions.drain(NODE);

        deliver(report({ phase: 'done', evicted: 4, done: true }));

        expect(actions.drainOf(NODE)?.done).toBe(true);
    });

    test('a failed drain keeps the reason it failed', async () => {
        await actions.drain(NODE);

        deliver(report({ phase: 'failed', error: 'nodes is forbidden', done: true }));

        expect(actions.drainOf(NODE)?.error).toBe('nodes is forbidden');
    });

    test('cancelling calls it off by the id the backend gave', async () => {
        await actions.drain(NODE);

        actions.cancelDrain(NODE);

        expect(CancelDrain).toHaveBeenCalledWith('drain-1');
    });

    test('cancelling a node that is not draining does nothing', () => {
        actions.cancelDrain(NODE);

        expect(CancelDrain).not.toHaveBeenCalled();
    });

    // A drain that would not start at all -- an unreachable cluster, no
    // permission -- has to say so rather than leave a bar spinning forever.
    test('a drain that will not start reports rather than hanging', async () => {
        Drain.mockRejectedValue(new Error('nodes is forbidden'));

        await expect(actions.drain(NODE)).rejects.toThrow('forbidden');
        expect(actions.drainOf(NODE)).toBeNull();
    });
});

// Deleting several at once: one call carrying every row, and the backend's
// report handed back as it is, so the table can say what went and what did not.
describe('deleting a selection', () => {
    const rows = [
        { namespace: 'default', name: 'web-1' },
        { namespace: 'default', name: 'web-2' },
    ];

    test('sends every row in one call and hands the report back', async () => {
        DeleteMany.mockResolvedValue({
            done: 1,
            failures: [{ namespace: 'default', name: 'web-2', error: 'forbidden' }],
        });

        const report = await actions.removeMany('cfg::prod', 'pods', rows);

        expect(DeleteMany).toHaveBeenCalledTimes(1);
        expect(DeleteMany).toHaveBeenCalledWith('cfg::prod', 'pods', rows);
        expect(report).toEqual({
            done: 1,
            failures: [{ namespace: 'default', name: 'web-2', error: 'forbidden' }],
        });
    });

    // Go sends null for an empty slice; nothing downstream should have to know.
    test('a report with no failures reads as an empty list', async () => {
        DeleteMany.mockResolvedValue({ done: 2, failures: null });

        const report = await actions.removeMany('cfg::prod', 'pods', rows);

        expect(report).toEqual({ done: 2, failures: [] });
    });

    test('a refused call is raised in the backend\'s words', async () => {
        DeleteMany.mockRejectedValue('unknown context "cfg::gone"');

        await expect(actions.removeMany('cfg::gone', 'pods', rows)).rejects.toThrow('unknown context');
    });
});

// Patching a selection: the patch travels as the JSON text the form showed,
// and every object that took it is said to have changed -- the refused ones
// are not, since nothing about them moved.
describe('patching a selection', () => {
    const rows = [
        { namespace: 'default', name: 'web-1' },
        { namespace: 'default', name: 'web-2' },
    ];
    const patch = JSON.stringify({ metadata: { labels: { team: 'web' } } });
    const one = { contextId: 'cfg::prod', kind: 'pods', namespace: 'default', name: 'web-1' };
    const two = { contextId: 'cfg::prod', kind: 'pods', namespace: 'default', name: 'web-2' };

    test('sends the patch text as it is, once, for every row', async () => {
        PatchMany.mockResolvedValue({ done: 2, failures: null });

        const report = await actions.patchMany('cfg::prod', 'pods', rows, patch);

        expect(PatchMany).toHaveBeenCalledTimes(1);
        expect(PatchMany).toHaveBeenCalledWith('cfg::prod', 'pods', rows, patch);
        expect(report).toEqual({ done: 2, failures: [] });
    });

    test("a preview is the backend's reading of the text, handed back as it came", async () => {
        PreviewPatch.mockResolvedValue({ json: '{"a":1}', empty: false, error: '', line: 0 });

        const preview = await actions.previewPatch('a: 1');

        expect(PreviewPatch).toHaveBeenCalledWith('a: 1');
        expect(preview).toEqual({ json: '{"a":1}', empty: false, error: '', line: 0 });
    });

    test('says the objects that took it changed, and not the refused ones', async () => {
        PatchMany.mockResolvedValue({
            done: 1,
            failures: [{ namespace: 'default', name: 'web-2', error: 'forbidden' }],
        });
        const before = [changes.revision(one), changes.revision(two)];

        await actions.patchMany('cfg::prod', 'pods', rows, patch);

        expect(changes.revision(one)).toBe(before[0] + 1);
        expect(changes.revision(two)).toBe(before[1]);
    });
});
