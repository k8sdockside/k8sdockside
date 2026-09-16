import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import DetailPanel from './DetailPanel.svelte';
import { detail } from '../state/detail.svelte';

// The real backend answers every settings write with the whole settings file,
// and the store adopts that answer whole. A mock answering `{}` says instead
// that every other section is empty, so a debounced write landing a quarter of
// a second into a test undoes whatever it had just set -- the dock folds itself
// back up, a preference goes back to its default -- which is a race the test
// loses about half the time. So the settings mock keeps what it is given and
// hands all of it back, the way the file does.
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
vi.mock('../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services', () => ({
    KubeconfigService: { Sync: vi.fn().mockResolvedValue([]), Files: vi.fn().mockResolvedValue([]) },
    ResourceService: {
        // The describe panel draws a budget for what it is describing, and
        // starts reading it as it mounts.
        Budget: vi.fn().mockResolvedValue({
            scope: { kind: 'cluster', name: '' },
            amounts: [],
            usage: { source: '', error: '' },
        }),
        CPUDelay: vi.fn().mockResolvedValue({ nodes: [], error: '' }),
        Describe: vi.fn().mockResolvedValue('Name: web'),
        ResourceYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        CheckYAML: vi.fn().mockResolvedValue({ valid: true, message: '', line: 0 }),
    },
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }),
        Delete: vi.fn().mockResolvedValue(undefined),
        Scale: vi.fn().mockResolvedValue(undefined),
        Restart: vi.fn().mockResolvedValue(undefined),
        Cordon: vi.fn().mockResolvedValue(undefined),
        Drain: vi.fn().mockResolvedValue('drain-1'),
        CancelDrain: vi.fn(),
    },
    LogService: {
        Containers: vi.fn().mockResolvedValue([]),
        Open: vi.fn().mockResolvedValue('logs-1'),
        Close: vi.fn(),
    },
    HelmService: {
        Releases: vi.fn().mockResolvedValue({ kind: 'helmreleases', columns: [], rows: [], namespaced: true, error: '' }),
        Detail: vi.fn().mockResolvedValue({
            name: 'ingress-nginx',
            namespace: 'default',
            revision: 7,
            status: 'deployed',
            chart: 'ingress-nginx-4.11.3',
            chartName: 'ingress-nginx',
            chartVersion: '4.11.3',
            appVersion: '1.11.3',
            description: 'Upgrade complete',
            firstDeployed: '2026-06-01T09:00:00Z',
            updated: '2026-08-05T11:27:04Z',
            notes: '',
            values: 'replicaCount: 2\n',
            userValues: '',
            resources: [],
            revisions: [],
        }),
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
}));

const { workspace } = await import('../state/workspace.svelte');
const { changes } = await import('../state/changes.svelte');
const { HelmService, ResourceService } = await import('../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services');

/** One release as the backend decodes it. See internal/kube/helmdetail.go. */
function releaseDetail(name: string, chart: string, values: string) {
    return {
        name,
        namespace: 'default',
        revision: 7,
        status: 'deployed',
        chart,
        chartName: name,
        chartVersion: '4.11.3',
        appVersion: '1.11.3',
        description: 'Upgrade complete',
        firstDeployed: '2026-06-01T09:00:00Z',
        updated: '2026-08-05T11:27:04Z',
        notes: '',
        values,
        userValues: '',
        resources: [],
        revisions: [],
    };
}

const PROD = '/home/u/.kube/prod::admin@prod';
const WEB = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };

/** Long enough for an effect to notice and its describe to answer. */
const settle = () => new Promise((r) => setTimeout(r, 150));

beforeEach(() => {
    workspace.closeAllDockTabs();
    detail.close();
    vi.mocked(ResourceService.Describe).mockReset().mockResolvedValue('Name: web\nStatus: Running');
    vi.mocked(HelmService.Detail)
        .mockReset()
        .mockResolvedValue(releaseDetail('ingress-nginx', 'ingress-nginx-4.11.3', 'replicaCount: 2\n'));
});

