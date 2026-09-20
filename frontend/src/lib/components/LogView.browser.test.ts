import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import LogView from './LogView.svelte';

const events = vi.hoisted(() => ({ handler: (_e: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return {
        ...actual,
        Events: {
            On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
                if (name === 'pod:logs') events.handler = handler;
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
        ObjectState: vi.fn().mockResolvedValue({
            scalable: false,
            replicas: 0,
            cordoned: false,
            containers: [],
        }),
    },
    LogService: {
        Containers: vi.fn(),
        Open: vi.fn(),
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

const { logs } = await import('../state/logs.svelte');
const { LogService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

const PROD = '/home/u/.kube/prod::admin@prod';

const TAB = {
    id: 'logs:prod#pods#default#web',
    view: 'logs' as const,
    contextId: PROD,
    kind: 'pods',
    namespace: 'default',
    name: 'web',
    title: 'web',
};

const ONE_POD = [
    { pod: 'web', container: 'app', init: false },
    { pod: 'web', container: 'sidecar', init: false },
];

const settle = () => new Promise((r) => setTimeout(r, 80));

/** Delivers a batch of lines as the backend sends them. */
function deliver(lines: { pod: string; container: string; text: string }[], done = false) {
    events.handler({ data: { streamId: 'logs-1', lines, error: '', done } });
}

beforeEach(() => {
    logs.forget(TAB.id);
    vi.mocked(LogService.Containers).mockReset().mockResolvedValue(ONE_POD);
    vi.mocked(LogService.Open).mockReset().mockResolvedValue('logs-1');
    vi.mocked(LogService.Close).mockReset();
});

test('opening follows every container and says which they are', async () => {
    render(LogView, { tab: TAB });

    await expect.element(page.getByRole('button', { name: 'app' })).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'sidecar' })).toBeVisible();
    await vi.waitFor(() =>
        expect(LogService.Open).toHaveBeenCalledWith(PROD, 'pods', 'default', 'web', [], true),
    );
});

test('lines arrive and are shown', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());

    deliver([{ pod: 'web', container: 'app', text: 'listening on :8080' }]);

    await expect.element(page.getByText('listening on :8080')).toBeVisible();
});

// One pod's name on every line would be the same word repeated down the view,
// taking room from the log itself.
test('a single pod does not label every line with its own name', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());

    deliver([{ pod: 'web', container: 'app', text: 'hello' }]);
    await expect.element(page.getByText('hello')).toBeVisible();

    // Scoped to the log itself: "app" is also the name of a picker button up
    // in the toolbar, which is not what this is about.
    const log = document.querySelector('[role="log"]')!;
    const labels = [...log.querySelectorAll('.line span')].map((el) => el.textContent?.trim());
    // The container is still named -- two of them are merged into this view.
    expect(labels).toContain('app');
    expect(labels).not.toContain('web');
});

// A deployment's view merges several pods, and then which pod a line came from
// is the thing you are reading it for.
test('several pods label their lines', async () => {
    vi.mocked(LogService.Containers).mockResolvedValue([
        { pod: 'web-a', container: 'app', init: false },
        { pod: 'web-b', container: 'app', init: false },
    ]);
    render(LogView, { tab: { ...TAB, kind: 'deployments' } });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());

    deliver([{ pod: 'web-a', container: 'app', text: 'from a' }]);

    await expect.element(page.getByText('web-a')).toBeVisible();
});

test('turning a container off follows only what is left', async () => {
    render(LogView, { tab: TAB });
    await expect.element(page.getByRole('button', { name: 'sidecar' })).toBeVisible();

    await page.getByRole('button', { name: 'sidecar' }).click();

    await vi.waitFor(() =>
        expect(LogService.Open).toHaveBeenLastCalledWith(PROD, 'pods', 'default', 'web', ['app'], true),
    );
});

