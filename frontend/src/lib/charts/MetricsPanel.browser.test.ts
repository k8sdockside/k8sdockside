import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

const Charts = vi.fn();

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
    ResourceService: { Describe: vi.fn().mockResolvedValue(''), Ping: vi.fn(), CustomResourceKinds: vi.fn().mockResolvedValue([]) },
    LogService: { Containers: vi.fn().mockResolvedValue([]), Open: vi.fn(), Close: vi.fn() },
    ThemeService: { List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }), Tokens: vi.fn().mockResolvedValue([]) },
    PluginService: { List: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }), Reload: vi.fn() },
    MetricsService: { Charts, Source: vi.fn(), SetEndpoint: vi.fn(), Attachments: vi.fn().mockResolvedValue([]) },
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
        Get: vi.fn().mockResolvedValue({}), ConfigPath: vi.fn().mockResolvedValue(''),
        SetPanes: vi.fn().mockResolvedValue({}), SetLayout: vi.fn().mockResolvedValue({}),
        SetPreferences: vi.fn().mockResolvedValue({}), SetContextPrefs: vi.fn().mockResolvedValue({}),
    },
}));

const { workspace } = await import('../state/workspace.svelte');
const MetricsPanel = (await import('./MetricsPanel.svelte')).default;

const PROD = '/home/u/.kube/config::admin@prod';

