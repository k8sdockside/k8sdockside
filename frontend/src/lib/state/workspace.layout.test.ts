import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { detail } from './detail.svelte';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => import('./workspace.mocks'));

const {
    workspace,
    resourceTabId,
    tabIdFor,
    CLUSTERS_TAB_ID,
    clustersTab,
    clusters,
    DEFAULT_PANE_SIZE,
    columnKeys,
    MAX_COLUMN_WIDTH,
    MIN_COLUMN_WIDTH,
    views,
    session,
    ResourceService,
    KubeconfigService,
    SettingsService,
    TerminalService,
    PROD,
    STAGING,
    document,
    open,
} = await import('./workspace.fixtures');

beforeEach(() => {
    workspace.closeAllTabs();
    clusters.prune([]);
    vi.mocked(ResourceService.Ping).mockReset().mockResolvedValue(undefined);
    expect(workspace.tabs).toHaveLength(0);
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
