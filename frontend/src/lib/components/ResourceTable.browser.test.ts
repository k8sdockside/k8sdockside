import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { detail } from '../state/detail.svelte';

// The table's rows arrive through a subscription. Stubbing that is what lets a
// test say "the cluster holds this" without a cluster.
const pushed = vi.hoisted(() => ({ send: (_table: unknown) => {}, fail: (_message: string) => {} }));
vi.mock('../state/subscriptions', () => ({
    subscribe: vi.fn(
        (_c: string, _k: string, _n: string, onTable: (t: unknown) => void, onError: (m: string) => void) => {
            pushed.send = onTable;
            pushed.fail = onError;
            return { setNamespaces: vi.fn(), close: vi.fn() };
        },
    ),
}));

// A link out goes through the runtime, which hands it to the user's browser.
const openURL = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return { ...actual, Browser: { ...actual.Browser, OpenURL: openURL } };
});

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
    KubeconfigService: { Sync: vi.fn().mockResolvedValue([]), Files: vi.fn().mockResolvedValue([]) },
    ResourceService: {
        Namespaces: vi.fn().mockResolvedValue(['default']),
        Describe: vi.fn().mockResolvedValue(''),
    },
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }),
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
}));

const ResourceTable = (await import('./ResourceTable.svelte')).default;
const { workspace } = await import('../state/workspace.svelte');
const { views } = await import('../state/views');
const { resourceTabId } = await import('../state/panes');

const PROD = '/home/u/.kube/prod::admin@prod';

const plain = (text: string) => ({ text, tone: '', sort: '', pills: null });

/** A pods table with one row, whose Containers cell holds rectangles. */
function podsTable(pills: { label: string; tone: string; detail: string }[] | null) {
    return {
        kind: 'pods',
        columns: ['Name', 'Ready', 'Containers', 'Status'],
        namespaced: true,
        error: '',
        rows: [
            {
                id: 'pods/default/web',
                name: 'web',
                namespace: 'default',
                cells: [
                    plain('web'),
                    plain('2/2'),
                    { text: 'app sidecar', tone: '', sort: '0002', pills },
                    plain('Running'),
                ],
            },
        ],
    };
}

beforeEach(() => {
    detail.close();
    views.forgetAll();
});

test('a cell carrying containers is drawn as rectangles, not as its text', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'pods' });

    pushed.send(
        podsTable([
            { label: 'app', tone: 'ok', detail: 'Running' },
            { label: 'sidecar', tone: 'error', detail: 'CrashLoopBackOff' },
        ]),
    );

    await expect.element(page.getByRole('img', { name: /app — Running/ })).toBeVisible();
    await expect.element(page.getByRole('img', { name: /sidecar — CrashLoopBackOff/ })).toBeVisible();
});

// Every other kind sends null here, and must go on rendering as it always has.
test('a cell with no containers still shows its text', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'pods' });

    pushed.send(podsTable(null));

    await expect.element(page.getByRole('cell', { name: 'app sidecar' })).toBeVisible();
});

// The rectangles are a picture in the table: pressing one must select the row
// underneath, the way pressing anywhere else in it does.
test('the rectangles in the table are not buttons', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'pods' });

    pushed.send(podsTable([{ label: 'app', tone: 'ok', detail: 'Running' }]));
    await expect.element(page.getByRole('img', { name: /app/ })).toBeVisible();

    expect(page.getByRole('button', { name: /app — Running/ }).elements()).toHaveLength(0);
});

