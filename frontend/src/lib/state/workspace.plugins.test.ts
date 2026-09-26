import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => import('./workspace.mocks'));

const {
    workspace,
    clusters,
    labelFor,
    views,
    notices,
    ResourceService,
    SettingsService,
    PluginService,
    MetricsService,
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
