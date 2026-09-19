import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Pane from './Pane.svelte';

// The main pane: the strip of tabs above the view, and the view under it.

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
        // Activating a tab unfolds the section its resource is listed under,
        // and that folding is a per-context preference, so it is written here.
        SetContextPrefs: vi.fn().mockResolvedValue({}),
        SetPanes: vi.fn().mockResolvedValue({}),
        SetLayout: vi.fn().mockResolvedValue({}),
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

/** Gives the contexts real names, so the menu can label the scoped items. */
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

/**
 * Right-click. Dispatched rather than driven through the pointer because the
 * menu positions itself from clientX/clientY, and naming them keeps the
 * assertions independent of where the tab happens to land.
 */
function rightClick(element: Element): void {
    element.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 60, clientY: 40 }),
    );
}

beforeEach(() => {
    workspace.closeAllTabs();
    workspace.files = [];
});

test('right-clicking a tab opens the menu', async () => {
    workspace.openTab(PROD, 'pods');
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').element());

    await expect.element(page.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
    await expect.element(page.getByRole('menuitem', { name: 'Close Others' })).toBeVisible();
    await expect.element(page.getByRole('menuitem', { name: 'Close All' })).toBeVisible();
});

test('the cluster-scoped items stay hidden while only one cluster has tabs', async () => {
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').first().element());

    await expect.element(page.getByRole('menuitem', { name: 'Close All' })).toBeVisible();
    expect(page.getByRole('menuitem', { name: /Close All in/ }).elements()).toHaveLength(0);
});

test('the cluster-scoped items name the cluster once a second one has tabs', async () => {
    withClusters();
    workspace.openTab(PROD, 'pods');
    workspace.openTab(STAGING, 'pods');
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').first().element());

    await expect.element(page.getByRole('menuitem', { name: 'Close Others in admin@prod' })).toBeVisible();
    await expect.element(page.getByRole('menuitem', { name: 'Close All in admin@prod' })).toBeVisible();
});

test('right-clicking activates the tab before acting on it', async () => {
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    workspace.activateTab(workspace.tabs[0].id);
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').nth(1).element());

    expect(workspace.activeTabId).toBe(workspace.tabs[1].id);
});

test('Close Others leaves the tab it was opened on', async () => {
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    workspace.openTab(PROD, 'services');
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').nth(1).element());
    await page.getByRole('menuitem', { name: 'Close Others' }).click();

    expect(workspace.tabs.map((t) => t.kind)).toEqual(['nodes']);
});

test('Close All in a cluster spares the other cluster', async () => {
    withClusters();
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    workspace.openTab(STAGING, 'pods');
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').first().element());
    await page.getByRole('menuitem', { name: 'Close All in admin@prod' }).click();

    expect(workspace.tabs.map((t) => t.contextId)).toEqual([STAGING]);
});

test('Escape dismisses the menu without closing anything', async () => {
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    render(Pane, { pane: 'main' });

    rightClick(await page.getByRole('tab').first().element());
    await expect.element(page.getByRole('menuitem', { name: 'Close All' })).toBeVisible();

    await page.getByRole('menu').element().dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );

    expect(page.getByRole('menuitem').elements()).toHaveLength(0);
    expect(workspace.tabs).toHaveLength(2);
});

// ----- scrolling ---------------------------------------------------------
//
// These are the reason the component tests run in a real browser: overflow,
// ResizeObserver and scrollIntoView have no meaningful behaviour in jsdom.

const MANY = ['pods', 'nodes', 'services', 'deployments', 'statefulsets', 'daemonsets',
    'jobs', 'cronjobs', 'ingresses', 'configmaps', 'secrets', 'persistentvolumeclaims',
    'events', 'namespaces', 'gateways', 'httproutes', 'grpcroutes', 'gatewayclasses'];

/** Opens enough tabs to overflow the window. */
function openMany(): void {
    for (const kind of MANY) workspace.openTab(PROD, kind);
}

function strip(): HTMLElement {
    return document.querySelector('.tabbar') as HTMLElement;
}

/** Puts the strip at its left end and waits for it to settle there. */
async function scrollToStart(): Promise<void> {
    workspace.activateTab(workspace.tabs[0].id);
    await expect.poll(() => strip().scrollLeft).toBe(0);
}

/** How many arrows of one direction are in the accessibility tree. */
function arrows(direction: 'left' | 'right'): number {
    return page.getByRole('button', { name: `Scroll tabs ${direction}` }).elements().length;
}

test('neither arrow shows while every tab fits', async () => {
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    render(Pane, { pane: 'main' });

    await expect.element(page.getByRole('tab').first()).toBeVisible();
    // A hidden arrow is out of the accessibility tree entirely, not merely
    // transparent -- so it cannot be found, rather than found and invisible.
    expect(arrows('left')).toBe(0);
    expect(arrows('right')).toBe(0);
});

test('the right arrow appears once the tabs overflow', async () => {
    openMany();
    render(Pane, { pane: 'main' });

    await expect.element(page.getByRole('button', { name: 'Scroll tabs right' })).toBeVisible();

    // Opening scrolled to the newest tab; back at the left end there is
    // nothing behind us to reach.
    await scrollToStart();
    await expect.poll(() => arrows('left')).toBe(0);
});

test('the arrow scrolls the strip, and the left one then appears', async () => {
    openMany();
    render(Pane, { pane: 'main' });
    await scrollToStart();

    await page.getByRole('button', { name: 'Scroll tabs right' }).click();

    await expect.poll(() => strip().scrollLeft).toBeGreaterThan(0);
    await expect.element(page.getByRole('button', { name: 'Scroll tabs left' })).toBeVisible();
});

test('a vertical wheel scrolls the strip sideways', async () => {
    openMany();
    render(Pane, { pane: 'main' });
    await scrollToStart();

    strip().dispatchEvent(new WheelEvent('wheel', { deltaY: 240, bubbles: true, cancelable: true }));

    await expect.poll(() => strip().scrollLeft).toBeGreaterThan(0);
});

test('selecting a tab that is scrolled out of sight brings it back', async () => {
    openMany();
    render(Pane, { pane: 'main' });

    const first = workspace.tabs[0].id;
    strip().scrollLeft = strip().scrollWidth;
    await expect.poll(() => strip().scrollLeft).toBeGreaterThan(0);

    workspace.activateTab(first);

    await expect.poll(() => strip().scrollLeft, { timeout: 3000 }).toBe(0);
});
