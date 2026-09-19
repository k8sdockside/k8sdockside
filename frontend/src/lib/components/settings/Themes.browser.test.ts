import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import type { Theme } from '../../theme/apply';

// The gallery draws every theme in its own palette at once, which is the whole
// point of it -- so what is worth testing in a real browser is that a card
// actually wears its theme's colours rather than the app's, and that a theme
// the app could not find is said out loud instead of silently swapped.
const THEMES: Theme[] = [
    {
        id: 'k8sdockside-dark',
        name: 'K8s Dockside Dark',
        tagline: 'default dark · navy + blue',
        base: 'dark',
        author: '',
        tokens: { bg: '#10151c' },
        resolved: {
            bg: '#10151c',
            'bg-sidebar': '#151b24',
            'bg-panel': '#19202a',
            'bg-raised': '#212b38',
            border: '#46536a',
            'border-soft': 'rgba(255,255,255,.05)',
            text: '#e8eef7',
            'text-dim': '#a9b6c6',
            'text-faint': '#8593a3',
            accent: '#4a86ff',
            'accent-text': '#ffffff',
            ok: '#5fd39b',
            warn: '#efb567',
            error: '#f4787f',
        },
        origin: 'builtin',
        pack: '',
        warnings: [],
    },
    {
        id: 'acme-neon',
        name: 'Acme Neon',
        tagline: 'loud dark · neon pink',
        base: 'dark',
        author: 'someone',
        tokens: { accent: '#ff3d9a' },
        resolved: {
            bg: '#0b0b12',
            'bg-sidebar': '#0b0b12',
            'bg-panel': '#0b0b12',
            'bg-raised': '#1a1a26',
            border: '#333344',
            'border-soft': 'rgba(255,255,255,.05)',
            text: '#f2e9ff',
            'text-dim': '#c0b0d0',
            'text-faint': '#9a8ab0',
            accent: '#ff3d9a',
            'accent-text': '#1a0010',
            ok: '#5fd39b',
            warn: '#efb567',
            error: '#f4787f',
        },
        origin: '/home/u/.config/k8sdockside/themes/neon.json',
        pack: 'Acme Pack',
        warnings: ['text-faint on bg-raised is 3.9:1, below the 4.5:1 needed to be readable'],
    },
];

const SetPreferences = vi.fn().mockResolvedValue({});
const List = vi.fn();
const RemoveFolder = vi.fn();

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
    LogService: { Containers: vi.fn().mockResolvedValue([]), Open: vi.fn(), Close: vi.fn() },
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
        List,
        Tokens: vi.fn().mockResolvedValue([{ name: 'bg', help: 'The window behind everything.' }]),
        RevealDir: vi.fn().mockResolvedValue(undefined),
        CreateExample: vi.fn().mockResolvedValue('/tmp/my-theme.json'),
        AddFolder: vi.fn(),
        RemoveFolder,
        BrowseForFolder: vi.fn(),
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
        SetPreferences,
        SetContextPrefs: vi.fn().mockResolvedValue({}),
    },
}));

const { workspace } = await import('../../state/workspace.svelte');
const ThemesSection = (await import('./ThemesSection.svelte')).default;

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--border:#46536a;--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);
--bg-active:rgba(255,255,255,.13);--text:#e8eef7;--text-dim:#a9b6c6;--text-faint:#8593a3;
--accent:#4a86ff;--accent-text:#fff;--ok:#5fd39b;--warn:#efb567;--error:#f4787f;
--radius:6px;--radius-sm:4px;--mono:monospace;--font:-apple-system,sans-serif;
font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0;width:900px}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

beforeEach(() => {
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);

    List.mockReset().mockResolvedValue({ themes: THEMES, dir: '/home/u/.config/k8sdockside/themes', folders: [], problems: [] });
    RemoveFolder.mockReset().mockResolvedValue({ themes: THEMES, dir: '', folders: [], problems: [] });
    SetPreferences.mockClear();

    workspace.themeCatalogue = {
        themes: structuredClone(THEMES),
        dir: '/home/u/.config/k8sdockside/themes',
        folders: [],
        problems: [],
    };
    workspace.settings.preferences = { ...workspace.settings.preferences, theme: 'k8sdockside-dark' };
});

