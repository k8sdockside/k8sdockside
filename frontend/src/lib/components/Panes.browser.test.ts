import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Pane from './Pane.svelte';
import { detail } from '../state/detail.svelte';

// A pane renders the view its active tab names, so this mounts real tables.
// Stubbing the subscription is what lets them exist without a cluster behind
// them; these tests are about where a view sits, not about its rows.
vi.mock('../state/subscriptions', () => ({
    subscribe: vi.fn(() => ({ setNamespaces: vi.fn(), close: vi.fn() })),
}));

// Dragging a view from one pane into another, which is the whole point of
// panes: where a thing sits is the user's answer, not the view type's.
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
        // The describe panel draws a budget for what it is describing, and
        // starts reading it as it mounts.
        Budget: vi.fn().mockResolvedValue({
            scope: { kind: 'cluster', name: '' },
            amounts: [],
            usage: { source: '', error: '' },
        }),
        CPUDelay: vi.fn().mockResolvedValue({ nodes: [], error: '' }),
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

const { workspace, CLUSTERS_TAB_ID, DETAILS_TAB_ID, clustersTab } = await import(
    '../state/workspace.svelte',
);

const PROD = '/home/u/.kube/prod::admin@prod';

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

const WEB = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };

/**
 * Drags one tab onto another pane's strip.
 *
 * Dispatched by hand rather than driven through the pointer: the strips are in
 * two separately rendered components here, and what is being checked is the
 * exchange between them -- that a dragstart in one is understood as a move by
 * the other -- rather than whether Chromium's own drag gesture works.
 */
function dragOnto(tab: Element, strip: Element): void {
    const dataTransfer = new DataTransfer();
    tab.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer }));
    strip.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer }));
    strip.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer }));
    tab.dispatchEvent(new DragEvent('dragend', { bubbles: true, cancelable: true, dataTransfer }));
}

const stripFor = (label: string) => document.querySelector(`[role="tablist"][aria-label="${label}"]`)!;

beforeEach(() => {
    document.body.innerHTML = '';
    workspace.closeAllTabsIn('main');
    workspace.closeAllTabsIn('right');
    workspace.closeAllTabsIn('bottom');
    // Put the tree back where it starts, since one of these tests moves it.
    if (workspace.paneOf(CLUSTERS_TAB_ID) === null) {
        workspace.panes.left.tabs = [clustersTab()];
    }
    workspace.moveTabToPane(CLUSTERS_TAB_ID, 'left');
    workspace.setPaneOpen('left', true);
    // And the describe tab's home, for the same reason: one of these tests
    // drags it to the foot of the window, and remembering that is the point of
    // it -- so the next test has to be told where it starts.
    detail.close();
    workspace.settings.layout.detailPane = 'right';
    withClusters();
});

test('an editor dragged from the foot of the window into the middle goes there', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(WEB);
    await expect.element(page.getByRole('tab', { name: /web/ })).toBeVisible();

    dragOnto(await page.getByRole('tab', { name: /web/ }).element(), stripFor('Open views'));

    expect(workspace.panes.bottom.tabs).toHaveLength(0);
    expect(workspace.panes.main.tabs.map((t) => t.name)).toEqual(['web']);
});

test('a list dragged out of the middle leaves it, and the pane it lands in shows it', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'bottom' });
    workspace.openTab(PROD, 'pods');
    await expect.element(page.getByRole('tab', { name: /Pods/ })).toBeVisible();

    dragOnto(await page.getByRole('tab', { name: /Pods/ }).element(), stripFor('Dock'));

    expect(workspace.panes.main.tabs).toHaveLength(0);
    expect(workspace.panes.bottom.tabs.map((t) => t.kind)).toEqual(['pods']);
    // Landing in a pane brings it forward there, so the drop shows its result.
    expect(workspace.panes.bottom.activeId).toBe(workspace.panes.bottom.tabs[0].id);
});

