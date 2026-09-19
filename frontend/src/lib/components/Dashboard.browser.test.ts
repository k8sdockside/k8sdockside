import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { detail } from '../state/detail.svelte';

// What is worth testing in a real browser here is the state the whole feature
// exists for: the plugin is installed on this machine and the solution is not
// in this cluster. The sidebar has to say so without hiding the plugin, and the
// overview has to explain it rather than showing four empty tables.

const ARGO = {
    id: 'argocd',
    name: 'Argo CD',
    tagline: 'GitOps continuous delivery',
    icon: 'rocket',
    author: '',
    docs: 'https://argo-cd.readthedocs.io',
    description: 'Argo CD keeps a cluster matching what is in Git.',
    requires: [
        { kind: 'crd:applications.argoproj.io', label: 'Applications', optional: false },
        { kind: 'crd:applicationsets.argoproj.io', label: 'Application Sets', optional: true },
    ],
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
    disabled: false,
};

// Hoisted, because vi.mock's factory is lifted above everything else in the
// file: a plain const up here does not exist yet when the factory runs. It got
// away with it while nothing imported a store at the top of this file, which
// made the factory resolve late enough not to notice.
const Summary = vi.hoisted(() => vi.fn());

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
        Overview: vi.fn(),
        Budget: vi.fn().mockResolvedValue({ scope: { kind: 'cluster', name: '' }, amounts: [], usage: { source: '', error: '' } }),
        Describe: vi.fn().mockResolvedValue(''),
        Ping: vi.fn().mockResolvedValue(undefined),
        CustomResourceKinds: vi.fn().mockResolvedValue([]),
    },
    LogService: { Containers: vi.fn().mockResolvedValue([]), Open: vi.fn(), Close: vi.fn() },
    ThemeService: {
        List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }),
        Tokens: vi.fn().mockResolvedValue([]),
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
        Summary,
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


// The cluster dashboard's counters and events are the way into the rest of
// the cluster: a tile opens the list it counts, and an event opens itself.
const { workspace } = await import('../state/workspace.svelte');
const { ResourceService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');
const Dashboard = (await import('./Dashboard.svelte')).default;

const PROD = '/home/u/.kube/config::admin@prod';

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--border:#46536a;--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);
--bg-active:rgba(255,255,255,.13);--text:#e8eef7;--text-dim:#a9b6c6;--text-faint:#8593a3;
--accent:#4a86ff;--accent-text:#fff;--ok:#5fd39b;--warn:#efb567;--error:#f4787f;
--radius:6px;--radius-sm:4px;--row-h:30px;--mono:monospace;--font:sans-serif;
font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0;width:900px}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

function cell(text: string) {
    return { text, sort: '', tone: '', pills: [] };
}

const OVERVIEW = {
    context: 'admin@prod',
    cluster: 'prod',
    server: 'https://10.0.0.1:6443',
    version: 'v1.36.3',
    distribution: 'Talos',
    namespaces: ['default'],
    stats: [
        { label: 'Nodes', kind: 'nodes', ready: 3, total: 3 },
        { label: 'Pods', kind: 'pods', ready: 87, total: 88 },
        { label: 'Deployments', kind: 'deployments', ready: 33, total: 33 },
        { label: 'Namespaces', kind: 'namespaces', ready: 17, total: 17 },
    ],
    events: {
        kind: 'events',
        columns: ['Namespace', 'Name', 'Type', 'Reason'],
        rows: [
            { id: 'default/web.1', name: 'web.1', namespace: 'default', cells: [cell('default'), cell('web.1'), cell('Warning'), cell('BackOff')] },
        ],
        namespaced: true,
        error: '',
    },
};

beforeEach(() => {
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);
    workspace.closeAllTabs();
    detail.target = null;
    vi.mocked(ResourceService.Overview).mockReset().mockResolvedValue(OVERVIEW as never);
});

test('a counter opens the list of what it counts', async () => {
    render(Dashboard, { contextId: PROD });
    await expect.element(page.getByText('Talos')).toBeVisible();

    await page.getByRole('button', { name: /Pods/ }).click();

    expect(workspace.tabs.map((t) => [t.contextId, t.kind])).toContainEqual([PROD, 'pods']);
});

test('an event opens its own report', async () => {
    render(Dashboard, { contextId: PROD });
    await expect.element(page.getByText('BackOff')).toBeVisible();

    await page.getByText('BackOff').click();

    expect(detail.target).toEqual({ contextId: PROD, kind: 'events', namespace: 'default', name: 'web.1' });
});