// A node that is Ready and cordoned is both. The cell says each, coloured as
// itself, rather than one word in one colour.
test('a cell saying several things shows each in its own tone', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'nodes' });

    pushed.send({
        kind: 'nodes',
        columns: ['Name', 'Conditions'],
        namespaced: false,
        error: '',
        rows: [
            {
                id: 'nodes//wrkr01',
                name: 'wrkr01',
                namespace: '',
                cells: [
                    plain('wrkr01'),
                    {
                        text: 'Ready,SchedulingDisabled',
                        tone: 'warn',
                        sort: '',
                        pills: null,
                        tags: [
                            { text: 'Ready', tone: 'ok' },
                            { text: 'SchedulingDisabled', tone: 'warn' },
                        ],
                    },
                ],
            },
        ],
    });

    await expect.element(page.getByText('Ready', { exact: true })).toHaveClass(/ok/);
    await expect.element(page.getByText('SchedulingDisabled', { exact: true })).toHaveClass(/warn/);
});

// A plugin view is a tab kind of its own -- plugin:argocd/applications -- but
// the rows in it are Applications. Opening one has to ask about the kind the
// view lists, or every describe, edit and action on it fails with "unknown
// resource kind: plugin:argocd/applications".
test('a row in a plugin view opens as the kind the view lists', async () => {
    workspace.pluginCatalogue = {
        plugins: [{
            id: 'argocd', name: 'Argo CD', tagline: '', icon: 'rocket', author: '', docs: '', description: '',
            origin: 'builtin', pack: '', disabled: false, requires: [],
            views: [{ id: 'applications', label: 'Applications', icon: 'rocket', type: 'list', kind: 'crd:applications.argoproj.io', namespace: '', selector: '' }],
        }],
        dir: '', folders: [], problems: [],
    };
    render(ResourceTable, { contextId: PROD, kind: 'plugin:argocd/applications' });
    pushed.send({
        kind: 'crd:applications.argoproj.io',
        columns: ['Name', 'Sync', 'Health'],
        namespaced: true,
        error: '',
        rows: [{ id: 'crd:applications.argoproj.io/argocd/web', name: 'web', namespace: 'argocd',
            cells: [plain('web'), plain('Synced'), plain('Healthy')] }],
    });

    await page.getByText('Synced').click();

    expect(detail.target).toEqual({ contextId: PROD, kind: 'crd:applications.argoproj.io', namespace: 'argocd', name: 'web' });
});

// A CRD is free to declare a printer column called "Name", beside the Name the
// app itself puts first -- vitistack.io's NetworkNamespace does, pointing it at
// a cluster identifier. Two headers with the same text must still render: a
// table that throws on them is a tab stuck at "Loading…".
test('a table whose headers repeat still renders its rows', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'crd:networknamespaces.vitistack.io' });

    pushed.send({
        kind: 'crd:networknamespaces.vitistack.io',
        columns: ['Name', 'Namespace', 'Name', 'Phase'],
        namespaced: true,
        error: '',
        rows: [{
            id: 'crd:networknamespaces.vitistack.io/default/team-a',
            name: 'team-a',
            namespace: 'default',
            cells: [plain('team-a'), plain('default'), plain('cluster-1'), plain('Ready')],
        }],
    });

    await expect.element(page.getByRole('cell', { name: 'team-a', exact: true })).toBeVisible();
    await expect.element(page.getByRole('cell', { name: 'cluster-1', exact: true })).toBeVisible();
    // The first header is the checkbox column's, which carries no text.
    expect([...document.querySelectorAll('thead th')].map((th) => th.textContent?.trim())).toEqual([
        '', 'Name', 'Namespace', 'Name', 'Phase',
    ]);
});

// Ticking namespaces in the picker moves the filter on the open subscription,
// as a list: two namespaces are one table.
test('choosing namespaces moves the filter on the open subscription', async () => {
    const { subscribe } = await import('../state/subscriptions');
    const moved = vi.fn();
    vi.mocked(subscribe).mockImplementationOnce((_c, _k, _n, onTable) => {
        pushed.send = onTable as (t: unknown) => void;
        return { setNamespaces: moved, close: vi.fn() };
    });
    const { ResourceService } = await import('../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services');
    vi.mocked(ResourceService.Namespaces).mockResolvedValueOnce(['default', 'kube-system']);

    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(podsTable(null));
    await expect.element(page.getByRole('button', { name: /^Namespaces/ })).toBeVisible();

    await page.getByRole('button', { name: /^Namespaces/ }).click();
    await page.getByRole('menuitemcheckbox', { name: 'kube-system' }).click();
    await expect.poll(() => moved.mock.lastCall?.[0]).toEqual(['kube-system']);

    await page.getByRole('menuitemcheckbox', { name: 'default' }).click();
    await expect.poll(() => moved.mock.lastCall?.[0]).toEqual(['default', 'kube-system']);
    await expect.element(page.getByRole('button', { name: /^Namespaces/ })).toHaveTextContent('default, kube-system');
});