// The move must not be mistaken for a reorder, which is what a drag within one
// strip is. Nothing should leave the pane it started in.
test('dragging a tab within its own strip still only reorders it', async () => {
    render(Pane, { pane: 'main' });
    workspace.openTab(PROD, 'pods');
    workspace.openTab(PROD, 'nodes');
    await expect.element(page.getByRole('tab', { name: /Nodes/ })).toBeVisible();

    dragOnto(await page.getByRole('tab', { name: /Nodes/ }).element(), stripFor('Open views'));

    expect(workspace.panes.main.tabs).toHaveLength(2);
    expect(workspace.panes.bottom.tabs).toHaveLength(0);
});

// The reason a tab id says what it shows rather than which one it is.
test('a moved editor is still the same editor', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'bottom' });
    workspace.openEditor(WEB);
    await expect.element(page.getByRole('tab', { name: /web/ })).toBeVisible();
    const before = workspace.panes.bottom.tabs[0].id;

    dragOnto(await page.getByRole('tab', { name: /web/ }).element(), stripFor('Open views'));

    expect(workspace.panes.main.tabs[0].id).toBe(before);
    expect(workspace.isEditing(WEB)).toBe(true);
});

// The cluster tree is a pane's tab now, with one difference from the rest: no
// close button, because closing it would leave no way to open anything.
test('the cluster tree has no close button, unlike every other tab', async () => {
    render(Pane, { pane: 'left' });
    render(Pane, { pane: 'main' });
    workspace.openTab(PROD, 'pods');
    await expect.element(page.getByRole('tab', { name: /Clusters/ })).toBeVisible();

    expect(page.getByRole('button', { name: 'Close Clusters' }).elements()).toHaveLength(0);
    // ...while an ordinary tab beside it has one.
    await expect.element(page.getByRole('button', { name: 'Close Pods' })).toBeVisible();
});

test('the tree can still be dragged into another pane', async () => {
    render(Pane, { pane: 'left' });
    render(Pane, { pane: 'bottom' });
    await expect.element(page.getByRole('tab', { name: /Clusters/ })).toBeVisible();

    dragOnto(await page.getByRole('tab', { name: /Clusters/ }).element(), stripFor('Dock'));

    expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('bottom');
});


// The describe panel was the last thing on screen with a place of its own: it
// docked to an edge of the window, sized itself, and carried three buttons for
// moving between the only three edges it knew. It is a tab now, so it is drawn
// by whichever pane holds it and moved by the same drag as everything else.
const HT1 = { contextId: PROD, kind: 'nodes', namespace: '', name: 'ht1' };
const HT2 = { contextId: PROD, kind: 'nodes', namespace: '', name: 'ht2' };

test('the describe panel is drawn by the pane its tab is in', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'right' });

    await detail.open(HT1);

    await expect.element(page.getByRole('tab', { name: /ht1/ })).toBeVisible();
    // The report itself, in the right panel rather than in a panel of its own.
    const panel = document.querySelector('.pane.right .panel');
    expect(panel).not.toBeNull();
    expect(panel?.getAttribute('aria-label')).toBe('Node details');
});

test('describing another row retitles the tab instead of adding one', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'right' });
    await detail.open(HT1);
    await expect.element(page.getByRole('tab', { name: /ht1/ })).toBeVisible();

    await detail.open(HT2);

    await expect.element(page.getByRole('tab', { name: /ht2/ })).toBeVisible();
    expect(page.getByRole('tab', { name: /ht1/ }).elements()).toHaveLength(0);
    expect(workspace.panes.right.tabs).toHaveLength(1);
});

// What replaces the three dock buttons the panel used to carry.
test('the describe tab can be dragged to the foot of the window', async () => {
    render(Pane, { pane: 'right' });
    render(Pane, { pane: 'bottom' });
    await detail.open(HT1);
    await expect.element(page.getByRole('tab', { name: /ht1/ })).toBeVisible();

    dragOnto(await page.getByRole('tab', { name: /ht1/ }).element(), stripFor('Dock'));

    expect(workspace.paneOf(DETAILS_TAB_ID)).toBe('bottom');
    // And that is where the next row describes itself, not back on the right.
    detail.close();
    await detail.open(HT2);
    expect(workspace.paneOf(DETAILS_TAB_ID)).toBe('bottom');
});

