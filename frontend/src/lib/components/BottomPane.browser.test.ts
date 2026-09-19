import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Pane from './Pane.svelte';

// The bottom pane, which is what the dock became. It is a pane like the others
// and differs only in what it starts out holding and in being the one that
// keeps its strip on screen when it is empty -- a place has to be visible
// before anything can be put in it.

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
        Describe: vi.fn().mockResolvedValue(''),
        ResourceYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        ApplyYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        CheckYAML: vi.fn().mockResolvedValue({ valid: true, message: '', line: 0 }),
    },
    // The describe panel is a tab now, so every pane can mount it and its
    // action bar -- which is why a test about where tabs go needs this.
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }),
        Delete: vi.fn().mockResolvedValue(undefined),
        Scale: vi.fn().mockResolvedValue(undefined),
        Restart: vi.fn().mockResolvedValue(undefined),
        Cordon: vi.fn().mockResolvedValue(undefined),
        Drain: vi.fn().mockResolvedValue('drain-1'),
        CancelDrain: vi.fn(),
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
    UpdateService: {
        Status: vi.fn().mockResolvedValue({ current: 'test', latest: null, newer: false, unread: false, checkedAt: '', error: '' }),
        Check: vi.fn().mockResolvedValue({ current: 'test', latest: null, newer: false, unread: false, checkedAt: '', error: '' }),
        MarkRead: vi.fn().mockResolvedValue({ current: 'test', latest: null, newer: false, unread: false, checkedAt: '', error: '' }),
        OpenRelease: vi.fn().mockResolvedValue(undefined),
    },
}));

const { workspace } = await import('../state/workspace.svelte');

const PROD = '/home/u/.kube/prod::admin@prod';
const STAGING = '/home/u/.kube/staging::admin@staging';

/** Gives the contexts real names, so the tabs and menu can label them. */
function withClusters(): void {
    workspace.files = [
        {
            path: '/home/u/.kube/prod',
            source: 'manual',
            error: '',
            contexts: [{ id: PROD, name: 'admin@prod' } as never],
        },
        {
            path: '/home/u/.kube/staging',
            source: 'manual',
            error: '',
            contexts: [{ id: STAGING, name: 'admin@staging' } as never],
        },
    ];
}

function object(contextId: string, name: string) {
    return { contextId, kind: 'pods', namespace: 'default', name };
}

/** Right-click, dispatched so the menu's coordinates are ours to name. */
function rightClick(element: Element): void {
    element.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 60, clientY: 40 }),
    );
}

beforeEach(() => {
    workspace.closeAllDockTabs();
    workspace.settings.panes.bottom.open = false;
    withClusters();
});

// The whole point of a dock: it is a place things go, and a place has to be
// there before anything is in it.
test('the strip is on screen with nothing open, and says what it is for', async () => {
    render(Pane, { pane: 'bottom' });

    await expect.element(page.getByRole('tablist', { name: 'Dock' })).toBeVisible();
    await expect.element(page.getByText(/in the details panel/)).toBeVisible();
});

test('an object opened for editing becomes a tab named after it', async () => {
    render(Pane, { pane: 'bottom' });

    workspace.openEditor(object(PROD, 'web'));

    const tab = page.getByRole('tab', { name: /web/ });
    await expect.element(tab).toBeVisible();
    // Every dock tab names its cluster: an object's name does not say which
    // cluster it is in, and that is the mistake worth spending the space on.
    await expect.element(page.getByRole('tab', { name: /admin@prod/ })).toBeVisible();
    await expect.element(page.getByRole('textbox', { name: 'web as YAML' })).toBeVisible();
});

test('folding the dock keeps the tabs and gives the room back', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    await expect.element(page.getByRole('textbox')).toBeVisible();

    await page.getByRole('button', { name: 'Hide the dock' }).click();

    // Polled rather than read straight after the click: the click resolves
    // when the event is dispatched, not when Svelte has finished with it.
    await expect.poll(() => page.getByRole('textbox').elements()).toHaveLength(0);
    await expect.element(page.getByRole('tab', { name: /web/ })).toBeVisible();
});

