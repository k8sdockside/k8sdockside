import { describe, expect, test } from 'vitest';
import { credentialAlerts, healthAlerts, inDays, troubleOf } from './alerts';
import { adoptPodTrouble, type ClusterHealth, type Credentials } from '../state/adopt';

function health(overrides: Partial<ClusterHealth> = {}, pods: Partial<ClusterHealth['pods']> = {}): ClusterHealth {
    return {
        contextId: 'prod',
        nodesReady: 3,
        nodesTotal: 3,
        notReadyNodes: [],
        podsRunning: 40,
        podsTotal: 40,
        pods: { ...adoptPodTrouble(null), ...pods },
        warnings: 0,
        warningReasons: [],
        error: '',
        ...overrides,
    };
}

function issue(name: string, trouble: string) {
    return { namespace: 'apps', name, trouble, reason: trouble, restarts: 0, message: '', lastTermination: '', lastRestart: '' };
}

describe('what got worse', () => {
    // A window opened on fifteen clusters must not open with fifteen
    // notifications about what was already true yesterday.
    test('the first reading of a cluster raises nothing', () => {
        expect(healthAlerts(null, health({ nodesReady: 1, notReadyNodes: ['a', 'b'] }, { evicted: 30 }))).toEqual([]);
    });

    test('a node going not ready is named', () => {
        const alerts = healthAlerts(health(), health({ nodesReady: 2, notReadyNodes: ['worker-2'] }));
        expect(alerts).toHaveLength(1);
        expect(alerts[0]).toMatchObject({ tone: 'error', title: 'Node worker-2 is not ready' });
        expect(alerts[0]!.body).toContain('2 of 3 nodes ready');
    });

    // The notification names three; the details list every one, each
    // openable, with why.
    test('the details list every object, each with a way to open it', () => {
        const before = health({}, { crashLooping: 0 });
        const pods = ['a', 'b', 'c', 'd'].map((n) => ({ ...issue(`api-${n}`, 'crashloop'), reason: 'CrashLoopBackOff', lastTermination: 'Error (exit 1)' }));
        const after = health({}, { crashLooping: 4, worst: pods });
        const [alert] = healthAlerts(before, after);
        expect(alert!.body).toBe('api-a, api-b, api-c and 1 more');
        expect(alert!.items).toHaveLength(4);
        expect(alert!.items![3]).toEqual({
            label: 'api-d',
            detail: 'apps · CrashLoopBackOff · Error (exit 1)',
            ref: { kind: 'pods', namespace: 'apps', name: 'api-d' },
        });
    });

    test('a node going down lists the node, and opens Nodes', () => {
        const [alert] = healthAlerts(health(), health({ nodesReady: 2, notReadyNodes: ['worker-2'] }));
        expect(alert!.items).toEqual([{ label: 'worker-2', detail: 'Not ready', ref: { kind: 'nodes', namespace: '', name: 'worker-2' } }]);
        expect(alert!.action).toMatchObject({ kind: 'list', list: 'nodes' });
    });

    test('a node that was already down is not news again', () => {
        const down = health({ nodesReady: 2, notReadyNodes: ['worker-2'] });
        expect(healthAlerts(down, down)).toEqual([]);
    });

    test('a pod that stops starting is named, and a pod already in trouble is not', () => {
        const before = health({}, { crashLooping: 1, worst: [issue('old-api', 'crashloop')] });
        const after = health({}, { crashLooping: 2, worst: [issue('old-api', 'crashloop'), issue('new-api', 'crashloop')] });
        const alerts = healthAlerts(before, after);
        expect(alerts).toHaveLength(1);
        expect(alerts[0]).toMatchObject({ key: 'crashlooping', tone: 'error', title: '2 pods not starting', body: 'new-api' });
    });

    test('more evictions are a warning that says how many are left', () => {
        const alerts = healthAlerts(health({}, { evicted: 3 }), health({}, { evicted: 5 }));
        expect(alerts).toHaveLength(1);
        expect(alerts[0]).toMatchObject({ key: 'evicted', tone: 'warn', title: '2 pods evicted', body: '5 evicted pods left on the cluster.' });
        expect(alerts[0]!.action).toEqual({ kind: 'list', label: 'Show the evicted pods', list: 'pods', query: 'Evicted' });
    });

    test('fewer evicted pods -- a clean-up -- is not news', () => {
        expect(healthAlerts(health({}, { evicted: 5 }), health({}, { evicted: 0 }))).toEqual([]);
    });

    test('a cluster that stops answering says so, and nothing else', () => {
        const alerts = healthAlerts(health(), health({ error: 'connection refused' }));
        expect(alerts).toHaveLength(1);
        expect(alerts[0]).toMatchObject({ key: 'unreachable', tone: 'error', title: 'Cannot reach the cluster', body: 'connection refused' });
    });

    test('a cluster that was already unreachable is not news again', () => {
        const gone = health({ error: 'connection refused' });
        expect(healthAlerts(gone, gone)).toEqual([]);
    });
});

