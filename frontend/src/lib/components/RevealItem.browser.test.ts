import { beforeEach, describe, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import Sidebar from './Sidebar.svelte';
import { DEFINITIONS_GROUP, NAV_GROUPS } from '../catalogue';
import { resourceTabId, workspace } from '../state/workspace.svelte';

vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return { ...actual, Window: { ...actual.Window, SetZoom: vi.fn().mockResolvedValue(undefined) } };
});

const TOKENS = `:root{--bg:#10151c;--bg-sidebar:#151b24;--bg-panel:#19202a;--bg-raised:#212b38;
--bg-hover:rgba(255,255,255,.055);--bg-active:rgba(255,255,255,.09);--border:#28323f;
--border-soft:rgba(255,255,255,.07);--text:#dee5ee;--text-dim:#8d9aaa;--text-faint:#616e7d;
--accent:#4a86ff;--ok:#4cc38a;--warn:#e0a458;--error:#e5646d;--radius:6px;--radius-sm:4px;
--row-h:30px;--font:-apple-system,sans-serif;--mono:Menlo,monospace;
font-family:var(--font);font-size:13px}
body{color:var(--text);margin:0}
button{font:inherit;color:inherit;background:none;border:none;padding:0;cursor:pointer}
input{font:inherit;color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:4px;padding:5px 8px}`;

const CTX = { id: 'c0', name: 'admin@prod', cluster: 'c0', user: 'admin',
    namespace: '', server: '', file: '/c', current: false };

// A custom resource sits two folds deep -- its API group, inside the
// definitions section -- which is what makes it different from every other row.
const API_GROUP = 'vitistack.io';
const CRD_KIND = 'crd:kubernetesclusters.vitistack.io';
const CRD_LABEL = 'KubernetesClusters';
const CUSTOM_KINDS = {
    status: 'ready' as const,
    message: '',
    groups: [{
        group: API_GROUP,
        kinds: [{ kind: CRD_KIND, label: CRD_LABEL, group: API_GROUP,
            plural: 'kubernetesclusters', scoped: false }],
    }],
};

const settle = () => new Promise((r) => setTimeout(r, 700));

/**
 * Waits for a row to be scrolled fully into view. The reveal scrolls smoothly,
 * and how long that takes depends on how busy the machine is -- with the whole
 * suite running in parallel, a fixed wait sometimes caught it mid-scroll.
 */
const arrives = (label: string) =>
    expect.poll(() => {
        const row = rowFor(label);
        return !!row && inView(row);
    }, { timeout: 5000, interval: 50 }).toBe(true);
const scroller = () => document.querySelector('.scroll') as HTMLElement;

/** The sidebar row for one resource kind, if it is rendered at all. */
function rowFor(label: string): HTMLElement | undefined {
    return [...document.querySelectorAll('.tree .item')]
        .find((el) => el.textContent?.trim() === label) as HTMLElement | undefined;
}

function inView(el: HTMLElement): boolean {
    const view = scroller().getBoundingClientRect();
    const box = el.getBoundingClientRect();
    return box.top >= view.top - 1 && box.bottom <= view.bottom + 1;
}

beforeEach(async () => {
    await page.viewport(320, 700);
    document.body.innerHTML = '';
    const style = document.createElement('style');
    style.textContent = TOKENS;
    document.head.appendChild(style);

    workspace.closeAllTabs();
    workspace.settings.layout.zoom = 1;
    workspace.files = [{ path: '/c', source: 'manual', error: '', contexts: [CTX] }];
    workspace.expanded = ['c0'];
    workspace.selectedContextId = 'c0';
    // Everything open, so the tree is as long as it really gets.
    workspace.settings.contexts = {
        c0: { alias: '', color: '', metrics: '', collapsedGroups: [], columns: {} },
    };
    workspace.settings.layout.collapsedGroups = [];
    render(Sidebar);
    (document.querySelector('.sidebar') as HTMLElement).style.height = '700px';
    await settle();
});

test('the tree is long enough that a row near the end is off screen', () => {
    expect(NAV_GROUPS.length).toBeGreaterThan(8);
    const row = rowFor('All definitions');
    expect(row).toBeTruthy();
    expect(inView(row!)).toBe(false);
});

// Activating a tab should show the row it belongs to. Bringing the cluster's
// own name into view says nothing about where in fifty rows that tab lives.
test('activating a tab brings its own row into view', async () => {
    workspace.openTab(CTX.id, 'customresourcedefinitions');
    workspace.openTab(CTX.id, 'pods');
    await settle();
    scroller().scrollTop = 0;
    await settle();

    workspace.activateTab(resourceTabId(CTX.id, 'customresourcedefinitions'));

    await arrives('All definitions');
});

// Reaching a custom resource used to leave the sidebar on the cluster's name:
// the row lives inside its API group inside the definitions section, activating
// the tab opened neither, and with no row rendered there was nothing to scroll
// to. These start from both folds shut, which is how a fresh context comes up.
describe('a custom resource, folded two deep', () => {
    beforeEach(async () => {
        workspace.customKinds = { c0: CUSTOM_KINDS };
        workspace.settings.contexts = {
            c0: { alias: '', color: '', metrics: '',
                collapsedGroups: [DEFINITIONS_GROUP], columns: {} },
        };
        workspace.expandedApiGroups = [];
        await settle();
    });

    test('starts out of the tree entirely, not merely out of view', () => {
        expect(rowFor(CRD_LABEL)).toBeUndefined();
    });

    test('activating its tab unfolds its section and its API group', async () => {
        workspace.openTab(CTX.id, CRD_KIND);
        await settle();

        expect(workspace.isGroupCollapsed(CTX.id, DEFINITIONS_GROUP)).toBe(false);
        expect(workspace.isApiGroupExpanded(CTX.id, API_GROUP)).toBe(true);
    });

    test('activating its tab brings its own row into view', async () => {
        workspace.openTab(CTX.id, 'pods');
        workspace.openTab(CTX.id, CRD_KIND);
        await settle();
        scroller().scrollTop = 0;
        await settle();

        workspace.activateTab(resourceTabId(CTX.id, CRD_KIND));

        await arrives(CRD_LABEL);
    });

    // Coming back to it is the same journey with the folds already open, and it
    // has to land on the row rather than wherever the previous tab left things.
    test('coming back to it from another tab finds it again', async () => {
        workspace.openTab(CTX.id, CRD_KIND);
        workspace.openTab(CTX.id, 'pods');
        await settle();
        workspace.activateTab(resourceTabId(CTX.id, 'pods'));
        await settle();

        workspace.activateTab(resourceTabId(CTX.id, CRD_KIND));

        await arrives(CRD_LABEL);
    });
});
