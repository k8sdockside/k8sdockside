import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import PortForwards from './PortForwards.svelte';

const events = vi.hoisted(() => ({ handler: (_e: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return {
        ...actual,
        Events: {
            On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
                if (name === 'portforward:changed') events.handler = handler;
            }),
        },
    };
});

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
    ResourceService: { Describe: vi.fn().mockResolvedValue('') },
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }),
    },
    LogService: { Containers: vi.fn(), Open: vi.fn(), Close: vi.fn() },
    MetricsService: {
        Source: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
        Charts: vi.fn().mockResolvedValue({ source: {}, charts: [], range: 60 }),
        Attachments: vi.fn().mockResolvedValue([]),
    },
    PluginService: {
        List: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
        Summary: vi.fn().mockResolvedValue({ pluginId: '', installed: false, checked: true, requirements: [], cards: [], error: '' }),
    },
    ThemeService: { List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }), Tokens: vi.fn().mockResolvedValue([]) },
    TerminalService: {
        Containers: vi.fn().mockResolvedValue([]),
        Open: vi.fn(),
        OpenNode: vi.fn(),
        Send: vi.fn(),
        Resize: vi.fn(),
        Close: vi.fn(),
        Externals: vi.fn().mockResolvedValue({ terminals: [], kubectl: '', reason: '' }),
        Launch: vi.fn(),
        LaunchNode: vi.fn(),
    },
    PortForwardService: {
        List: vi.fn().mockResolvedValue([]),
        Ports: vi.fn().mockResolvedValue([]),
        Start: vi.fn(),
        Reconnect: vi.fn(),
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

const { forwards } = await import('../state/forwards.svelte');
const { PortForwardService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

const PROD = '/home/u/.kube/prod::admin@prod';

function forward(over: Record<string, unknown> = {}) {
    return {
        id: 'pf-1',
        contextId: PROD,
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

/** Puts a list in front of the view, the way the backend would. */
async function showing(list: ReturnType<typeof forward>[]) {
    vi.mocked(PortForwardService.List).mockResolvedValue(list);
    await forwards.load();
}

beforeEach(async () => {
    vi.mocked(PortForwardService.Stop).mockReset();
    vi.mocked(PortForwardService.Reconnect).mockReset().mockResolvedValue(forward());
    vi.mocked(PortForwardService.Forget).mockReset().mockResolvedValue(undefined);
    vi.mocked(PortForwardService.Open).mockReset().mockResolvedValue(undefined);
    await showing([]);
});

test('a cluster with no forwards says where one is started', async () => {
    render(PortForwards, { contextId: PROD });

    await expect.element(page.getByText(/Nothing is being forwarded/)).toBeVisible();
});

test('a live forward shows where it listens and what it reached', async () => {
    await showing([forward()]);
    render(PortForwards, { contextId: PROD });

    await expect.element(page.getByRole('button', { name: 'localhost:51234' })).toBeVisible();
    await expect.element(page.getByText('Connected').first()).toBeVisible();
    // For a service, the pod behind it is neither of the things the user named,
    // so the row says which one it is.
    await expect.element(page.getByText('via api-7f9:8080')).toBeVisible();
});

test('the local port is a link that opens in the browser', async () => {
    await showing([forward()]);
    render(PortForwards, { contextId: PROD });

    await page.getByRole('button', { name: 'localhost:51234' }).click();

    // The URL is built by the backend from what it knows, rather than being
    // handed to it from here.
    await vi.waitFor(() => expect(PortForwardService.Open).toHaveBeenCalledWith('pf-1'));
});

test('a live forward offers to be disconnected', async () => {
    await showing([forward()]);
    render(PortForwards, { contextId: PROD });

    await page.getByRole('button', { name: 'Disconnect' }).click();

    await vi.waitFor(() => expect(PortForwardService.Stop).toHaveBeenCalledWith('pf-1'));
});

test('a disconnected one offers to be reconnected, and says the port it had', async () => {
    await showing([forward({ state: 'stopped', pod: '', podPort: 0 })]);
    render(PortForwards, { contextId: PROD });

    await expect.element(page.getByText('Disconnected').first()).toBeVisible();
    // Not a link: there is nothing at the far end of it.
    expect(page.getByRole('button', { name: 'localhost:51234' }).elements()).toHaveLength(0);

    await page.getByRole('button', { name: 'Reconnect' }).click();

    await vi.waitFor(() => expect(PortForwardService.Reconnect).toHaveBeenCalledWith('pf-1'));
});

test('one that failed says why', async () => {
    await showing([
        forward({ state: 'error', error: 'address already in use', pod: '', podPort: 0 }),
    ]);
    render(PortForwards, { contextId: PROD });

    await expect.element(page.getByText('Failed').first()).toBeVisible();
    await expect.element(page.getByText('address already in use')).toBeVisible();
});

test('removing one drops it from the list', async () => {
    await showing([forward()]);
    render(PortForwards, { contextId: PROD });

    await page.getByRole('button', { name: 'Remove' }).click();

    await vi.waitFor(() => expect(PortForwardService.Forget).toHaveBeenCalledWith('pf-1'));
    await expect.element(page.getByText(/Nothing is being forwarded/)).toBeVisible();
});

test('another cluster is another list', async () => {
    await showing([forward(), forward({ id: 'pf-2', contextId: 'other', name: 'elsewhere' })]);
    render(PortForwards, { contextId: PROD });

    // A tunnel goes to one cluster, and a list mixing several would be one you
    // had to read the fine print of before clicking anything in it.
    await expect.element(page.getByText('service api in web')).toBeVisible();
    expect(page.getByText('service elsewhere in web').elements()).toHaveLength(0);
});

test('what the backend says lands on the row', async () => {
    await showing([forward({ state: 'connecting', pod: '', podPort: 0 })]);
    render(PortForwards, { contextId: PROD });
    await expect.element(page.getByText('Connecting…').first()).toBeVisible();

    events.handler({ data: forward() });

    await expect.element(page.getByText('Connected').first()).toBeVisible();
});