test('every theme is offered, ours and theirs told apart', async () => {
    render(ThemesSection);

    await expect.element(page.getByRole('button', { name: /K8s Dockside Dark/ })).toBeVisible();
    await expect.element(page.getByRole('button', { name: /Acme Neon/ })).toBeVisible();
    // The user's own theme says which file it came from; a built-in has no
    // file to name and would only be showing the word "builtin".
    await expect.element(page.getByText('neon.json', { exact: false })).toBeVisible();
});

test('the theme in use is the pressed one', async () => {
    render(ThemesSection);

    await expect
        .element(page.getByRole('button', { name: /K8s Dockside Dark/ }))
        .toHaveAttribute('aria-pressed', 'true');
    await expect.element(page.getByRole('button', { name: /Acme Neon/ })).toHaveAttribute('aria-pressed', 'false');
});

test('choosing a theme saves it', async () => {
    render(ThemesSection);

    await page.getByRole('button', { name: /Acme Neon/ }).click();

    expect(workspace.theme).toBe('acme-neon');
    await vi.waitFor(() => {
        expect(SetPreferences).toHaveBeenCalled();
        expect(SetPreferences.mock.lastCall?.[0].theme).toBe('acme-neon');
    });
});

// The point of a preview over a row of swatches: each card is drawn in its own
// theme, not in the app's, so two themes can be compared by looking at them.
test('a card is painted in its own theme rather than the current one', async () => {
    render(ThemesSection);

    const neon = page.getByRole('button', { name: /Acme Neon/ }).element();
    const selected = neon.querySelector('.nav.on') as HTMLElement;

    expect(selected).not.toBeNull();
    // #ff3d9a, which is the Acme theme's accent and nothing the app is wearing.
    expect(getComputedStyle(selected).backgroundColor).toBe('rgb(255, 61, 154)');
});

test('a theme with readability problems says so on its card', async () => {
    render(ThemesSection);

    await expect.element(page.getByText('1 readability warning')).toBeVisible();
});

test('a theme file that would not load is named, with the reason', async () => {
    workspace.themeCatalogue = {
        ...workspace.themeCatalogue,
        problems: [{ path: '/home/u/themes/broken.json', message: 'not valid JSON' }],
    };
    render(ThemesSection);

    await expect.element(page.getByText('/home/u/themes/broken.json')).toBeVisible();
    await expect.element(page.getByText('not valid JSON')).toBeVisible();
});

// Falling back quietly would leave the user wondering why their theme is not
// on; rewriting their choice would lose it the moment the folder came back.
test('a theme that is not installed is called out, and the choice is kept', async () => {
    workspace.settings.preferences = { ...workspace.settings.preferences, theme: 'gone-missing' };
    render(ThemesSection);

    await expect.element(page.getByText('gone-missing')).toBeVisible();
    expect(workspace.theme).toBe('gone-missing');
    expect(workspace.activeTheme?.id).toBe('k8sdockside-dark');
});

test('the token reference is there when asked for and not before', async () => {
    workspace.themeTokens = [{ name: 'bg', help: 'The window behind everything.' }];
    render(ThemesSection);

    await expect.element(page.getByText('The window behind everything.')).not.toBeInTheDocument();

    await page.getByRole('button', { name: /colours a theme can set/ }).click();

    await expect.element(page.getByText('The window behind everything.')).toBeVisible();
});

test('the extra folders are listed with a way to drop each', async () => {
    workspace.themeCatalogue = { ...workspace.themeCatalogue, folders: ['/home/u/dotfiles/themes'] };
    render(ThemesSection);

    await page.getByRole('button', { name: 'Stop reading themes from /home/u/dotfiles/themes' }).click();

    await vi.waitFor(() => expect(RemoveFolder).toHaveBeenCalledWith('/home/u/dotfiles/themes'));
});
