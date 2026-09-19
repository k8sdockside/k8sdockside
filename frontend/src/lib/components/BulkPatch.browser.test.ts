import { EditorView } from '@codemirror/view';
import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { notices } from '../state/notices.svelte';
import { detail } from '../state/detail.svelte';

// The table's rows arrive through a subscription. Stubbing that is what lets a
// test say "the cluster holds this" without a cluster.
const pushed = vi.hoisted(() => ({ send: (_table: unknown) => {} }));
vi.mock('../state/subscriptions', () => ({
    subscribe: vi.fn((_c: string, _k: string, _n: string, onTable: (t: unknown) => void) => {
        pushed.send = onTable;
        return { setNamespaces: vi.fn(), close: vi.fn() };
    }),
}));

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
        Namespaces: vi.fn().mockResolvedValue(['default']),
        Describe: vi.fn().mockResolvedValue(''),
    },
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }),
        DeleteMany: vi.fn().mockResolvedValue({ done: 0, failures: [] }),
        PatchMany: vi.fn().mockResolvedValue({ done: 0, failures: [] }),
        PreviewPatch: vi.fn().mockResolvedValue({ json: '', empty: true, error: '', line: 0 }),
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
}));

const ResourceTable = (await import('./ResourceTable.svelte')).default;
const { workspace } = await import('../state/workspace.svelte');
const { ActionService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

// Patching several rows at once. The form builds one merge patch, shows it,
// and one call carries it with the whole selection to the backend.

const PROD = '/home/u/.kube/prod::admin@prod';

const plain = (text: string) => ({ text, tone: '', sort: '', pills: null });

function podRow(name: string) {
    return { id: `pods/default/${name}`, name, namespace: 'default', cells: [plain(name), plain('Running')] };
}

function pods() {
    return {
        kind: 'pods',
        columns: ['Name', 'Status'],
        namespaced: true,
        error: '',
        rows: [podRow('web-1'), podRow('web-2'), podRow('db-1')],
    };
}

/** The patch text the last call carried. */
function sent(): string {
    return vi.mocked(ActionService.PatchMany).mock.calls[0][3] as string;
}

beforeEach(() => {
    detail.close();
    notices.dismiss();
    vi.mocked(ActionService.PatchMany).mockReset().mockResolvedValue({ done: 2, failures: [] });
    vi.mocked(ActionService.PreviewPatch).mockReset().mockResolvedValue({ json: '', empty: true, error: '', line: 0 });
});

/** The form's YAML editor, reached the way CodeMirror itself offers. */
function editor(): EditorView {
    const view = EditorView.findFromDOM(document.querySelector('.cm-editor') as HTMLElement);
    if (!view) throw new Error('no editor is mounted');
    return view;
}

/** Types over the whole document, the way a user replacing it all would. */
function replaceAll(next: string): void {
    const view = editor();
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } });
}

/** Opens the form on the named rows and switches it to YAML. */
async function yamlMode(...names: string[]): Promise<void> {
    await opened(...names);
    await page.getByRole('radio', { name: 'YAML' }).click();
    await expect.element(page.getByRole('textbox', { name: 'Merge patch as YAML' })).toBeVisible();
}

/** Renders a pods table, ticks the named rows and opens the patch form. */
async function opened(...names: string[]): Promise<void> {
    render(ResourceTable, { contextId: PROD, kind: 'pods' });
    pushed.send(pods());
    await expect.element(page.getByRole('checkbox', { name: 'Select web-1' })).toBeVisible();
    for (const name of names) {
        await page.getByRole('checkbox', { name: `Select ${name}` }).click();
    }
    await page.getByRole('button', { name: 'Patch' }).click();
    await expect.element(page.getByRole('textbox', { name: 'Key' })).toBeVisible();
}

test('setting a label sends one merge patch carrying every ticked row, and keeps the selection', async () => {
    await opened('web-1', 'web-2');
    await page.getByRole('textbox', { name: 'Key' }).fill('team');
    await page.getByRole('textbox', { name: 'Value' }).fill('platform');
    await expect.element(page.getByText('{"metadata":{"labels":{"team":"platform"}}}')).toBeVisible();

    await page.getByRole('button', { name: 'Apply' }).click();

    await expect.poll(() => vi.mocked(ActionService.PatchMany).mock.calls.length).toBe(1);
    expect(ActionService.PatchMany).toHaveBeenCalledWith(
        PROD,
        'pods',
        [
            { namespace: 'default', name: 'web-1' },
            { namespace: 'default', name: 'web-2' },
        ],
        '{"metadata":{"labels":{"team":"platform"}}}',
    );
    expect(notices.current?.text).toBe('2 pods patched');
    await expect.element(page.getByText('2 selected')).toBeVisible();
});

test('removing sends null and asks for no value', async () => {
    await opened('web-1');
    await page.getByRole('combobox', { name: 'Set or remove' }).selectOptions('remove');
    await page.getByRole('textbox', { name: 'Key' }).fill('team');
    expect(page.getByRole('textbox', { name: 'Value' }).elements()).toHaveLength(0);

    await page.getByRole('button', { name: 'Apply' }).click();

    await expect.poll(() => vi.mocked(ActionService.PatchMany).mock.calls.length).toBe(1);
    expect(sent()).toBe('{"metadata":{"labels":{"team":null}}}');
});

