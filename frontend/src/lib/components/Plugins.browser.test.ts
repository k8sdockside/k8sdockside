import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import type { Plugin } from '../plugins/types';

// What is worth testing in a real browser here is the state the whole feature
// exists for: the plugin is installed on this machine and the solution is not
// in this cluster. The sidebar has to say so without hiding the plugin, and the
// overview has to explain it rather than showing four empty tables.

const ARGO: Plugin = {
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

const Summary = vi.fn();

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
        ServiceGet: vi.fn().mockResolvedValue({ service: 'calico-system/whisker:8081', status: 200, contentType: 'application/json', body: '{}' }),
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
const PluginOverview = (await import('./PluginOverview.svelte')).default;

const PROD = '/home/u/.kube/config::admin@prod';

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--border:#46536a;--border-soft:rgba(255,255,255,.05);--bg-hover:rgba(255,255,255,.08);
--bg-active:rgba(255,255,255,.13);--text:#e8eef7;--text-dim:#a9b6c6;--text-faint:#8593a3;
--accent:#4a86ff;--accent-text:#fff;--ok:#5fd39b;--warn:#efb567;--error:#f4787f;
--radius:6px;--radius-sm:4px;--row-h:30px;--mono:monospace;--font:sans-serif;
font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0;width:900px}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}`;

/** A summary as the Go side hands one over. */
function summary(over: Partial<Record<string, unknown>> = {}) {
    return {
        pluginId: 'argocd',
        installed: true,
        checked: true,
        requirements: [
            { kind: 'crd:applications.argoproj.io', label: 'Applications', optional: false, served: true, error: '' },
            { kind: 'crd:applicationsets.argoproj.io', label: 'Application Sets', optional: true, served: false, error: '' },
        ],
        cards: [
            {
                label: 'Applications by health',
                kind: 'crd:applications.argoproj.io',
                total: 42,
                grouped: true,
                buckets: [
                    { value: 'Degraded', count: 1, tone: 'error' },
                    { value: 'Progressing', count: 3, tone: 'warn' },
                    { value: 'Healthy', count: 38, tone: 'ok' },
                ],
                error: '',
            },
        ],
        error: '',
        ...over,
    };
}

beforeEach(() => {
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);

    Summary.mockReset().mockResolvedValue(summary());
    workspace.pluginCatalogue = { plugins: [structuredClone(ARGO)], dir: '', folders: [], problems: [] };
    workspace.closeAllTabs();
});

test('the overview leads with the counts when the solution is installed', async () => {
    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await expect.element(page.getByText('installed here')).toBeVisible();
    await expect.element(page.getByText('42')).toBeVisible();
    // Worst first: the one Degraded application is why the tile is on screen.
    await expect.element(page.getByText('Degraded')).toBeVisible();
});

// The state the whole feature exists for.
test('a cluster without the CRDs is explained, not left as an empty table', async () => {
    Summary.mockResolvedValue(
        summary({
            installed: false,
            requirements: [
                {
                    kind: 'crd:applications.argoproj.io',
                    label: 'Applications',
                    optional: false,
                    served: false,
                    error: '',
                },
            ],
            cards: [
                {
                    label: 'Applications by health',
                    kind: 'crd:applications.argoproj.io',
                    total: 0,
                    grouped: true,
                    buckets: [],
                    error: 'this cluster does not serve crd:applications.argoproj.io',
                },
            ],
        }),
    );

    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await expect.element(page.getByText('not installed here')).toBeVisible();
    await expect.element(page.getByText('does not look installed in this cluster')).toBeVisible();
    // The missing kind is named, so the reader knows what to go and install.
    await expect.element(page.getByText('crd:applications.argoproj.io').first()).toBeVisible();
});

// "Could not ask" and "asked and was told no" call for opposite reactions.
test('an unreachable cluster does not read as "not installed"', async () => {
    Summary.mockResolvedValue(
        summary({
            installed: false,
            checked: false,
            error: 'dial tcp 10.0.0.1:6443: connect: connection refused',
            requirements: [
                {
                    kind: 'crd:applications.argoproj.io',
                    label: 'Applications',
                    optional: false,
                    served: false,
                    error: 'connection refused',
                },
            ],
        }),
    );

    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await expect.element(page.getByText('could not check')).toBeVisible();
    await expect.element(page.getByText('This cluster did not answer')).toBeVisible();
    await expect.element(page.getByText('not installed here')).not.toBeInTheDocument();
});

test('an optional requirement is marked as optional rather than as a failure', async () => {
    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await expect.element(page.getByText('Application Sets')).toBeVisible();
    await expect.element(page.getByText('optional')).toBeVisible();
    // It is missing, yet the plugin still reads as installed.
    await expect.element(page.getByText('installed here')).toBeVisible();
});

test('a view can be opened from the overview', async () => {
    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await page.getByRole('button', { name: 'Applications', exact: true }).click();

    expect(workspace.activeTab?.kind).toBe('plugin:argocd/applications');
});

// A tab restored after its plugin's folder was dropped. An empty page would
// read as "Argo CD has nothing", which is the wrong thing to conclude.
test('a tab whose plugin is gone says so', async () => {
    workspace.pluginCatalogue = { plugins: [], dir: '', folders: [], problems: [] };

    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await expect.element(page.getByText('This plugin is not installed')).toBeVisible();
    expect(Summary).not.toHaveBeenCalled();
});

// A grouped count is a picture before it is a list: how much of the ring is
// red says more at a glance than three numbers do.
test('a grouped card is drawn as a ring with one slice per bucket', async () => {
    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });
    await expect.element(page.getByText('installed here')).toBeVisible();

    await vi.waitFor(() => expect(document.querySelectorAll('.card .ring .slice').length).toBe(3));
    // The total sits in the middle of the ring, and the buckets beside it.
    expect(document.querySelector('.card .ring')?.textContent).toContain('42');
    await expect.element(page.getByText('Degraded')).toBeVisible();
});

// The card counts what a view lists, so the card is the way into that view.
test('a card opens the view that lists what it counts', async () => {
    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });
    await expect.element(page.getByText('installed here')).toBeVisible();

    await page.getByRole('button', { name: /Applications by health/ }).click();

    expect(workspace.activeTab?.kind).toBe('plugin:argocd/applications');
});

// An overview is one plugin's page. Asked for as bare "overview", the chart
// panel drew every installed plugin's overview charts on every overview --
// Argo CD's application health on the MetalLB overview.
test("the overview asks for its own plugin's charts, not every plugin's", async () => {
    const { MetricsService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');
    const Charts = vi.mocked(MetricsService.Charts);
    Charts.mockClear();
    workspace.metricsAttachments = ['plugin:argocd/overview', 'plugin:flux/overview'];

    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });
    await expect.element(page.getByText('installed here')).toBeVisible();

    await vi.waitFor(() => expect(Charts).toHaveBeenCalled());
    for (const call of Charts.mock.calls) {
        expect(call[1]).toBe('plugin:argocd/overview');
    }
    workspace.metricsAttachments = [];
});

// A plugin that draws its own landing page gets its page in the overview tab,
// in the same sandboxed frame as its other views.
test('a plugin with an overview of its own opens that page in its frame', async () => {
    const PluginFrame = (await import('./PluginFrame.svelte')).default;
    workspace.pluginCatalogue = {
        plugins: [
            {
                ...structuredClone(ARGO),
                id: 'acme',
                name: 'Acme',
                origin: '/plugins/acme/plugin.json',
                ui: { readable: [], write: false, registries: false, services: [] },
                overview: { entry: 'home.html' },
            },
        ],
        dir: '',
        folders: [],
        problems: [],
    };

    render(PluginFrame, { contextId: PROD, kind: 'plugin:acme/overview' });

    const frame = document.querySelector('iframe');
    expect(frame).not.toBeNull();
    expect(frame?.getAttribute('src')).toBe('/plugin-ui/acme/home.html?scheme=dark');
    expect(frame?.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame?.getAttribute('title')).toBe('Acme: Acme');
});

// A page may call only the services its plugin declares, and the query it
// sends reaches Go as lists of strings whatever shape the page gave it.
test('a page calls the services its plugin declares, and no others', async () => {
    const { PluginService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');
    const ServiceGet = vi.mocked(PluginService.ServiceGet);
    ServiceGet.mockClear();
    const PluginFrame = (await import('./PluginFrame.svelte')).default;
    workspace.pluginCatalogue = {
        plugins: [
            {
                ...structuredClone(ARGO),
                id: 'flows',
                name: 'Flows',
                origin: '/plugins/flows/plugin.json',
                ui: {
                    readable: [],
                    write: false,
                    registries: false,
                    services: [{ id: 'whisker', label: 'Whisker', where: 'the service labelled k8s-app=whisker:8081', paths: ['/whisker-backend/'] }],
                },
                overview: { entry: 'index.html' },
            },
        ],
        dir: '',
        folders: [],
        problems: [],
    };
    render(PluginFrame, { contextId: PROD, kind: 'plugin:flows/overview' });
    const frame = document.querySelector('iframe')!;

    const ask = (id: number, params: Record<string, unknown>) =>
        window.dispatchEvent(
            new MessageEvent('message', {
                data: { protocol: 'k8sdockside/plugin@1', id, method: 'services.get', params },
                source: frame.contentWindow,
            }),
        );
    ask(1, { service: 'whisker', path: '/whisker-backend/flows', query: { limit: 50, filter: ['a', 'b'], watch: false, skip: null } });
    ask(2, { service: 'prometheus', path: '/api/v1/query' });

    await vi.waitFor(() => expect(ServiceGet).toHaveBeenCalledTimes(1));
    expect(ServiceGet).toHaveBeenCalledWith(PROD, 'flows', 'whisker', '/whisker-backend/flows', {
        limit: ['50'],
        filter: ['a', 'b'],
        watch: ['false'],
    });
});

// The links say what a plugin is about; the generated overview carries them
// in its foot, and a docs link the links already hold is not repeated.
test('the overview links to what the plugin is about', async () => {
    workspace.pluginCatalogue = {
        plugins: [
            {
                ...structuredClone(ARGO),
                version: '2.1.0',
                links: [
                    { label: 'argoproj.github.io', url: 'https://argoproj.github.io/cd/' },
                    { label: 'Docs', url: 'https://argo-cd.readthedocs.io' },
                ],
            },
        ],
        dir: '',
        folders: [],
        problems: [],
    };
    render(PluginOverview, { contextId: PROD, kind: 'plugin:argocd/overview' });

    await expect.element(page.getByRole('link', { name: 'argoproj.github.io' })).toBeVisible();
    await expect.element(page.getByText('· v2.1.0')).toBeVisible();
    expect(page.getByRole('link', { name: 'Documentation' }).elements()).toHaveLength(0);
});

// Settings lists the plugins the app knows of with one button each, says
// which clusters run them, and lists a plugin that would not load reason by
// reason rather than as one run-on sentence.
test('settings offers the known plugins and explains the ones that would not load', async () => {
    const PluginsSection = (await import('./settings/PluginsSection.svelte')).default;
    workspace.knownPlugins = [
        {
            id: 'cert-manager',
            name: 'cert-manager',
            tagline: 'TLS certificates',
            icon: 'lock',
            description: 'When every certificate expires.',
            repo: 'https://github.com/k8sdockside/certmanager.git',
            detect: ['crd:certificates.cert-manager.io'],
            links: [{ label: 'cert-manager.io', url: 'https://cert-manager.io' }],
            author: 'Roger Westerbo',
            authorUrl: 'https://github.com/k8sdockside',
            official: true,
            installed: false,
        },
        {
            id: 'argocd',
            name: 'Argo CD',
            tagline: '',
            icon: 'rocket',
            description: '',
            repo: 'https://github.com/example/argocd.git',
            detect: [],
            links: [],
            official: true,
            installed: true,
        },
    ];
    workspace.pluginCatalogue = {
        plugins: [structuredClone(ARGO)],
        dir: '/plugins',
        folders: [],
        problems: [
            {
                path: '/plugins/acme/plugin.json',
                message: 'plugin "acme": unknown field "lable" in views[0] (line 7, column 9) -- did you mean "label"?\nplugin "acme" requires "widgets", which is not a kind this app can open',
            },
        ],
    };

    render(PluginsSection);

    // One offer: Argo CD is installed already, and is listed where it is.
    await expect.element(page.getByTitle('git clone https://github.com/k8sdockside/certmanager.git')).toBeVisible();
    expect(page.getByTitle(/^git clone/).elements()).toHaveLength(1);
    await expect.element(page.getByRole('link', { name: 'cert-manager.io' })).toBeVisible();
    // Each offer credits its author, linked, and says whose it is.
    await expect.element(page.getByRole('link', { name: 'Roger Westerbo' })).toHaveAttribute('href', 'https://github.com/k8sdockside');
    await expect.element(page.getByText('Official', { exact: true })).toBeVisible();
    // The built-in Argo CD card credits the project.
    await expect.element(page.getByText('Built in', { exact: true }).first()).toBeVisible();
    await expect.element(page.getByText('K8s Dockside', { exact: true })).toBeVisible();
    await expect.element(page.getByText('acme/plugin.json', { exact: true })).toBeVisible();
    await expect.element(page.getByText('did you mean "label"?', { exact: false })).toBeVisible();
    await expect.element(page.getByText('requires "widgets"', { exact: false })).toBeVisible();
    workspace.knownPlugins = [];
});

// WebKit lays a frame under a zoomed element out at the zoomed size and then
// zooms it again, so the page overflowed and scrolled both ways. The frame is
// taken out of the app's zoom and scaled back up instead; it has to cover
// exactly the room its host has.
test("a plugin's frame follows the app's zoom by scaling, not by zooming", async () => {
    const PluginFrame = (await import('./PluginFrame.svelte')).default;
    workspace.pluginCatalogue = {
        plugins: [
            {
                ...structuredClone(ARGO),
                id: 'acme',
                name: 'Acme',
                origin: '/plugins/acme/plugin.json',
                ui: { readable: [], write: false, registries: false, services: [] },
                overview: { entry: 'home.html' },
            },
        ],
        dir: '',
        folders: [],
        problems: [],
    };
    workspace.settings.layout.zoom = 1.25;
    try {
        const shell = document.createElement('div');
        shell.style.cssText = 'zoom: 1.25; width: 640px; height: 400px; display: flex;';
        document.body.appendChild(shell);
        render(PluginFrame, { target: shell, props: { contextId: PROD, kind: 'plugin:acme/overview' } });

        const frame = document.querySelector('iframe') as HTMLIFrameElement;
        const viewport = frame.parentElement as HTMLElement;
        expect(viewport.style.zoom).toBe(String(1 / 1.25));
        expect(frame.style.transform).toBe('scale(1.25)');

        // The frame on screen is the host on screen, to the pixel.
        const host = viewport.parentElement as HTMLElement;
        const a = host.getBoundingClientRect();
        const b = frame.getBoundingClientRect();
        expect(Math.round(b.width)).toBe(Math.round(a.width));
        expect(Math.round(b.height)).toBe(Math.round(a.height));
    } finally {
        workspace.settings.layout.zoom = 1;
    }
});
