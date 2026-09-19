import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { detail } from '../state/detail.svelte';

const KubeVirtDetail = vi.hoisted(() => vi.fn());
vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    ResourceService: { KubeVirtDetail },
    KubeconfigService: { Sync: vi.fn().mockResolvedValue([]), Files: vi.fn().mockResolvedValue([]) },
    SettingsService: {
        Get: vi.fn().mockResolvedValue({}),
        ConfigPath: vi.fn().mockResolvedValue(''),
        SetContextPrefs: vi.fn().mockResolvedValue({}),
        SetPanes: vi.fn().mockResolvedValue({}),
        SetLayout: vi.fn().mockResolvedValue({}),
        SetPreferences: vi.fn().mockResolvedValue({}),
    },
    PluginService: {
        List: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    },
    MetricsService: { Attachments: vi.fn().mockResolvedValue([]) },
    ThemeService: { List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }) },
    // The workspace store imports the whole services module, so every service
    // it touches has to exist here even though this panel calls none of them.
    HelmService: { Tool: vi.fn().mockResolvedValue({ found: false, path: '', version: '', configured: false, reason: '' }) },
    LogService: { Containers: vi.fn().mockResolvedValue([]), Close: vi.fn() },
    TerminalService: { Close: vi.fn(), Externals: vi.fn().mockResolvedValue({ terminals: [], kubectl: '', reason: '' }) },
    PortForwardService: { List: vi.fn().mockResolvedValue([]) },
    ActionService: { ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }) },
    UpdateService: { Check: vi.fn().mockResolvedValue(null) },
}));

const VirtualMachine = (await import('./VirtualMachine.svelte')).default;
const { workspace } = await import('../state/workspace.svelte');

const PROD = '/home/u/.kube/prod::admin@prod';
const TARGET = {
    contextId: PROD,
    kind: 'crd:virtualmachines.kubevirt.io',
    namespace: 'vms',
    name: 'win11',
};

const fact = (label: string, value: string, over: Record<string, unknown> = {}) => ({
    label,
    value,
    ref: null,
    tone: '',
    note: '',
    ...over,
});

beforeEach(() => {
    document.body.innerHTML = '';
    detail.close();
    KubeVirtDetail.mockReset();
});

test('the facts are laid out as labelled values', async () => {
    KubeVirtDetail.mockResolvedValue({
        kind: TARGET.kind,
        error: '',
        sections: [
            {
                title: 'Virtual machine',
                facts: [fact('Status', 'Running', { tone: 'ok' }), fact('CPU', '4 cores')],
                columns: [],
                rows: [],
                empty: '',
            },
        ],
    });

    render(VirtualMachine, TARGET);

    await expect.element(page.getByText('Virtual machine')).toBeVisible();
    await expect.element(page.getByText('4 cores')).toBeVisible();
});

// The node is the fact the panel is worth opening for, and following it is the
// point of it being a link rather than text.
test('a value that names another object opens it', async () => {
    KubeVirtDetail.mockResolvedValue({
        kind: TARGET.kind,
        error: '',
        sections: [
            {
                title: 'Virtual machine',
                facts: [fact('Node', 'kvw2', { ref: { kind: 'nodes', namespace: '', name: 'kvw2' } })],
                columns: [],
                rows: [],
                empty: '',
            },
        ],
    });

    render(VirtualMachine, TARGET);
    await expect.element(page.getByRole('button', { name: 'kvw2' })).toBeVisible();

    await page.getByRole('button', { name: 'kvw2' }).click();

    await expect.poll(() => detail.target?.name).toBe('kvw2');
    expect(detail.target?.kind).toBe('nodes');
});

test('a table draws its rows, and a cell can be a link too', async () => {
    KubeVirtDetail.mockResolvedValue({
        kind: TARGET.kind,
        error: '',
        sections: [
            {
                title: 'Disks and volumes',
                facts: [],
                columns: ['Disk', 'Target', 'Backed by'],
                rows: [
                    [
                        fact('', 'disk-0'),
                        fact('', 'virtio'),
                        fact('', 'win11-root', {
                            ref: { kind: 'persistentvolumeclaims', namespace: 'vms', name: 'win11-root' },
                        }),
                    ],
                ],
                empty: '',
            },
        ],
    });

    render(VirtualMachine, TARGET);

    await expect.element(page.getByText('Backed by')).toBeVisible();
    await page.getByRole('button', { name: 'win11-root' }).click();

    await expect.poll(() => detail.target?.kind).toBe('persistentvolumeclaims');
});

// An absence that is a fact about the machine is worth saying: "not migrated"
// is an answer, where an empty block is not.
test('a section with nothing in it says what the absence means', async () => {
    KubeVirtDetail.mockResolvedValue({
        kind: TARGET.kind,
        error: '',
        sections: [
            {
                title: 'Migrations',
                facts: [],
                columns: ['Migration'],
                rows: [],
                empty: 'This machine has not been migrated.',
            },
        ],
    });

    render(VirtualMachine, TARGET);

    await expect.element(page.getByText('This machine has not been migrated.')).toBeVisible();
});

// Carried rather than fatal: the charts and the YAML report below this are
// still worth having when this one call was refused.
test('a refused read says so and leaves the rest of the panel alone', async () => {
    KubeVirtDetail.mockRejectedValue(new Error('virtualmachines.kubevirt.io is forbidden'));

    render(VirtualMachine, TARGET);

    await expect.element(page.getByText(/forbidden/)).toBeVisible();
});
