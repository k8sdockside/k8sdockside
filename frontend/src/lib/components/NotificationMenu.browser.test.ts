import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import NotificationMenu from './NotificationMenu.svelte';
import { notices } from '../state/notices.svelte';

// The bell asks the backend what it knows the moment it mounts, and every
// button on it is a call: what is under test is what the window shows for each
// answer, not the asking.
//
// Hoisted, because the mock factory is resolved before anything else in this
// file runs and a plain top-level const would not exist yet when it does.
const service = vi.hoisted(() => ({
    Status: vi.fn(),
    Check: vi.fn(),
    MarkRead: vi.fn(),
    OpenRelease: vi.fn(),
    OpenDownload: vi.fn(),
}));
const { Status, Check, MarkRead, OpenRelease, OpenDownload } = service;

// The rest of the backend is stubbed whole, the way the menu bar's test does
// it: the bell reports through the workspace, which names every service as it
// loads, and a mocked module has to provide each name it is asked for.
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
        Describe: vi.fn().mockResolvedValue(''),
        Namespaces: vi.fn().mockResolvedValue(['default']),
        ResourceYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        ApplyYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        CheckYAML: vi.fn().mockResolvedValue({ valid: true, message: '', line: 0 }),
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
    UpdateService: service,
}));

const { workspace } = await import('../state/workspace.svelte');
const { updates } = await import('../state/updates.svelte');

const RELEASE = {
    version: 'v0.0.3',
    name: 'v0.0.3',
    url: 'https://github.com/k8sdockside/k8sdockside/releases/tag/v0.0.3',
    publishedAt: '2026-09-05T16:02:10Z',
    assets: [],
};

function status(over: Record<string, unknown> = {}) {
    return { current: 'v0.0.2', latest: null, newer: false, unread: false, checkedAt: '', error: '', install: 'Debian package, amd64', download: '', ...over };
}

const NEWS = status({ latest: RELEASE, newer: true, unread: true, checkedAt: '2026-09-06T10:00:00Z' });

const bell = () => page.getByRole('button', { name: /^Notifications/ });

async function openPanel(): Promise<void> {
    await bell().click();
    await expect.element(page.getByRole('dialog', { name: 'Notifications' })).toBeVisible();
}

beforeEach(() => {
    document.body.innerHTML = '';
    Status.mockReset().mockResolvedValue(status());
    Check.mockReset().mockResolvedValue(status());
    MarkRead.mockReset().mockResolvedValue(status());
    OpenRelease.mockReset().mockResolvedValue(undefined);
    OpenDownload.mockReset().mockResolvedValue(undefined);
    updates.status = status({ current: '' });
    workspace.settings.preferences.checkForUpdates = true;
    notices.dismiss();
});

test('the bell is always there, and quiet when there is nothing unread', async () => {
    render(NotificationMenu);

    await expect.element(bell()).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Notifications' })).toBeVisible();
    expect(Status).toHaveBeenCalledOnce();
});

