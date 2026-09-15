import { expect, test } from 'vitest';
import { DASHBOARD, HELM_RELEASES, NAV_GROUPS, DASHBOARD_ITEM } from '../catalogue';
import { PATHS } from '../components/Icon.svelte';
import { HELP } from './help';
import { KUBERNETES_PRIMER } from './kubernetes';
import { forMode } from './mode';
import type { Page } from './types';

// The pages are data, so the mistakes they can carry are data mistakes: a
// "show me" that names a kind the sidebar does not list, an icon that is not
// drawn, a section id that collides. Each would be a dead button somewhere
// deep in a page nobody proof-reads twice, so they are checked here instead.

const PAGES: Page[] = [HELP, KUBERNETES_PRIMER];

const KINDS = new Set<string>([
    DASHBOARD,
    DASHBOARD_ITEM.kind,
    HELM_RELEASES,
    ...NAV_GROUPS.flatMap((group) => group.items.map((item) => item.kind)),
]);

function shownKinds(page: Page): string[] {
    const out: string[] = [];
    for (const section of page.sections) {
        for (const block of section.blocks) {
            if (block.type === 'actions') {
                for (const action of block.actions) if (action.kind === 'show') out.push(action.resource);
            }
            if (block.type === 'terms') {
                for (const term of block.terms) if (term.resource) out.push(term.resource);
            }
        }
    }
    return out;
}

test.each(PAGES.map((p) => [p.title, p] as const))('%s only offers to show kinds the sidebar lists', (_title, page) => {
    const unknown = shownKinds(page).filter((kind) => !KINDS.has(kind));
    expect(unknown).toEqual([]);
});

test.each(PAGES.map((p) => [p.title, p] as const))('%s names only icons that exist', (_title, page) => {
    const missing = page.sections.filter((s) => !(s.icon in PATHS)).map((s) => `${s.id}:${s.icon}`);
    expect(missing).toEqual([]);
});

test.each(PAGES.map((p) => [p.title, p] as const))('%s has unique section ids', (_title, page) => {
    const ids = page.sections.map((s) => s.id);
    expect(new Set(ids).size).toBe(ids.length);
});

// Help is one page for both versions of the app, with what only one of them
// can do marked for it. Each version must see its own and not the other's,
// and no section may be left with nothing in it -- a dead entry in the rail.
test.each([
    ['desktop', false],
    ['web', true],
] as const)('Help as the %s version tells it', (mode, server) => {
    const page = forMode(HELP, server);

    const foreign = page.sections.flatMap((s) => [s.only, ...s.blocks.map((b) => b.only)]).filter((only) => only && only !== mode);
    expect(foreign).toEqual([]);
    expect(page.sections.filter((s) => s.blocks.length === 0).map((s) => s.id)).toEqual([]);

    const ids = page.sections.map((s) => s.id);
    if (server) expect(ids[0]).toBe('web');
    else expect(ids).not.toContain('web');
});

// What the web version must never ask of anyone: a path on their own disk, a
// folder on their own machine, a terminal emulator of their own.
test('the web version of Help sends nobody to their own machine', () => {
    const text = JSON.stringify(forMode(HELP, true));
    for (const desktopOnly of ['~/.kube', '%AppData%', '$XDG_CONFIG_HOME', 'terminal emulator']) {
        expect(text, desktopOnly).not.toContain(desktopOnly);
    }
});

test('forMode keeps what is unmarked and drops what is marked for the other version', () => {
    const page: Page = {
        title: 'T',
        lede: '',
        sections: [
            { id: 'both', label: 'Both', icon: 'info', blocks: [{ type: 'p', text: 'a' }, { type: 'p', text: 'd', only: 'desktop' }, { type: 'p', text: 'w', only: 'web' }] },
            { id: 'web-only', label: 'Web', icon: 'info', only: 'web', blocks: [{ type: 'p', text: 'x' }] },
        ],
    };
    const text = (p: Page) => p.sections.map((s) => `${s.id}:${s.blocks.map((b) => (b.type === 'p' ? b.text : '')).join('')}`);
    expect(text(forMode(page, false))).toEqual(['both:ad']);
    expect(text(forMode(page, true))).toEqual(['both:aw', 'web-only:x']);
    // The page handed in is not changed.
    expect(page.sections[0].blocks).toHaveLength(3);
});

test.each(PAGES.map((p) => [p.title, p] as const))('%s links only to https pages', (_title, page) => {
    const bad: string[] = [];
    for (const section of page.sections) {
        for (const block of section.blocks) {
            if (block.type === 'links') for (const link of block.links) if (!link.href.startsWith('https://')) bad.push(link.href);
            if (block.type === 'terms') for (const term of block.terms) if (term.href && !term.href.startsWith('https://')) bad.push(term.href);
        }
    }
    expect(bad).toEqual([]);
});
