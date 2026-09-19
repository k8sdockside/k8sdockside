import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { notices } from '../state/notices.svelte';
import { detail } from '../state/detail.svelte';

// The table's rows arrive through a subscription. Stubbing that is what lets a
// test say "the cluster holds this" without a cluster.
const pushed = vi.hoisted(() => ({ send: (_table: unknown) => {} }));
vi.mock('../state/subscriptions', () => ({
    subscribe: vi.fn((_c: string, _k: string, _n: string, onTable: (t: unknown) => void) => {
        pushed.send = onTable;
        return { setNamespaces: vi.fn(), close: vi.fn() };
    }),
}));

// The real backend answers every settings write with the whole settings file,
// and the store adopts that answer whole. A mock answering `{}` says instead
// that every other section is empty, so a debounced write landing a quarter of
// a second into a test undoes whatever it had just set -- the dock folds itself
// back up, a preference goes back to its default -- which is a race the test
// loses about half the time. So the settings mock keeps what it is given and
// hands all of it back, the way the file does.
const settingsFile = vi.hoisted(() => {
    const saved: Record<string, unknown> = {};
    return {
        keep: (section: string) =>
            vi.fn((value: unknown) => {
                saved[section] = value;
                return Promise.resolve({ ...saved });
            }),
        keepPrefsFor: () =>
            vi.fn((contextId: string, prefs: unknown) => {
                saved.contexts = { ...(saved.contexts as object), [contextId]: prefs };
                return Promise.resolve({ ...saved });
            }),
    };
});
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
    KubeconfigService: { Sync: vi.fn().mockResolvedValue([]), Files: vi.fn().mockResolvedValue([]) },
    ResourceService: {
        Namespaces: vi.fn().mockResolvedValue(['default']),
        Describe: vi.fn().mockResolvedValue(''),
    },
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }),
        DeleteMany: vi.fn().mockResolvedValue({ done: 0, failures: [] }),
        PatchMany: vi.fn().mockResolvedValue({ done: 0, failures: [] }),
        PreviewPatch: vi.fn().mockResolvedValue({ json: '', empty: true, error: '', line: 0 }),
    },
    LogService: {
        Containers: vi.fn().mockResolvedValue([]),
        Open: vi.fn().mockResolvedValue('logs-1'),
        Close: vi.fn(),
    },
    MetricsService: {
        Source: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
        SetEndpoint: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
        Rediscover: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
        Charts: vi.fn().mockResolvedValue({ source: { endpoint: {}, available: false, error: '', configured: '' }, charts: [], range: 60 }),
        Attachments: vi.fn().mockResolvedValue([]),
    },
    PluginService: {
        List: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
        Reload: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
        Summary: vi.fn().mockResolvedValue({ pluginId: '', installed: false, checked: true, requirements: [], cards: [], error: '' }),
    },
    ThemeService: {
        List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }),
        Tokens: vi.fn().mockResolvedValue([]),
    },
    TerminalService: {
        Containers: vi.fn().mockResolvedValue([]),
        Open: vi.fn().mockResolvedValue({ id: 'term-1', namespace: 'default', pod: 'web', container: 'app', node: '' }),
        OpenNode: vi.fn().mockResolvedValue({ id: 'term-1', namespace: 'default', pod: '', container: '', node: 'wrkr01' }),
        Send: vi.fn(),
        Resize: vi.fn(),
        Close: vi.fn(),
        Externals: vi.fn().mockResolvedValue({ terminals: [], kubectl: '', reason: '' }),
        Launch: vi.fn().mockResolvedValue(undefined),
        LaunchNode: vi.fn().mockResolvedValue(undefined),
    },
    PortForwardService: {
        List: vi.fn().mockResolvedValue([]),
        Ports: vi.fn().mockResolvedValue([]),
        Start: vi.fn().mockResolvedValue({ id: 'pf-1', localPort: 51234, state: 'active' }),
        Reconnect: vi.fn().mockResolvedValue({ id: 'pf-1', localPort: 51234, state: 'active' }),
        Stop: vi.fn(),
        Forget: vi.fn().mockResolvedValue(undefined),
        Open: vi.fn().mockResolvedValue(undefined),
        URL: vi.fn().mockResolvedValue(''),
    },
    SettingsService: {
        Get: vi.fn().mockResolvedValue({}),
        ConfigPath: vi.fn().mockResolvedValue(''),
        SetContextPrefs: settingsFile.keepPrefsFor(),
        SetPanes: settingsFile.keep('panes'),
        SetLayout: settingsFile.keep('layout'),
        SetPreferences: settingsFile.keep('preferences'),
    },
}));

