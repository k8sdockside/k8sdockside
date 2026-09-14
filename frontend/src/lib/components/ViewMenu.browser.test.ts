import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import TopBar from './TopBar.svelte';

// A pane renders the view its active tab names, so this mounts real tables.
// Stubbing the subscription is what lets them exist without a cluster behind
// them; these tests are about where a view sits, not about its rows.
vi.mock('../state/subscriptions', () => ({
    subscribe: vi.fn(() => ({ setNamespaces: vi.fn(), close: vi.fn() })),
}));

// The View menu, which is the visible way back from a hidden panel. A
// keyboard shortcut is no answer to "it has disappeared": if the cluster
// tree can be hidden, something on screen has to be able to bring it back.
//
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
vi.mock('../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services', () => ({
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
    // The title bar carries the search box, whose store is loaded with it.
    SearchService: { Start: vi.fn().mockResolvedValue(undefined), Cancel: vi.fn().mockResolvedValue(undefined) },
    KubeconfigService: { Sync: vi.fn().mockResolvedValue([]), Files: vi.fn().mockResolvedValue([]) },
    ResourceService: {
        Describe: vi.fn().mockResolvedValue(''),
        Namespaces: vi.fn().mockResolvedValue(['default']),
        ResourceYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        ApplyYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        CheckYAML: vi.fn().mockResolvedValue({ valid: true, message: '', line: 0 }),
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

const { workspace, CLUSTERS_TAB_ID, clustersTab } = await import('../state/workspace.svelte');

const { SETTINGS, HELP, KUBERNETES } = await import('../catalogue');

const PROD = '/home/u/.kube/prod::admin@prod';

/**
 * One item, whichever role it carries. A panel toggle is a menuitemcheckbox
 * because it has a tick; an action is a plain menuitem. What a test is asking
 * for is the row, so it should not have to know which of the two it is.
 */
const item = (name: string | RegExp) =>
    page.getByRole('menuitemcheckbox', { name }).or(page.getByRole('menuitem', { name }));

async function openMenu(): Promise<void> {
    await page.getByRole('button', { name: 'View' }).click();
    await expect.element(page.getByRole('menu', { name: 'View' })).toBeVisible();
}

beforeEach(() => {
    document.body.innerHTML = '';
    workspace.closeAllTabsIn('main');
    workspace.closeAllTabsIn('right');
    workspace.closeAllTabsIn('bottom');
    if (workspace.paneOf(CLUSTERS_TAB_ID) === null) {
        workspace.panes.left.tabs = [clustersTab()];
    }
    workspace.moveTabToPane(CLUSTERS_TAB_ID, 'left');
    workspace.setPaneOpen('left', true);
    workspace.files = [
        {
            path: '/home/u/.kube/prod',
            source: 'manual',
            error: '',
            contexts: [{ id: PROD, name: 'admin@prod' } as never],
        },
    ];
});

test('the menu names the panel the cluster tree is in, so it can be found', async () => {
    render(TopBar);

    await openMenu();

    await expect.element(item(/Left panel \(Explorer\)/)).toBeVisible();
});

// The whole reason this menu exists.
test('a hidden cluster tree can be brought back from it', async () => {
    render(TopBar);
    workspace.toggleClusters();
    expect(workspace.isPaneOpen('left')).toBe(false);

    await openMenu();
    await item(/Left panel/).click();

    expect(workspace.isPaneOpen('left')).toBe(true);
});

test('the tick follows the panel, so the menu says what is showing', async () => {
    render(TopBar);

    await openMenu();
    await expect.element(item(/Left panel/)).toHaveAttribute('aria-checked', 'true');
    await item(/Left panel/).click();

    await openMenu();
    await expect.element(item(/Left panel/)).toHaveAttribute('aria-checked', 'false');
});

test('a panel with nothing in it cannot be toggled, because there is nothing to show', async () => {
    render(TopBar);

    await openMenu();

    await expect.element(item(/Right panel/)).toBeDisabled();
});

test('the name follows the tree when it is moved out of the left panel', async () => {
    render(TopBar);
    workspace.moveTabToPane(CLUSTERS_TAB_ID, 'bottom');

    await openMenu();

    await expect.element(item(/Bottom panel \(Explorer\)/)).toBeVisible();
});

test('reset layout puts a hidden tree and a moved view back', async () => {
    render(TopBar);
    workspace.openTab(PROD, 'pods');
    workspace.moveTabToPane(CLUSTERS_TAB_ID, 'right');

    await openMenu();
    await item('Reset layout').click();

    expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('left');
    expect(workspace.panes.main.tabs.map((t) => t.kind)).toEqual(['pods']);
});

// Its other route in is a button in the cluster tree, which is exactly the
// thing that may be hidden.
test('settings can be opened even with the tree hidden', async () => {
    render(TopBar);
    workspace.toggleClusters();

    await openMenu();
    await item('Settings').click();

    expect(workspace.allTabs.some((t) => t.kind === SETTINGS)).toBe(true);
});

// The trigger sits at the right end of the title bar, because the left of that
// bar belongs to the macOS traffic lights and the middle to the title. A menu
// that grows rightwards from a button in that position grows off the edge of
// the window, and half of every label goes with it. A menu whose whole purpose
// is to be found cannot be the thing that is half off screen.
test('the menu opens inside the window, though its button is at the right edge', async () => {
    render(TopBar);

    await openMenu();

    const menu = document.querySelector('[role="menu"]');
    expect(menu).not.toBeNull();
    const box = (menu as HTMLElement).getBoundingClientRect();
    expect(box.left).toBeGreaterThanOrEqual(0);
    expect(box.right).toBeLessThanOrEqual(document.documentElement.clientWidth);
});

// The two documentation pages sit in the menu above Settings, since a menu
// whose purpose is to be found is where help belongs too.
test('help and the Kubernetes primer can be opened from it', async () => {
    render(TopBar);

    await openMenu();
    await item('Help').click();
    expect(workspace.allTabs.some((t) => t.kind === HELP)).toBe(true);

    await openMenu();
    await item('Kubernetes primer').click();
    expect(workspace.allTabs.some((t) => t.kind === KUBERNETES)).toBe(true);
});
