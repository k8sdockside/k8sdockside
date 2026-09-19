import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Pane from './Pane.svelte';

// Where the right-click menu lands.
//
// It hangs off the tab it was asked for rather than off the pointer, so that
// the strip at the top of the window and the dock at the foot of it read the
// same: a menu dropping from the pointer belongs to a tab up there and floats
// over the document down here.

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
// A pane renders the view its active tab names, so a strip test now mounts a
// real table with it. Stubbing the subscription is what lets that table exist
// without a cluster behind it; these tests are about the strip, not the rows.
vi.mock('../state/subscriptions', () => ({
    subscribe: vi.fn(() => ({ setNamespaces: vi.fn(), close: vi.fn() })),
}));

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
        Namespaces: vi.fn().mockResolvedValue(['default']),
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

const settle = () => new Promise((r) => setTimeout(r, 150));

function withClusters(): void {
    workspace.files = [
        {
            path: '/home/u/.kube/prod',
            source: 'manual',
            error: '',
            contexts: [{ id: PROD, name: 'admin@prod' } as never],
        },
    ];
}

/**
 * Lays the strip out inside a full-height element carrying the app's zoom, the
 * way App.svelte does. `edge` is which end of the window the strip sits at:
 * `end` is the dock, `start` stands in for the strip above the view.
 *
 * Both halves matter. The strip's distance down the window is what decides
 * which side the menu opens on, and the zoom is what the coordinates it is
 * placed with have to be expressed in.
 */
function mountLikeTheApp(root: string, edge: 'start' | 'end', zoom = 1): void {
    document.documentElement.style.height = '100%';
    document.body.style.cssText = 'height:100%;margin:0';
    let node = document.querySelector(root)?.parentElement as HTMLElement | null;
    while (node && node !== document.body) {
        node.style.height = '100%';
        node = node.parentElement;
    }
    const host = document.querySelector(root)?.parentElement as HTMLElement;
    host.style.cssText = `height:100%;display:flex;flex-direction:column;justify-content:flex-${edge};zoom:${zoom}`;
}

/** Right-clicks an element's far right, well away from the edge it anchors to. */
function rightClickFarSide(element: Element): number {
    const box = element.getBoundingClientRect();
    const x = Math.round(box.right - 6);
    element.dispatchEvent(
        new MouseEvent('contextmenu', {
            bubbles: true,
            cancelable: true,
            clientX: x,
            clientY: Math.round(box.top + box.height / 2),
        }),
    );
    return x;
}

const boxOf = (sel: string) => document.querySelector(sel)!.getBoundingClientRect();
const tabBox = () => boxOf('[role="tab"]');
const menuBox = () => boxOf('[role="menu"]');

async function openTheMenu(): Promise<void> {
    await expect.element(page.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
    await settle();
}

beforeEach(async () => {
    await page.viewport(1200, 800);
    document.body.innerHTML = '';
    workspace.closeAllTabs();
    workspace.closeAllDockTabs();
    workspace.settings.panes.bottom.open = false;
    workspace.settings.layout.zoom = 1;
    withClusters();
});

/** The dock, at the foot of the window, holding one editor tab. */
async function dockAtTheFoot(zoom = 1): Promise<void> {
    render(Pane, { pane: 'bottom' });
    workspace.openEditor({ contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' });
    await expect.element(page.getByRole('tab', { name: /web/ })).toBeVisible();
    mountLikeTheApp('section.pane.bottom', 'end', zoom);
    await settle();
}

/** The strip above the view, at the top of the window, holding one tab. */
async function stripAtTheTop(): Promise<void> {
    render(Pane, { pane: 'main' });
    workspace.openTab(PROD, 'pods');
    await expect.element(page.getByRole('tab', { name: /Pods/ })).toBeVisible();
    mountLikeTheApp('div.strip', 'start');
    await settle();
}

test('the menu lines up with the tab, not with the pointer', async () => {
    await dockAtTheFoot();
    const tab = tabBox();

    const pointer = rightClickFarSide(document.querySelector('[role="tab"]')!);
    await openTheMenu();

    expect(menuBox().left).toBeCloseTo(tab.left, -1);
    // The pointer was at the other end of the tab; had the menu followed it,
    // the two would not be within a hundred pixels of each other.
    expect(Math.abs(menuBox().left - pointer)).toBeGreaterThan(40);
});

test('the dock, at the foot of the window, opens its menu above the tab', async () => {
    await dockAtTheFoot();
    const tab = tabBox();

    rightClickFarSide(document.querySelector('[role="tab"]')!);
    await openTheMenu();

    // Its lower edge meets the tab's upper edge: the menu rests on the tab.
    expect(menuBox().bottom).toBeCloseTo(tab.top, -1);
});

test('the strip above the view opens its menu below the tab', async () => {
    await stripAtTheTop();
    const tab = tabBox();

    rightClickFarSide(document.querySelector('[role="tab"]')!);
    await openTheMenu();

    expect(menuBox().top).toBeCloseTo(tab.bottom, -1);
});

// The app is drawn inside an element carrying `zoom`, and a fixed position
// inside a zoomed box is resolved in that box's own scaled space. Placing the
// menu at a viewport coordinate therefore lands it at coordinate x zoom --
// right at the top of the window, and tens of pixels out at the foot of it.
test('the menu still rests on its tab when the app is zoomed', async () => {
    await dockAtTheFoot(1.25);
    const tab = tabBox();

    rightClickFarSide(document.querySelector('[role="tab"]')!);
    await openTheMenu();

    expect(menuBox().bottom).toBeCloseTo(tab.top, -1);
    expect(menuBox().left).toBeCloseTo(tab.left, -1);
});
