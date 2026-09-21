import { beforeEach, expect, test, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';

vi.mock('@wailsio/runtime', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@wailsio/runtime')>();
    return { ...actual, Window: { ...actual.Window, SetZoom: vi.fn().mockResolvedValue(undefined) } };
});
const { workspace } = await import('../state/workspace.svelte');
const { HELP, DASHBOARD } = await import('../catalogue');
const { clusters } = await import('../state/health.svelte');
const App = (await import('../../App.svelte')).default;

const CTX = { id: 'c0', name: 'admin@prod', cluster: 'c0', user: 'admin',
    namespace: '', server: 'https://prod.example.com:6443', file: '/c', current: false };
const OTHER = { ...CTX, id: 'c1', name: 'admin@staging', cluster: 'c1', server: 'https://staging.example.com' };

const settle = () => new Promise((r) => setTimeout(r, 150));
const stage = () => document.querySelector('.welcome-stage');
const layer = () => document.querySelector<HTMLElement>('.backdrop .layer');

beforeEach(async () => {
    document.body.innerHTML = '';
    workspace.closeAllTabs();
    workspace.files = [{ path: '/c', source: 'manual', error: '', contexts: [CTX, OTHER] }];
    workspace.settings.preferences.background = { source: 'builtin', pinned: '', minutes: 15, palette: 'varied' };
    workspace.loaded = true;
    render(App);
    await settle();
});

test('the idle screen carries a picture behind it', async () => {
    expect(stage()).not.toBeNull();
    expect(layer()?.style.backgroundImage).toMatch(/url\("?(blob:|data:image\/svg)/);
});

// A picture kept in the settings is the one shown, and it says which it is.
test('a kept picture is the one on screen, named in the corner', async () => {
    workspace.setBackground({ pinned: 'scene:orbit' });
    await settle();

    expect(document.querySelector('.backdrop')?.getAttribute('data-picture')).toBe('scene:orbit');
    // Named with the colour scheme it is drawn in.
    expect(document.querySelector('.picture-name')?.textContent?.trim()).toMatch(/^Orbit · \w/);
});

test('no picture leaves the theme\'s own ground', async () => {
    workspace.setBackground({ source: 'none' });
    await settle();

    expect(layer()).toBeNull();
    expect(stage()).not.toBeNull();
});

// The reason it lives on the welcome panel and not on .content: a picture
// behind a table of pod names is read against every row.
test('opening a tab takes the picture away with the welcome panel', async () => {
    workspace.openTab(CTX.id, 'pods');
    await settle();

    expect(stage()).toBeNull();
});

test('the picture does not intercept the pointer', async () => {
    expect(getComputedStyle(document.querySelector('.backdrop')!).pointerEvents).toBe('none');
});

// Each cluster is a card, and the card is the way in.
test('each cluster is a card that opens its dashboard', async () => {
    const cards = [...document.querySelectorAll<HTMLElement>('.card')];
    expect(cards).toHaveLength(2);
    expect(cards[0].textContent).toContain('prod.example.com:6443');

    cards[0].querySelector<HTMLButtonElement>('.card-main')!.click();
    await settle();

    expect(workspace.allTabs.some((t) => t.contextId === CTX.id && t.kind === DASHBOARD)).toBe(true);
});

test('a card goes straight to a view, too', async () => {
    const pods = [...document.querySelectorAll('.card')[1].querySelectorAll<HTMLButtonElement>('.quick button')]
        .find((b) => b.textContent?.includes('Pods'))!;
    pods.click();
    await settle();

    expect(workspace.allTabs.some((t) => t.contextId === OTHER.id && t.kind === 'pods')).toBe(true);
});

// Connected ones first: they are the ones you were working in.
test('a connected cluster is listed first, and says so', async () => {
    clusters.report(OTHER.id, 'connected');
    await settle();

    const first = document.querySelector('.card')!;
    expect(first.textContent).toContain('staging');
    expect(first.querySelector('.state')?.textContent).toContain('Connected');
    clusters.forget(OTHER.id);
});

test('with no clusters it offers the first steps', async () => {
    workspace.files = [];
    await settle();

    expect(document.querySelector('.card')).toBeNull();
    const steps = [...document.querySelectorAll('.step h2')].map((h) => h.textContent);
    expect(steps).toEqual(['Add a kubeconfig', 'Watch a folder', 'New to Kubernetes?']);
});

// F1 is where every desktop app keeps help, and it is the one shortcut that
// takes no modifier.
test('F1 opens the help page', async () => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'F1', bubbles: true }));
    await settle();

    expect(workspace.allTabs.some((t) => t.kind === HELP)).toBe(true);
});