// Following nothing is an empty view with no way back to a full one.
test('the last container cannot be turned off', async () => {
    vi.mocked(LogService.Containers).mockResolvedValue([ONE_POD[0]]);
    render(LogView, { tab: TAB });
    await expect.element(page.getByRole('button', { name: 'app' })).toBeVisible();
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalledTimes(1));

    await page.getByRole('button', { name: 'app' }).click();
    await settle();

    expect(LogService.Open).toHaveBeenCalledTimes(1);
});

test('following can be turned off', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());

    await page.getByRole('button', { name: 'Follow' }).click();

    await vi.waitFor(() =>
        expect(LogService.Open).toHaveBeenLastCalledWith(PROD, 'pods', 'default', 'web', [], false),
    );
});

test('the filter hides the lines that do not match', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());
    deliver([
        { pod: 'web', container: 'app', text: 'GET /health 200' },
        { pod: 'web', container: 'app', text: 'GET /orders 500' },
    ]);
    await expect.element(page.getByText('GET /health 200')).toBeVisible();

    await page.getByRole('textbox', { name: 'Filter lines' }).fill('500');

    await expect.element(page.getByText('GET /orders 500')).toBeVisible();
    await expect.poll(() => page.getByText('GET /health 200').elements()).toHaveLength(0);
});

test('clearing empties the view', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());
    deliver([{ pod: 'web', container: 'app', text: 'gone soon' }]);
    await expect.element(page.getByText('gone soon')).toBeVisible();

    await page.getByRole('button', { name: 'Clear' }).click();

    await expect.poll(() => page.getByText('gone soon').elements()).toHaveLength(0);
});

test('a view that will not open says why rather than sitting empty', async () => {
    vi.mocked(LogService.Containers).mockRejectedValue(new Error('pods "web" is forbidden'));

    render(LogView, { tab: TAB });

    await expect.element(page.getByText(/forbidden/)).toBeVisible();
});

// Dropping lines silently would make the view look like it starts where the
// container started.
test('a truncated view says so', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());

    deliver(
        Array.from({ length: 5100 }, (_, i) => ({ pod: 'web', container: 'app', text: `line ${i}` })),
    );

    await expect.element(page.getByText(/Earlier lines have been dropped/)).toBeVisible();
});

// A stream that ended -- the pod restarted, the rollout replaced it -- used to
// leave the view showing the log of something that is no longer running, with
// no way on but closing the tab. Turning Follow off and on again was the trick
// people found, which is two clicks for a reload and turns the stream off on
// the way.
test('a stream that ended can be opened again without closing the tab', async () => {
    render(LogView, { tab: TAB });
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());
    deliver([{ pod: 'web', container: 'app', text: 'from the old pod' }], true);
    await expect.element(page.getByText('from the old pod')).toBeVisible();

    vi.mocked(LogService.Open).mockResolvedValue('logs-2');
    await page.getByRole('button', { name: 'Reload' }).click();
    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalledTimes(2));

    // The new stream's lines land, and the old ones went with the old stream.
    events.handler({ data: { streamId: 'logs-2', lines: [{ pod: 'web', container: 'app', text: 'from the new pod' }], error: '', done: false } });
    await expect.element(page.getByText('from the new pod')).toBeVisible();
    await expect.poll(() => page.getByText('from the old pod').elements()).toHaveLength(0);

    // Following is still on: reloading is not a way to turn it off.
    await expect.element(page.getByRole('button', { name: 'Follow' })).toHaveAttribute('aria-pressed', 'true');
});

// A view whose containers could not even be read has the same way back.
test('a view that would not open can be retried from where it failed', async () => {
    vi.mocked(LogService.Containers).mockRejectedValue(new Error('pods "web" is forbidden'));
    render(LogView, { tab: TAB });
    await expect.element(page.getByText(/forbidden/)).toBeVisible();

    vi.mocked(LogService.Containers).mockResolvedValue(ONE_POD);
    await page.getByRole('button', { name: 'Try again' }).click();

    await vi.waitFor(() => expect(LogService.Open).toHaveBeenCalled());
    deliver([{ pod: 'web', container: 'app', text: 'readable after all' }]);
    await expect.element(page.getByText('readable after all')).toBeVisible();
});
