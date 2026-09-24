import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { detail } from './detail.svelte';

// The workspace talks to the Go side the moment it does anything, so the
// bindings are stubbed. What is under test is which tabs survive a close and
// where focus lands -- none of which involves a cluster.
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
    KubeconfigService: {
        Sync: vi.fn().mockResolvedValue([]),
        Files: vi.fn().mockResolvedValue([]),
        RemoveContext: vi.fn().mockResolvedValue([]),
    },
    ResourceService: {
        Describe: vi.fn().mockResolvedValue(''),
        Ping: vi.fn().mockResolvedValue(undefined),
        Disconnect: vi.fn().mockResolvedValue(undefined),
        CustomResourceKinds: vi.fn().mockResolvedValue([]),
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
        SetEnabled: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
        Known: vi.fn().mockResolvedValue([]),
        InstallKnown: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
        InstallFromGit: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
        HideSuggestion: vi.fn().mockResolvedValue({}),
        Probe: vi.fn().mockResolvedValue({ known: [], absent: [] }),
    },
    ThemeService: {
        List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }),
        Tokens: vi.fn().mockResolvedValue([]),
        RevealDir: vi.fn().mockResolvedValue(undefined),
        CreateExample: vi.fn().mockResolvedValue(''),
        AddFolder: vi.fn().mockResolvedValue({}),
        RemoveFolder: vi.fn().mockResolvedValue({}),
        BrowseForFolder: vi.fn().mockResolvedValue({}),
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
        SetPanes: vi.fn().mockResolvedValue({}),
        SetLayout: vi.fn().mockResolvedValue({}),
        SetPreferences: vi.fn().mockResolvedValue({}),
        SetContextPrefs: vi.fn().mockResolvedValue({}),
    },
}));

const {
    workspace,
    isSettingsTab,
    isAppTab,
    resourceTabId,
    tabIdFor,
    CLUSTERS_TAB_ID,
    DETAILS_TAB_ID,
    clustersTab,
} = await import('./workspace.svelte');
const { clusters } = await import('./health.svelte');
const { DEFAULT_PANE_SIZE } = await import('./panes');
const { columnKeys, MAX_COLUMN_WIDTH, MIN_COLUMN_WIDTH } = await import('../columns');
const { labelFor, iconFor } = await import('../catalogue');
const { changes } = await import('./changes.svelte');
const { views } = await import('./views');
const { SETTINGS, HELP, KUBERNETES } = await import('../catalogue');
const { notices } = await import('./notices.svelte');
const { session } = await import('./session.svelte');
const { ResourceService, KubeconfigService, SettingsService, ThemeService, PluginService, MetricsService, TerminalService } = await import(
    '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services',
);

const PROD = '/home/u/.kube/prod::admin@prod';
const STAGING = '/home/u/.kube/staging::admin@staging';

/** One saved collection tab, as the settings file holds it. */
function resource(contextId: string, kind: string, namespaces: string[] = []) {
    return { type: 'resource', contextId, kind, namespace: '', name: '', namespaces };
}

/** One saved editor tab, as the settings file holds it. A document tab is a
 *  view onto one object and carries no namespace filter. */
function document(contextId: string, name: string, namespace = 'default', kind = 'pods') {
    return { type: 'edit', contextId, kind, namespace, name, namespaces: [] };
}

/** Opens tabs in order and returns their ids. */
function open(...pairs: [string, string][]): string[] {
    for (const [contextId, kind] of pairs) {
        workspace.openTab(contextId, kind);
    }
    return workspace.tabs.map((t) => t.id);
}

beforeEach(() => {
    workspace.closeAllTabs();
    clusters.prune([]);
    vi.mocked(ResourceService.Ping).mockReset().mockResolvedValue(undefined);
    expect(workspace.tabs).toHaveLength(0);
});

describe('closeTab', () => {
    test('moves focus to the tab on the right', () => {
        const [, , third] = open([PROD, 'pods'], [PROD, 'nodes'], [PROD, 'services']);
        workspace.activateTab(workspace.tabs[1].id);

        workspace.closeTab(workspace.tabs[1].id);

        expect(workspace.tabs.map((t) => t.kind)).toEqual(['pods', 'services']);
        expect(workspace.activeTabId).toBe(third);
    });

    test('falls back to the left when the last tab closes', () => {
        const [first] = open([PROD, 'pods'], [PROD, 'nodes']);
        workspace.activateTab(workspace.tabs[1].id);

        workspace.closeTab(workspace.tabs[1].id);

        expect(workspace.activeTabId).toBe(first);
    });

    test('leaves focus alone when another tab closes', () => {
        const [first] = open([PROD, 'pods'], [PROD, 'nodes']);
        workspace.activateTab(first);

        workspace.closeTab(workspace.tabs[1].id);

        expect(workspace.activeTabId).toBe(first);
    });

    test('ignores a tab that is not open', () => {
        open([PROD, 'pods']);
        workspace.closeTab('nonsense');
        expect(workspace.tabs).toHaveLength(1);
    });
});

describe('closeOtherTabs', () => {
    test('keeps only the named tab and focuses it', () => {
        const [, second] = open([PROD, 'pods'], [PROD, 'nodes'], [STAGING, 'pods']);
        workspace.activateTab(workspace.tabs[0].id);

        workspace.closeOtherTabs(second);

        expect(workspace.tabs.map((t) => t.id)).toEqual([second]);
        expect(workspace.activeTabId).toBe(second);
    });

    test('scoped to a context, spares the other clusters', () => {
        open([PROD, 'pods'], [PROD, 'nodes'], [STAGING, 'pods'], [STAGING, 'nodes']);
        const keep = workspace.tabs[0].id;

        workspace.closeOtherTabs(keep, PROD);

        // Both staging tabs survive; only the other prod tab goes.
        expect(workspace.tabs.map((t) => `${t.contextId}#${t.kind}`)).toEqual([
            `${PROD}#pods`,
            `${STAGING}#pods`,
            `${STAGING}#nodes`,
        ]);
    });

    test('scoped close does not disturb focus on a spared cluster', () => {
        open([PROD, 'pods'], [PROD, 'nodes'], [STAGING, 'pods']);
        const stagingTab = workspace.tabs[2].id;
        workspace.activateTab(stagingTab);

        workspace.closeOtherTabs(workspace.tabs[0].id, PROD);

        expect(workspace.activeTabId).toBe(stagingTab);
    });
});

describe('closeAllTabs', () => {
    test('empties the strip and clears the active tab', () => {
        open([PROD, 'pods'], [STAGING, 'nodes']);

        workspace.closeAllTabs();

        expect(workspace.tabs).toEqual([]);
        expect(workspace.activeTabId).toBeNull();
    });

    test('scoped to a context, closes only that cluster', () => {
        open([PROD, 'pods'], [PROD, 'nodes'], [STAGING, 'pods']);

        workspace.closeAllTabs(PROD);

        expect(workspace.tabs.map((t) => t.contextId)).toEqual([STAGING]);
        expect(workspace.activeTabId).toBe(workspace.tabs[0].id);
    });

    test('closing a cluster whose tabs are all inactive leaves focus alone', () => {
        open([STAGING, 'pods'], [PROD, 'pods']);
        const stagingTab = workspace.tabs[0].id;
        workspace.activateTab(stagingTab);

        workspace.closeAllTabs(PROD);

        expect(workspace.activeTabId).toBe(stagingTab);
    });

    test('does nothing when the context has no tabs open', () => {
        open([PROD, 'pods']);
        const before = workspace.tabs.map((t) => t.id);

        workspace.closeAllTabs(STAGING);

        expect(workspace.tabs.map((t) => t.id)).toEqual(before);
    });
});

describe('openTab placement', () => {
    test('opens immediately right of the selected tab', () => {
        open([PROD, 'pods'], [PROD, 'nodes'], [PROD, 'services']);
        workspace.activateTab(workspace.tabs[0].id); // back to pods

        workspace.openTab(PROD, 'events');

        expect(workspace.tabs.map((t) => t.kind)).toEqual(['pods', 'events', 'nodes', 'services']);
    });

    test('the new tab becomes the selected one', () => {
        open([PROD, 'pods'], [PROD, 'nodes']);
        workspace.activateTab(workspace.tabs[0].id);

        workspace.openTab(PROD, 'events');

        expect(workspace.activeTabId).toBe(workspace.tabs[1].id);
        expect(workspace.tabs[1].kind).toBe('events');
    });

    test('a run of opens keeps the order they were opened in', () => {
        // Each new tab becomes selected, so inserting to its right leaves the
        // strip reading oldest to newest rather than reversing it.
        open([PROD, 'pods'], [PROD, 'nodes'], [PROD, 'services']);

        expect(workspace.tabs.map((t) => t.kind)).toEqual(['pods', 'nodes', 'services']);
    });

    test('appends when nothing is selected', () => {
        open([PROD, 'pods'], [PROD, 'nodes']);
        workspace.activateTab(workspace.tabs[0].id);
        workspace.closeAllTabs();

        workspace.openTab(PROD, 'services');
        // Deactivate without closing, the state a restored session starts in.
        workspace.panes.main.activeId = null;
        workspace.openTab(PROD, 'events');

        expect(workspace.tabs.map((t) => t.kind)).toEqual(['services', 'events']);
    });

    test('re-opening a tab that is already open only focuses it', () => {
        open([PROD, 'pods'], [PROD, 'nodes'], [PROD, 'services']);
        const before = workspace.tabs.map((t) => t.id);
        workspace.activateTab(workspace.tabs[0].id);

        workspace.openTab(PROD, 'services');

        expect(workspace.tabs.map((t) => t.id)).toEqual(before);
        expect(workspace.activeTabId).toBe(before[2]);
    });

    test('opens beside the selected tab even when it belongs to another cluster', () => {
        open([PROD, 'pods'], [PROD, 'nodes']);
        workspace.activateTab(workspace.tabs[0].id);

        workspace.openTab(STAGING, 'pods');

        expect(workspace.tabs.map((t) => `${t.contextId}#${t.kind}`)).toEqual([
            `${PROD}#pods`,
            `${STAGING}#pods`,
            `${PROD}#nodes`,
        ]);
    });
});

describe('health', () => {
    /** A promise whose settling this test controls, to observe the in-flight state. */
    function withheld<T = undefined>() {
        let settle!: (value: T) => void;
        let fail!: (reason: unknown) => void;
        const promise = new Promise<T>((resolve, reject) => {
            settle = resolve;
            fail = reject;
        });
        return { promise, settle, fail };
    }

    test('a context nobody has looked at has no status', () => {
        expect(clusters.of(PROD).status).toBe('unknown');
    });

    test('a probe in flight reads as checking', async () => {
        const gate = withheld();
        vi.mocked(ResourceService.Ping).mockReturnValueOnce(gate.promise as never);

        const probing = clusters.probe(PROD);
        expect(clusters.of(PROD).status).toBe('checking');

        gate.settle(undefined);
        await probing;
    });

    test('a cluster that answers is connected', async () => {
        await clusters.probe(PROD);

        expect(clusters.of(PROD).status).toBe('connected');
        expect(clusters.of(PROD).message).toBe('');
    });

    test('a cluster that refuses is in error, and keeps the reason', async () => {
        vi.mocked(ResourceService.Ping).mockRejectedValueOnce(new Error('dial tcp: connection refused'));

        await clusters.probe(PROD);

        expect(clusters.of(PROD).status).toBe('error');
        expect(clusters.of(PROD).message).toBe('dial tcp: connection refused');
    });

    test('probing a context whose status is already known does not ask again', async () => {
        await clusters.probe(PROD);
        await clusters.probe(PROD);

        expect(ResourceService.Ping).toHaveBeenCalledTimes(1);
    });

    test('a forced probe asks again, so refresh can recheck', async () => {
        await clusters.probe(PROD);
        await clusters.probe(PROD, { force: true });

        expect(ResourceService.Ping).toHaveBeenCalledTimes(2);
    });

    test('two probes racing on one context only produce one request', async () => {
        const gate = withheld();
        vi.mocked(ResourceService.Ping).mockReturnValueOnce(gate.promise as never);

        const first = clusters.probe(PROD);
        const second = clusters.probe(PROD);
        gate.settle(undefined);
        await Promise.all([first, second]);

        expect(ResourceService.Ping).toHaveBeenCalledTimes(1);
    });

    test('a tab reporting a failure turns the indicator red without a second request', () => {
        clusters.report(PROD, 'error', 'the server could not find the requested resource');

        expect(clusters.of(PROD).status).toBe('error');
        expect(clusters.of(PROD).message).toBe('the server could not find the requested resource');
        expect(ResourceService.Ping).not.toHaveBeenCalled();
    });

    test('a tab that loads reports the context as connected', () => {
        clusters.report(PROD, 'connected');

        expect(clusters.of(PROD).status).toBe('connected');
    });

    test('a tab outcome overrides an earlier probe, being the newer evidence', async () => {
        await clusters.probe(PROD);
        expect(clusters.of(PROD).status).toBe('connected');

        clusters.report(PROD, 'error', 'connection refused');

        expect(clusters.of(PROD).status).toBe('error');
    });

    test('opening a tab probes the cluster it belongs to', () => {
        workspace.openTab(PROD, 'pods');

        expect(ResourceService.Ping).toHaveBeenCalledWith(PROD);
    });

    test('selecting a context in the sidebar probes it', () => {
        workspace.selectContext(STAGING);

        expect(ResourceService.Ping).toHaveBeenCalledWith(STAGING);
    });

    test('a context that leaves the kubeconfig loses its status', async () => {
        await clusters.probe(PROD);
        expect(clusters.of(PROD).status).toBe('connected');

        // Sync is mocked to find nothing, so every context has gone.
        await workspace.sync();

        expect(clusters.of(PROD).status).toBe('unknown');
    });
});

