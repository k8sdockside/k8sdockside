import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

const Compare = vi.hoisted(() => vi.fn());

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services')>();
    return { ...actual, ResourceService: { ...actual.ResourceService, Compare } };
});

const { workspace } = await import('../state/workspace.svelte');
const { compare } = await import('../state/compare.svelte');
const CompareView = (await import('./CompareView.svelte')).default;

const PROD = { id: 'p', name: 'admin@prod', cluster: 'p', user: 'admin', namespace: '', server: 'https://prod', file: '/c', current: false };
const STAGING = { ...PROD, id: 's', name: 'admin@staging', cluster: 's', server: 'https://staging' };

const LINES = [
    ...Array.from({ length: 12 }, (_, i) => ({ op: ' ', text: `line ${i + 1}`, left: i + 1, right: i + 1 })),
    { op: '-', text: '  replicas: 3', left: 13, right: 0 },
    { op: '+', text: '  replicas: 5', left: 0, right: 13 },
    { op: ' ', text: 'tail', left: 14, right: 14 },
];

beforeEach(() => {
    document.body.innerHTML = '';
    workspace.files = [{ path: '/c', source: 'manual', error: '', contexts: [PROD, STAGING] }];
    Compare.mockReset().mockResolvedValue({
        left: 'a', right: 'b', leftError: '', rightError: '', lines: LINES, same: false, changes: 2,
    });
});

// Opened from an object with the other cluster already known, it compares at
// once rather than waiting for a button.
test('opened for an object with both clusters known, it compares straight away', async () => {
    compare.right = { contextId: STAGING.id, kind: '', namespace: '', name: '' };
    compare.against({ contextId: PROD.id, kind: 'deployments', namespace: 'apps', name: 'web' });
    render(CompareView);

    await expect.element(page.getByText('2 lines differ')).toBeVisible();
    expect(Compare).toHaveBeenCalledWith(
        { contextId: PROD.id, kind: 'deployments', namespace: 'apps', name: 'web' },
        { contextId: STAGING.id, kind: 'deployments', namespace: 'apps', name: 'web' },
    );
});

// A dozen identical lines around one change is noise; they fold, and open on
// a click.
test('long runs of unchanged lines fold, and unfold when asked', async () => {
    compare.right = { contextId: STAGING.id, kind: '', namespace: '', name: '' };
    compare.against({ contextId: PROD.id, kind: 'deployments', namespace: 'apps', name: 'web' });
    render(CompareView);

    await expect.element(page.getByText('replicas: 5')).toBeVisible();
    const fold = page.getByRole('button', { name: /9 unchanged lines/ });
    await expect.element(fold).toBeVisible();
    expect(page.getByText('line 1', { exact: true }).elements()).toHaveLength(0);

    await fold.click();
    await expect.element(page.getByText('line 1', { exact: true })).toBeVisible();
});

test('a side that could not be read says so', async () => {
    Compare.mockResolvedValue({
        left: 'a', right: '', leftError: '', rightError: 'deployments.apps "web" not found', lines: [], same: false, changes: 1,
    });
    compare.right = { contextId: STAGING.id, kind: '', namespace: '', name: '' };
    compare.against({ contextId: PROD.id, kind: 'deployments', namespace: 'apps', name: 'web' });
    render(CompareView);

    await expect.element(page.getByText('deployments.apps "web" not found')).toBeVisible();
});