/** Two deployments, the older one first, as the backend orders them. */
function agesTable() {
    const aged = (name: string, age: string, seconds: string) => ({
        id: `deployments/default/${name}`,
        name,
        namespace: 'default',
        cells: [plain(name), { text: age, tone: '', sort: seconds, pills: null }],
    });
    return {
        kind: 'deployments',
        columns: ['Name', 'Age'],
        namespaced: true,
        error: '',
        rows: [aged('old', '3d', '259200'), aged('new', '5m', '300')],
    };
}

/** The Name column, top to bottom. Deployments can be deleted, so a checkbox column comes first. */
const names = () =>
    [...document.querySelectorAll('tbody tr')].map((tr) => tr.querySelector('td:not(.pick)')?.textContent?.trim() ?? '');

// The pane keys the view on its tab, so bringing another tab forward destroys
// this component and coming back builds a new one. Pods sorted by age and
// narrowed to a word is exactly the view somebody leaves for a moment and
// expects to find again -- and it used to come back as the default order,
// unfiltered, every time.
test('the sort and the search survive the tab being rebuilt', async () => {
    const first = await render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());
    await expect.poll(() => names()).toEqual(['old', 'new']);

    await page.getByRole('button', { name: 'Age' }).click();
    await expect.poll(() => names()).toEqual(['new', 'old']);
    await page.getByPlaceholder('Filter deployments').fill('ne');
    await expect.poll(() => names()).toEqual(['new']);

    await first.unmount();
    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());

    await expect.poll(() => names()).toEqual(['new']);
    expect(document.querySelector('th[aria-sort="ascending"]')?.textContent).toContain('Age');
    expect((page.getByPlaceholder('Filter deployments').element() as HTMLInputElement).value).toBe('ne');
});

// ...and only that tab's: the same kind in another cluster is another tab.
test('what one tab remembers does not leak into another', async () => {
    render(ResourceTable, { contextId: '/home/u/.kube/staging::admin@staging', kind: 'deployments' });
    pushed.send(agesTable());

    await expect.poll(() => names()).toEqual(['old', 'new']);
    expect((page.getByPlaceholder('Filter deployments').element() as HTMLInputElement).value).toBe('');
});

// ----- columns --------------------------------------------------------------
//
// Unlike the sort and the search above, which last only while the tab is open,
// what the columns look like goes to the settings file: it is a decision about
// the kind rather than about this moment.

/** The headings on screen, left to right, without the checkbox column. */
const headings = () =>
    [...document.querySelectorAll('thead th:not(.pick)')].map((th) => th.textContent?.trim() ?? '');

test('a column turned off in the picker leaves the table', async () => {
    workspace.settings.contexts = {};
    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());
    await expect.poll(() => names()).toEqual(['old', 'new']);

    await page.getByRole('button', { name: 'Columns' }).click();
    await page.getByRole('menuitemcheckbox', { name: 'Age' }).click();

    await expect.poll(() => headings()).toEqual(['Name']);
    expect(workspace.isColumnHidden(PROD, 'deployments', 'Age')).toBe(true);
});

test('a column hidden before the table loads is never drawn', async () => {
    workspace.settings.contexts = {};
    workspace.setColumnHidden(PROD, 'deployments', 'Age', true);

    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());

    await expect.poll(() => headings()).toEqual(['Name']);
});