test('a field takes a dotted path and the value for what it parses as', async () => {
    await opened('web-1');
    await page.getByRole('radio', { name: 'Field' }).click();
    await page.getByRole('textbox', { name: 'Field path' }).fill('spec.replicas');
    await page.getByRole('textbox', { name: 'Value' }).fill('2');

    await page.getByRole('button', { name: 'Apply' }).click();

    await expect.poll(() => vi.mocked(ActionService.PatchMany).mock.calls.length).toBe(1);
    expect(sent()).toBe('{"spec":{"replicas":2}}');
});

test('an annotation key keeps its dots and slash', async () => {
    await opened('web-1');
    await page.getByRole('radio', { name: 'Annotation' }).click();
    await page.getByRole('textbox', { name: 'Key' }).fill('example.com/owner');
    await page.getByRole('textbox', { name: 'Value' }).fill('roger');

    await page.getByRole('button', { name: 'Apply' }).click();

    await expect.poll(() => vi.mocked(ActionService.PatchMany).mock.calls.length).toBe(1);
    expect(sent()).toBe('{"metadata":{"annotations":{"example.com/owner":"roger"}}}');
});

test('nothing can be applied until there is a key', async () => {
    await opened('web-1');

    await expect.element(page.getByRole('button', { name: 'Apply' })).toBeDisabled();
    await page.getByRole('textbox', { name: 'Key' }).fill('team');
    await expect.element(page.getByRole('button', { name: 'Apply' })).toBeEnabled();
});

test('cancelling sends nothing and keeps the rows ticked', async () => {
    await opened('web-1');
    await page.getByRole('textbox', { name: 'Key' }).fill('team');

    await page.getByRole('button', { name: 'Cancel' }).click();

    expect(ActionService.PatchMany).not.toHaveBeenCalled();
    await expect.element(page.getByText('1 selected')).toBeVisible();
});

test('a refusal leaves only the refused row ticked, with the reason', async () => {
    vi.mocked(ActionService.PatchMany).mockResolvedValue({
        done: 1,
        failures: [{ namespace: 'default', name: 'web-2', error: 'pods "web-2" is forbidden' }],
    });
    await opened('web-1', 'web-2');
    await page.getByRole('textbox', { name: 'Key' }).fill('team');
    await page.getByRole('textbox', { name: 'Value' }).fill('x');

    await page.getByRole('button', { name: 'Apply' }).click();

    await expect.element(page.getByRole('checkbox', { name: 'Select web-1' })).not.toBeChecked();
    await expect.element(page.getByRole('checkbox', { name: 'Select web-2' })).toBeChecked();
    expect(notices.current?.tone).toBe('error');
    expect(notices.current?.text).toContain('web-2');
});

// The YAML mode: a whole merge patch, for several fields at once. The text is
// read by the backend as it is typed, and sent exactly as typed.
test('the YAML mode sends the document as typed, previewed by the backend', async () => {
    vi.mocked(ActionService.PreviewPatch).mockResolvedValue({
        json: '{"spec":{"replicas":2}}', empty: false, error: '', line: 0,
    });
    await yamlMode('web-1', 'web-2');

    replaceAll('spec:\n  replicas: 2\n');

    await expect.poll(() => vi.mocked(ActionService.PreviewPatch).mock.lastCall?.[0]).toBe('spec:\n  replicas: 2\n');
    await expect.element(page.getByText('{"spec":{"replicas":2}}')).toBeVisible();
    await page.getByRole('button', { name: 'Apply' }).click();

    await expect.poll(() => vi.mocked(ActionService.PatchMany).mock.calls.length).toBe(1);
    expect(sent()).toBe('spec:\n  replicas: 2\n');
    expect(notices.current?.text).toBe('2 pods patched');
});

test('the YAML mode starts as a commented example, with nothing to apply', async () => {
    await yamlMode('web-1');

    await expect.element(page.getByRole('button', { name: 'Apply' })).toBeDisabled();
    const lines = editor().state.doc.toString().split('\n');
    expect(lines.every((line) => line === '' || line.startsWith('#'))).toBe(true);
    expect(ActionService.PatchMany).not.toHaveBeenCalled();
});

test('a patch the backend cannot read is named with its line, and cannot be applied', async () => {
    vi.mocked(ActionService.PreviewPatch).mockResolvedValue({
        json: '', empty: false, error: 'did not find expected key', line: 2,
    });
    await yamlMode('web-1');

    replaceAll('spec:\n  replicas: [\n');

    await expect.element(page.getByText(/Line 2: did not find expected key/)).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Apply' })).toBeDisabled();
    expect(ActionService.PatchMany).not.toHaveBeenCalled();
});

test('what was typed survives switching to a field mode and back', async () => {
    await yamlMode('web-1');
    replaceAll('spec:\n  paused: true\n');

    await page.getByRole('radio', { name: 'Label' }).click();
    await expect.element(page.getByRole('textbox', { name: 'Key' })).toBeVisible();
    await page.getByRole('radio', { name: 'YAML' }).click();
    await expect.element(page.getByRole('textbox', { name: 'Merge patch as YAML' })).toBeVisible();

    expect(editor().state.doc.toString()).toBe('spec:\n  paused: true\n');
});
