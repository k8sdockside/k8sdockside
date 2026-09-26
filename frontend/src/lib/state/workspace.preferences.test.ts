import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => import('./workspace.mocks'));

const {
    workspace,
    clusters,
    ResourceService,
    SettingsService,
    ThemeService,
    PROD,
    open,
} = await import('./workspace.fixtures');

beforeEach(() => {
    workspace.closeAllTabs();
    clusters.prune([]);
    vi.mocked(ResourceService.Ping).mockReset().mockResolvedValue(undefined);
    expect(workspace.tabs).toHaveLength(0);
});

describe('preferences', () => {
    beforeEach(() => {
        workspace.settings.preferences = {
            theme: 'k8sdockside-dark',
            density: 'comfortable',
            restoreTabs: true,
            confirmSourceRemoval: false,
            showKubeconfigNames: false,
            contextSort: 'name',
            showLineNumbers: true,
            checkForUpdates: true,
            desktopNotifications: true,
            alerts: 'system',
            alertsSnoozedUntil: '',
            dateTime: { clock: 'system', dates: 'system', zone: 'local', ages: 'relative' },
            metricsRange: 60,
            terminal: {
                mode: 'app',
                external: '',
                shells: ['bash', 'sh'],
                nodeImage: 'busybox',
                nodeNamespace: 'default',
                fontSize: 12,
                scrollback: 5000,
            },
            helm: { path: '', wait: false, atomic: false, timeoutSeconds: 300 },
            background: { source: 'builtin', pinned: '', minutes: 15, palette: 'varied' },
        };
    });

    test('the chosen theme is stored by id', () => {
        workspace.setTheme('nord');
        expect(workspace.theme).toBe('nord');
    });

    test('turning tab restore off is a choice that sticks', () => {
        workspace.setRestoreTabs(false);
        // `??` rather than `||` on the way in: false must not read as unset.
        expect(workspace.restoreTabsOnLaunch).toBe(false);
    });
});