describe('expanding and collapsing every context', () => {
    /** Puts two contexts on the store, since expandAll works off the real list. */
    function seed(): void {
        workspace.files = [
            {
                path: '/home/u/.kube/prod',
                source: 'manual',
                error: '',
                contexts: [
                    {
                        id: PROD,
                        name: 'admin@prod',
                        cluster: 'prod',
                        user: 'admin',
                        namespace: '',
                        server: '',
                        file: '/home/u/.kube/prod',
                        current: false,
                    },
                ],
            },
            {
                path: '/home/u/.kube/staging',
                source: 'manual',
                error: '',
                contexts: [
                    {
                        id: STAGING,
                        name: 'admin@staging',
                        cluster: 'staging',
                        user: 'admin',
                        namespace: '',
                        server: '',
                        file: '/home/u/.kube/staging',
                        current: false,
                    },
                ],
            },
        ];
    }

    test('expandAll opens every context in the sidebar', () => {
        seed();
        workspace.collapseAll();

        workspace.expandAll();

        expect(workspace.isExpanded(PROD)).toBe(true);
        expect(workspace.isExpanded(STAGING)).toBe(true);
    });

    // The whole reason probing is lazy is that building a client can run an
    // exec credential plugin. Unfolding the tree must not be a way to set
    // twenty of those going at once.
    test('expandAll probes nothing, because unfolding is not touching a cluster', () => {
        seed();
        workspace.collapseAll();

        workspace.expandAll();

        expect(ResourceService.Ping).not.toHaveBeenCalled();
    });

    test('collapseAll closes everything', () => {
        seed();
        workspace.expandAll();

        workspace.collapseAll();

        expect(workspace.isExpanded(PROD)).toBe(false);
        expect(workspace.isExpanded(STAGING)).toBe(false);
        expect(workspace.expanded).toEqual([]);
    });

    test('anyExpanded drives which way the toggle goes', () => {
        seed();
        workspace.collapseAll();
        expect(workspace.anyExpanded).toBe(false);

        workspace.expandAll();
        expect(workspace.anyExpanded).toBe(true);
    });

    test('one open context is enough for the toggle to offer collapsing', () => {
        seed();
        workspace.collapseAll();

        workspace.toggleExpanded(PROD);

        expect(workspace.anyExpanded).toBe(true);
    });

    test('expandAll leaves contexts that are already open alone', () => {
        seed();
        workspace.collapseAll();
        workspace.toggleExpanded(PROD);

        workspace.expandAll();

        // No duplicate entry for the one that was already open.
        expect(workspace.expanded.filter((id) => id === PROD)).toHaveLength(1);
    });
});

describe('revealing the context a tab belongs to', () => {
    test('activating a tab asks for its context to be revealed', () => {
        open([PROD, 'pods']);
        workspace.openTab(STAGING, 'nodes');

        workspace.activateTab(resourceTabId(PROD, 'pods'));

        expect(workspace.reveal?.contextId).toBe(PROD);
    });

    // Clicking the tab you are already on is how you ask "where is this
    // cluster?", so it has to reveal again rather than do nothing.
    test('re-activating the same tab reveals again', () => {
        open([PROD, 'pods']);
        workspace.activateTab(resourceTabId(PROD, 'pods'));
        const first = workspace.reveal?.nonce;

        workspace.activateTab(resourceTabId(PROD, 'pods'));

        expect(workspace.reveal?.nonce).not.toBe(first);
        expect(workspace.reveal?.contextId).toBe(PROD);
    });

    // Selecting in the sidebar means the user is already pointing at the row;
    // scrolling it would move what is under their cursor.
    test('selecting a context in the sidebar does not ask for a reveal', () => {
        workspace.reveal = null;

        workspace.selectContext(STAGING);

        expect(workspace.reveal).toBeNull();
    });

    test('the nonce climbs, so consecutive reveals are always distinguishable', () => {
        open([PROD, 'pods'], [STAGING, 'pods']);
        const seen = new Set<number>();

        for (const tab of [
            resourceTabId(PROD, 'pods'),
            resourceTabId(STAGING, 'pods'),
            resourceTabId(PROD, 'pods'),
        ]) {
            workspace.activateTab(tab);
            seen.add(workspace.reveal!.nonce);
        }

        expect(seen.size).toBe(3);
    });
});

describe('the default folding', () => {
    beforeEach(() => {
        workspace.settings.contexts = {};
    });

    test('the specialist groups start folded on a fresh install', () => {
        workspace.settings.layout.collapsedGroups = null;

        expect(workspace.isGroupCollapsed(PROD, 'Gateway API')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Admission')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Scheduling')).toBe(true);
    });

    // Everything starts folded now: expanding a context shows the dashboard and
    // a list of headings, and you open the one you are after.
    test('every section starts folded, not just the specialist ones', () => {
        workspace.settings.layout.collapsedGroups = null;

        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Cluster')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Config')).toBe(true);
    });

    // The distinction the store goes to trouble to preserve: an empty list is a
    // choice, and re-applying the defaults over it would undo the user's work
    // every time they restarted.
    test('an explicitly empty list means nothing is folded, not "use the defaults"', () => {
        workspace.settings.layout.collapsedGroups = [];

        expect(workspace.isGroupCollapsed(PROD, 'Gateway API')).toBe(false);
        expect(workspace.isGroupCollapsed(PROD, 'Admission')).toBe(false);
    });

    test('a stored list is used exactly as it is', () => {
        workspace.settings.layout.collapsedGroups = ['Workloads'];

        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Gateway API')).toBe(false);
    });

    test('toggling folds an open group', () => {
        workspace.settings.layout.collapsedGroups = [];

        workspace.toggleGroup(PROD, 'Workloads');

        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(true);
    });

    test('toggling unfolds a folded group', () => {
        workspace.settings.layout.collapsedGroups = ['Workloads'];

        workspace.toggleGroup(PROD, 'Workloads');

        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(false);
    });

});

