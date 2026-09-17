import { beforeEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import ObjectActions from './ObjectActions.svelte';
import { notices } from '../state/notices.svelte';
import { detail } from '../state/detail.svelte';

// Hoisted, because vi.mock's factory is lifted above everything else in the
// file: a plain `let` up here would not exist yet when the factory runs.
const drainEvents = vi.hoisted(() => ({ handler: (_event: { data: unknown }) => {} }));
vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return {
        ...actual,
        Events: {
            On: vi.fn((name: string, handler: (event: { data: unknown }) => void) => {
                if (name === 'node:drain') drainEvents.handler = handler;
            }),
        },
    };
});
const deliver = (event: { data: unknown }) => drainEvents.handler(event);

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
        Describe: vi.fn().mockResolvedValue('Name: web'),
        ResourceYAML: vi.fn().mockResolvedValue('kind: Pod\n'),
        CheckYAML: vi.fn().mockResolvedValue({ valid: true, message: '', line: 0 }),
    },
    ActionService: {
        ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [], suspended: false, paused: false, pending: false }),
        Delete: vi.fn().mockResolvedValue(undefined),
        Scale: vi.fn().mockResolvedValue(undefined),
        Restart: vi.fn().mockResolvedValue(undefined),
        Cordon: vi.fn().mockResolvedValue(undefined),
        Drain: vi.fn().mockResolvedValue('drain-1'),
        CancelDrain: vi.fn(),
        VMState: vi.fn().mockResolvedValue({ isMachine: false, running: false, paused: false, migratable: false, status: '' }),
        VMOperation: vi.fn().mockResolvedValue(undefined),
        Suspend: vi.fn().mockResolvedValue(undefined),
        PauseRollout: vi.fn().mockResolvedValue(undefined),
        TriggerCronJob: vi.fn().mockResolvedValue('backup-manual-1'),
        Evict: vi.fn().mockResolvedValue(undefined),
        AnswerCSR: vi.fn().mockResolvedValue(undefined),
        RolloutHistory: vi.fn().mockResolvedValue([]),
        RollbackWorkload: vi.fn().mockResolvedValue(undefined),
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
const { views } = await import('../state/views');
const { resourceTabId } = await import('../state/panes');
const { actions } = await import('../state/actions.svelte');
const { helm } = await import('../state/helm.svelte');
const { ActionService, HelmService, PortForwardService, TerminalService } = await import(
    '../../../bindings/github.com/rogerwesterbo/k8sdockside/internal/services'
);

const PROD = '/home/u/.kube/prod::admin@prod';
const POD = { contextId: PROD, kind: 'pods', namespace: 'default', name: 'web' };
const NODE = { contextId: PROD, kind: 'nodes', namespace: '', name: 'wrkr01' };
const DEPLOYMENT = { contextId: PROD, kind: 'deployments', namespace: 'default', name: 'web' };
const SERVICE = { contextId: PROD, kind: 'services', namespace: 'web', name: 'api' };
const RELEASE = { contextId: PROD, kind: 'helmreleases', namespace: 'default', name: 'ingress-nginx' };
const CRONJOB = { contextId: PROD, kind: 'cronjobs', namespace: 'ops', name: 'backup' };
const CSR = { contextId: PROD, kind: 'certificatesigningrequests', namespace: '', name: 'csr-1' };

const settle = () => new Promise((r) => setTimeout(r, 60));

