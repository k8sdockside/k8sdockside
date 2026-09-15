import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

// The runtime is what a link out actually goes through: a plain target=_blank
// does nothing in the webview the app runs in. See lib/links.ts.
const openURL = vi.fn().mockResolvedValue(undefined);
vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return { ...actual, Browser: { ...actual.Browser, OpenURL: openURL } };
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
const { SETTINGS } = await import('../catalogue');
const DocPage = (await import('./DocPage.svelte')).default;
const { session } = await import('../state/session.svelte');
type Page = import('../docs/types').Page;

// One component draws both documentation pages from data. What is tested is
// the drawing: the rail, the blocks, and the three kinds of button a page
// can carry -- into a cluster, into settings, and to the other page.

const PROD = '/home/u/.kube/config::admin@prod';

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--border:#46536a;--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);
--bg-active:rgba(255,255,255,.13);--text:#e8eef7;--text-dim:#a9b6c6;--text-faint:#8593a3;
--accent:#4a86ff;--accent-text:#fff;--ok:#5fd39b;--warn:#efb567;--error:#f4787f;
--radius:6px;--radius-sm:4px;--row-h:30px;--mono:monospace;--font:sans-serif;
font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0;width:900px;height:600px}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

const PAGE: Page = {
    title: 'A guide',
    lede: 'What this is about.',
    sections: [
        {
            id: 'first',
            label: 'First things',
            icon: 'info',
            blocks: [
                { type: 'p', text: 'Press **Sync** to read `~/.kube` again.' },
                { type: 'actions', actions: [{ kind: 'show', resource: 'pods', label: 'Show me the pods' }] },
                { type: 'links', links: [{ label: 'The official docs', href: 'https://kubernetes.io/docs/', note: 'Everything, eventually.' }] },
            ],
        },
        {
            id: 'second',
            label: 'Second things',
            icon: 'puzzle',
            blocks: [
                { type: 'p', text: 'Plugins live in a folder.' },
                { type: 'actions', actions: [{ kind: 'settings', section: 'plugins', label: 'Open plugin settings' }] },
            ],
        },
    ],
};

// The page remembers its section across instances, as the settings tab
// does, so every test starts by going back to the first one.
async function openPage(): Promise<void> {
    render(DocPage, { page: PAGE });
    await page.getByRole('tab', { name: 'First things' }).click();
}

beforeEach(() => {
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);
    openURL.mockClear();
    workspace.closeAllTabs();
    workspace.files = [{ path: '/home/u/.kube/config', source: 'auto', error: '', contexts: [
        { id: PROD, name: 'admin@prod', cluster: 'prod', user: 'admin', namespace: '', server: '', file: '/home/u/.kube/config', current: false },
    ] }];
    workspace.selectContext(PROD);
});

// One page serves the desktop app and the web version, with what only one of
// them can do marked for it. The page follows the session: it is the desktop
// app's until the window learns otherwise.
test('the page tells each version of the app only what is true of it', async () => {
    const MODES: Page = {
        title: 'Both versions',
        lede: '',
        sections: [
            {
                id: 'everywhere',
                label: 'Everywhere',
                icon: 'info',
                blocks: [
                    { type: 'p', text: 'Said in both.' },
                    { type: 'p', text: 'Pick a file from your disk.', only: 'desktop' },
                    { type: 'p', text: 'Ask an administrator.', only: 'web' },
                ],
            },
            { id: 'signing-in', label: 'Signing in', icon: 'server', only: 'web', blocks: [{ type: 'p', text: 'Sign in first.' }] },
        ],
    };
    const desktop = session.info;
    try {
        render(DocPage, { page: MODES });

        await expect.element(page.getByText('Pick a file from your disk.')).toBeVisible();
        expect(document.body.textContent).not.toContain('Ask an administrator.');
        expect(document.body.textContent).not.toContain('Signing in');

        session.info = { ...desktop, server: true, admin: false, username: 'ada' };

        await expect.element(page.getByText('Ask an administrator.')).toBeVisible();
        await expect.element(page.getByRole('tab', { name: 'Signing in' })).toBeVisible();
        expect(document.body.textContent).not.toContain('Pick a file from your disk.');
        await expect.element(page.getByText('Said in both.')).toBeVisible();
    } finally {
        session.info = desktop;
    }
});

test('the rail lists the sections and shows the first, with its marks drawn', async () => {
    await openPage();

    await expect.element(page.getByRole('tab', { name: 'First things' })).toBeVisible();
    await expect.element(page.getByRole('tab', { name: 'Second things' })).toBeVisible();
    expect(document.querySelector('strong')?.textContent).toBe('Sync');
    expect(document.querySelector('.doc code')?.textContent).toBe('~/.kube');
    expect(document.body.textContent).not.toContain('Plugins live in a folder');
});

test('choosing a section shows it', async () => {
    await openPage();

    await page.getByRole('tab', { name: 'Second things' }).click();

    await expect.element(page.getByText('Plugins live in a folder.')).toBeVisible();
});

test('"show me" opens the kind in the selected cluster', async () => {
    await openPage();

    await page.getByRole('button', { name: 'Show me the pods' }).click();

    expect(workspace.activeTab?.kind).toBe('pods');
    expect(workspace.activeTab?.contextId).toBe(PROD);
});

test('without a selected cluster, "show me" is offered but explains itself', async () => {
    workspace.files = [];
    await openPage();

    const button = page.getByRole('button', { name: 'Show me the pods' });
    await expect.element(button).toBeDisabled();
    await expect.element(button).toHaveAttribute('title', expect.stringContaining('Select a context'));
});

test('a settings button opens the settings tab', async () => {
    await openPage();
    await page.getByRole('tab', { name: 'Second things' }).click();

    await page.getByRole('button', { name: 'Open plugin settings' }).click();

    expect(workspace.activeTab?.kind).toBe(SETTINGS);
});

test('links go to the browser, not into the window', async () => {
    await openPage();

    const link = document.querySelector('a[href="https://kubernetes.io/docs/"]') as HTMLAnchorElement;
    // The inert target is the backstop: were the click below ever missed, the
    // webview would do nothing rather than navigate away from the app.
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    await expect.element(page.getByText('Everything, eventually.')).toBeVisible();

    const click = new MouseEvent('click', { bubbles: true, cancelable: true });
    link.dispatchEvent(click);

    expect(openURL).toHaveBeenCalledWith('https://kubernetes.io/docs/');
    // Nothing is left for the webview to do with the address afterwards.
    expect(click.defaultPrevented).toBe(true);
});