describe('per-context folding', () => {
    beforeEach(() => {
        workspace.settings.layout.collapsedGroups = ['Gateway API'];
        workspace.settings.contexts = {};
    });

    test('a context nobody has folded anything in follows the defaults', () => {
        expect(workspace.isGroupCollapsed(PROD, 'Gateway API')).toBe(true);
        expect(workspace.isGroupCollapsed(STAGING, 'Gateway API')).toBe(true);
    });

    // The whole point: folding a section is about the cluster you are looking
    // at, not about every cluster you have.
    test('folding a group changes only the context it was folded in', () => {
        workspace.toggleGroup(PROD, 'Network');

        expect(workspace.isGroupCollapsed(PROD, 'Network')).toBe(true);
        expect(workspace.isGroupCollapsed(STAGING, 'Network')).toBe(false);
    });

    test('unfolding a group changes only that context too', () => {
        workspace.toggleGroup(PROD, 'Gateway API');

        expect(workspace.isGroupCollapsed(PROD, 'Gateway API')).toBe(false);
        expect(workspace.isGroupCollapsed(STAGING, 'Gateway API')).toBe(true);
    });

    // A context that has been folded in keeps what it was given, even if it
    // happens to agree with the defaults: it has its own answer now, and a
    // later "apply everywhere" elsewhere should not quietly move it.
    test('a context keeps its own folding once it has one', () => {
        workspace.toggleGroup(PROD, 'Network');
        workspace.toggleGroup(PROD, 'Network');

        expect(workspace.hasFoldingOverride(PROD)).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Network')).toBe(false);
    });

    test('each context can end up folded differently', () => {
        // Baseline folds only Gateway API, so each toggle here folds something
        // new in one context and leaves the other where it was.
        workspace.toggleGroup(PROD, 'Admission');
        workspace.toggleGroup(STAGING, 'Workloads');

        expect(workspace.isGroupCollapsed(PROD, 'Admission')).toBe(true);
        expect(workspace.isGroupCollapsed(STAGING, 'Admission')).toBe(false);
        expect(workspace.isGroupCollapsed(STAGING, 'Workloads')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(false);
    });

    test('folding survives beside an alias and colour', () => {
        workspace.setContextPrefs(PROD, 'Prod', '#ff0000');

        workspace.toggleGroup(PROD, 'Admission');

        expect(workspace.settings.contexts[PROD].alias).toBe('Prod');
        expect(workspace.settings.contexts[PROD].color).toBe('#ff0000');
        expect(workspace.hasFoldingOverride(PROD)).toBe(true);
    });

    describe('applying one to every cluster', () => {
        test('sets the shared default and brings every context into line', () => {
            workspace.toggleGroup(PROD, 'Workloads');
            workspace.toggleGroup(STAGING, 'Network');

            workspace.toggleGroup(PROD, 'Admission', { allContexts: true });

            // Every context now shows the same thing, including the one that
            // had been folded differently a moment ago.
            expect(workspace.isGroupCollapsed(PROD, 'Admission')).toBe(true);
            expect(workspace.isGroupCollapsed(STAGING, 'Admission')).toBe(true);
            expect(workspace.isGroupCollapsed(STAGING, 'Network')).toBe(false);
        });

        test('clears the per-context folding, so nothing is left disagreeing', () => {
            workspace.toggleGroup(PROD, 'Workloads');
            expect(workspace.hasFoldingOverride(PROD)).toBe(true);

            workspace.toggleGroup(PROD, 'Network', { allContexts: true });

            expect(workspace.hasFoldingOverride(PROD)).toBe(false);
            expect(workspace.hasFoldingOverride(STAGING)).toBe(false);
        });

        test('reaches contexts that do not exist yet', () => {
            workspace.toggleGroup(PROD, 'Network', { allContexts: true });

            expect(workspace.isGroupCollapsed('/new/config::admin@new', 'Network')).toBe(true);
        });
    });
});

describe('zoom', () => {
    beforeEach(() => {
        workspace.settings.layout.zoom = 1;
    });

    test('starts at normal size', () => {
        expect(workspace.zoom).toBe(1);
    });

    test('zooming in and out steps the scale', () => {
        workspace.zoomIn();
        expect(workspace.zoom).toBeGreaterThan(1);

        workspace.zoomOut();
        expect(workspace.zoom).toBe(1);
    });

    test('reset returns to normal size from either direction', () => {
        workspace.zoomIn();
        workspace.zoomIn();
        workspace.resetZoom();
        expect(workspace.zoom).toBe(1);

        workspace.zoomOut();
        workspace.resetZoom();
        expect(workspace.zoom).toBe(1);
    });

    test('will not zoom out past the point the title bar stops fitting', () => {
        for (let i = 0; i < 40; i++) workspace.zoomOut();
        expect(workspace.zoom).toBeGreaterThanOrEqual(0.5);
    });

    test('will not zoom in without limit', () => {
        for (let i = 0; i < 40; i++) workspace.zoomIn();
        expect(workspace.zoom).toBeLessThanOrEqual(2);
    });

    test('setting the scale directly is clamped the same way', () => {
        // The settings view's slider and presets go through this, so it must
        // not be a way around the bounds the steppers respect.
        workspace.setZoom(9);
        expect(workspace.zoom).toBe(workspace.maxZoom);

        workspace.setZoom(0.01);
        expect(workspace.zoom).toBe(workspace.minZoom);

        workspace.setZoom(1.25);
        expect(workspace.zoom).toBe(1.25);
    });
});

describe('showing the section an activated tab lives in', () => {
    beforeEach(() => {
        workspace.closeAllTabs();
        workspace.settings.layout.collapsedGroups = ['Admission'];
        workspace.settings.contexts = {};
    });

    test('activating a tab unfolds the section its resource is listed under', () => {
        workspace.openTab(PROD, 'mutatingwebhookconfigurations');
        workspace.openTab(PROD, 'pods');
        // Fold it back with the tab already open, then return to that tab.
        workspace.toggleGroup(PROD, 'Admission');
        expect(workspace.isGroupCollapsed(PROD, 'Admission')).toBe(true);

        workspace.activateTab(resourceTabId(PROD, 'mutatingwebhookconfigurations'));

        expect(workspace.isGroupCollapsed(PROD, 'Admission')).toBe(false);
    });

    test('it unfolds only for the context the tab belongs to', () => {
        workspace.openTab(PROD, 'mutatingwebhookconfigurations');
        workspace.openTab(PROD, 'pods');
        workspace.toggleGroup(PROD, 'Admission');

        workspace.activateTab(resourceTabId(PROD, 'mutatingwebhookconfigurations'));

        expect(workspace.isGroupCollapsed(STAGING, 'Admission')).toBe(true);
    });

    // Activating a tab in a section that is already open must not quietly give
    // the context its own folding, or every tab click would pin it.
    test('a tab in an open section leaves the folding alone', () => {
        workspace.openTab(PROD, 'pods');

        workspace.activateTab(resourceTabId(PROD, 'pods'));

        expect(workspace.hasFoldingOverride(PROD)).toBe(false);
    });

    test('a custom resource tab is harmless, having no section', () => {
        workspace.openTab(PROD, 'crd:certificates.cert-manager.io');

        expect(() => workspace.activateTab(resourceTabId(PROD, 'crd:certificates.cert-manager.io'))).not.toThrow();
        expect(workspace.hasFoldingOverride(PROD)).toBe(false);
    });

    // The dashboard sits outside the sections, so activating its tab has
    // nothing to unfold -- and must not invent a folding for the context.
    test('the dashboard needs no section opened for it', () => {
        workspace.openTab(PROD, 'dashboard');
        workspace.openTab(PROD, 'pods');

        workspace.activateTab(resourceTabId(PROD, 'dashboard'));

        expect(workspace.hasFoldingOverride(PROD)).toBe(false);
    });
});

describe('folding every section of one context at once', () => {
    beforeEach(() => {
        workspace.settings.layout.collapsedGroups = [];
        workspace.settings.contexts = {};
    });

    test('collapsing shuts every section for that context', () => {
        workspace.collapseAllGroups(PROD);

        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(true);
        expect(workspace.isGroupCollapsed(PROD, 'Network')).toBe(true);
        expect(workspace.anyGroupOpen(PROD)).toBe(false);
    });

    test('expanding opens every section for that context', () => {
        workspace.collapseAllGroups(PROD);

        workspace.expandAllGroups(PROD);

        expect(workspace.isGroupCollapsed(PROD, 'Workloads')).toBe(false);
        expect(workspace.anyGroupOpen(PROD)).toBe(true);
    });

    test('it is one context at a time, like folding a single section', () => {
        workspace.collapseAllGroups(PROD);

        expect(workspace.isGroupCollapsed(STAGING, 'Workloads')).toBe(false);
    });

    test('anyGroupOpen reports whether there is anything to collapse', () => {
        workspace.collapseAllGroups(PROD);
        expect(workspace.anyGroupOpen(PROD)).toBe(false);

        workspace.toggleGroup(PROD, 'Network');
        expect(workspace.anyGroupOpen(PROD)).toBe(true);
    });
});

describe('the custom resource definitions a cluster serves', () => {
    const GROUPS = [
        { group: 'cert-manager.io', kinds: [
            { kind: 'crd:certificates.cert-manager.io', label: 'Certificate', group: 'cert-manager.io', plural: 'certificates', scoped: true },
        ] },
        { group: 'vitistack.io', kinds: [
            { kind: 'crd:machines.vitistack.io', label: 'Machine', group: 'vitistack.io', plural: 'machines', scoped: true },
        ] },
    ];

    beforeEach(() => {
        workspace.customKinds = {};
        vi.mocked(ResourceService.CustomResourceKinds).mockReset().mockResolvedValue(GROUPS as never);
    });

    test('a context nobody has opened the section for has asked for nothing', () => {
        expect(workspace.customKindsFor(PROD).status).toBe('idle');
        expect(ResourceService.CustomResourceKinds).not.toHaveBeenCalled();
    });

    test('loading reports progress and then the groups', async () => {
        const loading = workspace.loadCustomKinds(PROD);
        expect(workspace.customKindsFor(PROD).status).toBe('loading');

        await loading;

        expect(workspace.customKindsFor(PROD).status).toBe('ready');
        expect(workspace.customKindsFor(PROD).groups.map((g) => g.group)).toEqual([
            'cert-manager.io',
            'vitistack.io',
        ]);
    });

    test('a failure is kept so the section can explain itself', async () => {
        vi.mocked(ResourceService.CustomResourceKinds).mockRejectedValueOnce(new Error('forbidden'));

        await workspace.loadCustomKinds(PROD);

        expect(workspace.customKindsFor(PROD).status).toBe('error');
        expect(workspace.customKindsFor(PROD).message).toBe('forbidden');
    });

    // Opening and closing the section repeatedly must not re-ask the cluster.
    test('a context already loaded is not asked again', async () => {
        await workspace.loadCustomKinds(PROD);
        await workspace.loadCustomKinds(PROD);

        expect(ResourceService.CustomResourceKinds).toHaveBeenCalledTimes(1);
    });

    test('a forced reload asks again, so a newly installed operator shows up', async () => {
        await workspace.loadCustomKinds(PROD);
        await workspace.loadCustomKinds(PROD, { force: true });

        expect(ResourceService.CustomResourceKinds).toHaveBeenCalledTimes(2);
    });

    test('each context is loaded separately', async () => {
        await workspace.loadCustomKinds(PROD);

        expect(workspace.customKindsFor(STAGING).status).toBe('idle');
    });

    // Without this a failure is permanent: the section keys off state, and an
    // errored context is not idle, so nothing would ask again.
    test('a sync re-reads the definitions of contexts that had them', async () => {
        vi.mocked(ResourceService.CustomResourceKinds).mockRejectedValueOnce(new Error('unreachable'));
        await workspace.loadCustomKinds(PROD);
        expect(workspace.customKindsFor(PROD).status).toBe('error');

        vi.mocked(KubeconfigService.Sync).mockResolvedValueOnce([
            { path: '/c', source: 'manual', error: '', contexts: [{ id: PROD, name: 'p', cluster: 'c', user: 'u', namespace: '', server: '', file: '/c', current: false }] },
        ] as never);
        await workspace.sync();
        await vi.waitFor(() => expect(workspace.customKindsFor(PROD).status).toBe('ready'));
    });

    test('a sync does not start reading for contexts nobody asked about', async () => {
        vi.mocked(KubeconfigService.Sync).mockResolvedValueOnce([
            { path: '/c', source: 'manual', error: '', contexts: [{ id: STAGING, name: 's', cluster: 'c', user: 'u', namespace: '', server: '', file: '/c', current: false }] },
        ] as never);

        await workspace.sync();

        expect(ResourceService.CustomResourceKinds).not.toHaveBeenCalled();
    });

    test('a context that leaves the kubeconfig loses what was loaded for it', async () => {
        await workspace.loadCustomKinds(PROD);
        expect(workspace.customKindsFor(PROD).status).toBe('ready');

        await workspace.sync();

        expect(workspace.customKindsFor(PROD).status).toBe('idle');
    });
});

describe('expanding an API group inside the definitions section', () => {
    beforeEach(() => {
        workspace.expandedApiGroups = [];
    });

    test('groups start closed', () => {
        expect(workspace.isApiGroupExpanded(PROD, 'vitistack.io')).toBe(false);
    });

    test('opening one leaves the others closed', () => {
        workspace.toggleApiGroup(PROD, 'vitistack.io');

        expect(workspace.isApiGroupExpanded(PROD, 'vitistack.io')).toBe(true);
        expect(workspace.isApiGroupExpanded(PROD, 'cert-manager.io')).toBe(false);
    });

    test('the same group in another context is its own', () => {
        workspace.toggleApiGroup(PROD, 'vitistack.io');

        expect(workspace.isApiGroupExpanded(STAGING, 'vitistack.io')).toBe(false);
    });

    test('toggling again closes it', () => {
        workspace.toggleApiGroup(PROD, 'vitistack.io');
        workspace.toggleApiGroup(PROD, 'vitistack.io');

        expect(workspace.isApiGroupExpanded(PROD, 'vitistack.io')).toBe(false);
    });
});

describe('clicking a context name', () => {
    beforeEach(() => {
        workspace.expanded = [];
        workspace.selectedContextId = null;
    });

    test('opens a closed context and selects it', () => {
        workspace.activateContext(PROD);

        expect(workspace.isExpanded(PROD)).toBe(true);
        expect(workspace.selectedContextId).toBe(PROD);
    });

    // The complaint this fixes: a second click did nothing at all.
    test('clicking the one already open closes it again', () => {
        workspace.activateContext(PROD);

        workspace.activateContext(PROD);

        expect(workspace.isExpanded(PROD)).toBe(false);
    });

    test('closing it does not deselect it, so its settings stay put', () => {
        workspace.activateContext(PROD);

        workspace.activateContext(PROD);

        expect(workspace.selectedContextId).toBe(PROD);
    });

    test('it opens again on the next click', () => {
        workspace.activateContext(PROD);
        workspace.activateContext(PROD);

        workspace.activateContext(PROD);

        expect(workspace.isExpanded(PROD)).toBe(true);
    });

    // Clicking a different context means "show me this one", never "fold it":
    // folding what you just reached for would be the opposite of the intent.
    test('switching to another open context selects it rather than closing it', () => {
        workspace.activateContext(PROD);
        workspace.activateContext(STAGING);
        expect(workspace.isExpanded(STAGING)).toBe(true);

        // PROD is still open but no longer selected; clicking it should select.
        workspace.activateContext(PROD);

        expect(workspace.isExpanded(PROD)).toBe(true);
        expect(workspace.selectedContextId).toBe(PROD);
    });
});

describe('the settings tab', () => {
    /** One context on disk, so the sync-driven pruning has something to keep. */
    function seedProd(): void {
        workspace.files = [
            {
                path: '/home/u/.kube/prod',
                source: 'manual',
                error: '',
                contexts: [
                    {
                        id: PROD,
                        name: 'admin@prod',
                        cluster: 'prod',
                        user: 'admin',
                        namespace: '',
                        server: '',
                        file: '/home/u/.kube/prod',
                        current: false,
                    },
                ],
            },
        ];
    }

    beforeEach(() => {
        workspace.files = [];
    });

    test('opens once, however many times it is asked for', () => {
        workspace.openSettings();
        workspace.openSettings();

        expect(workspace.tabs.filter((t) => t.kind === SETTINGS)).toHaveLength(1);
        expect(workspace.activeTab?.kind).toBe(SETTINGS);
    });

    test('goes to the end of the strip rather than beside the current tab', () => {
        open([PROD, 'pods'], [PROD, 'nodes']);
        workspace.activateTab(workspace.tabs[0].id);

        workspace.openSettings();

        expect(workspace.tabs.at(-1)?.kind).toBe(SETTINGS);
    });

    test('carries no context, so it is not painted as a cluster', () => {
        workspace.openSettings();
        const tab = workspace.tabs.find((t) => t.kind === SETTINGS);

        expect(tab?.contextId).toBe('');
        expect(isSettingsTab(tab!)).toBe(true);
        // Distinct from what any real context would be given.
        expect(workspace.colorOf('')).not.toBe(workspace.colorOf(PROD));
    });

    test('activating it leaves the sidebar on the context it was showing', () => {
        seedProd();
        open([PROD, 'pods']);
        expect(workspace.selectedContextId).toBe(PROD);

        workspace.openSettings();

        expect(workspace.activeTab?.kind).toBe(SETTINGS);
        expect(workspace.selectedContextId).toBe(PROD);
    });

    test('survives a sync that drops every cluster', async () => {
        seedProd();
        open([PROD, 'pods']);
        workspace.openSettings();

        // Every kubeconfig has gone.
        vi.mocked(KubeconfigService.Sync).mockResolvedValueOnce([]);
        await workspace.sync();

        expect(workspace.tabs.map((t) => t.kind)).toEqual([SETTINGS]);
    });

    test('closes with "close all tabs", but not with a context-scoped close', () => {
        seedProd();
        open([PROD, 'pods']);
        workspace.openSettings();

        workspace.closeAllTabs(PROD);
        expect(workspace.tabs.map((t) => t.kind)).toEqual([SETTINGS]);

        workspace.closeAllTabs();
        expect(workspace.tabs).toHaveLength(0);
    });
});

describe('preferences', () => {
    beforeEach(() => {
        workspace.settings.preferences = {
            theme: 'k8sdockside-dark',
            density: 'comfortable',
            restoreTabs: true,
            confirmSourceRemoval: false,
            showKubeconfigNames: false,
            contextSort: 'name',
            showLineNumbers: true,
            checkForUpdates: true,
            desktopNotifications: true,
            dateTime: { clock: 'system', dates: 'system', zone: 'local', ages: 'relative' },
            metricsRange: 60,
            terminal: {
                mode: 'app',
                external: '',
                shells: ['bash', 'sh'],
                nodeImage: 'busybox',
                nodeNamespace: 'default',
                fontSize: 12,
                scrollback: 5000,
            },
            helm: { path: '', wait: false, atomic: false, timeoutSeconds: 300 },
            background: { source: 'builtin', pinned: '', minutes: 15, palette: 'varied' },
        };
    });

    test('the chosen theme is stored by id', () => {
        workspace.setTheme('nord');
        expect(workspace.theme).toBe('nord');
    });

    test('turning tab restore off is a choice that sticks', () => {
        workspace.setRestoreTabs(false);
        // `??` rather than `||` on the way in: false must not read as unset.
        expect(workspace.restoreTabsOnLaunch).toBe(false);
    });
});

describe('restoring tabs at launch', () => {
    beforeEach(() => {
        workspace.files = [
            {
                path: '/home/u/.kube/prod',
                source: 'manual',
                error: '',
                contexts: [
                    {
                        id: PROD,
                        name: 'admin@prod',
                        cluster: 'prod',
                        user: 'admin',
                        namespace: '',
                        server: '',
                        file: '/home/u/.kube/prod',
                        current: false,
                    },
                ],
            },
        ];
        vi.mocked(KubeconfigService.Sync).mockResolvedValue(workspace.files);
    });

    test('reopens last session, settings tab included', async () => {
        workspace.settings.panes.main.tabs = [resource(PROD, 'pods'), resource('', SETTINGS)];
        workspace.settings.preferences.restoreTabs = true;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.tabs.map((t) => t.kind)).toEqual(['pods', SETTINGS]);
    });

    test('drops a remembered tab whose cluster has gone, but keeps settings', async () => {
        workspace.settings.panes.main.tabs = [resource(STAGING, 'pods'), resource('', SETTINGS)];
        workspace.settings.preferences.restoreTabs = true;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.tabs.map((t) => t.kind)).toEqual([SETTINGS]);
    });

    test('turned off, it starts empty but leaves the remembered order alone', async () => {
        const order = [resource(PROD, 'pods')];
        workspace.settings.panes.main.tabs = order;
        workspace.settings.preferences.restoreTabs = false;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.tabs).toHaveLength(0);
        // The order is what turning it back on has to restore, so the launch
        // that skipped it must not have overwritten it.
        expect(workspace.settings.panes.main.tabs).toEqual(order);
    });

    test('a settings tab restored first does not select a context', async () => {
        workspace.settings.panes.main.tabs = [resource('', SETTINGS)];
        workspace.settings.preferences.restoreTabs = true;
        workspace.selectedContextId = null;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.activeTab?.kind).toBe(SETTINGS);
        // ensureSelection may still pick one for the sidebar; what must not
        // happen is the settings tab claiming the empty id as a context.
        expect(workspace.selectedContextId).not.toBe('');
    });
});

describe('the dock', () => {
    /** The identity of one object, as the detail panel hands it over. */
    function object(contextId: string, name: string, namespace = 'default', kind = 'pods') {
        return { contextId, kind, namespace, name };
    }

    beforeEach(() => {
        workspace.closeAllDockTabs();
        workspace.settings.panes.bottom.open = false;
        expect(workspace.dockTabs).toHaveLength(0);
    });

    describe('a shell, with the preference saying "in my terminal"', () => {
        const DESKTOP = session.info;

        beforeEach(() => {
            vi.mocked(TerminalService.Launch).mockClear();
            workspace.settings.preferences.terminal.mode = 'external';
        });

        afterEach(() => {
            workspace.settings.preferences.terminal.mode = 'app';
            session.info = DESKTOP;
        });

        test('opens in the desktop app\'s own terminal emulator', () => {
            workspace.openShell(object(PROD, 'web'));

            expect(TerminalService.Launch).toHaveBeenCalledOnce();
            expect(workspace.dockTabs).toHaveLength(0);
        });

        // The user's own terminal would be one on the server, where nobody is
        // looking -- and the preference may well have been set on the desktop.
        test('opens in the dock in the web version, whatever the preference says', () => {
            session.info = { ...DESKTOP, server: true };

            workspace.openShell(object(PROD, 'web'));

            expect(TerminalService.Launch).not.toHaveBeenCalled();
            expect(workspace.activeDockTab?.view).toBe('shell');
            expect(workspace.activeDockTab?.name).toBe('web');
        });
    });

    test('editing an object opens it, focuses it and unfolds the dock', () => {
        workspace.openEditor(object(PROD, 'web'));

        expect(workspace.dockTabs.map((t) => t.title)).toEqual(['web']);
        expect(workspace.activeDockTab?.name).toBe('web');
        expect(workspace.dockOpen).toBe(true);
        expect(workspace.isEditing(object(PROD, 'web'))).toBe(true);
    });

    test('editing the same object again focuses the tab it already has', () => {
        workspace.openEditor(object(PROD, 'web'));
        workspace.openEditor(object(PROD, 'api'));
        workspace.openEditor(object(PROD, 'web'));

        expect(workspace.dockTabs).toHaveLength(2);
        expect(workspace.activeDockTab?.name).toBe('web');
    });

    // A name is only unique within a namespace, and two clusters can both have
    // a "web". Either would otherwise reopen the other's document.
    test('the same name in another namespace or cluster is another tab', () => {
        workspace.openEditor(object(PROD, 'web', 'default'));
        workspace.openEditor(object(PROD, 'web', 'kube-system'));
        workspace.openEditor(object(STAGING, 'web', 'default'));

        expect(workspace.dockTabs).toHaveLength(3);
    });

    test('reopening the tab you are on does not fold the dock away', () => {
        workspace.openEditor(object(PROD, 'web'));
        workspace.openEditor(object(PROD, 'web'));

        expect(workspace.dockOpen).toBe(true);
    });

    test('clicking the tab you are on folds the dock, and again brings it back', () => {
        workspace.openEditor(object(PROD, 'web'));
        const id = workspace.activeDockTabId!;

        workspace.activateDockTab(id);
        expect(workspace.dockOpen).toBe(false);
        // The tab is still there and still the one selected -- only the room
        // it was taking has gone back.
        expect(workspace.activeDockTabId).toBe(id);

        workspace.activateDockTab(id);
        expect(workspace.dockOpen).toBe(true);
    });

    test('closing moves focus to the right, then to the left', () => {
        workspace.openEditor(object(PROD, 'one'));
        workspace.openEditor(object(PROD, 'two'));
        workspace.openEditor(object(PROD, 'three'));
        workspace.activateDockTab(workspace.dockTabs[1].id);

        workspace.closeDockTab(workspace.dockTabs[1].id);
        expect(workspace.activeDockTab?.name).toBe('three');

        workspace.closeDockTab(workspace.dockTabs[1].id);
        expect(workspace.activeDockTab?.name).toBe('one');
    });

    test('closing the last tab folds the dock away', () => {
        workspace.openEditor(object(PROD, 'web'));

        workspace.closeDockTab(workspace.dockTabs[0].id);

        expect(workspace.dockTabs).toHaveLength(0);
        expect(workspace.activeDockTabId).toBeNull();
        expect(workspace.dockOpen).toBe(false);
    });

    test('closing others can be scoped to one cluster', () => {
        workspace.openEditor(object(PROD, 'one'));
        workspace.openEditor(object(PROD, 'two'));
        workspace.openEditor(object(STAGING, 'three'));
        const keep = workspace.dockTabs[0].id;

        workspace.closeOtherDockTabs(keep, PROD);

        expect(workspace.dockTabs.map((t) => t.name)).toEqual(['one', 'three']);
    });

    test('closing all can be scoped to one cluster', () => {
        workspace.openEditor(object(PROD, 'one'));
        workspace.openEditor(object(STAGING, 'two'));

        workspace.closeAllDockTabs(PROD);

        expect(workspace.dockTabs.map((t) => t.contextId)).toEqual([STAGING]);
    });

    test('tabs are dragged into order', () => {
        workspace.openEditor(object(PROD, 'one'));
        workspace.openEditor(object(PROD, 'two'));

        workspace.moveDockTab(1, 0);
        expect(workspace.dockTabs.map((t) => t.name)).toEqual(['two', 'one']);

        // Off the end is not a move, and must not drop the tab.
        workspace.moveDockTab(0, 5);
        expect(workspace.dockTabs.map((t) => t.name)).toEqual(['two', 'one']);
    });

    // The point of the dock: what is open in it is a document you are part way
    // through, and looking at something else must not close it.
    test('the dock is untouched by what happens to the tabs above it', () => {
        open([PROD, 'pods'], [STAGING, 'nodes']);
        workspace.openEditor(object(PROD, 'web'));

        workspace.activateTab(workspace.tabs[1].id);
        workspace.selectContext(STAGING);
        workspace.closeAllTabs();

        expect(workspace.dockTabs.map((t) => t.name)).toEqual(['web']);
        expect(workspace.dockOpen).toBe(true);
    });

    test('a cluster that leaves the kubeconfig takes its dock tabs with it', async () => {
        workspace.openEditor(object(PROD, 'web'));

        // Nothing on disk this time round, so every context has gone.
        vi.mocked(KubeconfigService.Sync).mockResolvedValueOnce([]);
        await workspace.sync();

        expect(workspace.dockTabs).toHaveLength(0);
        expect(workspace.dockOpen).toBe(false);
    });
});

