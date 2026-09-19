import { beforeEach, describe, expect, test, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ContextTree from './ContextTree.svelte';
import { DEFINITIONS_GROUP, NAV_GROUPS } from '../catalogue';
import { ResourceService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { workspace } from '../state/workspace.svelte';

vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services')>();
    return { ...actual, ResourceService: { ...actual.ResourceService, CustomResourceKinds: vi.fn().mockResolvedValue([]) } };
});

const CTX = { id: 'c0', name: 'admin@prod', cluster: 'c0', user: 'admin',
    namespace: '', server: '', file: '/c', current: false };

const GROUPS = [
    { group: 'cert-manager.io', kinds: [
        { kind: 'crd:certificates.cert-manager.io', label: 'Certificate', group: 'cert-manager.io', plural: 'certificates', scoped: true },
        { kind: 'crd:issuers.cert-manager.io', label: 'Issuer', group: 'cert-manager.io', plural: 'issuers', scoped: true },
    ] },
    { group: 'vitistack.io', kinds: [
        { kind: 'crd:machines.vitistack.io', label: 'Machine', group: 'vitistack.io', plural: 'machines', scoped: true },
    ] },
];

const settle = () => new Promise((r) => setTimeout(r, 120));
const heading = (label: string) =>
    [...document.querySelectorAll('button.group')].find((b) => b.textContent?.includes(label)) as HTMLElement;
const apiRows = () => [...document.querySelectorAll('button.api')].map((b) => b.textContent?.trim());
const items = () => [...document.querySelectorAll('.tree .item')].map((b) => b.textContent?.trim());

beforeEach(async () => {
    document.body.innerHTML = '';
    workspace.files = [{ path: '/c', source: 'manual', error: '', contexts: [CTX] }];
    workspace.expanded = ['c0'];
    workspace.customKinds = {};
    workspace.expandedApiGroups = [];
    workspace.settings.layout.collapsedGroups = null;
    workspace.settings.contexts = {};
    render(ContextTree, { props: { context: CTX } });
    await settle();
});

// Fetching on expand rather than on render is what keeps a kubeconfig full of
// clusters from querying every one of them.
test('the definitions are not fetched until the section is opened', () => {
    expect(workspace.customKindsFor(CTX.id).status).toBe('idle');
    expect(apiRows()).toEqual([]);
});

test('opening the section lists the API groups the cluster serves', async () => {
    workspace.customKinds = { c0: { status: 'ready', groups: GROUPS, message: '' } };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    expect(apiRows()?.some((t) => t?.startsWith('cert-manager.io'))).toBe(true);
    expect(apiRows()?.some((t) => t?.startsWith('vitistack.io'))).toBe(true);
    // The definitions themselves stay put away until a group is opened.
    expect(items()).not.toContain('Machine');
});

test('the section still leads with the full definitions table', async () => {
    workspace.customKinds = { c0: { status: 'ready', groups: GROUPS, message: '' } };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    expect(items()).toContain('All definitions');
});

test('opening an API group shows the kinds it defines', async () => {
    workspace.customKinds = { c0: { status: 'ready', groups: GROUPS, message: '' } };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    const group = [...document.querySelectorAll('button.api')]
        .find((b) => b.textContent?.trim().startsWith('vitistack.io')) as HTMLElement;
    group.click();
    await settle();

    expect(items()).toContain('Machine');
    // Only that group opened.
    expect(items()).not.toContain('Certificate');
});

test('clicking a definition opens a tab for its instances', async () => {
    workspace.closeAllTabs();
    workspace.customKinds = { c0: { status: 'ready', groups: GROUPS, message: '' } };
    heading(DEFINITIONS_GROUP).click();
    await settle();
    (([...document.querySelectorAll('button.api')]
        .find((b) => b.textContent?.trim().startsWith('vitistack.io'))) as HTMLElement).click();
    await settle();

    const machine = [...document.querySelectorAll('.tree .item')]
        .find((b) => b.textContent?.trim() === 'Machine') as HTMLElement;
    machine.click();
    await settle();

    expect(workspace.tabs.map((t) => t.kind)).toContain('crd:machines.vitistack.io');
});