describe('credentials', () => {
    function creds(daysLeft: number, notAfter = '2026-10-01T00:00:00Z'): Credentials {
        return {
            contextId: 'prod',
            items: [{ kind: 'Client certificate', subject: 'admin', notAfter, daysLeft, note: '', error: '' }],
            error: '',
        };
    }

    test('a certificate far from expiry says nothing', () => {
        expect(credentialAlerts(creds(200))).toEqual([]);
    });

    test('a month out is a warning, a week out urgent, and past it expired', () => {
        expect(credentialAlerts(creds(20))[0]).toMatchObject({ tone: 'warn', title: 'Client certificate expires in 20 days' });
        expect(credentialAlerts(creds(3))[0]).toMatchObject({ tone: 'error', title: 'Client certificate expires in 3 days' });
        expect(credentialAlerts(creds(-1))[0]).toMatchObject({ tone: 'error', title: 'Client certificate has expired' });
    });

    // Each step towards expiry is said once, which is what the key is for.
    test('each step has its own key', () => {
        const keys = [creds(20), creds(3), creds(-1)].map((c) => credentialAlerts(c)[0]!.key);
        expect(new Set(keys).size).toBe(3);
    });

    test('a credential with no date says nothing', () => {
        expect(credentialAlerts(creds(0, ''))).toEqual([]);
    });
});

describe('the mark in the sidebar', () => {
    test('a healthy cluster gets none', () => {
        expect(troubleOf(health(), null)).toBeNull();
    });

    test('nodes down or pods not starting are an error, evictions a warning', () => {
        expect(troubleOf(health({}, { evicted: 4 }), null)).toEqual({ tone: 'warn', count: 4, summary: '4 evicted pods' });
        expect(troubleOf(health({ nodesReady: 2 }, { evicted: 4 }), null)).toMatchObject({ tone: 'error', count: 5 });
    });

    test('an unreachable cluster gets no count -- the connection mark already says so', () => {
        expect(troubleOf(health({ error: 'refused' }, { evicted: 4 }), null)).toBeNull();
    });

    test('a certificate running out counts', () => {
        const creds: Credentials = {
            contextId: 'prod',
            items: [{ kind: 'Client certificate', subject: '', notAfter: '2026-10-01T00:00:00Z', daysLeft: 5, note: '', error: '' }],
            error: '',
        };
        expect(troubleOf(health(), creds)).toEqual({
            tone: 'error',
            count: 1,
            summary: 'Client certificate expires in 5 days',
        });
    });
});

test('days read as words', () => {
    expect([inDays(-3), inDays(-1), inDays(0), inDays(1), inDays(9)]).toEqual([
        '3 days ago',
        'yesterday',
        'today',
        'tomorrow',
        'in 9 days',
    ]);
});
