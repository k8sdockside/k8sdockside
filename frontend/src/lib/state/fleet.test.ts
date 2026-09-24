import { beforeEach, expect, test, vi } from 'vitest';

// The fleet store reads clusters through two bindings and posts through a
// third; all three are stood in for, so these tests are about what it does
// with the answers.
const Health = vi.hoisted(() => vi.fn());
const Credentials = vi.hoisted(() => vi.fn());
const Send = vi.hoisted(() => vi.fn());

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    ResourceService: { Health, Credentials, Ping: vi.fn() },
}));
vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services/notifyservice.js', () => ({
    Send,
    Status: vi.fn().mockResolvedValue({ available: true, authorized: true, reason: '' }),
    RequestPermission: vi.fn().mockResolvedValue(true),
}));
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => () => {}) } }));

const { fleet } = await import('./fleet.svelte');
const { clusters } = await import('./health.svelte');

function reading(contextId: string, overrides: Record<string, unknown> = {}) {
    return {
        contextId,
        nodesReady: 3,
        nodesTotal: 3,
        notReadyNodes: [],
        podsRunning: 10,
        podsTotal: 10,
        pods: { evicted: 0, failed: 0, crashLooping: 0, restarting: 0, restarts: 0, restartedRecently: 0, worst: [] },
        warnings: 0,
        warningReasons: [],
        error: '',
        ...overrides,
    };
}

const NO_CREDENTIALS = { contextId: '', items: [], error: '' };

let stop: (() => void) | null = null;

function start(watched: string[], notify: boolean | 'off' = true, snoozedUntil = 0) {
    stop?.();
    stop = fleet.start({
        watched: () => watched,
        all: () => [...watched, 'idle'],
        nameOf: (id) => `name of ${id}`,
        alerts: () => ({ mode: notify === 'off' ? 'off' : notify ? 'system' : 'bell', snoozedUntil }),
        open: vi.fn(),
    });
}

beforeEach(() => {
    stop?.();
    stop = null;
    Health.mockReset();
    Credentials.mockReset().mockResolvedValue(NO_CREDENTIALS);
    Send.mockReset().mockResolvedValue(undefined);
    fleet.clear();
    for (const id of ['a', 'b', 'idle', 'gone', 'late']) {
        fleet.forget(id);
        clusters.forget(id);
    }
});

test('only the watched clusters are read', async () => {
    Health.mockImplementation(async (id: string) => reading(id));
    start(['a', 'b']);
    await vi.waitFor(() => expect(fleet.of('b').checkedAt).not.toBeNull());

    expect(Health.mock.calls.map((c) => c[0]).sort()).toEqual(['a', 'b']);
    expect(fleet.of('idle').checkedAt).toBeNull();
});

test('checking all reads the ones not connected too', async () => {
    Health.mockImplementation(async (id: string) => reading(id));
    start([]);
    await fleet.checkAll();
    expect(fleet.of('idle').health?.nodesTotal).toBe(3);
});

test('a reading settles the sidebar connection mark', async () => {
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    expect(clusters.of('a').status).toBe('connected');

    Health.mockRejectedValueOnce(new Error(''));
    await fleet.check('a');
    // An empty error from the binding is still a failure, not a healthy cluster.
    expect(clusters.of('a').status).toBe('error');
    expect(fleet.of('a').health?.error).not.toBe('');
});

test('what gets worse is raised on the bell and posted to the system', async () => {
    start([]);
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    expect(fleet.alerts).toEqual([]);

    Health.mockResolvedValueOnce(reading('a', { nodesReady: 2, notReadyNodes: ['worker-3'] }));
    await fleet.check('a');

    expect(fleet.alerts).toHaveLength(1);
    expect(fleet.alerts[0]).toMatchObject({ contextId: 'a', tone: 'error', title: 'Node worker-3 is not ready', read: false });
    expect(fleet.unread).toBe(1);
    await vi.waitFor(() => expect(Send).toHaveBeenCalled());
    expect(Send.mock.calls[0]!.slice(1, 3)).toEqual(['name of a', 'Node worker-3 is not ready']);

    fleet.markAllRead();
    expect(fleet.unread).toBe(0);
});

