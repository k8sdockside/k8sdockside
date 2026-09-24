import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

const Health = vi.hoisted(() => vi.fn());
const Credentials = vi.hoisted(() => vi.fn());

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services')>();
    return { ...actual, ResourceService: { ...actual.ResourceService, Health, Credentials } };
});

const { workspace } = await import('../state/workspace.svelte');
const { fleet } = await import('../state/fleet.svelte');
const { clusters } = await import('../state/health.svelte');
const { DASHBOARD } = await import('../catalogue');
const FleetView = (await import('./FleetView.svelte')).default;

const PROD = { id: 'p', name: 'admin@prod', cluster: 'p', user: 'admin', namespace: '', server: 'https://prod', file: '/c', current: false };
const EDGE = { ...PROD, id: 'e', name: 'admin@edge', cluster: 'e', server: 'https://edge' };
const IDLE = { ...PROD, id: 'i', name: 'admin@idle', cluster: 'i', server: 'https://idle' };

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

beforeEach(async () => {
    document.body.innerHTML = '';
    workspace.closeAllTabs();
    workspace.files = [{ path: '/c', source: 'manual', error: '', contexts: [PROD, EDGE, IDLE] }];
    for (const c of [PROD, EDGE, IDLE]) {
        fleet.forget(c.id);
        clusters.forget(c.id);
    }
    Credentials.mockResolvedValue({ contextId: '', items: [], error: '' });
    Health.mockImplementation(async (id: string) =>
        id === EDGE.id
            ? reading(id, {
                  nodesReady: 2,
                  notReadyNodes: ['edge-2'],
                  pods: { ...reading(id).pods, evicted: 12 },
                  warnings: 30,
                  warningReasons: [{ reason: 'BackOff', count: 30 }],
              })
            : reading(id),
    );
    await fleet.check(PROD.id);
    await fleet.check(EDGE.id);
});

// The point of the page: the cluster that is not fine is the one on top.
test('the clusters in trouble come first, with what is wrong', async () => {
    render(FleetView);

    const rows = page.getByRole('row');
    await expect.element(rows.nth(1)).toHaveTextContent('admin@edge');
    await expect.element(rows.nth(1)).toHaveTextContent('2/3');
    await expect.element(rows.nth(1)).toHaveTextContent('BackOff');
    await expect.element(rows.nth(2)).toHaveTextContent('admin@prod');
});

// A cluster nobody has connected is listed, and left alone until asked.
test('a cluster not connected is not read until somebody asks', async () => {
    render(FleetView);

    const idle = page.getByRole('row').filter({ hasText: 'admin@idle' });
    await expect.element(idle).toHaveTextContent('Not connected');
    expect(Health.mock.calls.some((c) => c[0] === IDLE.id)).toBe(false);

    await idle.getByRole('button', { name: 'Check' }).click();
    await vi.waitFor(() => expect(Health.mock.calls.some((c) => c[0] === IDLE.id)).toBe(true));
});

test('a row opens that cluster’s dashboard', async () => {
    render(FleetView);

    await page.getByRole('button', { name: 'admin@edge' }).click();

    expect(workspace.allTabs.some((t) => t.contextId === EDGE.id && t.kind === DASHBOARD)).toBe(true);
});