// Which namespaces you work in is a standing fact about your job rather than
// something about this session, so it is the one part of how a table was left
// that survives a restart. The sort and the search deliberately do not -- see
// views.ts for why.
describe('a tab\'s namespace filter', () => {
    beforeEach(() => {
        views.forgetAll();
        workspace.files = [
            {
                path: '/home/u/.kube/prod',
                source: 'manual',
                error: '',
                contexts: [{
                    id: PROD, name: 'admin@prod', cluster: 'prod', user: 'admin',
                    namespace: '', server: '', file: '/home/u/.kube/prod', current: false,
                }],
            },
        ];
        vi.mocked(KubeconfigService.Sync).mockResolvedValue(workspace.files);
        workspace.settings.preferences.restoreTabs = true;
    });

    test('comes back on the tab it was set on', async () => {
        workspace.settings.panes.main.tabs = [resource(PROD, 'pods', ['team-a', 'kube-system'])];

        await workspace.sync({ restoreTabs: true });

        const tab = workspace.tabs.find((t) => t.kind === 'pods');
        expect(tab).toBeTruthy();
        // Seeded into views, which is where the table looks as it mounts.
        expect(views.recall(tab!.id)?.namespaces).toEqual(['team-a', 'kube-system']);
    });

    test('is per tab, not shared between them', async () => {
        workspace.settings.panes.main.tabs = [
            resource(PROD, 'pods', ['team-a']),
            resource(PROD, 'services', ['team-b']),
        ];

        await workspace.sync({ restoreTabs: true });

        const pods = workspace.tabs.find((t) => t.kind === 'pods')!;
        const services = workspace.tabs.find((t) => t.kind === 'services')!;
        expect(views.recall(pods.id)?.namespaces).toEqual(['team-a']);
        expect(views.recall(services.id)?.namespaces).toEqual(['team-b']);
    });

    // A tab saved before this existed, or one genuinely showing everything,
    // must open on the whole cluster rather than on a filter of nothing.
    test('a tab saved without one opens on the whole cluster', async () => {
        workspace.settings.panes.main.tabs = [resource(PROD, 'pods')];

        await workspace.sync({ restoreTabs: true });

        const tab = workspace.tabs.find((t) => t.kind === 'pods')!;
        expect(views.recall(tab.id)?.namespaces ?? []).toEqual([]);
    });

    // Only the filter. A sort is a column index, and a file older than a change
    // to a kind's columns would sort by the wrong one.
    test('the sort and the search are not restored with it', async () => {
        workspace.settings.panes.main.tabs = [resource(PROD, 'pods', ['team-a'])];

        await workspace.sync({ restoreTabs: true });

        const view = views.recall(workspace.tabs.find((t) => t.kind === 'pods')!.id);
        expect(view?.sortColumn).toBeNull();
        expect(view?.query).toBe('');
    });

    test('is written back to the settings file as the table sets it', async () => {
        workspace.settings.panes.main.tabs = [];
        await workspace.sync({ restoreTabs: true });
        workspace.openTab(PROD, 'pods');
        const tab = workspace.tabs.find((t) => t.kind === 'pods')!;

        // What the table does when the picker changes.
        views.remember(tab.id, {
            sortColumn: null, sortDescending: false,
            namespaces: ['team-a'], query: '', node: '',
        });
        workspace.rememberNamespaces();
        await vi.waitFor(() => expect(SettingsService.SetPanes).toHaveBeenCalled());

        const written = vi.mocked(SettingsService.SetPanes).mock.calls.at(-1)![0];
        const saved = written.main?.tabs?.find((t) => t?.kind === 'pods');
        expect(saved?.namespaces).toEqual(['team-a']);
    });
});

describe('restoring the dock at launch', () => {
    beforeEach(() => {
        workspace.closeAllDockTabs();
        workspace.files = [
            {
                path: '/home/u/.kube/prod',
                source: 'manual',
                error: '',
                contexts: [
                    {
                        id: PROD,
                        name: 'admin@prod',
                        cluster: 'prod',
                        user: 'admin',
                        namespace: '',
                        server: '',
                        file: '/home/u/.kube/prod',
                        current: false,
                    },
                ],
            },
        ];
        vi.mocked(KubeconfigService.Sync).mockResolvedValue(workspace.files);
        workspace.settings.preferences.restoreTabs = true;
    });

    test('reopens the editors from last session', async () => {
        workspace.settings.panes.bottom.tabs = [document(PROD, 'web')];

        await workspace.sync({ restoreTabs: true });

        expect(workspace.dockTabs.map((t) => t.name)).toEqual(['web']);
        expect(workspace.activeDockTab?.namespace).toBe('default');
    });

    test('skips a tab whose cluster has gone, and a view this build does not have', async () => {
        workspace.settings.panes.bottom.tabs = [
            document(STAGING, 'gone'),
            { ...document(PROD, 'future'), type: 'terminal' },
            document(PROD, 'web'),
        ];

        await workspace.sync({ restoreTabs: true });

        expect(workspace.dockTabs.map((t) => t.name)).toEqual(['web']);
    });

    test('turned off, the dock starts empty but the remembered tabs are left alone', async () => {
        const remembered = [document(PROD, 'web')];
        workspace.settings.panes.bottom.tabs = remembered;
        workspace.settings.preferences.restoreTabs = false;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.dockTabs).toHaveLength(0);
        expect(workspace.settings.panes.bottom.tabs).toEqual(remembered);
    });
});

describe('saving settings', () => {
    // The bug this guards against: opening an editor fills the bottom pane and
    // unfolds it, and a settings write from another section already on its way
    // answers with the file as it was before either happened. Adopting that
    // answer whole shut the pane again a quarter of a second after it opened.
    //
    // Putting every pane in one section removed the version of this that had
    // two of them racing each other. What is left is the cross-section case,
    // which is the one adopt()'s pending guard exists for.
    test('an answer from a write in flight does not roll back a later change', async () => {
        vi.useFakeTimers();
        let landed: (saved: unknown) => void = () => {};
        try {
            // The layout call answers with a file that predates the pane being
            // filled, which is what one in flight really carries...
            vi.mocked(SettingsService.SetLayout).mockResolvedValue({} as never);
            // ...while the panes' own call is still out, so its answer cannot
            // be what puts things right.
            vi.mocked(SettingsService.SetPanes).mockReturnValue(
                new Promise((resolve) => {
                    landed = resolve;
                }) as never,
            );

            workspace.setSidebarWidth(300);
            workspace.openEditor({ contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' });
            await vi.advanceTimersByTimeAsync(1000);

            expect(workspace.dockTabs.map((t) => t.name)).toEqual(['web']);
            expect(workspace.dockOpen).toBe(true);
            // And what was sent is the pane as it actually stood.
            expect(SettingsService.SetPanes).toHaveBeenCalledWith(
                expect.objectContaining({
                    bottom: expect.objectContaining({
                        open: true,
                        tabs: [expect.objectContaining({ name: 'web' })],
                    }),
                }),
            );
        } finally {
            landed({});
            vi.useRealTimers();
            vi.mocked(SettingsService.SetLayout).mockReset().mockResolvedValue({} as never);
            vi.mocked(SettingsService.SetPanes).mockReset().mockResolvedValue({} as never);
        }
    });

    // The cluster tree's pane was left out of the write, so its width -- and
    // anything moved into it -- was back at the default after every restart.
    test('writes the left pane with the others, so its width survives a restart', async () => {
        vi.mocked(SettingsService.SetPanes).mockClear();

        workspace.setSidebarWidth(300);
        await vi.waitFor(() => expect(SettingsService.SetPanes).toHaveBeenCalled());

        const written = vi.mocked(SettingsService.SetPanes).mock.calls.at(-1)![0];
        expect(written.left?.size).toBe(300);
        expect(written.left?.tabs?.map((t) => t?.type)).toContain('clusters');
    });
});

describe('the detail panel', () => {
    const WEB = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };

    /** A describe that only answers when the returned function is called. */
    function heldDescribe(): (text: string) => void {
        let answer: (text: string) => void = () => {};
        vi.mocked(ResourceService.Describe).mockReturnValueOnce(
            new Promise<string>((resolve) => {
                answer = resolve;
            }) as never,
        );
        return answer!;
    }

    beforeEach(() => {
        detail.close();
        vi.mocked(ResourceService.Describe).mockReset().mockResolvedValue('Name: web\nStatus: Running');
    });

    test('re-reading swaps the report for what the cluster has now', async () => {
        await detail.open(WEB);
        vi.mocked(ResourceService.Describe).mockResolvedValue('Name: web\nStatus: Pending');

        await detail.refresh();

        expect(detail.text).toBe('Name: web\nStatus: Pending');
    });

    // Blanking to "Describing…" every time the object is saved makes the panel
    // flicker for as long as the cluster takes to answer. The report it already
    // has is a moment out of date, which is better than nothing at all.
    test('re-reading keeps the old report on screen until the new one lands', async () => {
        await detail.open(WEB);
        const answer = heldDescribe();

        const done = detail.refresh();

        expect(detail.loading).toBe(false);
        expect(detail.text).toBe('Name: web\nStatus: Running');

        answer('Name: web\nStatus: Pending');
        await done;
        expect(detail.text).toBe('Name: web\nStatus: Pending');
    });

    test('re-reading a closed panel touches no cluster', async () => {
        await detail.refresh();

        expect(ResourceService.Describe).not.toHaveBeenCalled();
    });

    // Two reads can be in flight at once now that a save can start one: the
    // slower must not be allowed to put the panel back to what it said before.
    test('a slow read overtaken by a newer one does not win', async () => {
        await detail.open(WEB);
        const slow = heldDescribe();

        const first = detail.refresh();
        vi.mocked(ResourceService.Describe).mockResolvedValue('Name: web\nStatus: Pending');
        await detail.refresh();
        slow('Name: web\nStatus: Running');
        await first;

        expect(detail.text).toBe('Name: web\nStatus: Pending');
    });

    test('a read still in flight does not reopen a closed panel', async () => {
        const answer = heldDescribe();
        const opening = detail.open(WEB);

        detail.close();
        answer('Name: web\nStatus: Running');
        await opening;

        expect(detail.target).toBeNull();
        expect(detail.text).toBe('');
    });

    // What the panel's report was read at. The panel compares it against the
    // object's revision to notice that a save has made it stale.
    test('opening it records the revision its report was read at', async () => {
        changes.changed(WEB);
        await detail.open(WEB);

        expect(detail.revision).toBe(changes.revision(WEB));
    });

    test('a save leaves the panel behind the object', async () => {
        await detail.open(WEB);

        changes.changed(WEB);

        expect(detail.revision).not.toBe(changes.revision(WEB));
    });

    test('re-reading catches it up', async () => {
        await detail.open(WEB);
        changes.changed(WEB);

        await detail.refresh();

        expect(detail.revision).toBe(changes.revision(WEB));
    });
});

// The catalogue is the one piece of state that can name something that is not
// there: a settings file survives the theme it asks for being deleted, the
// folder it came from being dropped, or being opened on another machine.
describe('themes', () => {
    /** A theme as the Go side hands one over. */
    function theme(id: string, extra: Record<string, unknown> = {}) {
        return {
            id,
            name: id,
            tagline: '',
            base: 'dark',
            author: '',
            tokens: { bg: '#000000' },
            resolved: { bg: '#000000', text: '#ffffff' },
            origin: 'builtin',
            pack: '',
            repo: '',
            warnings: [],
            ...extra,
        };
    }

    beforeEach(() => {
        workspace.themeCatalogue = { themes: [], dir: '', folders: [], problems: [] };
        workspace.settings.preferences = { ...workspace.settings.preferences, theme: 'k8sdockside-dark' };
    });

    test('loads the catalogue and the token documentation', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark'), theme('nord')],
            dir: '/home/u/.config/k8sdockside/themes',
            folders: ['/home/u/dotfiles/themes'],
            problems: [{ path: '/tmp/bad.json', message: 'not valid JSON' }],
        });
        vi.mocked(ThemeService.Tokens).mockResolvedValueOnce([{ name: 'bg', help: 'The window.' }]);

        await workspace.loadThemes();

        expect(workspace.themes.map((t) => t.id)).toEqual(['k8sdockside-dark', 'nord']);
        expect(workspace.themeDir).toBe('/home/u/.config/k8sdockside/themes');
        expect(workspace.themeFolders).toEqual(['/home/u/dotfiles/themes']);
        expect(workspace.themeProblems).toHaveLength(1);
        expect(workspace.themeTokens.map((t) => t.name)).toEqual(['bg']);
    });

    test('a null theme list does not become undefined further in', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: null,
            dir: '',
            folders: null,
            problems: null,
        });

        await workspace.loadThemes();

        expect(workspace.themes).toEqual([]);
        expect(workspace.themeFolders).toEqual([]);
        expect(workspace.themeProblems).toEqual([]);
    });

    test('the active theme is the one the settings name', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark'), theme('nord')],
            dir: '',
            folders: [],
            problems: [],
        });
        await workspace.loadThemes();

        workspace.setTheme('nord');

        expect(workspace.activeTheme?.id).toBe('nord');
        expect(workspace.themeMissing).toBe(false);
    });

    // Falling back rather than failing, and saying so rather than silently
    // rewriting the choice: the theme may be one folder away from coming back.
    test('a theme that is not installed falls back to the default and is flagged', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark')],
            dir: '',
            folders: [],
            problems: [],
        });
        await workspace.loadThemes();

        workspace.setTheme('someone-elses-theme');

        expect(workspace.activeTheme?.id).toBe('k8sdockside-dark');
        expect(workspace.themeMissing).toBe(true);
        // The choice itself is untouched, so reinstalling the theme restores it.
        expect(workspace.theme).toBe('someone-elses-theme');
    });

    test('nothing is missing before the catalogue has loaded', () => {
        workspace.setTheme('nord');
        expect(workspace.themeMissing).toBe(false);
        expect(workspace.activeTheme).toBeNull();
    });

    test('dropping a folder replaces the catalogue with what the service returns', async () => {
        vi.mocked(ThemeService.RemoveFolder).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark')],
            dir: '',
            folders: [],
            problems: [],
        });

        await workspace.removeThemeFolder('/home/u/dotfiles/themes');

        expect(workspace.themeFolders).toEqual([]);
        expect(ThemeService.RemoveFolder).toHaveBeenCalledWith('/home/u/dotfiles/themes');
    });
});