test('the panel offers to edit what it is describing', async () => {
    render(DetailPanel);
    await detail.open({ contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' });

    await page.getByRole('button', { name: 'Edit' }).click();

    // The object is now open in the dock, under its own name.
    expect(workspace.dockTabs.map((t) => t.name)).toEqual(['web']);
    expect(workspace.dockOpen).toBe(true);
});

// A Helm release is not a Kubernetes object -- it is a Secret the backend
// decodes -- so there is nothing here for an editor to open.
test('a Helm release has no edit button', async () => {
    render(DetailPanel);
    await detail.open({
        contextId: PROD,
        kind: 'helmreleases',
        namespace: 'default',
        name: 'ingress-nginx',
    });

    await expect.element(page.getByRole('heading', { name: 'ingress-nginx' })).toBeVisible();
    expect(page.getByRole('button', { name: 'Edit' }).elements()).toHaveLength(0);
});

// The bug this whole path exists for. A release has no Kubernetes kind, so the
// describe call it used to make could only answer "unknown resource kind:
// helmreleases" -- which is what the panel showed. It reads the release's own
// record now, and must not make that call at all.
test('a Helm release is described by its own record rather than by a describe call', async () => {
    render(DetailPanel);
    await detail.open({
        contextId: PROD,
        kind: 'helmreleases',
        namespace: 'default',
        name: 'ingress-nginx',
    });

    await expect.element(page.getByText('ingress-nginx-4.11.3')).toBeVisible();
    await expect.element(page.getByText('replicaCount: 2')).toBeVisible();
    expect(ResourceService.Describe).not.toHaveBeenCalled();
});

// Opening a second release must not leave the first one's values sitting under
// the second one's name.
test('opening another release re-reads the drawer', async () => {
    render(DetailPanel);
    await detail.open({
        contextId: PROD,
        kind: 'helmreleases',
        namespace: 'default',
        name: 'ingress-nginx',
    });
    await expect.element(page.getByText('replicaCount: 2')).toBeVisible();

    vi.mocked(HelmService.Detail).mockResolvedValue(
        releaseDetail('cert-manager', 'cert-manager-v1.16.1', 'installCRDs: true\n'),
    );
    await detail.open({
        contextId: PROD,
        kind: 'helmreleases',
        namespace: 'default',
        name: 'cert-manager',
    });

    await expect.element(page.getByText('installCRDs: true')).toBeVisible();
    await expect.element(page.getByText('replicaCount: 2')).not.toBeInTheDocument();
    expect(HelmService.Detail).toHaveBeenLastCalledWith(PROD, 'default', 'cert-manager');
});

// The bug this exists for: the panel is describing an object, the object is
// edited in the dock beside it, and the panel goes on showing what the cluster
// had before the save.
test('an object written elsewhere brings the report up to date', async () => {
    render(DetailPanel);
    await detail.open(WEB);
    await expect.element(page.getByText('Status: Running')).toBeVisible();

    vi.mocked(ResourceService.Describe).mockResolvedValue('Name: web\nStatus: Pending');
    changes.changed(WEB);

    await expect.element(page.getByText('Status: Pending')).toBeVisible();
});

test('an object the panel is not describing leaves it alone', async () => {
    render(DetailPanel);
    await detail.open(WEB);
    await expect.element(page.getByText('Status: Running')).toBeVisible();

    changes.changed({ ...WEB, name: 'api' });
    await settle();

    expect(ResourceService.Describe).toHaveBeenCalledTimes(1);
});

// Re-reading must not put the panel back into its loading state: the report it
// already has is a moment out of date, and blanking it flickers on every save.
test('the report stays on screen while it is re-read', async () => {
    render(DetailPanel);
    await detail.open(WEB);

    let answer: (text: string) => void = () => {};
    vi.mocked(ResourceService.Describe).mockReturnValueOnce(
        new Promise<string>((resolve) => {
            answer = resolve;
        }) as never,
    );
    changes.changed(WEB);
    await settle();

    await expect.element(page.getByText('Status: Running')).toBeVisible();
    expect(page.getByText('Describing web').elements()).toHaveLength(0);

    answer('Name: web\nStatus: Pending');
    await expect.element(page.getByText('Status: Pending')).toBeVisible();
});

const CREDS = { contextId: PROD, kind: 'secrets', namespace: 'default', name: 'creds' };
const HIDDEN = 'Name: creds\ndata:\n  password: aHVudGVyMg==\n  username: YWRtaW4=\n';
const SHOWN = 'Name: creds\nstringData:\n  password: hunter2\n  username: admin\n';

// A Secret reads back base64, which is exactly as unhelpful on screen as it is
// in the editor. The values are there to be seen when asked for -- and only
// when asked for, since a panel is often open while a screen is being shared.
test('a secret opens hidden and reveals its values on request', async () => {
    vi.mocked(ResourceService.Describe).mockResolvedValue(HIDDEN);
    render(DetailPanel);
    await detail.open(CREDS);
    await settle();

    expect(ResourceService.Describe).toHaveBeenLastCalledWith(PROD, 'secrets', 'default', 'creds', false);

    vi.mocked(ResourceService.Describe).mockResolvedValue(SHOWN);
    await page.getByRole('button', { name: 'Show values' }).click();
    await settle();

    // Read again with reveal set, rather than decoded from the report already
    // on screen: the plaintext crosses only once somebody has asked for it.
    expect(ResourceService.Describe).toHaveBeenLastCalledWith(PROD, 'secrets', 'default', 'creds', true);
    await expect.element(page.getByRole('button', { name: 'Hide values' })).toBeVisible();
});

// Revealing one secret must not leave the next one revealed: that would put a
// password on screen because of a button pressed on a different object.
test('the next object opens hidden again', async () => {
    vi.mocked(ResourceService.Describe).mockResolvedValue(HIDDEN);
    render(DetailPanel);
    await detail.open(CREDS);
    await settle();
    await page.getByRole('button', { name: 'Show values' }).click();
    await settle();
    expect(detail.revealed).toBe(true);

    await detail.open({ ...CREDS, name: 'other' });
    await settle();

    expect(detail.revealed).toBe(false);
    expect(ResourceService.Describe).toHaveBeenLastCalledWith(PROD, 'secrets', 'default', 'other', false);
});

// Nothing else has values to hide, so nothing else carries the button.
test('only a secret offers the button', async () => {
    render(DetailPanel);
    await detail.open(WEB);
    await settle();

    await expect.element(page.getByRole('button', { name: 'Show values' })).not.toBeInTheDocument();
});

test('the report can be searched, and says how many matches there are', async () => {
    vi.mocked(ResourceService.Describe).mockResolvedValue(HIDDEN);
    render(DetailPanel);
    await detail.open(CREDS);
    await settle();

    await page.getByRole('textbox', { name: 'Find in this report' }).fill('name');

    // Case-insensitive, so "Name:" is found by typing it in lower case.
    await expect.element(page.getByText('2 matches')).toBeVisible();
    await expect.poll(() => document.querySelectorAll('.body mark').length).toBe(2);
});

test('a search with nothing to find says so rather than emptying the report', async () => {
    vi.mocked(ResourceService.Describe).mockResolvedValue(HIDDEN);
    render(DetailPanel);
    await detail.open(CREDS);
    await settle();

    await page.getByRole('textbox', { name: 'Find in this report' }).fill('nowhere');

    await expect.element(page.getByText('No matches')).toBeVisible();
    // The report is still whole: highlighting marks matches, it does not filter
    // lines away, because a YAML line out of its block says much less.
    await expect.poll(() => document.querySelector('.body pre')?.textContent).toBe(HIDDEN);
});

test('the query is dropped when the panel moves to another object', async () => {
    vi.mocked(ResourceService.Describe).mockResolvedValue(HIDDEN);
    render(DetailPanel);
    await detail.open(CREDS);
    await settle();
    await page.getByRole('textbox', { name: 'Find in this report' }).fill('password');
    await expect.element(page.getByText('1 match')).toBeVisible();

    await detail.open({ ...CREDS, name: 'other' });
    await settle();

    await expect.poll(
        () => (document.querySelector('.find input') as HTMLInputElement | null)?.value,
    ).toBe('');
});

// Selecting copies a part; the button copies all of it, which is what goes
// into a ticket or a chat.
test('the whole report can be copied', async () => {
    const write = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined);
    render(DetailPanel);
    await detail.open(WEB);
    await settle();
    // A search only marks the report; what is copied is still all of it.
    await page.getByRole('textbox', { name: 'Find in this report' }).fill('status');

    await page.getByRole('button', { name: 'Copy the report' }).click();

    expect(write).toHaveBeenCalledWith('Name: web\nStatus: Running');
    await expect.element(page.getByText('Copied')).toBeInTheDocument();
    write.mockRestore();
});