beforeEach(async () => {
    // VM included: its state says isMachine, and a stale one would give the
    // next object's bar a set of buttons that are not about it.
    for (const ref of [POD, NODE, DEPLOYMENT, SERVICE, RELEASE, VM, CRONJOB, CSR]) actions.forget(ref);
    detail.close();
    // A test that opened something in the dock has to have it taken away again,
    // and the write that goes with it has to land before the next test starts.
    // The store replaces its whole settings object with whatever the backend
    // answers, so a dock write still in flight arrives in the middle of the
    // next test and takes with it anything that test had set directly.
    if (workspace.dockTabs.length > 0) {
        workspace.closeAllDockTabs();
        await new Promise((r) => setTimeout(r, 320));
    }
    notices.current = null;
    helm.forget(RELEASE);
    // Where helm is, re-answered per test: one of them takes it away.
    helm.probed = false;
    vi.mocked(HelmService.Tool).mockReset().mockResolvedValue({
        found: true,
        path: '/usr/bin/helm',
        version: 'v3.16.2',
        configured: false,
        reason: '',
    });
    vi.mocked(HelmService.Uninstall).mockReset().mockResolvedValue('');
    vi.mocked(HelmService.Rollback).mockReset().mockResolvedValue('');
    vi.mocked(ActionService.ObjectState).mockReset().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [], suspended: false, paused: false, pending: false });
    vi.mocked(ActionService.Delete).mockReset().mockResolvedValue(undefined);
    vi.mocked(ActionService.Scale).mockReset().mockResolvedValue(undefined);
    vi.mocked(ActionService.Cordon).mockReset().mockResolvedValue(undefined);
    vi.mocked(ActionService.Drain).mockReset().mockResolvedValue('drain-1');
    vi.mocked(ActionService.CancelDrain).mockReset();
    vi.mocked(PortForwardService.Ports).mockReset().mockResolvedValue([]);
    vi.mocked(PortForwardService.Start).mockReset().mockResolvedValue({
        id: 'pf-1',
        contextId: PROD,
        kind: 'services',
        namespace: 'web',
        name: 'api',
        remotePort: 80,
        localPort: 51234,
        random: true,
        browser: false,
        state: 'active',
        error: '',
        pod: 'api-7f9',
        podPort: 8080,
        note: '',
    });
    vi.mocked(TerminalService.Launch).mockReset().mockResolvedValue(undefined);
    vi.mocked(TerminalService.LaunchNode).mockReset().mockResolvedValue(undefined);
    vi.mocked(ActionService.VMOperation).mockReset().mockResolvedValue(undefined);
    vi.mocked(ActionService.VMState)
        .mockReset()
        .mockResolvedValue({ isMachine: false, running: false, paused: false, migratable: false, status: '' });
});

test('an ordinary object offers to be edited and deleted', async () => {
    render(ObjectActions, { object: POD });

    await expect.element(page.getByRole('button', { name: 'Edit' })).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Delete' })).toBeVisible();
    expect(page.getByRole('button', { name: 'Scale' }).elements()).toHaveLength(0);
});

test('a node offers what only a node can do', async () => {
    render(ObjectActions, { object: NODE });

    await expect.element(page.getByRole('button', { name: 'Cordon' })).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'Drain' })).toBeVisible();
});

// "What is actually running here" is the question asked immediately before
// cordoning or draining, which is why it sits beside them rather than being a
// link on the node's name in the list -- a name opens a describe panel, in
// every table.
test('a node offers the pods placed on it, beside cordon and drain', async () => {
    render(ObjectActions, { object: NODE });

    await page.getByRole('button', { name: 'Pods' }).click();

    await expect.poll(() => workspace.activeTab?.kind).toBe('pods');
    expect(views.recall(resourceTabId(PROD, 'pods'))?.node).toBe('wrkr01');
});

// ----- virtual machines -----------------------------------------------------

const VM = { contextId: PROD, kind: 'crd:virtualmachines.kubevirt.io', namespace: 'vms', name: 'win11' };

/** Answers VMState for the machine tests, which every one of them needs. */
function machine(over: Record<string, unknown> = {}) {
    vi.mocked(ActionService.VMState).mockResolvedValue({
        isMachine: true,
        running: true,
        paused: false,
        migratable: true,
        status: 'Running',
        ...over,
    });
}

test('a running machine offers the virtctl lifecycle set', async () => {
    machine();

    render(ObjectActions, { object: VM });

    for (const label of ['Pause', 'Restart', 'Reboot', 'Migrate', 'Stop']) {
        await expect.element(page.getByRole('button', { name: label, exact: true })).toBeVisible();
    }
});

test('a stopped machine offers only Start', async () => {
    machine({ running: false, status: 'Stopped' });

    render(ObjectActions, { object: VM });

    await expect.element(page.getByRole('button', { name: 'Start', exact: true })).toBeVisible();
    expect(page.getByRole('button', { name: 'Stop', exact: true }).elements()).toHaveLength(0);
});

test('starting a machine sends the operation the backend names', async () => {
    machine({ running: false, status: 'Stopped' });
    render(ObjectActions, { object: VM });
    await expect.element(page.getByRole('button', { name: 'Start', exact: true })).toBeVisible();

    await page.getByRole('button', { name: 'Start', exact: true }).click();

    await expect
        .poll(() => vi.mocked(ActionService.VMOperation).mock.calls[0])
        .toEqual([PROD, 'start', 'vms', 'win11']);
});

