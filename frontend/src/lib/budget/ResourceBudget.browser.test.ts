import { expect, test, vi, beforeEach } from 'vitest';
import { render } from 'vitest-browser-svelte';

// The service is mocked because the component's job is to render a budget, and
// what is worth checking is what it says when a number is missing -- which is
// the ordinary case on a cluster with no metrics stack.
const Budget = vi.hoisted(() => vi.fn());
const CPUDelay = vi.hoisted(() => vi.fn());
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
    ResourceService: { Budget, CPUDelay },
}));

import ResourceBudget from './ResourceBudget.svelte';

const settle = () => new Promise((r) => setTimeout(r, 80));

function reply(over: Record<string, unknown> = {}) {
    return {
        scope: { kind: 'cluster', name: '' },
        amounts: [
            {
                label: 'CPU', unit: 'cores',
                capacity: 16, allocatable: 15, requested: 6, limits: 30, used: 3,
                hasCapacity: true, hasDemand: true, hasUsed: true,
            },
        ],
        usage: { source: 'metrics-server', error: '' },
        error: '',
        ...over,
    };
}

/** A throttling reading. Nothing throttled unless a test says otherwise. */
function delay(over: Record<string, unknown> = {}) {
    return {
        source: 'kubelet', error: '',
        throttled: 0, stalled: 0, limited: true,
        pressure: 0, hasPressure: false,
        nodes: 1, sampled: 1, waiting: false,
        ...over,
    };
}

beforeEach(() => {
    document.body.innerHTML = '';
    Budget.mockReset();
    CPUDelay.mockReset();
    CPUDelay.mockResolvedValue(delay());
});

test('shows requested, limits and used together', async () => {
    Budget.mockResolvedValue(reply());

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    const text = document.body.textContent ?? '';
    expect(text).toContain('Requested');
    expect(text).toContain('Limits');
    expect(text).toContain('Used');
});

test('says why the used column is empty rather than showing zero', async () => {
    // No metrics-server and no Prometheus. Everything else still renders.
    Budget.mockResolvedValue(
        reply({
            amounts: [
                {
                    label: 'CPU', unit: 'cores',
                    capacity: 16, allocatable: 15, requested: 6, limits: 30, used: 0,
                    hasCapacity: true, hasDemand: true, hasUsed: false,
                },
            ],
            usage: { source: '', error: 'metrics-server: not found; prometheus: none found in this cluster' },
        }),
    );

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    const text = document.body.textContent ?? '';
    expect(text).toContain('Requested');
    // The warning names what is missing, and does not claim usage is zero.
    expect(text).toContain('metrics-server');
    expect(text).not.toContain('Used');
});

test('names the source when one answered', async () => {
    Budget.mockResolvedValue(reply());

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    expect(document.body.textContent ?? '').toContain('metrics-server');
});

test('reports a budget that could not be read at all', async () => {
    Budget.mockRejectedValue(new Error('the server rejected the request'));

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    expect(document.body.textContent ?? '').toContain('the server rejected the request');
});

test('draws bars for a namespace with no quota', async () => {
    // The reported bug: an unquota'd namespace has no ceiling, so every bar sat
    // at zero width and the section looked broken even though the numbers were
    // right there beside it.
    Budget.mockResolvedValue(
        reply({
            scope: { kind: 'namespace', name: 'test1' },
            amounts: [
                {
                    label: 'CPU', unit: 'cores',
                    capacity: 0, allocatable: 0, requested: 1.22, limits: 0.06, used: 1.08,
                    hasCapacity: false, hasDemand: true, hasUsed: true,
                },
                {
                    label: 'Pods', unit: '',
                    capacity: 0, allocatable: 0, requested: 0, limits: 0, used: 4,
                    hasCapacity: false, hasDemand: false, hasUsed: true,
                },
            ],
        }),
    );

    render(ResourceBudget, { props: { contextId: 'x', scope: 'namespace', name: 'test1' } });
    await settle();

    const widths = [...document.querySelectorAll('.fill')].map((el) =>
        parseFloat((el as HTMLElement).style.width),
    );
    expect(widths.length).toBeGreaterThan(0);
    // The largest sets the scale, and nothing is left at zero width.
    expect(Math.max(...widths)).toBe(100);
    expect(widths.every((w) => w > 0)).toBe(true);

    // Pods is alone with no ceiling, so it gets no track rather than a full one.
    expect(document.querySelectorAll('.track').length).toBe(3);
    expect(document.body.textContent).toContain('no quota');
});

test('puts the throttling reading under the CPU bars', async () => {
    Budget.mockResolvedValue(reply());
    CPUDelay.mockResolvedValue(delay({ throttled: 0.42, stalled: 0.75, pressure: 0.2, hasPressure: true }));

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    const text = document.body.textContent ?? '';
    expect(text).toContain('Throttled');
    expect(text).toContain('42%');
    // The delay itself, not only how often it happened.
    expect(text).toContain('750 ms');
    expect(text).toContain('CPU pressure 20%');
});

test('says nothing is capped rather than drawing a zero', async () => {
    // No CPU limit anywhere in scope means the kernel never throttles. A bar
    // sitting at zero would read as "measured, and fine", which is a different
    // claim from "there is nothing here to measure".
    Budget.mockResolvedValue(reply());
    CPUDelay.mockResolvedValue(delay({ limited: false }));

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    const text = document.body.textContent ?? '';
    expect(text).toContain('Nothing here has a CPU limit');
    expect(text).not.toContain('Throttled');
});

test('names the permission when the kubelets refuse', async () => {
    Budget.mockResolvedValue(reply());
    CPUDelay.mockResolvedValue(
        delay({ source: '', error: "not allowed to read node worker-1's kubelet metrics -- that needs the nodes/proxy permission" }),
    );

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    expect(document.body.textContent ?? '').toContain('nodes/proxy');
});

test('still draws the bars when the delay call fails outright', async () => {
    // The bars are what this panel is for, and they come from a different call
    // needing a different permission.
    Budget.mockResolvedValue(reply());
    CPUDelay.mockRejectedValue(new Error('boom'));

    render(ResourceBudget, { props: { contextId: 'x', scope: 'cluster' } });
    await settle();

    const text = document.body.textContent ?? '';
    expect(text).toContain('Requested');
    expect(text).not.toContain('boom');
});