// "Could not read definitions" leaves the reader guessing between no
// permission, no network and nothing installed -- which call for different
// reactions. The failure is named using the same reading the error pages use.
test('a cluster that refuses the list says why, not just that it failed', async () => {
    workspace.customKinds = {
        c0: {
            status: 'error',
            groups: [],
            message: 'customresourcedefinitions.apiextensions.k8s.io is forbidden: User "x" cannot list resource',
        },
    };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    const note = document.querySelector('.note.failed');
    expect(note?.textContent).toContain('Access denied');
    expect(note?.getAttribute('title')).toContain('forbidden');
});

test('an unreachable cluster is named as unreachable', async () => {
    workspace.customKinds = {
        c0: {
            status: 'error',
            groups: [],
            message: 'Get "https://localhost:6443/apis": dial tcp [::1]:6443: connect: connection refused',
        },
    };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    expect(document.querySelector('.note.failed')?.textContent).toContain('Cannot reach the API server');
});

// The two states have to be distinguishable at a glance.
test('having none reads differently from having failed', async () => {
    workspace.customKinds = { c0: { status: 'ready', groups: [], message: '' } };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    expect(document.querySelector('.note.failed')).toBeNull();
    expect(document.querySelector('.note')?.textContent).toContain('No custom resources');
});

test('a cluster with no custom resources says that too', async () => {
    workspace.customKinds = { c0: { status: 'ready', groups: [], message: '' } };
    heading(DEFINITIONS_GROUP).click();
    await settle();

    expect(document.querySelector('.note')?.textContent).toContain('No custom resources');
});

// Every route to "the section is open" has to fetch, not just a click on the
// heading. These are the ones that do not involve clicking it.
test('a section already open when the context is expanded fetches', async () => {
    document.body.innerHTML = '';
    workspace.customKinds = {};
    // Left open from a previous session, or by the per-context folding.
    workspace.settings.contexts = {
        c0: { alias: '', color: '', metrics: '', columns: {}, collapsedGroups: NAV_GROUPS
            .map((g) => g.label)
            .filter((label) => label !== DEFINITIONS_GROUP) },
    };
    render(ContextTree, { props: { context: CTX } });
    await settle();

    expect(workspace.customKindsFor(CTX.id).status).not.toBe('idle');
});

test('a context expanded with the section already open fetches', async () => {
    document.body.innerHTML = '';
    workspace.customKinds = {};
    workspace.expanded = [];
    workspace.settings.contexts = {
        c0: { alias: '', color: '', metrics: '', collapsedGroups: [], columns: {} },
    };
    render(ContextTree, { props: { context: CTX } });
    await settle();

    // Now open the context itself; the section inside it is already unfolded.
    workspace.toggleExpanded(CTX.id);
    await settle();

    expect(workspace.customKindsFor(CTX.id).status).not.toBe('idle');
});

describe('re-reading the definitions', () => {
    /** Opens the section and clears whatever the effect did on the way. */
    async function openSection() {
        workspace.settings.contexts = {
            c0: { alias: '', color: '', metrics: '', columns: {}, collapsedGroups: NAV_GROUPS
                .map((g) => g.label)
                .filter((label) => label !== DEFINITIONS_GROUP) },
        };
        document.body.innerHTML = '';
        workspace.customKinds = { c0: { status: 'ready', groups: GROUPS, message: '' } };
        render(ContextTree, { props: { context: CTX } });
        await settle();
    }

    test('the section offers a way to read it again', async () => {
        await openSection();

        expect(document.querySelector('button.reload')).not.toBeNull();
    });

    test('there is nothing to refresh while the section is shut', async () => {
        workspace.settings.contexts = {};
        workspace.settings.layout.collapsedGroups = null;
        document.body.innerHTML = '';
        render(ContextTree, { props: { context: CTX } });
        await settle();

        expect(document.querySelector('button.reload')).toBeNull();
    });

    // A newly installed operator is the whole reason this exists, so it has to
    // ask the cluster again rather than trust what it already has.
    test('refreshing asks the cluster again', async () => {
        await openSection();
        const before = vi.mocked(ResourceService.CustomResourceKinds).mock.calls.length;

        (document.querySelector('button.reload') as HTMLElement).click();
        await settle();

        expect(vi.mocked(ResourceService.CustomResourceKinds).mock.calls.length).toBeGreaterThan(before);
    });

    // It sits on the heading, which folds the section when clicked.
    test('refreshing does not fold the section it is sitting on', async () => {
        await openSection();

        (document.querySelector('button.reload') as HTMLElement).click();
        await settle();

        expect(workspace.isGroupCollapsed(CTX.id, DEFINITIONS_GROUP)).toBe(false);
    });
});