test('a newer release lights the bell', async () => {
    Status.mockResolvedValueOnce(NEWS);
    render(NotificationMenu);

    await expect.element(page.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();
});

test('opening it names the release and the build you are on', async () => {
    Status.mockResolvedValueOnce(NEWS);
    render(NotificationMenu);
    await expect.element(page.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();

    await openPanel();

    await expect.element(page.getByText('K8s Dockside v0.0.3 is available')).toBeVisible();
    await expect.element(page.getByText('You have v0.0.2', { exact: false })).toBeVisible();
});

// The whole point of the bell: the notice can be put away, and putting it
// away is not the same as it being gone.
test('marking it as read puts the dot away and keeps the notice', async () => {
    Status.mockResolvedValueOnce(NEWS);
    MarkRead.mockResolvedValueOnce({ ...NEWS, unread: false });
    render(NotificationMenu);
    await expect.element(page.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();
    await openPanel();

    await page.getByRole('button', { name: 'Mark as read' }).click();

    expect(MarkRead).toHaveBeenCalledOnce();
    await expect.element(page.getByRole('button', { name: 'Notifications' })).toBeVisible();
    await expect.element(page.getByText('K8s Dockside v0.0.3 is available')).toBeVisible();
    expect(document.querySelector('button[aria-label="Notifications, 1 unread"]')).toBeNull();
    await expect.element(page.getByRole('button', { name: 'View release' })).toBeVisible();
});

test('a read notice offers no second "mark as read"', async () => {
    Status.mockResolvedValueOnce({ ...NEWS, unread: false });
    render(NotificationMenu);
    await expect.element(bell()).toBeVisible();

    await openPanel();

    await expect.element(page.getByText('K8s Dockside v0.0.3 is available')).toBeVisible();
    expect([...document.querySelectorAll('button')].some((b) => b.textContent?.includes('Mark as read'))).toBe(false);
});

// The address comes from the backend, which built it from the tag. The window
// never holds a URL it could be talked into opening.
test('viewing the release hands off to the backend and closes the panel', async () => {
    Status.mockResolvedValueOnce(NEWS);
    render(NotificationMenu);
    await expect.element(page.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();
    await openPanel();

    await page.getByRole('button', { name: 'View release' }).click();

    expect(OpenRelease).toHaveBeenCalledOnce();
    await expect.element(page.getByRole('dialog')).not.toBeInTheDocument();
});

// The release page is always offered; the file for this install only when the
// backend found one, since it is the backend that knows what this build is.
test('offers the download for this install when the release has one', async () => {
    const DOWNLOAD = 'https://github.com/k8sdockside/k8sdockside/releases/download/v0.0.3/k8sdockside-0.0.3-linux-amd64.deb';
    Status.mockResolvedValueOnce({ ...NEWS, download: DOWNLOAD });
    render(NotificationMenu);
    await expect.element(page.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();
    await openPanel();

    const download = page.getByRole('button', { name: /^Download/ });
    await expect.element(download).toBeVisible();
    await expect.element(download).toHaveAttribute('title', expect.stringContaining('Debian package, amd64'));
    await download.click();

    expect(OpenDownload).toHaveBeenCalledOnce();
    await expect.element(page.getByRole('dialog')).not.toBeInTheDocument();
});

test('without a file for this install, only the release page is offered', async () => {
    Status.mockResolvedValueOnce(NEWS);
    render(NotificationMenu);
    await expect.element(page.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();
    await openPanel();

    await expect.element(page.getByRole('button', { name: 'View release' })).toBeVisible();
    expect([...document.querySelectorAll('button')].some((b) => b.textContent?.startsWith('Download'))).toBe(false);
});

test('with nothing new it says so, and offers to check', async () => {
    Status.mockResolvedValueOnce(status({ latest: { ...RELEASE, version: 'v0.0.2' }, checkedAt: '2026-09-06T10:00:00Z' }));
    render(NotificationMenu);
    await expect.element(bell()).toBeVisible();

    await openPanel();
    await expect.element(page.getByText("You're up to date", { exact: false })).toBeVisible();

    Check.mockResolvedValueOnce(NEWS);
    await page.getByRole('button', { name: 'Check now' }).click();

    expect(Check).toHaveBeenCalledOnce();
    await expect.element(page.getByText('K8s Dockside v0.0.3 is available')).toBeVisible();
});

test('a check that failed says so where the check button is', async () => {
    Status.mockResolvedValueOnce(status({ error: "GitHub's API rate limit was reached", checkedAt: '2026-09-06T10:00:00Z' }));
    render(NotificationMenu);
    await expect.element(bell()).toBeVisible();

    await openPanel();

    await expect.element(page.getByText('Could not check for updates.')).toBeVisible();
    await expect.element(page.getByText("GitHub's API rate limit was reached")).toBeVisible();
});

test('says when automatic checks are off, so nobody waits for news that is not coming', async () => {
    workspace.settings.preferences.checkForUpdates = false;
    render(NotificationMenu);
    await expect.element(bell()).toBeVisible();

    await openPanel();

    await expect.element(page.getByText('Update checks are off', { exact: false })).toBeVisible();
});

test('Escape closes the panel and returns focus to the bell', async () => {
    render(NotificationMenu);
    await expect.element(bell()).toBeVisible();
    await openPanel();

    await page.getByRole('dialog').element().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

    await expect.element(page.getByRole('dialog')).not.toBeInTheDocument();
    expect(document.activeElement).toBe(bell().element());
});