// Both take the guest down, and a virtual machine is not a pod: nothing
// reschedules it, and what was running inside was not built to be killed.
test('stopping asks before it does anything', async () => {
    machine();
    render(ObjectActions, { object: VM });
    await expect.element(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();

    await page.getByRole('button', { name: 'Stop', exact: true }).click();

    expect(vi.mocked(ActionService.VMOperation)).not.toHaveBeenCalled();
});

// Every one of these used to ask "Delete Virtualmachineinstance win11?", because
// the question fell through to Delete's for any action it did not name. A
// confirmation that describes the wrong operation is worse than none: it is
// read, believed, and answered Yes.
test.each([
    ['Stop', /Stop virtual machine win11\?/],
    ['Restart', /Restart virtual machine win11\?/],
    ['Reboot', /Reboot virtual machine win11\?/],
    ['Migrate', /Migrate virtual machine win11 to another node\?/],
])('%s asks about %s, not about deleting', async (button, asked) => {
    machine();
    render(ObjectActions, { object: VM });
    await expect.element(page.getByRole('button', { name: button, exact: true })).toBeVisible();

    await page.getByRole('button', { name: button, exact: true }).click();

    await expect.element(page.getByText(asked)).toBeVisible();
    expect(page.getByText(/Delete/).elements()).toHaveLength(0);
});

test('a machine KubeVirt says cannot move is not offered a migration', async () => {
    machine({ migratable: false });

    render(ObjectActions, { object: VM });

    await expect.element(page.getByRole('button', { name: 'Pause' })).toBeVisible();
    expect(page.getByRole('button', { name: 'Migrate', exact: true }).elements()).toHaveLength(0);
});

test('an ordinary object grows no machine buttons', async () => {
    render(ObjectActions, { object: DEPLOYMENT });

    await expect.element(page.getByRole('button', { name: 'Edit' })).toBeVisible();
    // Exact: a Deployment has Restart, and "Restart" contains "Start".
    expect(page.getByRole('button', { name: 'Start', exact: true }).elements()).toHaveLength(0);
    expect(page.getByRole('button', { name: 'Migrate', exact: true }).elements()).toHaveLength(0);
});

test('nothing else offers it: a pod is not a place other pods are placed', async () => {
    render(ObjectActions, { object: POD });

    await expect.element(page.getByRole('button', { name: 'Edit' })).toBeVisible();
    expect(page.getByRole('button', { name: 'Pods', exact: true }).elements()).toHaveLength(0);
});

// The one button whose label is the cluster's answer rather than ours: offering
// to cordon a node that is already cordoned is a button that does nothing.
test('a cordoned node offers to be uncordoned instead', async () => {
    vi.mocked(ActionService.ObjectState).mockResolvedValue({ scalable: false, replicas: 0, cordoned: true, containers: [], suspended: false, paused: false, pending: false });

    render(ObjectActions, { object: NODE });

    await expect.element(page.getByRole('button', { name: 'Uncordon' })).toBeVisible();
    expect(page.getByRole('button', { name: 'Cordon', exact: true }).elements()).toHaveLength(0);
});

test('cordoning a node sends it', async () => {
    render(ObjectActions, { object: NODE });
    await expect.element(page.getByRole('button', { name: 'Cordon' })).toBeVisible();

    await page.getByRole('button', { name: 'Cordon' }).click();

    await vi.waitFor(() => expect(ActionService.Cordon).toHaveBeenCalledWith(PROD, 'wrkr01', true));
});

test('deleting asks first, and names what it would delete', async () => {
    render(ObjectActions, { object: POD });

    await page.getByRole('button', { name: 'Delete' }).click();

    await expect.element(page.getByText(/Delete Pod web\?/)).toBeVisible();
    expect(ActionService.Delete).not.toHaveBeenCalled();
});

// The destructive button must not be the one a stray Enter lands on.
test('the question puts focus on Cancel, not on Delete', async () => {
    render(ObjectActions, { object: POD });

    await page.getByRole('button', { name: 'Delete' }).click();
    await settle();

    expect(document.activeElement?.textContent?.trim()).toBe('Cancel');
});

test('cancelling puts the buttons back and deletes nothing', async () => {
    render(ObjectActions, { object: POD });
    await page.getByRole('button', { name: 'Delete' }).click();
    await expect.element(page.getByRole('button', { name: 'Cancel' })).toBeVisible();

    await page.getByRole('button', { name: 'Cancel' }).click();

    await expect.element(page.getByRole('button', { name: 'Edit' })).toBeVisible();
    expect(ActionService.Delete).not.toHaveBeenCalled();
});

test('Escape cancels the question too', async () => {
    render(ObjectActions, { object: POD });
    await page.getByRole('button', { name: 'Delete' }).click();
    await expect.element(page.getByText(/Delete Pod web\?/)).toBeVisible();

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

    await expect.element(page.getByRole('button', { name: 'Edit' })).toBeVisible();
    expect(ActionService.Delete).not.toHaveBeenCalled();
});

test('confirming deletes the object and closes the panel over it', async () => {
    await detail.open(POD);
    render(ObjectActions, { object: POD });
    await page.getByRole('button', { name: 'Delete' }).click();

    await page.getByRole('button', { name: 'Delete', exact: true }).click();

    await vi.waitFor(() =>
        expect(ActionService.Delete).toHaveBeenCalledWith(PROD, 'pods', 'default', 'web'),
    );
    // Nothing is left to describe, so the panel goes rather than showing a 404.
    await vi.waitFor(() => expect(detail.target).toBeNull());
});

test('a refused delete says why and leaves the object alone', async () => {
    vi.mocked(ActionService.Delete).mockRejectedValue(new Error('pods "web" is forbidden'));
    await detail.open(POD);
    render(ObjectActions, { object: POD });
    await page.getByRole('button', { name: 'Delete' }).click();

    await page.getByRole('button', { name: 'Delete', exact: true }).click();

    await vi.waitFor(() => expect(notices.current?.text).toContain('forbidden'));
    expect(detail.target).not.toBeNull();
});

test('scale opens a field holding the count the workload is at', async () => {
    vi.mocked(ActionService.ObjectState).mockResolvedValue({ scalable: true, replicas: 3, cordoned: false, containers: [], suspended: false, paused: false, pending: false });
    render(ObjectActions, { object: DEPLOYMENT });
    await expect.element(page.getByRole('button', { name: 'Scale' })).toBeVisible();

    await page.getByRole('button', { name: 'Scale' }).click();

    await expect.element(page.getByRole('spinbutton', { name: /Replicas/ })).toHaveValue(3);
});

test('scaling sends the number that was typed', async () => {
    vi.mocked(ActionService.ObjectState).mockResolvedValue({ scalable: true, replicas: 3, cordoned: false, containers: [], suspended: false, paused: false, pending: false });
    render(ObjectActions, { object: DEPLOYMENT });
    await page.getByRole('button', { name: 'Scale' }).click();

    await page.getByRole('spinbutton', { name: /Replicas/ }).fill('5');
    await page.getByRole('button', { name: 'Apply' }).click();

    await vi.waitFor(() =>
        expect(ActionService.Scale).toHaveBeenCalledWith(PROD, 'deployments', 'default', 'web', 5),
    );
});

/** Opens the scale form on a deployment running three. */
async function openScale(): Promise<HTMLInputElement> {
    vi.mocked(ActionService.ObjectState).mockResolvedValue({ scalable: true, replicas: 3, cordoned: false, containers: [], suspended: false, paused: false, pending: false });
    render(ObjectActions, { object: DEPLOYMENT });
    await page.getByRole('button', { name: 'Scale' }).click();
    await expect.element(page.getByRole('slider', { name: 'Replica count' })).toBeVisible();
    return page.getByRole('slider', { name: 'Replica count' }).element() as HTMLInputElement;
}

test('the slider and the − and + buttons move the same count', async () => {
    const slider = await openScale();

    // Dragging is the browser's business; what the form owns is what the
    // slider's input event does with the value it lands on.
    slider.value = '7';
    slider.dispatchEvent(new Event('input', { bubbles: true }));
    await expect.element(page.getByRole('spinbutton', { name: /Replicas/ })).toHaveValue(7);

    await page.getByRole('button', { name: 'One fewer replica' }).click();
    await page.getByRole('button', { name: 'One fewer replica' }).click();
    await page.getByRole('button', { name: 'One more replica' }).click();
    await expect.element(page.getByRole('spinbutton', { name: /Replicas/ })).toHaveValue(6);
    expect(slider.value).toBe('6');
    await expect.element(page.getByText(/3 → 6 \(\+3\)/)).toBeVisible();

    await page.getByRole('button', { name: 'Apply' }).click();
    await vi.waitFor(() =>
        expect(ActionService.Scale).toHaveBeenCalledWith(PROD, 'deployments', 'default', 'web', 6),
    );
});

test('Apply waits for a count different from the one running', async () => {
    await openScale();

    await expect.element(page.getByRole('button', { name: 'Apply' })).toBeDisabled();
    await page.getByRole('spinbutton', { name: /Replicas/ }).fill('4');
    await expect.element(page.getByRole('button', { name: 'Apply' })).toBeEnabled();
});

test('typing past the end of the slider moves the end out', async () => {
    const slider = await openScale();
    // Three running puts the end at ten.
    expect(slider.max).toBe('10');

    await page.getByRole('spinbutton', { name: /Replicas/ }).fill('40');

    await vi.waitFor(() => {
        expect(slider.max).toBe('40');
        expect(slider.value).toBe('40');
    });
});

test('scaling to zero says it stops every pod', async () => {
    await openScale();

    await page.getByRole('spinbutton', { name: /Replicas/ }).fill('0');

    await expect.element(page.getByText(/stops every pod/)).toBeVisible();
    await expect.element(page.getByRole('button', { name: 'One fewer replica' })).toBeDisabled();
});

test('Enter in the count applies it', async () => {
    await openScale();
    const box = page.getByRole('spinbutton', { name: /Replicas/ });

    await box.fill('8');
    box.element().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

    await vi.waitFor(() =>
        expect(ActionService.Scale).toHaveBeenCalledWith(PROD, 'deployments', 'default', 'web', 8),
    );
});
test('draining asks first, then reports as it goes', async () => {
    render(ObjectActions, { object: NODE });
    await page.getByRole('button', { name: 'Drain' }).click();
    await page.getByRole('button', { name: 'Drain', exact: true }).click();
    await vi.waitFor(() =>
        expect(ActionService.Drain).toHaveBeenCalledWith(PROD, 'wrkr01', {
            gracePeriodSeconds: null,
            timeoutSeconds: 0,
            deleteEmptyDirData: false,
            force: false,
            disableEviction: false,
            podSelector: '',
        }),
    );

    deliver({
        data: {
            drainId: 'drain-1',
            node: 'wrkr01',
            phase: 'evicting',
            evicted: 3,
            total: 4,
            refused: [],
            error: '',
            done: false,
        },
    });

    await expect.element(page.getByText(/3 of 4/)).toBeVisible();
});

// The whole reason the drain reports refusals rather than forcing them: the
// user has to be told which pods were left, and why.
test('the pods a drain would not move are named', async () => {
    render(ObjectActions, { object: NODE });
    await page.getByRole('button', { name: 'Drain' }).click();
    await page.getByRole('button', { name: 'Drain', exact: true }).click();
    await vi.waitFor(() => expect(ActionService.Drain).toHaveBeenCalled());

    deliver({
        data: {
            drainId: 'drain-1',
            node: 'wrkr01',
            phase: 'done',
            evicted: 2,
            total: 2,
            refused: [
                { pod: { namespace: 'default', name: 'debug' }, reason: 'nothing manages it' },
            ],
            error: '',
            done: true,
        },
    });

    await expect.element(page.getByText(/default\/debug/)).toBeVisible();
    await expect.element(page.getByText(/nothing manages it/)).toBeVisible();
});

// kubectl drain's flags, as k9s's dialog asks for them.
test('a drain can be told what kubectl drain is told', async () => {
    render(ObjectActions, { object: NODE });
    await page.getByRole('button', { name: 'Drain' }).click();

    await page.getByRole('checkbox', { name: /Delete emptyDir data/ }).click();
    await page.getByRole('spinbutton', { name: /Grace period/ }).fill('30');
    await page.getByRole('textbox', { name: /Pod selector/ }).fill(' app=web ');
    await page.getByRole('button', { name: 'Drain', exact: true }).click();

    await vi.waitFor(() =>
        expect(ActionService.Drain).toHaveBeenCalledWith(
            PROD,
            'wrkr01',
            expect.objectContaining({
                deleteEmptyDirData: true,
                force: false,
                gracePeriodSeconds: 30,
                timeoutSeconds: 0,
                podSelector: 'app=web',
            }),
        ),
    );
});

// A refusal names the option that would have moved its pod, so what was left
// behind is one click from the question with that box already ticked.
test('the pods a drain left behind can be drained again with what would move them', async () => {
    render(ObjectActions, { object: NODE });
    await page.getByRole('button', { name: 'Drain' }).click();
    await page.getByRole('button', { name: 'Drain', exact: true }).click();
    await vi.waitFor(() => expect(ActionService.Drain).toHaveBeenCalled());

    deliver({
        data: {
            drainId: 'drain-1',
            node: 'wrkr01',
            phase: 'done',
            evicted: 1,
            total: 1,
            refused: [
                {
                    pod: { namespace: 'default', name: 'cache' },
                    reason: 'it holds data in an emptyDir volume',
                    option: 'deleteEmptyDirData',
                },
            ],
            error: '',
            done: true,
        },
    });

    await page.getByRole('button', { name: /Drain these too/ }).click();

    await expect.element(page.getByRole('checkbox', { name: /Delete emptyDir data/ })).toBeChecked();
    await expect.element(page.getByRole('checkbox', { name: /Force/ })).not.toBeChecked();
    // Ticked, not run: the question still has to be answered.
    expect(ActionService.Drain).toHaveBeenCalledTimes(1);
});

test('a drain in flight can be called off', async () => {
    render(ObjectActions, { object: NODE });
    await page.getByRole('button', { name: 'Drain' }).click();
    await page.getByRole('button', { name: 'Drain', exact: true }).click();
    await vi.waitFor(() => expect(ActionService.Drain).toHaveBeenCalled());

    await page.getByRole('button', { name: 'Stop' }).click();

    expect(ActionService.CancelDrain).toHaveBeenCalledWith('drain-1');
});

// A Helm release is a decoded Secret rather than an object, so it gets none of
// the object verbs -- an Edit would open a Secret full of gzipped base64, and a
// Delete would leave everything the release installed running.
test('a Helm release gets Helm verbs rather than object ones', async () => {
    render(ObjectActions, { object: RELEASE });
    await settle();

    const labels = page.getByRole('button').elements().map((b) => b.textContent?.trim());
    expect(labels).toEqual(['Values', 'Rollback', 'Uninstall']);
});

// The way in to the values editor. Without this the whole upgrade path is code
// nothing reaches.
test('a release opens its values in the dock', async () => {
    render(ObjectActions, { object: RELEASE });
    await settle();

    await page.getByRole('button', { name: 'Values' }).click();

    expect(
        workspace.dockTabs.some((t) => t.view === 'helmvalues' && t.name === 'ingress-nginx'),
    ).toBe(true);
    expect(workspace.dockOpen).toBe(true);
});

// One release is many objects, which is what makes this worth spelling out
// rather than asking "Uninstall X?".
test('uninstalling asks first, and says what it will take with it', async () => {
    render(ObjectActions, { object: RELEASE });
    await settle();

    await page.getByRole('button', { name: 'Uninstall' }).click();

    await expect.element(page.getByText(/Everything the release installed will be removed/)).toBeVisible();
    expect(HelmService.Uninstall).not.toHaveBeenCalled();
});

// The only way an uninstall is undoable at all.
test('an uninstall can keep the history so the release can be rolled back', async () => {
    render(ObjectActions, { object: RELEASE });
    await settle();

    await page.getByRole('button', { name: 'Uninstall' }).click();
    await page.getByRole('checkbox', { name: /Keep the history/ }).click();
    await page.getByRole('button', { name: 'Uninstall' }).click();

    await vi.waitFor(() =>
        expect(HelmService.Uninstall).toHaveBeenCalledWith(PROD, 'default', 'ingress-nginx', true),
    );
});

// A button that cannot work says why on itself rather than failing when it is
// pressed. Reading the release still works -- that needs no helm at all.
test('without helm the two that need it are disabled, with the reason', async () => {
    vi.mocked(HelmService.Tool).mockResolvedValue({
        found: false,
        path: '',
        version: '',
        configured: false,
        reason: 'helm was not found on your PATH',
    });
    helm.probed = false;

    render(ObjectActions, { object: RELEASE });
    await settle();

    await expect.element(page.getByRole('button', { name: 'Rollback' })).toBeDisabled();
    await expect.element(page.getByRole('button', { name: 'Uninstall' })).toBeDisabled();
    // Values reads the release, which the app does itself.
    await expect.element(page.getByRole('button', { name: 'Values' })).toBeEnabled();
});

// The way in to the logs. Without this the log view is code nothing reaches.
test('a pod offers its logs, and pressing it opens them in the dock', async () => {
    render(ObjectActions, { object: POD });

    await page.getByRole('button', { name: 'Logs' }).click();

    expect(workspace.dockTabs.map((t) => t.view)).toEqual(['logs']);
    expect(workspace.dockTabs[0].name).toBe('web');
    expect(workspace.dockOpen).toBe(true);
});

// Two views onto one object, not one tab that changes what it shows.
test('logs and the editor are two dock tabs on the same object', async () => {
    render(ObjectActions, { object: POD });

    await page.getByRole('button', { name: 'Logs' }).click();
    await page.getByRole('button', { name: 'Edit' }).click();

    expect(workspace.dockTabs.map((t) => t.view).sort()).toEqual(['edit', 'logs']);
});

// ---- shells and forwards ---------------------------------------------------

test('a shell opens in the dock without asking anything first', async () => {
    render(ObjectActions, { object: POD });
    await expect.element(page.getByRole('button', { name: 'Shell' })).toBeVisible();

    await page.getByRole('button', { name: 'Shell' }).click();

    // Which shell, and which terminal, are settings answered once -- a shell
    // you have to fill in a form for is not a shell anybody would use.
    await vi.waitFor(() => expect(workspace.dockTabs.some((t) => t.view === 'shell')).toBe(true));
    workspace.closeAllDockTabs();
});

// Chosen through the store rather than by assigning to `settings.preferences`:
// a settings write landing mid-test is adopted whole, and only what the store
// itself wrote comes back in the answer. A preference poked straight into the
// object does not, so it returns to its default halfway through the test.
test('a shell goes to your own terminal when that is what you chose', async () => {
    workspace.setTerminal({ mode: 'external' });
    render(ObjectActions, { object: POD });
    await expect.element(page.getByRole('button', { name: 'Shell' })).toBeVisible();

    await page.getByRole('button', { name: 'Shell' }).click();

    await vi.waitFor(() =>
        expect(TerminalService.Launch).toHaveBeenCalledWith(PROD, 'pods', 'default', 'web', '', ''),
    );
    // And nothing opens in the dock: the shell is over there.
    expect(workspace.dockTabs.some((t) => t.view === 'shell')).toBe(false);
    workspace.setTerminal({ mode: 'app' });
});

test('a node shell says which node rather than which pod', async () => {
    workspace.setTerminal({ mode: 'external' });
    render(ObjectActions, { object: NODE });

    await page.getByRole('button', { name: 'Shell' }).click();

    await vi.waitFor(() => expect(TerminalService.LaunchNode).toHaveBeenCalledWith(PROD, 'wrkr01'));
    workspace.setTerminal({ mode: 'app' });
});

test('forwarding offers the ports the object actually has', async () => {
    vi.mocked(PortForwardService.Ports).mockResolvedValue([
        { name: 'http', port: 80, protocol: 'TCP', target: '8080' },
        { name: '', port: 9090, protocol: 'TCP', target: '' },
    ]);
    render(ObjectActions, { object: SERVICE });
    await expect.element(page.getByRole('button', { name: 'Forward' })).toBeVisible();

    await page.getByRole('button', { name: 'Forward' }).click();

    // A service port reads with where it lands on the pod, which is rarely the
    // same number.
    const picker = page.getByRole('combobox', { name: 'Port' });
    await expect.element(picker).toBeVisible();

    const options = [...(picker.element() as HTMLSelectElement).options].map((o) => o.textContent?.trim());
    expect(options).toEqual(['80 · http · → 8080', '9090']);
});

test('forwarding sends what was chosen, with a blank local port meaning any', async () => {
    vi.mocked(PortForwardService.Ports).mockResolvedValue([
        { name: 'http', port: 80, protocol: 'TCP', target: '8080' },
    ]);
    render(ObjectActions, { object: SERVICE });
    await page.getByRole('button', { name: 'Forward' }).click();
    await expect.element(page.getByRole('combobox', { name: 'Port' })).toBeVisible();

    await page.getByRole('button', { name: 'Forward', exact: true }).click();

    await vi.waitFor(() =>
        // 0 is "any free port", and true is the browser checkbox, which is on
        // unless it is turned off.
        expect(PortForwardService.Start).toHaveBeenCalledWith(PROD, 'services', 'web', 'api', 80, 0, true),
    );
});

test('a local port typed by hand is the one asked for', async () => {
    vi.mocked(PortForwardService.Ports).mockResolvedValue([
        { name: 'http', port: 80, protocol: 'TCP', target: '8080' },
    ]);
    render(ObjectActions, { object: SERVICE });
    await page.getByRole('button', { name: 'Forward' }).click();
    await expect.element(page.getByRole('spinbutton', { name: 'Local' })).toBeVisible();

    await page.getByRole('spinbutton', { name: 'Local' }).fill('8081');
    await page.getByRole('checkbox', { name: 'Open a browser' }).click();
    await page.getByRole('button', { name: 'Forward', exact: true }).click();

    await vi.waitFor(() =>
        expect(PortForwardService.Start).toHaveBeenCalledWith(PROD, 'services', 'web', 'api', 80, 8081, false),
    );
});

test('an object whose ports could not be read says so rather than offering none', async () => {
    vi.mocked(PortForwardService.Ports).mockRejectedValue(
        new Error('service api selects no pods -- its endpoints are set by hand'),
    );
    render(ObjectActions, { object: SERVICE });

    await page.getByRole('button', { name: 'Forward' }).click();

    await expect.element(page.getByText(/selects no pods/)).toBeVisible();
});

// ----- the kind-specific buttons beyond scale and restart --------------------

/** Answers ObjectState with the given facts on top of an ordinary object. */
function facts(over: Record<string, unknown> = {}) {
    vi.mocked(ActionService.ObjectState).mockResolvedValue({
        scalable: false,
        replicas: 0,
        cordoned: false,
        containers: [],
        suspended: false,
        paused: false,
        pending: false,
        ...over,
    });
}

// Like Cordon, the label is the object's answer: a suspended cron job offers
// to resume, and pressing it sends the opposite of what it is.
test('a suspended cron job offers to resume, and resuming sends false', async () => {
    facts({ suspended: true });
    render(ObjectActions, { object: CRONJOB });

    await page.getByRole('button', { name: 'Resume' }).click();

    await vi.waitFor(() => expect(ActionService.Suspend).toHaveBeenCalledWith(PROD, 'cronjobs', 'ops', 'backup', false));
});

test('run now starts a job and says which', async () => {
    facts();
    render(ObjectActions, { object: CRONJOB });

    await page.getByRole('button', { name: 'Run now' }).click();

    await vi.waitFor(() => expect(ActionService.TriggerCronJob).toHaveBeenCalledWith(PROD, 'ops', 'backup'));
    await vi.waitFor(() => expect(notices.current?.text).toContain('backup-manual-1'));
});

test('a paused deployment offers to resume its rollout', async () => {
    facts({ paused: true, scalable: true, replicas: 2 });
    render(ObjectActions, { object: DEPLOYMENT });

    await page.getByRole('button', { name: 'Resume rollout' }).click();

    await vi.waitFor(() => expect(ActionService.PauseRollout).toHaveBeenCalledWith(PROD, 'default', 'web', false));
});

// Rollback reads the history when pressed and picks the revision before the
// current one, which is what `kubectl rollout undo` does with no flags.
test('rollback offers the earlier revisions and sends the one chosen', async () => {
    facts({ scalable: true, replicas: 2 });
    vi.mocked(ActionService.RolloutHistory).mockResolvedValue([
        { revision: 3, images: ['web:3'], changeCause: '', age: '1h', current: true },
        { revision: 2, images: ['web:2'], changeCause: 'bump', age: '2d', current: false },
        { revision: 1, images: ['web:1'], changeCause: '', age: '9d', current: false },
    ]);
    render(ObjectActions, { object: DEPLOYMENT });

    await page.getByRole('button', { name: 'Rollback' }).click();

    const picker = page.getByRole('combobox', { name: /Roll back to/ });
    await expect.element(picker).toHaveValue('2');
    expect(page.getByRole('option', { name: /Revision 3/ }).elements()).toHaveLength(0);

    await picker.selectOptions('1');
    await page.getByRole('button', { name: 'Rollback' }).click();

    await vi.waitFor(() =>
        expect(ActionService.RollbackWorkload).toHaveBeenCalledWith(PROD, 'deployments', 'default', 'web', 1),
    );
});

test('rollback says so when there is nothing to go back to', async () => {
    facts({ scalable: true, replicas: 2 });
    vi.mocked(ActionService.RolloutHistory).mockResolvedValue([
        { revision: 1, images: ['web:1'], changeCause: '', age: '1h', current: true },
    ]);
    render(ObjectActions, { object: DEPLOYMENT });

    await page.getByRole('button', { name: 'Rollback' }).click();

    await expect.element(page.getByText(/nothing behind it to go back to/)).toBeVisible();
});

test('evicting a pod asks first, then evicts it', async () => {
    facts();
    render(ObjectActions, { object: POD });

    await page.getByRole('button', { name: 'Evict' }).click();
    await expect.element(page.getByText(/disruption budget/)).toBeVisible();
    expect(ActionService.Evict).not.toHaveBeenCalled();

    await page.getByRole('button', { name: 'Evict' }).click();

    await vi.waitFor(() => expect(ActionService.Evict).toHaveBeenCalledWith(PROD, 'default', 'web'));
});

// A request that has been answered cannot be answered again, so the buttons
// go with the answer.
test('a signing request offers approve and deny only while it is pending', async () => {
    facts({ pending: false });
    const answered = await render(ObjectActions, { object: CSR });
    await expect.element(page.getByRole('button', { name: 'Delete' })).toBeVisible();
    expect(page.getByRole('button', { name: 'Approve' }).elements()).toHaveLength(0);
    await answered.unmount();

    actions.forget(CSR);
    facts({ pending: true });
    render(ObjectActions, { object: CSR });

    await page.getByRole('button', { name: 'Approve' }).click();
    await expect.element(page.getByText(/cannot be taken back/)).toBeVisible();
    await page.getByRole('button', { name: 'Approve' }).click();

    await vi.waitFor(() => expect(ActionService.AnswerCSR).toHaveBeenCalledWith(PROD, 'csr-1', true));
});