test('a width dragged is remembered for this kind in this cluster', async () => {
    workspace.settings.contexts = {};
    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());
    await expect.poll(() => names()).toEqual(['old', 'new']);

    const grip = document.querySelector('[aria-label="Resize Name"]') as HTMLElement;
    grip.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', shiftKey: true, bubbles: true }));

    await expect.poll(() => workspace.columnPrefs(PROD, 'deployments').widths.Name).toBeGreaterThan(0);
    expect((document.querySelector('thead th:not(.pick)') as HTMLElement).style.width).not.toBe('');
});

// The chevron saying which way the rows run is on a heading that is no longer
// there, so the order would have nothing on screen to explain it.
test('hiding the sorted column gives the rows back their natural order', async () => {
    workspace.settings.contexts = {};
    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());
    await expect.poll(() => names()).toEqual(['old', 'new']);

    await page.getByRole('button', { name: 'Age' }).click();
    await expect.poll(() => names()).toEqual(['new', 'old']);

    await page.getByRole('button', { name: 'Columns' }).click();
    await page.getByRole('menuitemcheckbox', { name: 'Age' }).click();

    await expect.poll(() => names()).toEqual(['old', 'new']);
    expect(document.querySelector('th[aria-sort="ascending"]')).toBeNull();
});

// A CRD's printer columns are the definition's to change. A width kept for a
// column nobody can see comes back, at last year's size, if the column returns.
test('settings for a column the kind no longer has are dropped as it loads', async () => {
    workspace.settings.contexts = {};
    workspace.setColumnWidth(PROD, 'deployments', 'Replicas', 300);
    workspace.setColumnWidth(PROD, 'deployments', 'Name', 200);

    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());

    await expect.poll(() => workspace.columnPrefs(PROD, 'deployments').widths).toEqual({ Name: 200 });
});

// ----- pods on one node -----------------------------------------------------
//
// The question a node raises is "what is running on it" -- before a drain,
// after an eviction, or when one node is the busy one. It used to mean opening
// every pod in the cluster and reading the Node column.

/** A pods table across two nodes, with the Node column the filter reads. */
function podsOnNodes() {
    const on = (name: string, node: string) => ({
        id: `pods/default/${name}`,
        name,
        namespace: 'default',
        cells: [plain(name), plain('1/1'), plain(node), plain('Running')],
    });
    return {
        kind: 'pods',
        columns: ['Name', 'Ready', 'Node', 'Status'],
        namespaced: true,
        error: '',
        rows: [on('api', 'worker-1'), on('web', 'worker-2'), on('cache', 'worker-1')],
    };
}

test('a pod listing narrowed to a node shows only the pods placed there', async () => {
    views.forgetAll();
    views.focusNode(resourceTabId(PROD, 'pods'), 'worker-1');

    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(podsOnNodes());

    await expect.poll(() => names()).toEqual(['api', 'cache']);
});

// worker-1 and worker-11 are different machines. The search box below the chip
// is already the place for "anything containing".
test('the node filter is the whole name, not a substring of it', async () => {
    views.forgetAll();
    views.focusNode(resourceTabId(PROD, 'pods'), 'worker-1');

    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    const table = podsOnNodes();
    table.rows.push({
        id: 'pods/default/edge',
        name: 'edge',
        namespace: 'default',
        cells: [plain('edge'), plain('1/1'), plain('worker-11'), plain('Running')],
    });
    pushed.send(table);

    await expect.poll(() => names()).toEqual(['api', 'cache']);
});

test('the chip says which node, and takes the filter off again', async () => {
    views.forgetAll();
    views.focusNode(resourceTabId(PROD, 'pods'), 'worker-1');

    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(podsOnNodes());
    await expect.poll(() => names()).toEqual(['api', 'cache']);

    await page.getByTitle('Show the pods on every node again').click();

    await expect.poll(() => names()).toEqual(['api', 'web', 'cache']);
});

