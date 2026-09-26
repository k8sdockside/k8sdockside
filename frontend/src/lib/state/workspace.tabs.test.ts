import { beforeEach, describe, expect, test, vi } from 'vitest';
import { detail } from './detail.svelte';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => import('./workspace.mocks'));

const {
    workspace,
    isSettingsTab,
    isAppTab,
    resourceTabId,
    DETAILS_TAB_ID,
    clusters,
    labelFor,
    iconFor,
    changes,
    views,
    SETTINGS,
    HELP,
    KUBERNETES,
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