// The distinction this whole feature turns on: a plugin is installed on *this
// machine*, and the solution it describes is installed in a *cluster*. Those
// come apart constantly, and the sidebar has to say which it means.
describe('solution plugins', () => {
    function plugin(id: string, requires: { kind: string; optional?: boolean; selector?: string }[] = []) {
        return {
            id,
            name: id,
            tagline: '',
            icon: 'puzzle',
            author: '',
            docs: '',
            description: '',
            requires: requires.map((r) => ({
                kind: r.kind,
                label: r.kind,
                optional: r.optional ?? false,
                selector: r.selector ?? '',
            })),
            views: [{ id: 'things', label: 'Things', icon: 'box', type: 'table', kind: 'pods', namespace: '', selector: '' }],
            origin: 'builtin',
            pack: '',
            repo: '',
            disabled: false,
        };
    }

    /** Says the cluster's definitions have been read, and what they contain. */
    function clusterServes(contextId: string, kinds: string[]) {
        workspace.customKinds = {
            ...workspace.customKinds,
            [contextId]: {
                status: 'ready',
                message: '',
                groups: [
                    {
                        group: 'argoproj.io',
                        kinds: kinds.map((kind) => ({
                            kind,
                            label: kind,
                            group: 'argoproj.io',
                            plural: kind,
                            scoped: true,
                        })),
                    },
                ],
            },
        };
    }

    beforeEach(() => {
        workspace.pluginCatalogue = { plugins: [], dir: '', folders: [], problems: [] };
        workspace.customKinds = {};
        // What a cluster was asked about its plugins is cached per context, so
        // a test starting with last test's answer would be testing that.
        workspace.pluginProbes = {};
        workspace.knownPlugins = [];
        workspace.expandedPlugins = [];
    });

    // Switching a plugin off has to hide what it offers without uninstalling
    // it: the settings list is where it is switched back on, so it cannot
    // disappear from the catalogue itself.
    test('a switched-off plugin leaves the sidebar but stays in the catalogue', () => {
        const off = { ...plugin('argocd'), disabled: true };
        workspace.pluginCatalogue = { plugins: [off, plugin('flux')], dir: '', folders: [], problems: [] };

        expect(workspace.plugins.map((p) => p.id)).toEqual(['argocd', 'flux']);
        expect(workspace.enabledPlugins.map((p) => p.id)).toEqual(['flux']);
    });

    test('setPluginEnabled sends the wanted state and adopts what comes back', async () => {
        workspace.pluginCatalogue = { plugins: [plugin('argocd')], dir: '', folders: [], problems: [] };
        vi.mocked(PluginService.SetEnabled).mockResolvedValueOnce({
            plugins: [{ ...plugin('argocd'), disabled: true }],
            dir: '',
            folders: [],
            problems: [],
        });

        await workspace.setPluginEnabled('argocd', false);

        // The wanted state, not a toggle: a switch that sent "off" twice must
        // end up off rather than back on.
        expect(PluginService.SetEnabled).toHaveBeenCalledWith('argocd', false);
        expect(workspace.enabledPlugins).toEqual([]);
        expect(workspace.plugins.map((p) => p.id)).toEqual(['argocd']);
    });

    // The Metrics panel is drawn or left out on the strength of the attachment
    // list, so switching a plugin off has to refresh it there and then.
    // Otherwise switching Prometheus off leaves a Metrics heading on the
    // dashboard saying no Prometheus was found -- until the next reload.
    test('switching a plugin off refreshes which surfaces draw charts', async () => {
        workspace.metricsAttachments = ['dashboard', 'pods'];
        vi.mocked(PluginService.SetEnabled).mockResolvedValueOnce({
            plugins: [{ ...plugin('prometheus'), disabled: true }],
            dir: '',
            folders: [],
            problems: [],
        });
        vi.mocked(MetricsService.Attachments).mockResolvedValueOnce([]);

        await workspace.setPluginEnabled('prometheus', false);

        expect(workspace.metricsAttachments).toEqual([]);
        expect(workspace.chartsAttachTo('dashboard')).toBe(false);
    });

    test('loads the catalogue and registers its views for tab titles', async () => {
        vi.mocked(PluginService.List).mockResolvedValueOnce({
            plugins: [
                {
                    id: 'argocd',
                    name: 'Argo CD',
                    tagline: 'GitOps',
                    icon: 'rocket',
                    author: '',
                    docs: '',
                    description: '',
                    requires: [{ kind: 'crd:applications.argoproj.io', label: 'Applications', optional: false }],
                    views: [
                        {
                            id: 'applications',
                            label: 'Applications',
                            icon: 'rocket',
                            type: 'table',
                            kind: 'crd:applications.argoproj.io',
                            namespace: '',
                            selector: '',
                        },
                    ],
                    origin: 'builtin',
                    pack: '',
                    repo: '',
                    disabled: false,
                },
            ],
            dir: '/home/u/.config/k8sdockside/plugins',
            folders: [],
            problems: [],
        });

        await workspace.loadPlugins();

        expect(workspace.plugins.map((p) => p.id)).toEqual(['argocd']);
        expect(workspace.pluginDir).toBe('/home/u/.config/k8sdockside/plugins');
        // The tab bar titles a plugin view through labelFor, which only knows
        // what loadPlugins registered.
        expect(labelFor('plugin:argocd/applications')).toBe('Applications');
        // Every plugin has an overview whether or not it declares one.
        expect(labelFor('plugin:argocd/overview')).toBe('Argo CD');
    });

    test('a null plugin list does not become undefined further in', async () => {
        vi.mocked(PluginService.List).mockResolvedValueOnce({
            plugins: null,
            dir: '',
            folders: null,
            problems: null,
        });

        await workspace.loadPlugins();

        expect(workspace.plugins).toEqual([]);
        expect(workspace.pluginFolders).toEqual([]);
        expect(workspace.pluginProblems).toEqual([]);
    });

    // Before the definitions have been read we genuinely do not know. A row
    // that said "not installed" before it had looked would be wrong more often
    // than right.
    test('presence is unknown until the cluster has been asked', () => {
        const argo = plugin('argocd', [{ kind: 'crd:applications.argoproj.io' }]);
        expect(workspace.pluginInstalledIn(PROD, argo)).toBeNull();
    });

    test('a cluster serving every required CRD counts as installed', () => {
        clusterServes(PROD, ['crd:applications.argoproj.io', 'crd:appprojects.argoproj.io']);
        const argo = plugin('argocd', [
            { kind: 'crd:applications.argoproj.io' },
            { kind: 'crd:appprojects.argoproj.io' },
        ]);

        expect(workspace.pluginInstalledIn(PROD, argo)).toBe(true);
    });

    test('one missing required CRD is enough to count as not installed', () => {
        clusterServes(PROD, ['crd:applications.argoproj.io']);
        const argo = plugin('argocd', [
            { kind: 'crd:applications.argoproj.io' },
            { kind: 'crd:appprojects.argoproj.io' },
        ]);

        expect(workspace.pluginInstalledIn(PROD, argo)).toBe(false);
    });

    // Argo CD without ApplicationSets is still Argo CD.
    test('an optional CRD does not decide it', () => {
        clusterServes(PROD, ['crd:applications.argoproj.io']);
        const argo = plugin('argocd', [
            { kind: 'crd:applications.argoproj.io' },
            { kind: 'crd:applicationsets.argoproj.io', optional: true },
        ]);

        expect(workspace.pluginInstalledIn(PROD, argo)).toBe(true);
    });

    // A requirement on a built-in kind is served by every cluster worth
    // connecting to, and cannot be looked up in a list of custom resources.
    test('a requirement on a built-in kind is taken as met', () => {
        clusterServes(PROD, []);
        expect(workspace.pluginInstalledIn(PROD, plugin('x', [{ kind: 'deployments' }]))).toBe(true);
    });

    test('a plugin with no requirements is taken at its word', () => {
        clusterServes(PROD, []);
        expect(workspace.pluginInstalledIn(PROD, plugin('x'))).toBe(true);
    });

    // Flannel's requirements are DaemonSets, Nodes and ConfigMaps -- kinds
    // every cluster serves -- so taking them as met made it read "installed
    // here" in every cluster. A requirement that names objects is the answer,
    // and only the backend can check it.
    test('a requirement that names objects waits for the cluster to be asked', async () => {
        const flannel = plugin('flannel', [{ kind: 'daemonsets', selector: 'app=flannel' }]);
        clusterServes(PROD, []);
        expect(workspace.pluginInstalledIn(PROD, flannel)).toBeNull();

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: [], absent: ['flannel'] });
        await workspace.loadCustomKinds(PROD, { force: true });
        await vi.waitFor(() => expect(workspace.pluginInstalledIn(PROD, flannel)).toBe(false));
    });

    test('a cluster that has the objects counts as installed', async () => {
        const flannel = plugin('flannel', [{ kind: 'daemonsets', selector: 'app=flannel' }]);
        clusterServes(PROD, []);

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: [], absent: [] });
        await workspace.loadCustomKinds(PROD, { force: true });
        await vi.waitFor(() => expect(workspace.pluginInstalledIn(PROD, flannel)).toBe(true));
    });

    // A cluster that would not answer leaves the row as it was: the backend
    // reports nothing about it, rather than reporting it as missing.
    // The published Flannel plugin predates requirements that name objects, so
    // its own manifest still asks only for kinds every cluster serves. The
    // known list knows how to find flannel, and that is enough: an installed
    // plugin nobody has updated should not read as present everywhere.
    test('a plugin the known list can recognise is judged by that, not by its kinds', async () => {
        const flannel = plugin('flannel', [{ kind: 'daemonsets' }, { kind: 'nodes' }]);
        workspace.knownPlugins = [{ ...known('flannel', []), probed: true }];
        clusterServes(PROD, []);

        // A probe is coming, so the row waits rather than saying "installed".
        expect(workspace.pluginInstalledIn(PROD, flannel)).toBeNull();

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: [], absent: ['flannel'] });
        await workspace.loadCustomKinds(PROD, { force: true });
        await vi.waitFor(() => expect(workspace.pluginInstalledIn(PROD, flannel)).toBe(false));
    });

    test('a plugin nothing can probe is still taken at its kinds', async () => {
        const argo = plugin('argocd', [{ kind: 'crd:applications.argoproj.io' }]);
        workspace.knownPlugins = [known('argocd', ['crd:applications.argoproj.io'])];
        clusterServes(PROD, ['crd:applications.argoproj.io']);

        expect(workspace.pluginInstalledIn(PROD, argo)).toBe(true);
    });

    test('a cluster that could not be probed does not read as missing the plugin', async () => {
        const flannel = plugin('flannel', [{ kind: 'daemonsets', selector: 'app=flannel' }]);
        clusterServes(PROD, []);

        vi.mocked(PluginService.Probe).mockRejectedValueOnce(new Error('forbidden'));
        await workspace.loadCustomKinds(PROD, { force: true });
        await vi.waitFor(() => expect(workspace.pluginProbes[PROD]).toBeDefined());
        expect(workspace.pluginInstalledIn(PROD, flannel)).toBe(true);
    });

    // The descheduler is the case this exists for: its plugin puts a panel on
    // every Pod and a button beside it, and a cluster with no descheduler in it
    // was getting both. A plugin is installed on this machine, but what it
    // draws onto a cluster's own objects belongs to the cluster that has the
    // product.
    test('a plugin the cluster does not have draws nothing onto that cluster\'s objects', async () => {
        const descheduler = {
            ...plugin('descheduler', [{ kind: 'configmaps' }, { kind: 'events' }]),
            sections: [{ id: 'pod', label: 'Descheduler', kind: 'pods', entry: 'pod.html', height: 260 }],
            actions: [{ id: 'allow-eviction', label: 'Allow descheduling', icon: 'check', kind: 'pods' }],
        };
        workspace.pluginCatalogue = { plugins: [descheduler], dir: '', folders: [], problems: [] };
        workspace.knownPlugins = [{ ...known('descheduler', []), probed: true }];
        clusterServes(PROD, []);

        // Before the cluster has answered nothing is drawn: a panel that shows
        // up and then vanishes is what this is here to stop.
        expect(workspace.pluginSectionsFor(PROD, 'pods')).toHaveLength(0);
        expect(workspace.pluginActsOn(PROD, 'pods')).toBe(false);

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: [], absent: ['descheduler'] });
        await workspace.loadCustomKinds(PROD, { force: true });

        await vi.waitFor(() => expect(workspace.pluginSectionsFor(PROD, 'pods')).toHaveLength(0));
        expect(workspace.pluginActsOn(PROD, 'pods')).toBe(false);
        // And the plugin is still installed: its sidebar row stays, marked.
        expect(workspace.enabledPlugins.map((p) => p.id)).toEqual(['descheduler']);
        expect(workspace.pluginInstalledIn(PROD, descheduler)).toBe(false);
    });

    // The same cluster, with the descheduler in it, gets everything.
    test('a plugin the cluster does have keeps its panels and its buttons', async () => {
        const descheduler = {
            ...plugin('descheduler', [{ kind: 'configmaps' }, { kind: 'events' }]),
            sections: [{ id: 'pod', label: 'Descheduler', kind: 'pods', entry: 'pod.html', height: 260 }],
            actions: [{ id: 'allow-eviction', label: 'Allow descheduling', icon: 'check', kind: 'pods' }],
        };
        workspace.pluginCatalogue = { plugins: [descheduler], dir: '', folders: [], problems: [] };
        workspace.knownPlugins = [{ ...known('descheduler', []), probed: true }];
        clusterServes(PROD, []);

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: [], absent: [] });
        await workspace.loadCustomKinds(PROD, { force: true });

        await vi.waitFor(() => expect(workspace.pluginInstalledIn(PROD, descheduler)).toBe(true));
        expect(workspace.pluginSectionsFor(PROD, 'pods')).toHaveLength(1);
        expect(workspace.pluginActsOn(PROD, 'pods')).toBe(true);
    });

    // A cluster that would not say what it serves cannot say which products
    // it runs either, and taking every plugin away from it for that would be
    // the wrong way round.
    test('a cluster that would not answer keeps every plugin drawn', async () => {
        const descheduler = {
            ...plugin('descheduler', [{ kind: 'configmaps' }]),
            sections: [{ id: 'pod', label: 'Descheduler', kind: 'pods', entry: 'pod.html', height: 260 }],
        };
        workspace.pluginCatalogue = { plugins: [descheduler], dir: '', folders: [], problems: [] };
        workspace.knownPlugins = [{ ...known('descheduler', []), probed: true }];

        vi.mocked(ResourceService.CustomResourceKinds).mockRejectedValueOnce(new Error('forbidden'));
        await workspace.loadCustomKinds(PROD);

        expect(workspace.customKindsFor(PROD).status).toBe('error');
        expect(workspace.pluginSectionsFor(PROD, 'pods')).toHaveLength(1);
    });

    // Opening a context and looking at a pod is reason enough to ask: the
    // sidebar's Plugins section may never be opened, and the pod's panel
    // depends on the answer all the same.
    test('a cluster that has answered is asked which plugins it has', async () => {
        vi.mocked(ResourceService.CustomResourceKinds).mockResolvedValueOnce([] as never);
        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: [], absent: ['descheduler'] });

        workspace.askAboutPlugins([PROD]);

        await vi.waitFor(() => expect(workspace.pluginProbes[PROD]).toEqual({ known: [], absent: ['descheduler'] }));
        expect(workspace.customKindsFor(PROD).status).toBe('ready');
        expect(workspace.customKindsFor(STAGING).status).toBe('idle');
    });

    // The background recheck is how installing a product into an open
    // cluster brings its plugin to life -- and it must not flicker the
    // plugin out while it asks.
    test('a recheck brings a plugin to life without taking it away in between', async () => {
        const descheduler = {
            ...plugin('descheduler', [{ kind: 'configmaps' }]),
            sections: [{ id: 'pod', label: 'Descheduler', kind: 'pods', entry: 'pod.html', height: 260 }],
        };
        workspace.pluginCatalogue = { plugins: [descheduler], dir: '', folders: [], problems: [] };
        workspace.knownPlugins = [{ ...known('descheduler', []), probed: true }];
        clusterServes(PROD, []);
        workspace.pluginProbes = { [PROD]: { known: [], absent: ['descheduler'] } };
        clusters.report(PROD, 'connected');

        let answer!: (value: { known: string[]; absent: string[] }) => void;
        vi.mocked(ResourceService.CustomResourceKinds).mockResolvedValueOnce([] as never);
        vi.mocked(PluginService.Probe).mockReturnValueOnce(new Promise((r) => (answer = r)) as never);

        workspace.recheckPlugins();
        // Still asking: the last answer stands.
        await vi.waitFor(() => expect(PluginService.Probe).toHaveBeenCalledWith(PROD));
        expect(workspace.customKindsFor(PROD).status).toBe('ready');
        expect(workspace.pluginSectionsFor(PROD, 'pods')).toHaveLength(0);

        answer({ known: [], absent: [] });
        await vi.waitFor(() => expect(workspace.pluginSectionsFor(PROD, 'pods')).toHaveLength(1));
        clusters.forget(PROD);
    });

    test('a recheck that cannot get through keeps the answer it had', async () => {
        clusterServes(PROD, []);
        workspace.pluginProbes = { [PROD]: { known: [], absent: ['descheduler'] } };
        clusters.report(PROD, 'connected');

        vi.mocked(ResourceService.CustomResourceKinds).mockRejectedValueOnce(new Error('timeout'));
        workspace.recheckPlugins();
        await vi.waitFor(() => expect(ResourceService.CustomResourceKinds).toHaveBeenCalledWith(PROD));
        // Past the rejection, so what is checked is what it left behind.
        await new Promise((resolve) => setTimeout(resolve, 0));

        expect(workspace.customKindsFor(PROD).status).toBe('ready');
        expect(workspace.pluginProbes[PROD]).toEqual({ known: [], absent: ['descheduler'] });
        clusters.forget(PROD);
    });

    test('a recheck leaves alone the clusters that are not connected', () => {
        clusterServes(PROD, []);
        vi.mocked(ResourceService.CustomResourceKinds).mockClear();

        workspace.recheckPlugins();

        expect(ResourceService.CustomResourceKinds).not.toHaveBeenCalled();
    });

    // A cluster opened again is asked again: that is how a product installed
    // while it was let go of gets its plugin back.
    test('disconnecting forgets which plugins the cluster had', async () => {
        clusterServes(PROD, []);
        workspace.pluginProbes = { [PROD]: { known: [], absent: ['descheduler'] } };

        await workspace.disconnect(PROD, { quiet: true });

        expect(workspace.customKindsFor(PROD).status).toBe('idle');
        expect(workspace.pluginProbes[PROD]).toBeUndefined();
    });

    test('the plugins a cluster does not have fold away per context', () => {
        expect(workspace.isAbsentPluginsExpanded(PROD)).toBe(false);

        workspace.toggleAbsentPlugins(PROD);
        expect(workspace.isAbsentPluginsExpanded(PROD)).toBe(true);
        expect(workspace.isAbsentPluginsExpanded(STAGING)).toBe(false);

        workspace.toggleAbsentPlugins(PROD);
        expect(workspace.isAbsentPluginsExpanded(PROD)).toBe(false);
    });

    test('unfolding a plugin is per context', () => {
        workspace.togglePlugin(PROD, 'argocd');

        expect(workspace.isPluginExpanded(PROD, 'argocd')).toBe(true);
        expect(workspace.isPluginExpanded(STAGING, 'argocd')).toBe(false);

        workspace.togglePlugin(PROD, 'argocd');
        expect(workspace.isPluginExpanded(PROD, 'argocd')).toBe(false);
    });

    test('a view that pins a namespace reports it, and one that does not reports nothing', async () => {
        vi.mocked(PluginService.List).mockResolvedValueOnce({
            plugins: [
                {
                    ...plugin('argocd'),
                    views: [
                        { id: 'free', label: 'Free', icon: 'box', type: 'table', kind: 'pods', namespace: '', selector: '' },
                        { id: 'pinned', label: 'Pinned', icon: 'box', type: 'table', kind: 'pods', namespace: 'argocd', selector: '' },
                    ],
                },
            ],
            dir: '',
            folders: [],
            problems: [],
        });
        await workspace.loadPlugins();

        expect(workspace.pinnedNamespace('plugin:argocd/pinned')).toBe('argocd');
        expect(workspace.pinnedNamespace('plugin:argocd/free')).toBe('');
        // A kind that is not a plugin view at all, which is nearly every tab.
        expect(workspace.pinnedNamespace('pods')).toBe('');
        // A plugin that is not installed cannot pin anything.
        expect(workspace.pinnedNamespace('plugin:gone/anything')).toBe('');
    });

    test('opening the overview opens a tab on the plugin, not on a resource', () => {
        workspace.openPluginOverview(PROD, 'argocd');

        expect(workspace.activeTab?.kind).toBe('plugin:argocd/overview');
    });

    function known(id: string, detect: string[]) {
        return {
            id,
            name: id,
            tagline: '',
            icon: 'box',
            description: '',
            repo: `https://github.com/example/${id}.git`,
            detect,
            links: [],
            official: true,
            installed: false,
        };
    }

    // A suggestion is an offer for what a cluster runs, so it needs the
    // cluster to have been looked at, the product to be there, and no plugin
    // for it on this machine already.
    test('a known plugin is suggested only for a cluster running what it is about', () => {
        workspace.knownPlugins = [
            known('cert-manager', ['crd:certificates.cert-manager.io']),
            known('metallb', ['crd:ipaddresspools.metallb.io']),
            known('anywhere', []),
        ];
        expect(workspace.pluginSuggestionsFor(PROD)).toEqual([]);

        clusterServes(PROD, ['crd:certificates.cert-manager.io', 'crd:ipaddresspools.metallb.io']);
        expect(workspace.pluginSuggestionsFor(PROD).map((k) => k.id)).toEqual(['cert-manager', 'metallb']);

        workspace.pluginCatalogue = { plugins: [plugin('metallb')], dir: '', folders: [], problems: [] };
        expect(workspace.pluginSuggestionsFor(PROD).map((k) => k.id)).toEqual(['cert-manager']);

        workspace.settings = { ...workspace.settings, hiddenPluginSuggestions: ['cert-manager'] };
        expect(workspace.pluginSuggestionsFor(PROD)).toEqual([]);
        workspace.settings = { ...workspace.settings, hiddenPluginSuggestions: [] };
    });

    // Flannel installs no custom resources at all, so there is nothing in a
    // cluster's definitions to recognise it by -- and with an empty `detect`
    // it was offered for every cluster. The backend looks for what it runs
    // instead, and the answer arrives with the definitions.
    test('a plugin with nothing to detect is suggested only where its workload was found', async () => {
        workspace.knownPlugins = [known('flannel', [])];

        clusterServes(PROD, []);
        expect(workspace.pluginSuggestionsFor(PROD)).toEqual([]);

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: ['flannel'], absent: [] });
        await workspace.loadCustomKinds(PROD, { force: true });

        expect(PluginService.Probe).toHaveBeenCalledWith(PROD);
        // The probe is a second request, so the suggestion arrives after the
        // definitions rather than holding them up.
        await vi.waitFor(() => expect(workspace.pluginSuggestionsFor(PROD).map((k) => k.id)).toEqual(['flannel']));
        // Another cluster nobody looked at says nothing about this one.
        expect(workspace.pluginSuggestionsFor(STAGING)).toEqual([]);
    });

    test('a cluster that could not be probed suggests nothing, and says nothing about it', async () => {
        workspace.knownPlugins = [known('flannel', [])];
        vi.mocked(PluginService.Probe).mockRejectedValueOnce(new Error('forbidden'));

        await workspace.loadCustomKinds(PROD, { force: true });
        await vi.waitFor(() => expect(workspace.pluginProbes[PROD]).toEqual({ known: [], absent: [] }));

        expect(workspace.pluginSuggestionsFor(PROD)).toEqual([]);
        // Nothing is said about it: the definitions beside it already report a
        // cluster that would not answer, and a plugin left unsuggested is not
        // something anyone can act on.
        expect(notices.current?.tone).not.toBe('error');
        expect(notices.current?.text ?? '').not.toContain('flannel');
    });

    test('the settings list says which clusters run a known plugin', () => {
        workspace.knownPlugins = [known('cert-manager', ['crd:certificates.cert-manager.io'])];
        clusterServes(PROD, ['crd:certificates.cert-manager.io']);
        clusterServes(STAGING, ['crd:applications.argoproj.io']);
        workspace.files = [
            { path: '/home/u/.kube/prod', contexts: [{ id: PROD, name: 'admin@prod' }] },
            { path: '/home/u/.kube/staging', contexts: [{ id: STAGING, name: 'admin@staging' }] },
        ] as unknown as typeof workspace.files;

        expect(workspace.clustersRunning(workspace.knownPlugins[0])).toEqual(['admin@prod']);
        workspace.files = [];
    });

    test('the settings list counts a cluster where the workload was found, not only the definitions', async () => {
        workspace.knownPlugins = [known('flannel', [])];
        workspace.files = [
            { path: '/home/u/.kube/prod', contexts: [{ id: PROD, name: 'admin@prod' }] },
            { path: '/home/u/.kube/staging', contexts: [{ id: STAGING, name: 'admin@staging' }] },
        ] as unknown as typeof workspace.files;
        clusterServes(STAGING, []);

        vi.mocked(PluginService.Probe).mockResolvedValueOnce({ known: ['flannel'], absent: [] });
        await workspace.loadCustomKinds(PROD, { force: true });

        await vi.waitFor(() => expect(workspace.clustersRunning(workspace.knownPlugins[0])).toEqual(['admin@prod']));
        workspace.files = [];
    });

    test('installing a known plugin names it, and reads what it loaded', async () => {
        workspace.knownPlugins = [{ ...known('metallb', []), name: 'MetalLB' }];
        vi.mocked(PluginService.InstallKnown).mockResolvedValueOnce({
            plugins: [plugin('metallb')],
            dir: '',
            folders: [],
            problems: [],
        });

        expect(await workspace.installKnownPlugin('metallb')).toBe(true);
        expect(PluginService.InstallKnown).toHaveBeenCalledWith('metallb');
        expect(workspace.hasPlugin('metallb')).toBe(true);
        expect(notices.current?.text).toBe('Installed MetalLB');
    });

    // The clone is kept when what it holds will not load, so the reasons have
    // to reach the settings list, and the status bar has room for one line.
    test('a clone that would not load is re-read, and reported on one line', async () => {
        workspace.knownPlugins = [{ ...known('metallb', []), name: 'MetalLB' }];
        vi.mocked(PluginService.InstallKnown).mockRejectedValueOnce(
            new Error('cloned into /p/metallb, but it would not load:\nplugin "metallb" needs K8s Dockside 0.0.15 or newer'),
        );
        vi.mocked(PluginService.List).mockResolvedValueOnce({
            plugins: [],
            dir: '',
            folders: [],
            problems: [{ path: '/p/metallb/plugin.json', message: 'plugin "metallb" needs K8s Dockside 0.0.15 or newer' }],
        });

        expect(await workspace.installKnownPlugin('metallb')).toBe(false);
        expect(workspace.pluginProblems).toHaveLength(1);
        expect(notices.current?.text).toBe(
            'Could not install MetalLB: cloned into /p/metallb, but it would not load: (Settings → Plugins has the rest)',
        );
    });

    test('hiding a suggestion is saved', async () => {
        vi.mocked(PluginService.HideSuggestion).mockResolvedValueOnce({
            ...(await SettingsService.Get()),
            hiddenPluginSuggestions: ['metallb'],
        });
        await workspace.hidePluginSuggestion('metallb');

        expect(PluginService.HideSuggestion).toHaveBeenCalledWith('metallb', true);
        expect(workspace.settings.hiddenPluginSuggestions).toEqual(['metallb']);
    });
});