const TOKENS = `:root{--bg:#10151c;--bg-panel:#19202a;--bg-raised:#212b38;--border:#46536a;
--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);--text:#e8eef7;
--text-dim:#a9b6c6;--text-faint:#8593a3;--accent:#4a86ff;--warn:#efb567;--radius:6px;
--radius-sm:4px;--mono:monospace;--font:sans-serif;
--chart-1:#3987e5;--chart-2:#d95926;--chart-grid:rgba(255,255,255,.1);font-size:13px}
body{color:var(--text);margin:0;width:640px;font-family:var(--font)}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

function panel(over: Record<string, unknown> = {}) {
    return {
        attached: true,
        source: { endpoint: { namespace: 'monitoring', service: 'prometheus-operated', port: 'web', url: '', source: 'discovered' }, configured: '', available: true, error: '' },
        charts: [
            {
                pluginId: 'prometheus', pluginName: 'Prometheus', id: 'pod-cpu', label: 'CPU',
                unit: 'cores', description: 'Cores used per container.', error: '',
                series: [{ name: 'server', points: [{ t: 1700000000, v: 0.4 }, { t: 1700000030, v: 0.6 }] }],
            },
        ],
        range: 60,
        ...over,
    };
}

beforeEach(() => {
    document.body.innerHTML = '';
    document.head.querySelectorAll('style[data-t]').forEach((n) => n.remove());
    const style = document.createElement('style');
    style.dataset.t = 'y';
    style.textContent = TOKENS;
    document.head.appendChild(style);

    Charts.mockReset().mockResolvedValue(panel());
    workspace.metricsAttachments = ['pods', 'dashboard'];
    workspace.settings.preferences = { ...workspace.settings.preferences, metricsRange: 60 };
});

// Every pod in a cluster with no charting plugin would otherwise carry an empty
// Metrics section — a heading that appears and then removes itself.
test('draws nothing at all when no plugin charts this surface', async () => {
    workspace.metricsAttachments = ['dashboard'];

    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });
    await new Promise((r) => setTimeout(r, 80));

    expect(document.querySelector('.metrics')).toBeNull();
    expect(Charts).not.toHaveBeenCalled();
});

test('draws a chart per plugin chart', async () => {
    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });

    await expect.element(page.getByText('CPU')).toBeVisible();
    await vi.waitFor(() => expect(document.querySelectorAll('path.line').length).toBe(1));
});

// The two reasons there are no charts need different things done about them, so
// they are never worded the same way.
test('a cluster with no Prometheus is told apart from one we could not ask', async () => {
    Charts.mockResolvedValue(panel({ charts: [], source: { endpoint: {}, configured: '', available: false, error: '' } }));
    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });
    await expect.element(page.getByText('No Prometheus found in this cluster')).toBeVisible();

    document.body.innerHTML = '';
    Charts.mockResolvedValue(panel({ charts: [], source: { endpoint: {}, configured: '', available: false, error: 'connection refused' } }));
    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });
    await expect.element(page.getByText('Could not look for a Prometheus', { exact: false })).toBeVisible();
});

// A cluster missing kube-state-metrics has some charts working and some not,
// and that is worth seeing rather than losing the page over.
test('one failing query does not take the others with it', async () => {
    Charts.mockResolvedValue(
        panel({
            charts: [
                ...panel().charts,
                { pluginId: 'prometheus', pluginName: 'Prometheus', id: 'pods', label: 'Pods', unit: 'count', description: '', series: [], error: 'this cluster does not serve kube_pod_info' },
            ],
        }),
    );

    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });

    await expect.element(page.getByText('this cluster does not serve kube_pod_info')).toBeVisible();
    await vi.waitFor(() => expect(document.querySelectorAll('path.line').length).toBe(1));
});

// One control scoping every chart: a page where each carried its own window is a
// page whose numbers cannot be compared.
test('the range is chosen once and applies to every chart', async () => {
    render(MetricsPanel, { props: { contextId: PROD, attach: 'dashboard' } });
    await vi.waitFor(() => expect(Charts).toHaveBeenCalled());

    await page.getByRole('button', { name: '6h' }).click();

    await vi.waitFor(() => {
        expect(Charts.mock.lastCall?.[4]).toBe(360);
    });
    expect(workspace.metricsRange).toBe(360);
});

test('the range control says which window is on', async () => {
    render(MetricsPanel, { props: { contextId: PROD, attach: 'dashboard' } });

    await expect.element(page.getByRole('button', { name: '1h' })).toHaveAttribute('aria-pressed', 'true');
    await expect.element(page.getByRole('button', { name: '15m' })).toHaveAttribute('aria-pressed', 'false');
});

// A query that answers with no series at all is usually a metric nobody is
// scraping -- Argo CD's ServiceMonitor not selected by the Prometheus, say.
// The plugin's description of the chart tends to say what it needs, so it is
// the hint to give, rather than an empty frame.
test('a chart with no series says so and repeats what the plugin says it needs', async () => {
    Charts.mockResolvedValue(
        panel({
            charts: [{
                pluginId: 'argocd', pluginName: 'Argo CD', id: 'apps-by-health', label: 'Applications by health',
                unit: 'count', description: "From Argo CD's own metrics service. Needs its ServiceMonitor scraped.", error: '',
                series: [],
            }],
        }),
    );
    render(MetricsPanel, { props: { contextId: PROD, attach: 'plugin:argocd/overview' } });
    workspace.metricsAttachments = ['plugin:argocd/overview'];

    await expect.element(page.getByText('No data came back', { exact: false })).toBeVisible();
    await expect.element(page.getByText('Needs its ServiceMonitor scraped', { exact: false })).toBeVisible();
});

// The description behind the ⓘ used to be a `title` attribute, which the
// webview does not draw: the icon was there, hovering it did nothing, and what
// a query actually measures was unreachable.
test('the info button shows what a chart measures', async () => {
    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });
    await expect.element(page.getByText('CPU')).toBeVisible();

    expect(document.body.textContent).not.toContain('Cores used per container.');

    const info = document.querySelector<HTMLButtonElement>('button.what');
    expect(info).not.toBeNull();
    info?.dispatchEvent(new MouseEvent('mouseenter'));
    await vi.waitFor(() => expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('Cores used per container.'));

    info?.dispatchEvent(new MouseEvent('mouseleave'));
    await vi.waitFor(() => expect(document.querySelector('[role="tooltip"]')).toBeNull());
});

// A tooltip only a mouse can reach is a tooltip half the readers never see.
test('the description is reachable by keyboard too', async () => {
    render(MetricsPanel, { props: { contextId: PROD, attach: 'pods', name: 'p', namespace: 'n' } });
    await expect.element(page.getByText('CPU')).toBeVisible();

    const info = document.querySelector<HTMLButtonElement>('button.what');
    info?.focus();
    await vi.waitFor(() => expect(document.querySelector('[role="tooltip"]')).not.toBeNull());

    info?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await vi.waitFor(() => expect(document.querySelector('[role="tooltip"]')).toBeNull());
});
