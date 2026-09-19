import { beforeEach, expect, test, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { EditorView } from '@codemirror/view';

import YamlEditor from './YamlEditor.svelte';

// Declared through vi.hoisted because the mock factory below is lifted above
// every other statement in the file, and it needs this string.
const { LIVE } = vi.hoisted(() => ({ LIVE: 'apiVersion: v1\nkind: Pod\nmetadata:\n  name: web\n' }));

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
        Describe: vi.fn().mockResolvedValue(''),
        ResourceYAML: vi.fn().mockResolvedValue(LIVE),
        ApplyYAML: vi.fn().mockResolvedValue(LIVE),
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
}));

const { workspace } = await import('../state/workspace.svelte');
const { editors } = await import('../state/editor.svelte');
const { ResourceService } = await import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services');

/**
 * The editor is CodeMirror now, so its text is not an input's value. The view
 * is reached the way CodeMirror itself offers -- findFromDOM -- rather than
 * through a seam put in the component for tests.
 */
function editor(): EditorView {
    const view = EditorView.findFromDOM(document.querySelector('.cm-editor') as HTMLElement);
    if (!view) throw new Error('no editor is mounted');
    return view;
}

/** What the editor is showing. */
function text(): string {
    return editor().state.doc.toString();
}

/** Types over the whole document, the way a user replacing it all would. */
function replaceAll(next: string): void {
    const view = editor();
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } });
}

/** Waits for the editor to be showing something. */
async function ready(): Promise<void> {
    await vi.waitFor(() => expect(document.querySelector('.cm-content')).toBeTruthy());
}

const PROD = '/home/u/.kube/prod::admin@prod';

/** One editor tab, as the dock hands it to the component. */
const TAB = {
    id: 'edit:prod#pods#default#web',
    view: 'edit' as const,
    contextId: PROD,
    kind: 'pods',
    namespace: 'default',
    name: 'web',
    title: 'web',
};

/**
 * The elements a gutter actually draws.
 *
 * CodeMirror puts a hidden spacer first in every gutter, sized to the widest
 * entry so the column does not jump about. It carries a real number and a real
 * fold arrow, so anything reading the gutter has to skip it.
 */
function drawn(selector: string): HTMLElement[] {
    return [...document.querySelectorAll<HTMLElement>(selector)].filter(
        (el) => el.style.visibility !== 'hidden',
    );
}

/** The line numbers CodeMirror is drawing, or none when they are turned off. */
function gutter(): string[] {
    return drawn('.cm-lineNumbers .cm-gutterElement').map((el) => el.textContent?.trim() ?? '');
}

beforeEach(() => {
    editors.forget(TAB.id);
    workspace.settings.preferences.showLineNumbers = true;
    vi.mocked(ResourceService.ResourceYAML).mockReset().mockResolvedValue(LIVE);
    vi.mocked(ResourceService.ApplyYAML).mockReset().mockResolvedValue(LIVE);
    vi.mocked(ResourceService.CheckYAML)
        .mockReset()
        .mockResolvedValue({ valid: true, message: '', line: 0 });
});

test('opens on the object as the cluster has it', async () => {
    render(YamlEditor, { tab: TAB });

    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));
    expect(ResourceService.ResourceYAML).toHaveBeenCalledWith(PROD, 'pods', 'default', 'web');
});

test('numbers every line, and stops when the setting is turned off', async () => {
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    // Four lines of YAML and the empty one after the trailing newline.
    expect(gutter()).toEqual(['1', '2', '3', '4', '5']);

    workspace.settings.preferences.showLineNumbers = false;
    await expect.poll(() => gutter()).toEqual([]);
});

test('there is nothing to save until something is typed', async () => {
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));
    const save = page.getByRole('button', { name: 'Save' });

    await expect.element(save).toBeDisabled();

    replaceAll('apiVersion: v1\nkind: Pod\n');
    await expect.element(save).toBeEnabled();
});

test('broken YAML says where it broke and holds the save back', async () => {
    vi.mocked(ResourceService.CheckYAML).mockResolvedValue({
        valid: false,
        message: 'mapping values are not allowed in this context',
        line: 2,
    });
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    replaceAll('a: b\nc: d: e\n');

    await expect.element(page.getByText(/Line 2:/)).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Save' })).toBeDisabled();
    // The line itself is marked, so the message has somewhere to point rather
    // than only naming a number.
    await expect.poll(() => document.querySelectorAll('.cm-badLine').length).toBe(1);
});