// The point of panes: which one a view lives in is the user's answer, not the
// view type's. A pod list can sit at the foot and an editor can fill the middle.
describe('moving a view between panes', () => {
    const WEB = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };

    beforeEach(() => {
        workspace.closeAllTabsIn('main');
        workspace.closeAllTabsIn('right');
        workspace.closeAllTabsIn('bottom');
    });

    test('a collection opens in the middle and a document at the foot', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openEditor(WEB);

        expect(workspace.panes.main.tabs.map((t) => t.kind)).toEqual(['pods']);
        expect(workspace.panes.bottom.tabs.map((t) => t.name)).toEqual(['web']);
    });

    test('a tab dragged to another pane leaves the first and lands focused', () => {
        workspace.openEditor(WEB);
        const id = workspace.panes.bottom.tabs[0].id;

        workspace.moveTabToPane(id, 'right');

        expect(workspace.panes.bottom.tabs).toHaveLength(0);
        expect(workspace.panes.right.tabs.map((t) => t.id)).toEqual([id]);
        expect(workspace.panes.right.activeId).toBe(id);
    });

    // The reason a tab id says what it shows rather than which one it is: an
    // editor moved across the window is the same editor, with the same buffer.
    test('a moved tab keeps its id, so what is open in it survives the move', () => {
        workspace.openEditor(WEB);
        const before = workspace.panes.bottom.tabs[0].id;

        workspace.moveTabToPane(before, 'main');

        expect(workspace.panes.main.tabs.map((t) => t.id)).toEqual([before]);
        // And it is still the tab the editor's own state is filed under.
        expect(workspace.isEditing(WEB)).toBe(true);
    });

    test('reopening a view that has been moved focuses it where it was put', () => {
        workspace.openEditor(WEB);
        workspace.moveTabToPane(workspace.panes.bottom.tabs[0].id, 'right');
        workspace.panes.right.activeId = null;

        workspace.openEditor(WEB);

        expect(workspace.panes.bottom.tabs).toHaveLength(0);
        expect(workspace.panes.right.activeId).toBe(workspace.panes.right.tabs[0].id);
    });

    test('a tab dropped at a position lands there rather than at the end', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openTab(PROD, 'nodes');
        workspace.openEditor(WEB);
        const editor = workspace.panes.bottom.tabs[0].id;

        workspace.moveTabToPane(editor, 'main', 0);

        expect(workspace.panes.main.tabs.map((t) => t.id)[0]).toBe(editor);
    });

    test('emptying the bottom pane by dragging its last tab out folds it', () => {
        workspace.openEditor(WEB);
        expect(workspace.dockOpen).toBe(true);

        workspace.moveTabToPane(workspace.panes.bottom.tabs[0].id, 'main');

        expect(workspace.panes.bottom.open).toBe(false);
    });

    test('moving a tab to the pane it is already in only reorders it', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openTab(PROD, 'nodes');
        const [pods, nodes] = workspace.panes.main.tabs.map((t) => t.id);

        workspace.moveTabToPane(nodes, 'main', 0);

        expect(workspace.panes.main.tabs.map((t) => t.id)).toEqual([nodes, pods]);
    });

    test('closing a tab in one pane leaves the others alone', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openEditor(WEB);
        const editor = workspace.panes.bottom.tabs[0].id;

        workspace.closeAllTabsIn('main');

        expect(workspace.panes.bottom.tabs.map((t) => t.id)).toEqual([editor]);
    });

    test('every pane is written as one section, so a move cannot half-save', async () => {
        vi.useFakeTimers();
        try {
            workspace.openEditor(WEB);
            await vi.advanceTimersByTimeAsync(1000);
            vi.mocked(SettingsService.SetPanes).mockClear();

            workspace.moveTabToPane(workspace.panes.bottom.tabs[0].id, 'right');
            // The write is debounced, because a drag moves a tab on every
            // pointer move and one drag should cost one save.
            await vi.advanceTimersByTimeAsync(1000);

            // The call carries both halves of the move: the pane it left is
            // empty in the same value that shows the pane it arrived in.
            const sent = vi.mocked(SettingsService.SetPanes).mock.lastCall?.[0] as unknown as {
                right: { tabs: unknown[] };
                bottom: { tabs: unknown[] };
            };
            expect(sent.bottom.tabs).toHaveLength(0);
            expect(sent.right.tabs).toHaveLength(1);
        } finally {
            vi.useRealTimers();
        }
    });
});

