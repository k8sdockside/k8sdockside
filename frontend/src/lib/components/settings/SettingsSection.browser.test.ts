import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import SettingsView from './SettingsView.svelte';

// The About section asks the backend what this build is the moment it mounts,
// and the sources section reads the file list; neither is what is under test.
vi.mock('../../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
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
    ResourceService: { Describe: vi.fn().mockResolvedValue('') },
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
        ConfigPath: vi.fn().mockResolvedValue('/home/u/.config/k8sdockside/settings.json'),
        About: vi.fn().mockResolvedValue({ version: 'test', wails: 'test', go: 'test', platform: 'test' }),
        SetPanes: vi.fn().mockResolvedValue({}),
        SetLayout: vi.fn().mockResolvedValue({}),
        SetPreferences: vi.fn().mockResolvedValue({}),
        SetContextPrefs: vi.fn().mockResolvedValue({}),
    },
    UpdateService: {
        Status: vi.fn().mockResolvedValue({ current: 'test', latest: null, newer: false, unread: false, checkedAt: '', error: '' }),
        Check: vi.fn().mockResolvedValue({ current: 'test', latest: null, newer: false, unread: false, checkedAt: '', error: '' }),
        MarkRead: vi.fn().mockResolvedValue({ current: 'test', latest: null, newer: false, unread: false, checkedAt: '', error: '' }),
        OpenRelease: vi.fn().mockResolvedValue(undefined),
        OpenDownload: vi.fn().mockResolvedValue(undefined),
    },
}));

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--border:#46536a;--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);
--bg-active:rgba(255,255,255,.13);--text:#e8eef7;--text-dim:#a9b6c6;--text-faint:#8593a3;
--accent:#4a86ff;--ok:#5fd39b;--warn:#efb567;--error:#f4787f;--radius:6px;--radius-sm:4px;
--mono:monospace;--font:-apple-system,sans-serif;font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0;width:900px}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

const rail = () => page.getByRole('tab');

beforeEach(() => {
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);
});

test('the sections are ordered with the everyday ones first and About last', async () => {
    render(SettingsView);

    const labels = (await rail().all()).map((item) => item.element().textContent?.trim());
    expect(labels).toEqual([
        'Appearance',
        'Themes',
        'Plugins',
        'Behaviour',
        'Terminal',
        'Helm',
        'Kubeconfig sources',
        'About',
    ]);
});

test('it opens on Appearance', async () => {
    render(SettingsView);

    await expect.element(page.getByRole('tab', { name: 'Appearance' })).toHaveAttribute('aria-selected', 'true');
});

// These two run in order on purpose: together they are one scenario that
// cannot be written as a single test, because what is being checked is that
// the choice outlives the component instance that made it.
//
// The shell wraps the active view in {#key activeTab.id}, so switching to a
// cluster tab destroys this component and returning builds a new one. The
// section therefore cannot live in component state -- it used to, and every
// visit landed back on the first section.
test('choosing a section selects it', async () => {
    render(SettingsView);

    await page.getByRole('tab', { name: 'Behaviour' }).click();

    await expect.element(page.getByRole('tab', { name: 'Behaviour' })).toHaveAttribute('aria-selected', 'true');
});

test('...and a freshly mounted view comes back to it, not to the first section', async () => {
    render(SettingsView);

    await expect.element(page.getByRole('tab', { name: 'Behaviour' })).toHaveAttribute('aria-selected', 'true');
    await expect.element(page.getByRole('tab', { name: 'Appearance' })).toHaveAttribute('aria-selected', 'false');
});
