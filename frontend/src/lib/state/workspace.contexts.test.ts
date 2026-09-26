import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => import('./workspace.mocks'));

const {
    workspace,
    resourceTabId,
    CLUSTERS_TAB_ID,
    clustersTab,
    clusters,
    changes,
    session,
    ResourceService,
    KubeconfigService,
    SettingsService,
    PROD,
    STAGING,
    resource,
    open,
} = await import('./workspace.fixtures');

beforeEach(() => {
    workspace.closeAllTabs();
    clusters.prune([]);
    vi.mocked(ResourceService.Ping).mockReset().mockResolvedValue(undefined);
    expect(workspace.tabs).toHaveLength(0);
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