// The cluster tree is a view like the others -- it can be moved -- with one
// difference: it cannot be closed, because it is how everything else is opened.
describe('the cluster tree', () => {
    beforeEach(() => {
        workspace.closeAllTabsIn('main');
        workspace.closeAllTabsIn('right');
        workspace.closeAllTabsIn('bottom');
        if (workspace.paneOf(CLUSTERS_TAB_ID) === null) {
            workspace.panes.left.tabs = [clustersTab()];
        }
        workspace.moveTabToPane(CLUSTERS_TAB_ID, 'left');
        workspace.setPaneOpen('left', true);
    });

    test('starts in the left panel', () => {
        expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('left');
    });

    test('closing it does nothing, because there would be no way back', () => {
        workspace.closeTab(CLUSTERS_TAB_ID);

        expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('left');
    });

    test('"close all" in its pane spares it and takes the rest', () => {
        workspace.openTab(PROD, 'pods');
        workspace.moveTabToPane(resourceTabId(PROD, 'pods'), 'left');

        workspace.closeAllTabsIn('left');

        expect(workspace.panes.left.tabs.map((t) => t.id)).toEqual([CLUSTERS_TAB_ID]);
    });

    test('but it can be moved, like anything else', () => {
        workspace.moveTabToPane(CLUSTERS_TAB_ID, 'right');

        expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('right');
        expect(workspace.panes.left.tabs).toHaveLength(0);
    });

    // Hiding is the reversible thing a close button looks like it would do.
    test('hiding it folds the pane it is in, and asking again brings it back', () => {
        workspace.toggleClusters();
        expect(workspace.isPaneOpen('left')).toBe(false);

        workspace.toggleClusters();
        expect(workspace.isPaneOpen('left')).toBe(true);
        expect(workspace.panes.left.activeId).toBe(CLUSTERS_TAB_ID);
    });

    test('hiding follows it to whichever pane it was moved to', () => {
        workspace.moveTabToPane(CLUSTERS_TAB_ID, 'bottom');

        workspace.toggleClusters();

        expect(workspace.isPaneOpen('bottom')).toBe(false);
        expect(workspace.isPaneOpen('left')).toBe(false);
    });

    // A settings file that has lost it -- hand-edited, or written before it was
    // a tab -- must not open a window with no way to navigate.
    test('a restored session with no tree in it gets one back', async () => {
        workspace.settings.panes.left.tabs = [];
        workspace.settings.panes.main.tabs = [];
        workspace.settings.panes.right.tabs = [];
        workspace.settings.panes.bottom.tabs = [];
        workspace.settings.preferences.restoreTabs = true;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('left');
    });

    // ...including with tab restoring turned off, which starts from empty panes
    // and so never reaches the copy the store keeps.
    test('it is there even when last session is deliberately not restored', async () => {
        workspace.settings.preferences.restoreTabs = false;

        await workspace.sync({ restoreTabs: true });

        expect(workspace.paneOf(CLUSTERS_TAB_ID)).not.toBeNull();
        workspace.settings.preferences.restoreTabs = true;
    });

    test('losing every cluster does not take it away', async () => {
        workspace.files = [];

        await workspace.sync();

        expect(workspace.paneOf(CLUSTERS_TAB_ID)).not.toBeNull();
    });
});

// The way out of an arrangement that has gone wrong. It gives up the
// arrangement, never the work: a reset closes nothing.
describe('resetting the layout', () => {
    const WEB = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };

    beforeEach(() => {
        workspace.closeAllTabsIn('main');
        workspace.closeAllTabsIn('right');
        workspace.closeAllTabsIn('bottom');
        if (workspace.paneOf(CLUSTERS_TAB_ID) === null) {
            workspace.panes.left.tabs = [clustersTab()];
        }
        workspace.moveTabToPane(CLUSTERS_TAB_ID, 'left');
    });

    test('sends every view back to the pane its kind opens in', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openEditor(WEB);
        // Everything piled into one pane, which is the state this undoes.
        workspace.moveTabToPane(resourceTabId(PROD, 'pods'), 'right');
        workspace.moveTabToPane(tabIdFor('edit', WEB), 'right');

        workspace.resetLayout();

        expect(workspace.panes.main.tabs.map((t) => t.kind)).toEqual(['pods']);
        expect(workspace.panes.bottom.tabs.map((t) => t.name)).toEqual(['web']);
        expect(workspace.panes.right.tabs).toHaveLength(0);
    });

    test('closes nothing: the tabs are the work, only their places are given up', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openEditor(WEB);
        const before = workspace.allTabs.length;

        workspace.resetLayout();

        expect(workspace.allTabs).toHaveLength(before);
        // And the editor is still the same editor, so nothing in it was lost.
        expect(workspace.isEditing(WEB)).toBe(true);
    });

    test('brings back a panel that had been hidden, and the tree with it', () => {
        workspace.toggleClusters();
        expect(workspace.isPaneOpen('left')).toBe(false);

        workspace.resetLayout();

        expect(workspace.paneOf(CLUSTERS_TAB_ID)).toBe('left');
        expect(workspace.isPaneOpen('left')).toBe(true);
    });

    test('puts the sizes back', () => {
        workspace.setPaneSize('left', 500);

        workspace.resetLayout();

        expect(workspace.panes.left.size).toBe(DEFAULT_PANE_SIZE.left);
    });

    test('leaves the bottom pane folded when the reset put nothing in it', () => {
        workspace.openTab(PROD, 'pods');

        workspace.resetLayout();

        expect(workspace.panes.bottom.open).toBe(false);
    });

    // One tree, not two: it is already in a fresh set of panes, so the one the
    // user had must not be added beside it.
    test('does not end up with two cluster trees', () => {
        workspace.moveTabToPane(CLUSTERS_TAB_ID, 'bottom');

        workspace.resetLayout();

        expect(workspace.allTabs.filter((t) => t.view === 'clusters')).toHaveLength(1);
    });
});


// The describe panel used to dock to an edge of the window with a size, a
// resize handle and three position buttons of its own -- a second layout system
// for one panel. It is a tab now, so it goes where the user drags it and is
// sized by the pane it lands in. What it does not become is a tab per object:
// it follows the selection, because reading down a list is the thing people
// actually do with it.
describe('the describe tab', () => {
    const HT1 = { contextId: PROD, kind: 'nodes', namespace: '', name: 'ht1' };
    const HT2 = { contextId: PROD, kind: 'nodes', namespace: '', name: 'ht2' };
    const WEB = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };

    beforeEach(() => {
        detail.close();
        for (const pane of ['main', 'right', 'bottom'] as const) workspace.closeAllTabsIn(pane);
        workspace.settings.layout.detailPane = 'right';
    });

    test('opens beside the list rather than under it', async () => {
        await detail.open(HT1);

        expect(workspace.paneOf(DETAILS_TAB_ID)).toBe('right');
        expect(workspace.isPaneOpen('right')).toBe(true);
    });

    test('describing a second row refills the tab rather than opening another', async () => {
        await detail.open(HT1);
        await detail.open(HT2);

        expect(workspace.allTabs.filter((t) => t.view === 'details')).toHaveLength(1);
        expect(detail.target?.name).toBe('ht2');
    });

    test('wears the name of what it is describing', async () => {
        await detail.open(HT1);

        expect(workspace.tabFor(DETAILS_TAB_ID)?.title).toBe('ht1');
    });

    // The gesture that replaces the three dock buttons.
    test('stays where it was dragged, and the next selection opens it there', async () => {
        await detail.open(HT1);
        workspace.moveTabToPane(DETAILS_TAB_ID, 'bottom');
        detail.close();

        await detail.open(HT2);

        expect(workspace.paneOf(DETAILS_TAB_ID)).toBe('bottom');
    });

    test('closing it and selecting again brings it back', async () => {
        await detail.open(HT1);
        workspace.closeTab(DETAILS_TAB_ID);
        expect(detail.target).toBeNull();

        await detail.open(HT2);

        expect(workspace.paneOf(DETAILS_TAB_ID)).toBe('right');
    });

    // The report describes a row in a list, so it goes when you leave that list.
    test('closes when a different list is brought forward', async () => {
        workspace.openTab(PROD, 'nodes');
        workspace.openTab(PROD, 'pods');
        // On the nodes list, describing a node in it.
        workspace.activateTab(resourceTabId(PROD, 'nodes'));
        await detail.open(HT1);

        workspace.activateTab(resourceTabId(PROD, 'pods'));

        expect(detail.target).toBeNull();
        expect(workspace.paneOf(DETAILS_TAB_ID)).toBeNull();
    });

    // A round trip you can make now that the report is a tab beside the list:
    // clicking back to the very list the object came from is not leaving it.
    test('survives going back to the list the object came from', async () => {
        workspace.openTab(PROD, 'nodes');
        await detail.open(HT1);
        workspace.moveTabToPane(DETAILS_TAB_ID, 'main');

        workspace.activateTab(resourceTabId(PROD, 'nodes'));

        expect(detail.target?.name).toBe('ht1');
    });

    test('closes when the list it was read from is closed', async () => {
        workspace.openTab(PROD, 'nodes');
        await detail.open(HT1);

        workspace.closeTab(resourceTabId(PROD, 'nodes'));

        expect(detail.target).toBeNull();
    });

    test('is left alone when some other list is closed', async () => {
        workspace.openTab(PROD, 'nodes');
        workspace.openTab(PROD, 'pods');
        await detail.open(WEB);
        workspace.activateTab(resourceTabId(PROD, 'pods'));

        workspace.closeTab(resourceTabId(PROD, 'nodes'));

        expect(detail.target?.name).toBe('web');
    });

    // It comes and goes with the selection, so there is nothing to restore it
    // against: a saved tab would come back describing nothing.
    test('is never written to the settings file', async () => {
        vi.mocked(SettingsService.SetPanes).mockClear();
        workspace.openTab(PROD, 'nodes');
        await detail.open(HT1);
        // The panes are written on a 250ms debounce, so the assertion has to
        // wait for the write it is about rather than race it.
        await new Promise((r) => setTimeout(r, 400));

        const calls = vi.mocked(SettingsService.SetPanes).mock.calls;
        expect(calls.length).toBeGreaterThan(0);
        const written = calls.at(-1)?.[0];
        const tabs = [written?.left, written?.main, written?.right, written?.bottom].flatMap(
            (pane) => pane?.tabs ?? [],
        );
        // The list it was read from is there; the report itself is not.
        expect(tabs.map((t) => t.type)).toContain('resource');
        expect(tabs.map((t) => t.type)).not.toContain('details');
    });

    test('goes back to the pane it opens in when the layout is reset', async () => {
        await detail.open(HT1);
        workspace.moveTabToPane(DETAILS_TAB_ID, 'bottom');

        workspace.resetLayout();

        expect(workspace.settings.layout.detailPane).toBe('right');
    });
});