const ResourceTable = (await import('./ResourceTable.svelte')).default;
const { workspace } = await import('../state/workspace.svelte');
const { views } = await import('../state/views');
const { ActionService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

// Deleting several rows at once. The rows are ticked in the table, a bar
// counts them, and one call carries the whole selection to the backend, which
// answers with what went and what was refused.

const PROD = '/home/u/.kube/prod::admin@prod';

const plain = (text: string) => ({ text, tone: '', sort: '', pills: null });

function podRow(name: string) {
    return { id: `pods/default/${name}`, name, namespace: 'default', cells: [plain(name), plain('Running')] };
}

/** Three pods -- two web, one db -- so a filter can tell them apart. */
function pods() {
    return {
        kind: 'pods',
        columns: ['Name', 'Status'],
        namespaced: true,
        error: '',
        rows: [podRow('web-1'), podRow('web-2'), podRow('db-1')],
    };
}

beforeEach(() => {
    detail.close();
    notices.dismiss();
    // A table remembers its filter for the tab's lifetime, and these tables
    // are never closed through the workspace: one test's search must not
    // hide the rows the next one clicks.
    views.forgetAll();
    vi.mocked(ActionService.DeleteMany).mockReset().mockResolvedValue({ done: 2, failures: [] });
});

/** Renders a pods table and waits for its rows. */
async function shown(): Promise<void> {
    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(pods());
    await expect.element(page.getByRole('checkbox', { name: 'Select web-1' })).toBeVisible();
}

test('ticking rows raises a bar that counts them', async () => {
    await shown();

    await page.getByRole('checkbox', { name: 'Select web-1' }).click();
    await expect.element(page.getByText('1 selected')).toBeVisible();

    await page.getByRole('checkbox', { name: 'Select db-1' }).click();
    await expect.element(page.getByText('2 selected')).toBeVisible();
});

test('the header checkbox takes every row the filter shows, and no more', async () => {
    await shown();

    await page.getByPlaceholder('Filter pods').fill('web');
    await page.getByRole('checkbox', { name: 'Select all' }).click();

    await expect.element(page.getByText('2 selected')).toBeVisible();
});

test('shift-click sweeps from the last ticked row to this one', async () => {
    await shown();

    await page.getByRole('checkbox', { name: 'Select web-1' }).click();
    await page.getByRole('checkbox', { name: 'Select db-1' }).click({ modifiers: ['Shift'] });

    await expect.element(page.getByText('3 selected')).toBeVisible();
});

test('deleting asks first, naming the count, then sends the whole selection in one call', async () => {
    await shown();
    await page.getByRole('checkbox', { name: 'Select web-1' }).click();
    await page.getByRole('checkbox', { name: 'Select web-2' }).click();

    await page.getByRole('button', { name: 'Delete' }).click();
    await expect.element(page.getByText(/Delete 2 pods\?/)).toBeVisible();
    expect(ActionService.DeleteMany).not.toHaveBeenCalled();

    await page.getByRole('button', { name: 'Delete' }).click();

    await expect.poll(() => vi.mocked(ActionService.DeleteMany).mock.calls.length).toBe(1);
    expect(ActionService.DeleteMany).toHaveBeenCalledWith(PROD, 'pods', [
        { namespace: 'default', name: 'web-1' },
        { namespace: 'default', name: 'web-2' },
    ]);
    await expect.poll(() => page.getByText(/selected/).elements().length).toBe(0);
    expect(notices.current?.text).toBe('2 pods deleted');
});

test('cancelling deletes nothing and keeps the selection', async () => {
    await shown();
    await page.getByRole('checkbox', { name: 'Select web-1' }).click();
    await page.getByRole('button', { name: 'Delete' }).click();
    await expect.element(page.getByText(/Delete 1 pod\?/)).toBeVisible();

    await page.getByRole('button', { name: 'Cancel' }).click();

    expect(ActionService.DeleteMany).not.toHaveBeenCalled();
    await expect.element(page.getByText('1 selected')).toBeVisible();
});

test('a refusal keeps that row ticked and says why', async () => {
    vi.mocked(ActionService.DeleteMany).mockResolvedValue({
        done: 1,
        failures: [{ namespace: 'default', name: 'web-2', error: 'pods "web-2" is forbidden' }],
    });
    await shown();
    await page.getByRole('checkbox', { name: 'Select web-1' }).click();
    await page.getByRole('checkbox', { name: 'Select web-2' }).click();
    await page.getByRole('button', { name: 'Delete' }).click();
    await expect.element(page.getByText(/Delete 2 pods\?/)).toBeVisible();

    await page.getByRole('button', { name: 'Delete' }).click();

    await expect.element(page.getByText('1 selected')).toBeVisible();
    await expect.element(page.getByRole('checkbox', { name: 'Select web-2' })).toBeChecked();
    await expect.element(page.getByRole('checkbox', { name: 'Select web-1' })).not.toBeChecked();
    expect(notices.current?.tone).toBe('error');
    expect(notices.current?.text).toContain('web-2');
});

test('a listing whose rows cannot be deleted grows no checkboxes', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'helmreleases' });
    pushed.send({
        kind: 'helmreleases',
        columns: ['Name'],
        namespaced: true,
        error: '',
        rows: [{ id: 'helmreleases/default/web', name: 'web', namespace: 'default', cells: [plain('web')] }],
    });

    await expect.element(page.getByRole('cell', { name: 'web' })).toBeVisible();
    expect(page.getByRole('checkbox').elements()).toHaveLength(0);
});
