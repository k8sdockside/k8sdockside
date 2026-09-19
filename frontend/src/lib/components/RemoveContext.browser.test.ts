import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Sidebar from './Sidebar.svelte';

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
        RestoreContext: vi.fn().mockResolvedValue([]),
    },
    ResourceService: { Describe: vi.fn().mockResolvedValue(''), Ping: vi.fn().mockResolvedValue(undefined) },
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
        SetPanes: vi.fn().mockResolvedValue({}),
        SetLayout: vi.fn().mockResolvedValue({}),
        SetPreferences: vi.fn().mockResolvedValue({}),
        SetContextPrefs: vi.fn().mockResolvedValue({}),
    },
}));

const { workspace } = await import('../state/workspace.svelte');

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--border:#46536a;--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);
--bg-active:rgba(255,255,255,.13);--text:#e8eef7;--text-dim:#a9b6c6;--text-faint:#8593a3;
--accent:#4a86ff;--ok:#5fd39b;--warn:#efb567;--error:#f4787f;--radius:6px;--radius-sm:4px;
--row-h:30px;--mono:monospace;--font:-apple-system,sans-serif;font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0;width:280px}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

function ctx(file: string, name: string) {
    return { id: `${file}::${name}`, name, cluster: name, user: 'admin',
        namespace: '', server: '', file, current: false };
}


// Removing one context, from its own row. It removes the context from this
// app and nothing else: the kubeconfig is not written, and the file's other
// contexts stay. A rescan finds the context and hides it again, so -- exactly
// like a hidden file -- it is listed under Hidden, where it can be brought
// back.
const { KubeconfigService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

const CONFIG = '/home/u/.kube/config';
const PROD = `${CONFIG}::prod`;
const DEV = `${CONFIG}::dev`;

beforeEach(() => {
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);

    workspace.files = [{ path: CONFIG, source: 'auto', error: '', contexts: [ctx(CONFIG, 'prod'), ctx(CONFIG, 'dev')] }];
    workspace.settings.excludedContexts = [];
    workspace.settings.preferences.showKubeconfigNames = false;
    workspace.settings.preferences.confirmSourceRemoval = false;
    vi.mocked(KubeconfigService.RemoveContext).mockReset().mockResolvedValue([
        { path: CONFIG, source: 'auto', error: '', contexts: [ctx(CONFIG, 'prod')] },
    ]);
    vi.mocked(KubeconfigService.RestoreContext).mockReset().mockResolvedValue([
        { path: CONFIG, source: 'auto', error: '', contexts: [ctx(CONFIG, 'prod'), ctx(CONFIG, 'dev')] },
    ]);
});

test('each context row offers to remove that context, whether or not file names are shown', async () => {
    render(Sidebar);

    await page.getByRole('button', { name: 'Remove dev' }).click();

    expect(KubeconfigService.RemoveContext).toHaveBeenCalledWith(DEV);
    await expect.element(page.getByRole('button', { name: 'Remove prod' })).toBeInTheDocument();
    expect(document.querySelectorAll('.context').length).toBe(1);
});

// The removal outlives a rescan, so it has to be visible somewhere: without
// this the only way back was to delete the context from the kubeconfig, sync,
// and write it again -- and the sidebar's refresh button looked broken.
test('a removed context is listed under Hidden, where it can be brought back', async () => {
    workspace.files = [{ path: CONFIG, source: 'auto', error: '', contexts: [ctx(CONFIG, 'prod')] }];
    workspace.settings.excludedContexts = [DEV];
    render(Sidebar);

    await expect.element(page.getByText('Hidden')).toBeInTheDocument();

    await page.getByRole('button', { name: 'Show context dev again' }).click();

    expect(KubeconfigService.RestoreContext).toHaveBeenCalledWith(DEV);
    await expect.element(page.getByRole('button', { name: 'Remove dev' })).toBeInTheDocument();
    expect(document.querySelectorAll('.context').length).toBe(2);
});

// It is named by its context, not by the path its id is built from: nobody
// removed "/home/u/.kube/config::dev", they removed dev.
test('a removed context is listed by name', async () => {
    workspace.files = [{ path: CONFIG, source: 'auto', error: '', contexts: [ctx(CONFIG, 'prod')] }];
    workspace.settings.excludedContexts = [DEV];
    render(Sidebar);

    await expect.element(page.getByText('Hidden')).toBeInTheDocument();

    const row = [...document.querySelectorAll('.source-row')].find((el) =>
        el.querySelector('button')?.getAttribute('aria-label') === 'Show context dev again',
    );
    expect(row?.querySelector('.path')?.textContent).toBe('dev');
    expect(row?.querySelector('.path')?.getAttribute('title')).toContain(CONFIG);
});