// Unlike the cluster tree, it is an ordinary closable tab: it comes back the
// moment another row is selected, so a close button strands nobody.
test('the describe tab closes from its own close button', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'right' });
    await detail.open(HT1);

    await page.getByRole('button', { name: 'Close ht1' }).click();

    expect(detail.target).toBeNull();
    expect(workspace.paneOf(DETAILS_TAB_ID)).toBeNull();
});

// Where a tab can land is shown while it is in the air, and every one of those
// targets has to go away the moment it lands -- wherever it lands. A tab
// dropped into another pane leaves the strip it came from, and a drag source
// removed from the document fires no dragend; the panes that were not dropped
// on used to wait for exactly that dragend to put their targets away.
test('every drop target goes away once the tab has landed, whichever pane took it', async () => {
    render(Pane, { pane: 'left' });
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'bottom' });
    workspace.openTab(PROD, 'pods');
    await expect.element(page.getByRole('tab', { name: /Pods/ })).toBeVisible();
    const tab = await page.getByRole('tab', { name: /Pods/ }).element();
    const dataTransfer = new DataTransfer();

    tab.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer }));
    // The two panes it could go to offer themselves; the one it is in does not.
    await expect.poll(() => document.querySelectorAll('.dropzone').length).toBe(2);
    expect(document.querySelector('.pane.main .dropzone')).toBeNull();

    const strip = stripFor('Dock');
    strip.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer }));
    strip.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer }));
    // No dragend follows: the tab has left the document with the move.

    expect(workspace.panes.bottom.tabs.map((t) => t.kind)).toEqual(['pods']);
    await expect.poll(() => document.querySelectorAll('.dropzone').length).toBe(0);
    expect(document.querySelector('.pane.receiving')).toBeNull();
});

// Folded and empty, the dock is a strip a few pixels tall, most of it a hint
// about the details panel -- and the hint did not take a drop. That is the
// "where is the bottom area" anybody dragging a tab there for the first time
// asked. It opens some room to aim at while a tab is in the air, and every
// part of the strip takes the drop.
test('a folded dock opens a drop area while a tab is in the air, and its hint takes the drop', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'bottom' });
    workspace.openTab(PROD, 'pods');
    await expect.element(page.getByRole('tab', { name: /Pods/ })).toBeVisible();
    const body = document.querySelector('.pane.bottom .body') as HTMLElement;
    expect(body.getBoundingClientRect().height).toBe(0);
    const tab = await page.getByRole('tab', { name: /Pods/ }).element();
    const dataTransfer = new DataTransfer();

    tab.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer }));

    await expect.element(page.getByText('Drop to move to the dock')).toBeVisible();
    expect(body.getBoundingClientRect().height).toBeGreaterThan(0);

    const hint = page.getByText('Select a resource, then', { exact: false }).element();
    hint.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer }));
    hint.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer }));

    expect(workspace.panes.bottom.tabs.map((t) => t.kind)).toEqual(['pods']);
    await expect.poll(() => document.querySelectorAll('.dropzone').length).toBe(0);
});

// The mirror of the case above: a drag that ends on nothing -- let go over
// the table, or outside the window -- still puts every target away.
test('a drag let go on nothing puts every drop target away too', async () => {
    render(Pane, { pane: 'main' });
    render(Pane, { pane: 'bottom' });
    workspace.openTab(PROD, 'pods');
    await expect.element(page.getByRole('tab', { name: /Pods/ })).toBeVisible();
    const tab = await page.getByRole('tab', { name: /Pods/ }).element();
    const dataTransfer = new DataTransfer();

    tab.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer }));
    await expect.poll(() => document.querySelectorAll('.dropzone').length).toBe(1);
    tab.dispatchEvent(new DragEvent('dragend', { bubbles: true, cancelable: true, dataTransfer }));

    await expect.poll(() => document.querySelectorAll('.dropzone').length).toBe(0);
    expect(workspace.panes.main.tabs.map((t) => t.kind)).toEqual(['pods']);
});