test('with notifications off the alert stays on the bell only', async () => {
    start([], false);
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    Health.mockResolvedValueOnce(reading('a', { pods: { ...reading('a').pods, evicted: 3 } }));
    await fleet.check('a');

    expect(fleet.alerts).toHaveLength(1);
    expect(Send).not.toHaveBeenCalled();
});

test('a certificate running out is said once, not on every reading', async () => {
    start([]);
    Health.mockResolvedValue(reading('a'));
    Credentials.mockResolvedValue({
        contextId: 'a',
        items: [{ kind: 'Client certificate', subject: 'admin', notAfter: '2026-10-01T00:00:00Z', daysLeft: 5, note: '', error: '' }],
        error: '',
    });

    await fleet.check('a', { credentials: true });
    await fleet.check('a', { credentials: true });

    expect(fleet.alerts.map((a) => a.title)).toEqual(['Client certificate expires in 5 days']);
    expect(fleet.trouble('a')).toMatchObject({ tone: 'error', count: 1 });
});

// Disconnecting a cluster while its reading is on the way must not bring it
// back when the reading lands.
test('a reading that lands after the cluster was let go of is dropped', async () => {
    let answer!: (value: unknown) => void;
    Health.mockReturnValueOnce(new Promise((resolve) => (answer = resolve)));
    const pending = fleet.check('late');

    fleet.forget('late');
    clusters.forget('late');
    answer(reading('late'));
    await pending;

    expect(fleet.of('late').checkedAt).toBeNull();
    expect(clusters.of('late').status).toBe('unknown');
});

// A clicked notification brings back which alert it was, so the bell can show
// that alert's details rather than only its cluster.
test('a notification carries its alert, and revealing it opens that alert', async () => {
    start([]);
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    Health.mockResolvedValueOnce(reading('a', { nodesReady: 2, notReadyNodes: ['worker-3'] }));
    await fleet.check('a');

    const alert = fleet.alerts[0]!;
    await vi.waitFor(() => expect(Send).toHaveBeenCalled());
    expect(Send.mock.calls[0]!.slice(4)).toEqual(['a', alert.id]);
    expect(alert.items).toEqual([{ label: 'worker-3', detail: 'Not ready', ref: { kind: 'nodes', namespace: '', name: 'worker-3' } }]);

    expect(fleet.reveal(alert.id)).toBe(true);
    expect(fleet.revealed?.id).toBe(alert.id);
    expect(fleet.alerts[0]!.read).toBe(true);
    expect(fleet.reveal('alert-that-was-cleared')).toBe(false);
});

// Off means off: the sidebar and the fleet view still say what is wrong, but
// nothing is raised.
test('with alerts off nothing is raised', async () => {
    start([], 'off');
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    Health.mockResolvedValueOnce(reading('a', { nodesReady: 2, notReadyNodes: ['worker-3'] }));
    await fleet.check('a');

    expect(fleet.alerts).toEqual([]);
    expect(Send).not.toHaveBeenCalled();
    // What is wrong is still known, for the sidebar's mark.
    expect(fleet.trouble('a')).toMatchObject({ tone: 'error' });
});

// Snoozed, nothing pops up and the bell stays quiet -- but nothing is lost.
test('snoozed, an alert is kept on the bell, read and unposted', async () => {
    start([], true, Date.now() + 60 * 60 * 1000);
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    Health.mockResolvedValueOnce(reading('a', { nodesReady: 2, notReadyNodes: ['worker-3'] }));
    await fleet.check('a');

    expect(fleet.alerts).toHaveLength(1);
    expect(fleet.alerts[0]!.read).toBe(true);
    expect(fleet.unread).toBe(0);
    await new Promise((r) => setTimeout(r, 20));
    expect(Send).not.toHaveBeenCalled();
});

test('a snooze that has run out is no snooze', async () => {
    start([], true, Date.now() - 1000);
    Health.mockResolvedValueOnce(reading('a'));
    await fleet.check('a');
    Health.mockResolvedValueOnce(reading('a', { nodesReady: 2, notReadyNodes: ['worker-3'] }));
    await fleet.check('a');

    expect(fleet.unread).toBe(1);
    await vi.waitFor(() => expect(Send).toHaveBeenCalled());
});
