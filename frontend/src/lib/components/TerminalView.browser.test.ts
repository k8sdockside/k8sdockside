import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import TerminalView from './TerminalView.svelte';

// A real browser is the only place this component can be tested: xterm draws to
// a canvas, measures the element it is attached to, and only then knows how
// many columns the shell has.
const events = vi.hoisted(() => ({ handler: (_e: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return {
        ...actual,
        Events: {
            On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
                if (name === 'terminal:data') events.handler = handler;
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
        Launch: vi.fn().mockResolvedValue(undefined),
        LaunchNode: vi.fn().mockResolvedValue(undefined),
    },
    PortForwardService: {
        List: vi.fn().mockResolvedValue([]),
        Ports: vi.fn().mockResolvedValue([]),
        Start: vi.fn(),
        Reconnect: vi.fn(),
        Stop: vi.fn(),
        Forget: vi.fn(),
        Open: vi.fn(),
        URL: vi.fn(),
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

const { terminals } = await import('../state/terminals.svelte');
const { TerminalService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

const PROD = '/home/u/.kube/prod::admin@prod';

const POD_TAB = {
    id: 'shell:pod',
    view: 'shell' as const,
    contextId: PROD,
    kind: 'pods',
    namespace: 'default',
    name: 'web',
    title: 'web',
};

const NODE_TAB = { ...POD_TAB, id: 'shell:node', kind: 'nodes', namespace: '', name: 'wrkr01', title: 'wrkr01' };

const SESSION = { id: 'term-1', namespace: 'default', pod: 'web', container: 'app', node: '' };

/** Encodes what the backend would send, which is bytes in base64. */
function chunk(text: string, over: Record<string, unknown> = {}) {
    return { data: { sessionId: 'term-1', data: btoa(text), error: '', done: false, ...over } };
}

beforeEach(() => {
    terminals.forget(POD_TAB.id);
    terminals.forget(NODE_TAB.id);
    vi.mocked(TerminalService.Containers).mockReset().mockResolvedValue([
        { pod: 'web', container: 'app', init: false },
        { pod: 'web', container: 'sidecar', init: false },
    ]);
    vi.mocked(TerminalService.Open).mockReset().mockResolvedValue(SESSION);
    vi.mocked(TerminalService.OpenNode).mockReset().mockResolvedValue({
        id: 'term-2',
        namespace: 'default',
        pod: '',
        container: '',
        node: 'wrkr01',
    });
    vi.mocked(TerminalService.Close).mockReset();
    vi.mocked(TerminalService.Resize).mockReset();
    vi.mocked(TerminalService.Launch).mockReset().mockResolvedValue(undefined);
});

test('it opens a session and says what it attached to', async () => {
    render(TerminalView, { tab: POD_TAB });

    await expect.element(page.getByText('Connected')).toBeVisible();
    expect(TerminalService.Open).toHaveBeenCalledWith(PROD, 'pods', 'default', 'web', '', '');
    // The shell assumes 80x24 until it is told, and it is almost never that.
    await vi.waitFor(() => expect(TerminalService.Resize).toHaveBeenCalled());
});

test('what the shell writes reaches the screen', async () => {
    render(TerminalView, { tab: POD_TAB });
    await expect.element(page.getByText('Connected')).toBeVisible();

    events.handler(chunk('k8sdockside-on-screen\r\n'));

    // Drawn by xterm into rows of its own, so it is looked for in the output
    // rather than in anything this component rendered.
    await expect.element(page.getByText(/k8sdockside-on-screen/)).toBeVisible();
});

test('the picker offers every container, and choosing one is a new session', async () => {
    render(TerminalView, { tab: POD_TAB });
    await expect.element(page.getByRole('button', { name: 'sidecar' })).toBeVisible();

    await page.getByRole('button', { name: 'sidecar' }).click();

    await vi.waitFor(() => expect(TerminalService.Close).toHaveBeenCalledWith('term-1'));
    expect(TerminalService.Open).toHaveBeenLastCalledWith(PROD, 'pods', 'default', 'web', 'web', 'sidecar');
});

test('a session that ends offers to be opened again', async () => {
    render(TerminalView, { tab: POD_TAB });
    await expect.element(page.getByText('Connected')).toBeVisible();

    events.handler(chunk('', { done: true }));
    await expect.element(page.getByText('Ended')).toBeVisible();

    await page.getByRole('button', { name: 'Reconnect' }).click();

    await vi.waitFor(() => expect(TerminalService.Open).toHaveBeenCalledTimes(2));
    await expect.element(page.getByText('Connected')).toBeVisible();
});

test('a node opens a node shell, and has no container to pick', async () => {
    render(TerminalView, { tab: NODE_TAB });

    await expect.element(page.getByText('Connected')).toBeVisible();
    expect(TerminalService.OpenNode).toHaveBeenCalledWith(PROD, 'wrkr01');
    expect(TerminalService.Containers).not.toHaveBeenCalled();
});

test('the bar offers to hand the session to your own terminal', async () => {
    render(TerminalView, { tab: POD_TAB });
    await expect.element(page.getByText('Connected')).toBeVisible();

    await page.getByRole('button', { name: 'External' }).click();

    await vi.waitFor(() =>
        expect(TerminalService.Launch).toHaveBeenCalledWith(PROD, 'pods', 'default', 'web', '', ''),
    );
});

test('a shell that could not be opened says why instead of sitting empty', async () => {
    vi.mocked(TerminalService.Open).mockRejectedValueOnce(
        new Error('pods "web" is forbidden: cannot create resource "pods/exec"'),
    );
    render(TerminalView, { tab: POD_TAB });

    await expect.element(page.getByText(/pods\/exec/)).toBeVisible();
});