test('saving sends what is in the editor and takes back what was stored', async () => {
    const stored = `${LIVE}  resourceVersion: "4822"\n`;
    vi.mocked(ResourceService.ApplyYAML).mockResolvedValue(stored);
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    replaceAll('apiVersion: v1\nkind: Pod\n');
    await page.getByRole('button', { name: 'Save' }).click();

    await vi.waitFor(() => expect(text()).toBe(stored));
    // Last argument is the document as opened, which is what lets a save the
    // cluster refuses be replayed against what it holds now.
    expect(ResourceService.ApplyYAML).toHaveBeenCalledWith(
        PROD,
        'pods',
        'default',
        'web',
        'apiVersion: v1\nkind: Pod\n',
        LIVE,
    );
    await expect.element(page.getByText('Saved')).toBeVisible();
});

test('a refused save is reported in the API server\u2019s own words', async () => {
    vi.mocked(ResourceService.ApplyYAML).mockRejectedValue(
        new Error('Operation cannot be fulfilled on pods "web": the object has been modified'),
    );
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    replaceAll('apiVersion: v1\nkind: Pod\n');
    await page.getByRole('button', { name: 'Save' }).click();

    await expect.element(page.getByText(/the object has been modified/)).toBeVisible();
    // The edit is still the only copy of what was written; it must survive.
    await vi.waitFor(() => expect(text()).toBe('apiVersion: v1\nkind: Pod\n'));
});

test('a cluster that will not answer offers a retry rather than an empty editor', async () => {
    vi.mocked(ResourceService.ResourceYAML).mockRejectedValueOnce(new Error('connection refused'));
    render(YamlEditor, { tab: TAB });

    await expect.element(page.getByRole('button', { name: /Try again|Retry/ })).toBeVisible();
});

// ---- what the swap from a textarea was for --------------------------------

// A textarea cannot mark the text inside it, which is why finding was not
// possible before and is now.
test('the find panel opens on the editor and marks what it finds', async () => {
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    editor().focus();
    // CodeMirror binds the search panel to Mod-f and resolves Mod the way the
    // browser it is running in does -- Cmd on a Mac, Ctrl everywhere else, via
    // the same /Mac/.test(navigator.platform) check used here. Pressing Meta
    // unconditionally opened the panel only on a Mac, so this passed on a
    // developer's laptop and failed on every Linux and Windows runner.
    const mod = /Mac/.test(navigator.platform) ? 'Meta' : 'Control';
    await userEvent.keyboard(`{${mod}>}f{/${mod}}`);

    const find = document.querySelector('.cm-panel.cm-search input') as HTMLInputElement;
    expect(find).toBeTruthy();

    await userEvent.type(find, 'kind');
    await expect.poll(() => document.querySelectorAll('.cm-searchMatch').length).toBeGreaterThan(0);
});

// The other thing a textarea makes impossible: hiding a run of lines while
// leaving the rest editable.
test('a nested block can be folded away and opened again', async () => {
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    // One arrow, on the only line of this document that opens a block.
    const arrows = drawn('.cm-foldGutter .cm-gutterElement')
        .map((el) => el.querySelector<HTMLElement>('[title="Fold line"]'))
        .filter((el): el is HTMLElement => el !== null);
    expect(arrows).toHaveLength(1);

    arrows[0].click();

    await expect.poll(() => document.querySelectorAll('.cm-foldPlaceholder').length).toBe(1);
    // Folding hides lines; it must not change the document.
    expect(text()).toBe(LIVE);

    (document.querySelector('.cm-foldPlaceholder') as HTMLElement).click();
    await expect.poll(() => document.querySelectorAll('.cm-foldPlaceholder').length).toBe(0);
});

test('the YAML is coloured by what the tokens are', async () => {
    render(YamlEditor, { tab: TAB });
    await ready();
    await vi.waitFor(() => expect(text()).toBe(LIVE));

    // Highlighting means spans inside the lines rather than bare text nodes.
    await expect
        .poll(() => document.querySelectorAll('.cm-line span[class*="ͼ"]').length)
        .toBeGreaterThan(0);
});