// Removing one context: the app's own list, never the kubeconfig. The backend
// answers with the files as they now stand, and the tabs that pointed at the
// removed context go with it, the way they do when its file is removed.
describe('removing a context', () => {
    // Two files, since a context's id is its file and its name: removing one
    // leaves its file listed, with nothing in it.
    const PROD_FILE = '/home/u/.kube/prod';
    const STAGING_FILE = '/home/u/.kube/staging';
    const kept = { id: PROD, name: 'admin@prod', cluster: 'prod', user: 'admin', namespace: '', server: '', file: PROD_FILE, current: false };
    const removed = { id: STAGING, name: 'admin@staging', cluster: 'staging', user: 'admin', namespace: '', server: '', file: STAGING_FILE, current: false };

    beforeEach(() => {
        workspace.files = [
            { path: PROD_FILE, source: 'manual', error: '', contexts: [kept] },
            { path: STAGING_FILE, source: 'manual', error: '', contexts: [removed] },
        ];
        workspace.settings.excludedContexts = [];
        vi.mocked(KubeconfigService.RemoveContext).mockReset().mockResolvedValue([
            { path: PROD_FILE, source: 'manual', error: '', contexts: [kept] },
            { path: STAGING_FILE, source: 'manual', error: '', contexts: [] },
        ]);
        vi.mocked(SettingsService.Get).mockReset().mockResolvedValue({ excludedContexts: [STAGING] } as never);
    });

    test('asks the backend, and closes the tabs that were open on it', async () => {
        open([PROD, 'pods'], [STAGING, 'pods'], [STAGING, 'nodes']);

        await workspace.removeContext(STAGING);

        expect(KubeconfigService.RemoveContext).toHaveBeenCalledWith(STAGING);
        expect(workspace.contexts.map((c) => c.id)).toEqual([PROD]);
        expect(workspace.tabs.map((t) => t.contextId)).toEqual([PROD]);
    });

    test('the removal is remembered by the backend, not listed for undo', async () => {
        await workspace.removeContext(STAGING);

        expect(workspace.settings.excludedContexts).toEqual([STAGING]);
        expect(workspace.excluded).toEqual([]);
    });
});

describe('disconnecting a context', () => {
    const FILE = '/home/u/.kube/config';
    const ctx = (id: string, name: string) => ({ id, name, cluster: name, user: 'admin', namespace: '', server: '', file: FILE, current: false });

    beforeEach(() => {
        workspace.files = [{ path: FILE, source: 'manual', error: '', contexts: [ctx(PROD, 'admin@prod'), ctx(STAGING, 'admin@staging')] }];
        workspace.closeAllDockTabs();
        vi.mocked(ResourceService.Disconnect).mockReset().mockResolvedValue(undefined);
    });

    test('closes its tabs, folds it away and forgets that it answered, and leaves the context listed', async () => {
        await clusters.probe(PROD);
        await clusters.probe(STAGING);
        open([PROD, 'pods'], [STAGING, 'pods'], [STAGING, 'nodes']);
        workspace.selectContext(STAGING);
        expect(workspace.isConnected(STAGING)).toBe(true);

        await workspace.disconnect(STAGING);

        expect(ResourceService.Disconnect).toHaveBeenCalledWith(STAGING);
        expect(workspace.tabs.map((t) => t.contextId)).toEqual([PROD]);
        expect(workspace.isExpanded(STAGING)).toBe(false);
        expect(clusters.of(STAGING).status).toBe('unknown');
        expect(workspace.isConnected(STAGING)).toBe(false);
        expect(workspace.contexts.map((c) => c.id)).toEqual([PROD, STAGING]);
        // The other context is untouched.
        expect(clusters.of(PROD).status).toBe('connected');
        expect(workspace.connectedContexts.map((c) => c.id)).toEqual([PROD]);
    });

    test('a probe that set out before the disconnect does not mark it connected again', async () => {
        let answer!: () => void;
        vi.mocked(ResourceService.Ping).mockReturnValueOnce(new Promise<void>((r) => (answer = r)) as never);
        const probing = clusters.probe(STAGING);

        await workspace.disconnect(STAGING);
        answer();
        await probing;

        expect(clusters.of(STAGING).status).toBe('unknown');
    });

    test('opening it again connects again', async () => {
        await clusters.probe(STAGING);
        await workspace.disconnect(STAGING);

        workspace.selectContext(STAGING);
        await Promise.resolve();

        expect(ResourceService.Ping).toHaveBeenLastCalledWith(STAGING);
    });

    test('everything connected can be disconnected at once', async () => {
        await clusters.probe(PROD);
        open([STAGING, 'pods']);

        await workspace.disconnectAll();

        expect(vi.mocked(ResourceService.Disconnect).mock.calls.map(([id]) => id)).toEqual([PROD, STAGING]);
        expect(workspace.tabs).toHaveLength(0);
        expect(workspace.connectedContexts).toEqual([]);
    });
});

describe('listedKind', () => {
    test('resolves a plugin view to the kind it lists, and passes any other kind through', () => {
        workspace.pluginCatalogue = {
            plugins: [{
                id: 'argocd', name: 'Argo CD', tagline: '', icon: 'rocket', author: '', docs: '', description: '',
                origin: 'builtin', pack: '', repo: '', disabled: false, requires: [],
                views: [{ id: 'applications', label: 'Applications', icon: 'rocket', type: 'list', kind: 'crd:applications.argoproj.io', namespace: '', selector: '' }],
            }],
            dir: '', folders: [], problems: [],
        };

        expect(workspace.listedKind('plugin:argocd/applications')).toBe('crd:applications.argoproj.io');
        expect(workspace.listedKind('pods')).toBe('pods');
        // A view nothing installed can explain is left as it is: the backend
        // will say so, which is better than guessing.
        expect(workspace.listedKind('plugin:flux/kustomizations')).toBe('plugin:flux/kustomizations');
    });
});

// The two documentation pages are tabs of the same shape as Settings: they
// belong to the window, not to a cluster, and there is one of each.
describe('the help and primer tabs', () => {
    beforeEach(() => {
        workspace.files = [];
    });

    test('each opens once, at the end of the strip, with no context', () => {
        open([PROD, 'pods']);
        workspace.activateTab(workspace.tabs[0].id);

        workspace.openHelp();
        workspace.openHelp();
        workspace.openKubernetesPrimer();

        expect(workspace.tabs.map((t) => t.kind)).toEqual(['pods', HELP, KUBERNETES]);
        expect(workspace.activeTab?.kind).toBe(KUBERNETES);
        expect(workspace.tabs.find((t) => t.kind === HELP)?.contextId).toBe('');
        expect(isAppTab({ kind: HELP })).toBe(true);
        expect(isAppTab({ kind: KUBERNETES })).toBe(true);
        expect(isAppTab({ kind: SETTINGS })).toBe(true);
        expect(isAppTab({ kind: 'pods' })).toBe(false);
    });

    test('they survive a sync that drops every cluster, like settings does', async () => {
        workspace.files = [{ path: '/home/u/.kube/prod', source: 'manual', error: '',
            contexts: [{ id: PROD, name: 'admin@prod', cluster: 'prod', user: 'admin', namespace: '', server: '', file: '/home/u/.kube/prod', current: false }] }];
        open([PROD, 'pods']);
        workspace.openHelp();
        workspace.openKubernetesPrimer();

        vi.mocked(KubeconfigService.Sync).mockResolvedValueOnce([]);
        await workspace.sync();

        expect(workspace.tabs.map((t) => t.kind)).toEqual([HELP, KUBERNETES]);
    });

    test('they are titled and iconed like any other tab', () => {
        expect(labelFor(HELP)).toBe('Help');
        expect(labelFor(KUBERNETES)).toBe('Kubernetes primer');
        expect(iconFor(HELP)).not.toBe('box');
        expect(iconFor(KUBERNETES)).not.toBe('box');
    });
});

// A list's sort and filter are kept while its tab is open, so switching away
// and back finds them again -- and dropped with the tab, so a search typed
// this morning does not hide rows in a tab opened this afternoon.
describe('what a list was showing', () => {
    beforeEach(() => {
        views.forgetAll();
        workspace.closeAllTabs();
    });

    test('is dropped when its tab is closed', () => {
        workspace.openTab(PROD, 'pods');
        const id = resourceTabId(PROD, 'pods');
        views.remember(id, { sortColumn: 3, sortDescending: true, namespaces: ['web'], query: 'api', node: '' });

        workspace.closeTab(id);

        expect(views.recall(id)).toBeNull();
    });

    test('survives another tab being brought forward, and a move between panes', () => {
        workspace.openTab(PROD, 'pods');
        workspace.openTab(PROD, 'nodes');
        const id = resourceTabId(PROD, 'pods');
        views.remember(id, { sortColumn: 3, sortDescending: true, namespaces: [], query: '', node: '' });

        workspace.activateTab(resourceTabId(PROD, 'nodes'));
        workspace.moveTabToPane(id, 'bottom');
        workspace.closeTab(resourceTabId(PROD, 'nodes'));

        expect(views.recall(id)?.sortColumn).toBe(3);
    });
});

// Unlike a sort or a filter, which last only while the tab is open, what a
// table's columns look like is a decision about the kind and is written to the
// settings file -- per kind, and per context, because the same kind is not the
// same table in two clusters.
describe("a table's columns", () => {
    beforeEach(() => {
        workspace.settings.contexts = {};
    });

    test('start as the backend sends them: nothing hidden, nothing pinned', () => {
        const prefs = workspace.columnPrefs(PROD, 'pods');

        expect(prefs.hidden).toEqual([]);
        expect(prefs.widths).toEqual({});
        expect(workspace.hasColumnPrefs(PROD, 'pods')).toBe(false);
    });

    test('a width is remembered, held inside a range a column can be found in', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);
        workspace.setColumnWidth(PROD, 'pods', 'Node', 2);
        workspace.setColumnWidth(PROD, 'pods', 'Age', 99999);

        expect(workspace.columnPrefs(PROD, 'pods').widths).toEqual({
            Name: 420,
            Node: MIN_COLUMN_WIDTH,
            Age: MAX_COLUMN_WIDTH,
        });
    });

    test('a width is given back to the contents, which is the only way to that default', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);

        workspace.clearColumnWidth(PROD, 'pods', 'Name');

        expect(workspace.columnPrefs(PROD, 'pods').widths).toEqual({});
        expect(workspace.hasColumnPrefs(PROD, 'pods')).toBe(false);
    });

    test('a hidden column is remembered, sorted so a re-tick rewrites nothing', () => {
        workspace.setColumnHidden(PROD, 'pods', 'Node', true);
        workspace.setColumnHidden(PROD, 'pods', 'Age', true);

        expect(workspace.columnPrefs(PROD, 'pods').hidden).toEqual(['Age', 'Node']);
        expect(workspace.isColumnHidden(PROD, 'pods', 'Node')).toBe(true);
        expect(workspace.isColumnHidden(PROD, 'pods', 'Name')).toBe(false);
    });

    test('showing them all leaves the widths alone', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);
        workspace.setColumnHidden(PROD, 'pods', 'Node', true);

        workspace.showAllColumns(PROD, 'pods');

        expect(workspace.columnPrefs(PROD, 'pods').hidden).toEqual([]);
        expect(workspace.columnPrefs(PROD, 'pods').widths).toEqual({ Name: 420 });
    });

    test('resetting gives back the table the backend sends', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);
        workspace.setColumnHidden(PROD, 'pods', 'Node', true);

        workspace.resetColumns(PROD, 'pods');

        expect(workspace.hasColumnPrefs(PROD, 'pods')).toBe(false);
        expect(workspace.settings.contexts[PROD]).toBeUndefined();
    });

    test('one kind is not another, and one cluster is not another', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);

        expect(workspace.columnPrefs(PROD, 'services').widths).toEqual({});
        expect(workspace.columnPrefs(STAGING, 'pods').widths).toEqual({});
    });

    test('the alias and the colour survive a column being dragged', () => {
        workspace.setContextPrefs(PROD, 'Production', '#b8384b');

        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);

        expect(workspace.settings.contexts[PROD].alias).toBe('Production');
        expect(workspace.settings.contexts[PROD].color).toBe('#b8384b');
    });

    test('...and a column survives the context being renamed', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);

        workspace.setContextPrefs(PROD, 'Production', '');

        expect(workspace.columnPrefs(PROD, 'pods').widths).toEqual({ Name: 420 });
    });

    // A CRD's printer columns are the definition's to change, and a kind the app
    // itself lists can gain or lose one between releases. A width kept for a
    // column nobody can see comes back, at last year's size, if it ever returns.
    test('settings for a column the kind no longer has are dropped as it loads', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);
        workspace.setColumnHidden(PROD, 'pods', 'Node', true);

        workspace.pruneColumns(PROD, 'pods', ['Name', 'Ready', 'Age']);

        expect(workspace.columnPrefs(PROD, 'pods')).toEqual({ widths: { Name: 420 }, hidden: [] });
    });

    test('a table that reported no columns at all cannot clear the record', () => {
        workspace.setColumnWidth(PROD, 'pods', 'Name', 420);

        workspace.pruneColumns(PROD, 'pods', []);

        expect(workspace.columnPrefs(PROD, 'pods').widths).toEqual({ Name: 420 });
    });

    // Two columns can share a name -- a CRD printer column called "Name" beside
    // the Name the app puts first -- and they are not the same column.
    test('a repeated column name is told apart from the first of that name', () => {
        const keys = columnKeys(['Name', 'Ready', 'Name']);

        expect(keys).toEqual(['Name', 'Ready', 'Name#2']);

        workspace.setColumnHidden(PROD, 'crd:widgets', keys[2], true);

        expect(workspace.isColumnHidden(PROD, 'crd:widgets', 'Name')).toBe(false);
        expect(workspace.isColumnHidden(PROD, 'crd:widgets', 'Name#2')).toBe(true);
    });
});
