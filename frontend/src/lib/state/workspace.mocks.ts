import { vi } from 'vitest';

// The Wails bindings, stubbed. The workspace talks to the Go side the moment it
// does anything, and what its tests check -- which tabs survive a close, where
// focus lands -- involves no cluster. Every workspace.*.test.ts file mocks the
// bindings module with this one:
//
//     vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => import('./workspace.mocks'));

export const HelmService = {
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
};

export const KubeconfigService = {
    Sync: vi.fn().mockResolvedValue([]),
    Files: vi.fn().mockResolvedValue([]),
    RemoveContext: vi.fn().mockResolvedValue([]),
};

export const ResourceService = {
    Describe: vi.fn().mockResolvedValue(''),
    Ping: vi.fn().mockResolvedValue(undefined),
    Disconnect: vi.fn().mockResolvedValue(undefined),
    CustomResourceKinds: vi.fn().mockResolvedValue([]),
};

export const LogService = {
    Containers: vi.fn().mockResolvedValue([]),
    Open: vi.fn().mockResolvedValue('logs-1'),
    Close: vi.fn(),
};

export const MetricsService = {
    Source: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
    SetEndpoint: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
    Rediscover: vi.fn().mockResolvedValue({ endpoint: {}, configured: '', available: false, error: '' }),
    Charts: vi.fn().mockResolvedValue({ source: { endpoint: {}, available: false, error: '', configured: '' }, charts: [], range: 60 }),
    Attachments: vi.fn().mockResolvedValue([]),
};

export const PluginService = {
    List: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    Reload: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    Summary: vi.fn().mockResolvedValue({ pluginId: '', installed: false, checked: true, requirements: [], cards: [], error: '' }),
    SetEnabled: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    Known: vi.fn().mockResolvedValue([]),
    InstallKnown: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    InstallFromGit: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    HideSuggestion: vi.fn().mockResolvedValue({}),
    Probe: vi.fn().mockResolvedValue({ known: [], absent: [] }),
};

export const ThemeService = {
    List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }),
    Tokens: vi.fn().mockResolvedValue([]),
    RevealDir: vi.fn().mockResolvedValue(undefined),
    CreateExample: vi.fn().mockResolvedValue(''),
    AddFolder: vi.fn().mockResolvedValue({}),
    RemoveFolder: vi.fn().mockResolvedValue({}),
    BrowseForFolder: vi.fn().mockResolvedValue({}),
};

export const TerminalService = {
    Containers: vi.fn().mockResolvedValue([]),
    Open: vi.fn().mockResolvedValue({ id: 'term-1', namespace: 'default', pod: 'web', container: 'app', node: '' }),
    OpenNode: vi.fn().mockResolvedValue({ id: 'term-1', namespace: 'default', pod: '', container: '', node: 'wrkr01' }),
    Send: vi.fn(),
    Resize: vi.fn(),
    Close: vi.fn(),
    Externals: vi.fn().mockResolvedValue({ terminals: [], kubectl: '', reason: '' }),
    Launch: vi.fn().mockResolvedValue(undefined),
    LaunchNode: vi.fn().mockResolvedValue(undefined),
};

export const PortForwardService = {
    List: vi.fn().mockResolvedValue([]),
    Ports: vi.fn().mockResolvedValue([]),
    Start: vi.fn().mockResolvedValue({ id: 'pf-1', localPort: 51234, state: 'active' }),
    Reconnect: vi.fn().mockResolvedValue({ id: 'pf-1', localPort: 51234, state: 'active' }),
    Stop: vi.fn(),
    Forget: vi.fn().mockResolvedValue(undefined),
    Open: vi.fn().mockResolvedValue(undefined),
    URL: vi.fn().mockResolvedValue(''),
};

export const SettingsService = {
    Get: vi.fn().mockResolvedValue({}),
    ConfigPath: vi.fn().mockResolvedValue(''),
    SetPanes: vi.fn().mockResolvedValue({}),
    SetLayout: vi.fn().mockResolvedValue({}),
    SetPreferences: vi.fn().mockResolvedValue({}),
    SetContextPrefs: vi.fn().mockResolvedValue({}),
};