describe('saving settings', () => {
    // The bug this guards against: opening an editor fills the bottom pane and
    // unfolds it, and a settings write from another section already on its way
    // answers with the file as it was before either happened. Adopting that
    // answer whole shut the pane again a quarter of a second after it opened.
    //
    // Putting every pane in one section removed the version of this that had
    // two of them racing each other. What is left is the cross-section case,
    // which is the one adopt()'s pending guard exists for.
    test('an answer from a write in flight does not roll back a later change', async () => {
        vi.useFakeTimers();
        let landed: (saved: unknown) => void = () => {};
        try {
            // The layout call answers with a file that predates the pane being
            // filled, which is what one in flight really carries...
            vi.mocked(SettingsService.SetLayout).mockResolvedValue({} as never);
            // ...while the panes' own call is still out, so its answer cannot
            // be what puts things right.
            vi.mocked(SettingsService.SetPanes).mockReturnValue(
                new Promise((resolve) => {
                    landed = resolve;
                }) as never,
            );

            workspace.setSidebarWidth(300);
            workspace.openEditor({ contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' });
            await vi.advanceTimersByTimeAsync(1000);

            expect(workspace.dockTabs.map((t) => t.name)).toEqual(['web']);
            expect(workspace.dockOpen).toBe(true);
            // And what was sent is the pane as it actually stood.
            expect(SettingsService.SetPanes).toHaveBeenCalledWith(
                expect.objectContaining({
                    bottom: expect.objectContaining({
                        open: true,
                        tabs: [expect.objectContaining({ name: 'web' })],
                    }),
                }),
            );
        } finally {
            landed({});
            vi.useRealTimers();
            vi.mocked(SettingsService.SetLayout).mockReset().mockResolvedValue({} as never);
            vi.mocked(SettingsService.SetPanes).mockReset().mockResolvedValue({} as never);
        }
    });

    // The cluster tree's pane was left out of the write, so its width -- and
    // anything moved into it -- was back at the default after every restart.
    test('writes the left pane with the others, so its width survives a restart', async () => {
        vi.mocked(SettingsService.SetPanes).mockClear();

        workspace.setSidebarWidth(300);
        await vi.waitFor(() => expect(SettingsService.SetPanes).toHaveBeenCalled());

        const written = vi.mocked(SettingsService.SetPanes).mock.calls.at(-1)![0];
        expect(written.left?.size).toBe(300);
        expect(written.left?.tabs?.map((t) => t?.type)).toContain('clusters');
    });
});

describe('themes', () => {
    /** A theme as the Go side hands one over. */
    function theme(id: string, extra: Record<string, unknown> = {}) {
        return {
            id,
            name: id,
            tagline: '',
            base: 'dark',
            author: '',
            tokens: { bg: '#000000' },
            resolved: { bg: '#000000', text: '#ffffff' },
            origin: 'builtin',
            pack: '',
            repo: '',
            warnings: [],
            ...extra,
        };
    }

    beforeEach(() => {
        workspace.themeCatalogue = { themes: [], dir: '', folders: [], problems: [] };
        workspace.settings.preferences = { ...workspace.settings.preferences, theme: 'k8sdockside-dark' };
    });

    test('loads the catalogue and the token documentation', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark'), theme('nord')],
            dir: '/home/u/.config/k8sdockside/themes',
            folders: ['/home/u/dotfiles/themes'],
            problems: [{ path: '/tmp/bad.json', message: 'not valid JSON' }],
        });
        vi.mocked(ThemeService.Tokens).mockResolvedValueOnce([{ name: 'bg', help: 'The window.' }]);

        await workspace.loadThemes();

        expect(workspace.themes.map((t) => t.id)).toEqual(['k8sdockside-dark', 'nord']);
        expect(workspace.themeDir).toBe('/home/u/.config/k8sdockside/themes');
        expect(workspace.themeFolders).toEqual(['/home/u/dotfiles/themes']);
        expect(workspace.themeProblems).toHaveLength(1);
        expect(workspace.themeTokens.map((t) => t.name)).toEqual(['bg']);
    });

    test('a null theme list does not become undefined further in', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: null,
            dir: '',
            folders: null,
            problems: null,
        });

        await workspace.loadThemes();

        expect(workspace.themes).toEqual([]);
        expect(workspace.themeFolders).toEqual([]);
        expect(workspace.themeProblems).toEqual([]);
    });

    test('the active theme is the one the settings name', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark'), theme('nord')],
            dir: '',
            folders: [],
            problems: [],
        });
        await workspace.loadThemes();

        workspace.setTheme('nord');

        expect(workspace.activeTheme?.id).toBe('nord');
        expect(workspace.themeMissing).toBe(false);
    });

    // Falling back rather than failing, and saying so rather than silently
    // rewriting the choice: the theme may be one folder away from coming back.
    test('a theme that is not installed falls back to the default and is flagged', async () => {
        vi.mocked(ThemeService.List).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark')],
            dir: '',
            folders: [],
            problems: [],
        });
        await workspace.loadThemes();

        workspace.setTheme('someone-elses-theme');

        expect(workspace.activeTheme?.id).toBe('k8sdockside-dark');
        expect(workspace.themeMissing).toBe(true);
        // The choice itself is untouched, so reinstalling the theme restores it.
        expect(workspace.theme).toBe('someone-elses-theme');
    });

    test('nothing is missing before the catalogue has loaded', () => {
        workspace.setTheme('nord');
        expect(workspace.themeMissing).toBe(false);
        expect(workspace.activeTheme).toBeNull();
    });

    test('dropping a folder replaces the catalogue with what the service returns', async () => {
        vi.mocked(ThemeService.RemoveFolder).mockResolvedValueOnce({
            themes: [theme('k8sdockside-dark')],
            dir: '',
            folders: [],
            problems: [],
        });

        await workspace.removeThemeFolder('/home/u/dotfiles/themes');

        expect(workspace.themeFolders).toEqual([]);
        expect(ThemeService.RemoveFolder).toHaveBeenCalledWith('/home/u/dotfiles/themes');
    });
});

// The distinction this whole feature turns on: a plugin is installed on *this
// machine*, and the solution it describes is installed in a *cluster*. Those
// come apart constantly, and the sidebar has to say which it means.
