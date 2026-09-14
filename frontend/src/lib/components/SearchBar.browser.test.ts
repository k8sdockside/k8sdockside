import { beforeEach, expect, test } from 'vitest';
import { render } from 'vitest-browser-svelte';
import { search } from '../state/search.svelte';

// Wails makes the webview the first responder when the window is shown, and
// WebKit hands that focus to the first text field in the page -- this box.
// Opening the panel on focus opened it at every launch, so it opens when
// someone means to search: a click in the box, typing, or the shortcut.

const settle = () => new Promise((r) => setTimeout(r, 40));
const panel = () => document.getElementById('search-panel');
const box = () => document.querySelector('input[aria-label="Search every cluster"]') as HTMLInputElement;

beforeEach(() => {
    document.body.innerHTML = '';
    search.open = false;
    search.query = '';
});

test('focus alone does not open the panel, as the window taking focus at launch gives it', async () => {
    const SearchBar = (await import('./SearchBar.svelte')).default;
    render(SearchBar);
    await settle();

    box().focus();
    await settle();

    expect(document.activeElement).toBe(box());
    expect(panel()).toBeNull();
});

test('a click in the box, typing in it or the shortcut opens the panel', async () => {
    const SearchBar = (await import('./SearchBar.svelte')).default;
    render(SearchBar);
    await settle();

    box().dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    await settle();
    expect(panel()).not.toBeNull();

    search.open = false;
    await settle();
    box().focus();
    box().value = 'x';
    box().dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    expect(panel()).not.toBeNull();

    search.open = false;
    search.query = '';
    await settle();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }));
    await settle();
    expect(panel()).not.toBeNull();
});