// A listing with no Node column has no node to filter on, and must not grow a
// chip that would hide every row.
test('a listing that does not say where a row runs is left alone', async () => {
    views.forgetAll();
    views.focusNode(resourceTabId(PROD, 'deployments'), 'worker-1');

    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send(agesTable());

    await expect.poll(() => names()).toEqual(['old', 'new']);
});

// The tab may already be open and mounted when a node is clicked, in which case
// nothing else would tell it -- see views.watch.
test('a tab already on screen picks up a node chosen elsewhere', async () => {
    views.forgetAll();
    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(podsOnNodes());
    await expect.poll(() => names()).toEqual(['api', 'web', 'cache']);

    workspace.showPodsOnNode(PROD, 'worker-2');

    await expect.poll(() => names()).toEqual(['web']);
});

// The name is how a row's describe panel opens, in every table. A node whose
// name quietly listed pods instead took that away, which is a different app per
// table -- so the way to the pods is the action bar, beside Cordon and Drain.
test('a node name is a name, not a link to something else', async () => {
    views.forgetAll();
    render(ResourceTable, { contextId: PROD, kind: 'nodes' });
    pushed.send({
        kind: 'nodes',
        columns: ['Name', 'Roles', 'Version'],
        namespaced: false,
        error: '',
        rows: [
            {
                id: 'nodes//worker-1',
                name: 'worker-1',
                namespace: '',
                cells: [plain('worker-1'), plain('control-plane'), plain('v1.31.0')],
            },
        ],
    });

    await expect.poll(() => names()).toEqual(['worker-1']);
    expect(page.getByRole('button', { name: 'worker-1' }).elements()).toHaveLength(0);
});

// A custom resource's columns come from the CRD's own additionalPrinterColumns,
// not from us: KubeVirt calls its VirtualMachineInstance column "NodeName".
// Matching on the heading is what lets a plugin's kinds reach a node without
// the app knowing anything about that plugin.
test('a NodeName column leads to the node, the way a Node column does', async () => {
    views.forgetAll();
    render(ResourceTable, { contextId: PROD, kind: 'crd:virtualmachineinstances.kubevirt.io' });
    pushed.send({
        kind: 'crd:virtualmachineinstances.kubevirt.io',
        columns: ['Name', 'Phase', 'IP', 'NodeName', 'Ready'],
        namespaced: true,
        error: '',
        rows: [
            {
                id: 'vmi/default/win11',
                name: 'win11',
                namespace: 'default',
                cells: [plain('win11'), plain('Running'), plain('10.1.2.3'), plain('wrkr01'), plain('True')],
            },
        ],
    });
    await expect.poll(() => names()).toEqual(['win11']);

    await page.getByRole('button', { name: 'wrkr01' }).click();

    await expect.poll(() => workspace.activeTab?.kind).toBe('pods');
    expect(views.recall(resourceTabId(PROD, 'pods'))?.node).toBe('wrkr01');
});

// A column called "Node Selector" is not a node, and turning it into a link to
// one would be worse than leaving it alone.
test('a column that merely mentions nodes is left alone', async () => {
    views.forgetAll();
    render(ResourceTable, { contextId: PROD, kind: 'deployments' });
    pushed.send({
        kind: 'deployments',
        columns: ['Name', 'Node Selector'],
        namespaced: true,
        error: '',
        rows: [
            {
                id: 'deployments/default/web',
                name: 'web',
                namespace: 'default',
                cells: [plain('web'), plain('disktype=ssd')],
            },
        ],
    });

    await expect.poll(() => names()).toEqual(['web']);
    expect(page.getByRole('button', { name: 'disktype=ssd' }).elements()).toHaveLength(0);
});

test('a node name in the pods listing leads to the other pods there', async () => {
    views.forgetAll();
    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(podsOnNodes());
    await expect.poll(() => names()).toEqual(['api', 'web', 'cache']);

    await page.getByRole('button', { name: 'worker-2' }).click();

    await expect.poll(() => names()).toEqual(['web']);
});