test('closing the last tab leaves the strip and its hint behind', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    await expect.element(page.getByRole('tab', { name: /web/ })).toBeVisible();

    await page.getByRole('button', { name: 'Close web' }).click();

    await expect.element(page.getByText(/in the details panel/)).toBeVisible();
    await expect.element(page.getByRole('tablist', { name: 'Dock' })).toBeVisible();
});

test('the cluster-scoped menu items appear once two clusters are open here', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    workspace.openEditor(object(STAGING, 'api'));
    await expect.element(page.getByRole('tab', { name: /api/ })).toBeVisible();

    rightClick(await page.getByRole('tab', { name: /api/ }).element());

    await expect.element(page.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
    await expect.element(page.getByRole('menuitem', { name: 'Close All in admin@staging' })).toBeVisible();
});

test('an unsaved document is marked on its tab', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    await expect.element(page.getByRole('textbox')).toBeVisible();

    await page.getByRole('textbox').fill('kind: Pod\nmetadata: {}\n');

    await expect.element(page.getByRole('button', { name: /unsaved changes/ })).toBeVisible();
});

// Down here activation is a toggle: clicking the tab you are on folds the dock
// and gives the room back. The menu opened on a tab by activating it first, so
// asking the active tab for its menu folded the dock out from under the menu.
test('right-clicking the tab you are on opens the menu and leaves the dock open', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    await expect.element(page.getByRole('textbox', { name: 'web as YAML' })).toBeVisible();

    rightClick(await page.getByRole('tab', { name: /web/ }).element());

    expect(workspace.dockOpen).toBe(true);
    await expect.element(page.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
    await expect.element(page.getByRole('textbox', { name: 'web as YAML' })).toBeVisible();
});

// The keyboard way in has the same shape and had the same fault.
test('the menu key on the tab you are on leaves the dock open too', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    await expect.element(page.getByRole('textbox', { name: 'web as YAML' })).toBeVisible();

    (await page.getByRole('tab', { name: /web/ }).element()).dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ContextMenu', bubbles: true, cancelable: true }),
    );

    expect(workspace.dockOpen).toBe(true);
    await expect.element(page.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
});

// Activating first is still right when it is a different tab: closing a
// document you cannot see is how the wrong one gets closed.
test('right-clicking another tab brings it forward before the menu opens', async () => {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(object(PROD, 'web'));
    workspace.openEditor(object(PROD, 'api'));
    await expect.element(page.getByRole('textbox', { name: 'api as YAML' })).toBeVisible();

    rightClick(await page.getByRole('tab', { name: /web/ }).element());

    await expect.element(page.getByRole('textbox', { name: 'web as YAML' })).toBeVisible();
    await expect.element(page.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
});

// The dock has two views now, and the tab says which it is. Without this the
// log view is a component nothing renders.
test('a logs tab shows the log view rather than the editor', async () => {
    render(Pane, { pane: 'bottom' });

    workspace.openLogs(object(PROD, 'web'));

    await expect.element(page.getByRole('log', { name: 'Log output' })).toBeVisible();
    expect(page.getByRole('textbox', { name: 'web as YAML' }).elements()).toHaveLength(0);
});

test('an edit tab still shows the editor', async () => {
    render(Pane, { pane: 'bottom' });

    workspace.openEditor(object(PROD, 'web'));

    await expect.element(page.getByRole('textbox', { name: 'web as YAML' })).toBeVisible();
});

// Both views onto one object, side by side in the strip.
test('the two views on one object are two tabs', async () => {
    render(Pane, { pane: 'bottom' });

    workspace.openEditor(object(PROD, 'web'));
    workspace.openLogs(object(PROD, 'web'));

    await expect.poll(() => page.getByRole('tab').elements()).toHaveLength(2);
});