// ----- links out and kinds the cluster does not serve ---------------------

function routesTable() {
    return {
        kind: 'httproutes',
        columns: ['Name', 'Hostnames'],
        namespaced: true,
        error: '',
        rows: [
            {
                id: 'httproutes/prod/api',
                name: 'api',
                namespace: 'prod',
                cells: [
                    plain('api'),
                    {
                        text: 'api.example.com, *.example.com',
                        tone: '',
                        sort: '',
                        pills: null,
                        links: [
                            { text: 'api.example.com', url: 'https://api.example.com' },
                            { text: '*.example.com', url: '' },
                        ],
                    },
                ],
            },
        ],
    };
}

test('a hostname opens in the browser rather than selecting the row', async () => {
    openURL.mockClear();
    render(ResourceTable, { contextId: PROD, kind: 'httproutes' });
    pushed.send(routesTable());

    await page.getByRole('link', { name: 'api.example.com' }).click();

    expect(openURL).toHaveBeenCalledWith('https://api.example.com');
    expect(detail.target).toBeNull();
});

// A wildcard names every host under it and none in particular.
test('a wildcard hostname is shown but is not a link', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'httproutes' });
    pushed.send(routesTable());

    await expect.element(page.getByRole('cell', { name: 'api.example.com, *.example.com' })).toBeVisible();
    expect(page.getByRole('link', { name: '*.example.com' }).elements()).toHaveLength(0);
});

// The Gateway API is optional. A cluster without it is healthy, and must not be
// told something went wrong.
test('a kind the cluster does not serve says it is not installed, not that something failed', async () => {
    const { clusters } = await import('../state/health.svelte');
    render(ResourceTable, { contextId: PROD, kind: 'httproutes' });

    pushed.fail('this cluster does not serve httproutes -- the gateway.networking.k8s.io API is not installed');

    await expect.element(page.getByRole('heading', { name: 'The Gateway API is not installed on this cluster' })).toBeVisible();
    await expect.element(page.getByRole('link', { name: 'How to install it' })).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Check again' })).toBeVisible();
    expect(page.getByText('Something went wrong').elements()).toHaveLength(0);
    expect(clusters.of(PROD).status).toBe('connected');
});

test('any other optional kind is described by its API group', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'crd:certificates.cert-manager.io' });

    pushed.fail('this cluster does not serve certificates.cert-manager.io -- the cert-manager.io API is not installed');

    await expect
        .element(page.getByRole('heading', { name: 'This cluster does not serve Certificates' }))
        .toBeVisible();
    await expect.element(page.getByText(/cert-manager\.io API is not installed on/)).toBeVisible();
});

// A kind that ships with Kubernetes has nothing to install: it is too new for
// the cluster, or switched off on its API server, and the page says which.
test('a built-in kind the cluster does not serve is described as switched off', async () => {
    render(ResourceTable, { contextId: PROD, kind: 'mutatingadmissionpolicies' });

    pushed.fail('this cluster does not serve mutatingadmissionpolicies -- the admissionregistration.k8s.io API is not installed');

    await expect
        .element(page.getByRole('heading', { name: 'Mutating Admission Policies are switched off on this cluster' }))
        .toBeVisible();
    await expect.element(page.getByRole('link', { name: 'How API versions are enabled' })).toBeVisible();
    expect(page.getByText(/is not installed on/).elements()).toHaveLength(0);
});

test('a real failure still reads as one', async () => {
    const { clusters } = await import('../state/health.svelte');
    render(ResourceTable, { contextId: PROD, kind: 'httproutes' });

    pushed.fail('dial tcp 10.0.0.1:6443: connect: connection refused');

    await expect.element(page.getByRole('heading', { name: 'Cannot reach the API server' })).toBeVisible();
    expect(clusters.of(PROD).status).toBe('error');
});
